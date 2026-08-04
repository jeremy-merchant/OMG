package foundation

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jeremy-merchant/oh-my-group/internal/domain"
	"github.com/jeremy-merchant/oh-my-group/internal/ports"
	"github.com/jeremy-merchant/oh-my-group/internal/store/sqlite"
)

type resolverStub struct{ resolved ports.ResolvedStore }

func (r resolverStub) Resolve(context.Context, ports.ResolveRequest) (ports.ResolvedStore, error) {
	return r.resolved, nil
}

type configInitializerStub func(context.Context, string) error

func (f configInitializerStub) InitializeProjectConfig(ctx context.Context, root string) error {
	return f(ctx, root)
}

func sqliteOpener(ctx context.Context, path string, options ports.OpenOptions) (ports.FoundationStore, ports.OpenStatus, error) {
	store, status, err := sqlite.Open(ctx, path, options)
	if err != nil {
		return nil, ports.OpenStatus{}, err
	}
	return store, status, nil
}

func TestStatusDoesNotCreateUninitializedState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "state.db")
	service := New(Dependencies{Resolver: resolverStub{resolved: ports.ResolvedStore{Path: path}}, Open: sqliteOpener})

	status, err := service.Status(context.Background(), Selection{}, false)
	if err.Code != "" {
		t.Fatal(err)
	}
	if status.Initialized {
		t.Fatal("status reported an uninitialized store as initialized")
	}
	if _, statErr := os.Stat(filepath.Dir(path)); !os.IsNotExist(statErr) {
		t.Fatalf("status created state directory: %v", statErr)
	}
}

func TestWithCurrentStoreDoesNotCreateUninitializedState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	service := New(Dependencies{Resolver: resolverStub{resolved: ports.ResolvedStore{Path: path}}, Open: sqliteOpener})

	called := false
	err := service.WithCurrentStore(context.Background(), Selection{}, func(ports.ResolvedStore, ports.Store) error {
		called = true
		return nil
	})
	if err.Code != "uninitialized" || called {
		t.Fatalf("error = %+v, called = %t", err, called)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("read-only boundary created state: %v", statErr)
	}
}

func TestWithReadOnlyCurrentStoreDoesNotCreateUninitializedState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "state.db")
	service := New(Dependencies{Resolver: resolverStub{resolved: ports.ResolvedStore{Path: path}}, Open: sqliteOpener})

	called := false
	err := service.WithReadOnlyCurrentStore(context.Background(), Selection{}, func(ports.ResolvedStore, ports.Store) error {
		called = true
		return nil
	})
	if err.Code != domain.CodeUninitialized || called {
		t.Fatalf("error = %+v, called = %t", err, called)
	}
	if _, statErr := os.Stat(filepath.Dir(path)); !os.IsNotExist(statErr) {
		t.Fatalf("read-only boundary created state directory: %v", statErr)
	}
}

func TestWithReadOnlyCurrentStoreDoesNotAppendWALFallbackAuditOrReceipts(t *testing.T) {
	path, db := currentStoreWithoutProject(t, "readonly-query")
	defer db.Close()
	service := New(Dependencies{
		Resolver: resolverStub{resolved: ports.ResolvedStore{Path: path}},
		Open: func(ctx context.Context, path string, options ports.OpenOptions) (ports.FoundationStore, ports.OpenStatus, error) {
			options.WALEligible = func(string) bool { return false }
			return sqliteOpener(ctx, path, options)
		},
	})

	var auditBefore int
	if err := db.QueryRow("SELECT COUNT(*) FROM audit_events").Scan(&auditBefore); err != nil {
		t.Fatal(err)
	}
	var receiptTableBefore bool
	if err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name='command_receipts')").Scan(&receiptTableBefore); err != nil {
		t.Fatal(err)
	}
	before, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}

	queryErr := service.WithReadOnlyCurrentStore(context.Background(), Selection{}, func(_ ports.ResolvedStore, store ports.Store) error {
		return store.Read(context.Background(), func(repositories ports.Repositories) error {
			if _, err := repositories.Audit().LatestCursor(context.Background()); err != nil {
				return err
			}
			return nil
		})
	})
	if queryErr.Code != "" {
		t.Fatal(queryErr)
	}

	var auditAfter int
	if err := db.QueryRow("SELECT COUNT(*) FROM audit_events").Scan(&auditAfter); err != nil {
		t.Fatal(err)
	}
	var receiptTableAfter bool
	if err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name='command_receipts')").Scan(&receiptTableAfter); err != nil {
		t.Fatal(err)
	}
	after, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if auditAfter != auditBefore || receiptTableAfter != receiptTableBefore || string(after) != string(before) {
		t.Fatalf("read-only query mutated audit=%d->%d receipt_table=%t->%t database=%t", auditBefore, auditAfter, receiptTableBefore, receiptTableAfter, string(before) != string(after))
	}
	for _, artifact := range []string{path + "-wal", path + "-shm", path + "-journal"} {
		if _, err := os.Lstat(artifact); !os.IsNotExist(err) {
			t.Fatalf("read-only query created SQLite artifact %s: %v", artifact, err)
		}
	}
}

func TestWithCurrentStoreMapsUnsafeExistingStateToUnavailable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	service := New(Dependencies{Resolver: resolverStub{resolved: ports.ResolvedStore{Path: path}}, Open: sqliteOpener})

	err := service.WithCurrentStore(context.Background(), Selection{}, func(ports.ResolvedStore, ports.Store) error {
		t.Fatal("unsafe state reached application callback")
		return nil
	})
	if err.Code != domain.CodeUnavailable {
		t.Fatalf("unsafe state error = %+v; want unavailable", err)
	}
}

func TestPlanDoesNotEnrollProjectOnCurrentStore(t *testing.T) {
	const project = "plan-observer"
	path, db := currentStoreWithoutProject(t, project)
	defer db.Close()
	service := New(Dependencies{Resolver: resolverStub{resolved: ports.ResolvedStore{Path: path, Project: domain.ProjectID(project)}}, Open: sqliteOpener})

	if _, err := service.Plan(context.Background(), Selection{}); err.Code != "" {
		t.Fatal(err)
	}
	if count := projectCount(t, db, project); count != 0 {
		t.Fatalf("plan enrolled project: count=%d", count)
	}
}

func TestApplyEnrollsProjectOnCurrentStore(t *testing.T) {
	const project = "apply-enrollment"
	path, db := currentStoreWithoutProject(t, project)
	defer db.Close()
	service := New(Dependencies{Resolver: resolverStub{resolved: ports.ResolvedStore{Path: path, Project: domain.ProjectID(project)}}, Open: sqliteOpener})
	ctx := context.Background()
	plan, err := service.Plan(ctx, Selection{})
	if err.Code != "" {
		t.Fatal(err)
	}
	backup, err := service.Backup(ctx, Selection{}, &plan)
	if err.Code != "" {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	approval := ApprovalFile{
		ApprovalID: "apply-" + project, ApprovedBy: "tester", EvidenceReference: "test",
		PlanID: plan.ID, Project: project, FromVersion: plan.FromVersion, ToVersion: plan.ToVersion,
		Checksums: plan.Checksums, BackupLocation: backup.Location, BackupChecksum: backup.Checksum,
		Command: "omg migration apply", Timestamp: now.Format(time.RFC3339Nano),
		ExpiresAtRaw: now.Add(time.Minute).Format(time.RFC3339Nano),
	}
	if applyErr := service.Apply(ctx, Selection{}, plan, approval); applyErr.Code != "" {
		t.Fatal(applyErr)
	}
	if count := projectCount(t, db, project); count != 1 {
		t.Fatalf("apply project count=%d", count)
	}
}

func TestAutoMigrateBacksUpAndAppliesOnlyDeclaredSafeIncrementalPlan(t *testing.T) {
	const project = "automatic-upgrade"
	const baseSQL = `CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, checksum TEXT NOT NULL, applied_at TEXT NOT NULL);
CREATE TABLE projects(id TEXT PRIMARY KEY, created_at TEXT NOT NULL);
CREATE TABLE command_receipts(id TEXT PRIMARY KEY, project_id TEXT NOT NULL, idempotency_key TEXT NOT NULL, operation TEXT NOT NULL, outcome TEXT NOT NULL, result_json BLOB NOT NULL, created_at TEXT NOT NULL, UNIQUE(project_id,idempotency_key));
CREATE TABLE audit_events(sequence_no INTEGER PRIMARY KEY AUTOINCREMENT, id TEXT NOT NULL UNIQUE, project_id TEXT NOT NULL, receipt_id TEXT, event_type TEXT NOT NULL, payload_json BLOB NOT NULL, occurred_at TEXT NOT NULL);
CREATE TABLE migration_approvals(approval_id TEXT PRIMARY KEY, plan_id TEXT NOT NULL, project_id TEXT NOT NULL, approved_by TEXT NOT NULL, evidence_reference TEXT NOT NULL, from_version INTEGER NOT NULL, to_version INTEGER NOT NULL, checksums_json BLOB NOT NULL, backup_location TEXT NOT NULL, backup_checksum TEXT NOT NULL, command TEXT NOT NULL, approved_at TEXT NOT NULL, expires_at TEXT NOT NULL, consumed_at TEXT NOT NULL, authorization_kind TEXT NOT NULL DEFAULT 'human');`
	const safeSQL = `CREATE TABLE automatic_upgrade_marker(id INTEGER PRIMARY KEY);`
	path := filepath.Join(t.TempDir(), "state.db")
	ctx := context.Background()
	baseMigrations := []ports.Migration{{Version: 1, SQL: baseSQL}}
	store, _, err := sqlite.Open(ctx, path, ports.OpenOptions{Migrations: baseMigrations})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := store.PlanMigrations(ctx, project)
	if err != nil {
		t.Fatal(err)
	}
	backup, err := store.CreateMigrationBackup(ctx, plan)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := store.ApplyMigrations(ctx, plan, ports.MigrationApproval{
		ApprovalID: "seed-v1", ApprovedBy: "tester", EvidenceReference: "fixture", PlanID: plan.ID, Project: project,
		FromVersion: plan.FromVersion, ToVersion: plan.ToVersion, Checksums: plan.Checksums, BackupLocation: backup.Location,
		BackupChecksum: backup.Checksum, Command: "omg migration apply", Timestamp: now, ExpiresAt: now.Add(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	service := New(Dependencies{
		Resolver: resolverStub{resolved: ports.ResolvedStore{Path: path, Project: domain.ProjectID(project)}},
		Open: func(ctx context.Context, path string, options ports.OpenOptions) (ports.FoundationStore, ports.OpenStatus, error) {
			options.Migrations = []ports.Migration{{Version: 1, SQL: baseSQL}, {Version: 2, SQL: safeSQL}}
			return sqliteOpener(ctx, path, options)
		},
	})
	result, autoErr := service.AutoMigrate(ctx, Selection{})
	if autoErr.Code != "" {
		t.Fatal(autoErr)
	}
	if !result.Eligible || !result.Applied || !result.Integrity || result.FromVersion != 1 || result.ToVersion != 2 || result.BackupChecksum == "" {
		t.Fatalf("automatic result = %#v", result)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var kind string
	if err := db.QueryRow(`SELECT authorization_kind FROM migration_approvals WHERE approval_id=?`, "auto-backup-"+result.PlanID).Scan(&kind); err != nil {
		t.Fatal(err)
	}
	if kind != string(ports.MigrationAuthorizationAutomaticSafe) {
		t.Fatalf("authorization kind = %q", kind)
	}
}

func TestAutoMigrateAppliesFreshCompiledPlanWithoutHumanApproval(t *testing.T) {
	const project = "automatic-fresh"
	const foundationSQL = `CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, checksum TEXT NOT NULL, applied_at TEXT NOT NULL);
CREATE TABLE projects(id TEXT PRIMARY KEY, created_at TEXT NOT NULL);
CREATE TABLE command_receipts(id TEXT PRIMARY KEY, project_id TEXT NOT NULL, idempotency_key TEXT NOT NULL, operation TEXT NOT NULL, outcome TEXT NOT NULL, result_json BLOB NOT NULL, created_at TEXT NOT NULL, UNIQUE(project_id,idempotency_key));
CREATE TABLE audit_events(sequence_no INTEGER PRIMARY KEY AUTOINCREMENT, id TEXT NOT NULL UNIQUE, project_id TEXT NOT NULL, receipt_id TEXT, event_type TEXT NOT NULL, payload_json BLOB NOT NULL, occurred_at TEXT NOT NULL);
CREATE TABLE migration_approvals(approval_id TEXT PRIMARY KEY, plan_id TEXT NOT NULL, project_id TEXT NOT NULL, approved_by TEXT NOT NULL, evidence_reference TEXT NOT NULL, from_version INTEGER NOT NULL, to_version INTEGER NOT NULL, checksums_json BLOB NOT NULL, backup_location TEXT NOT NULL, backup_checksum TEXT NOT NULL, command TEXT NOT NULL, approved_at TEXT NOT NULL, expires_at TEXT NOT NULL, consumed_at TEXT NOT NULL, authorization_kind TEXT NOT NULL DEFAULT 'human');`
	path := filepath.Join(t.TempDir(), "state.db")
	ctx := context.Background()
	migrations := []ports.Migration{{Version: 1, SQL: foundationSQL}}
	store, _, err := sqlite.Open(ctx, path, ports.OpenOptions{Migrations: migrations})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	service := New(Dependencies{
		Resolver: resolverStub{resolved: ports.ResolvedStore{Path: path, Project: domain.ProjectID(project)}},
		Open: func(ctx context.Context, path string, options ports.OpenOptions) (ports.FoundationStore, ports.OpenStatus, error) {
			options.Migrations = migrations
			return sqliteOpener(ctx, path, options)
		},
	})
	result, autoErr := service.AutoMigrate(ctx, Selection{})
	if autoErr.Code != "" {
		t.Fatal(autoErr)
	}
	if !result.Eligible || !result.Applied || !result.Integrity || result.FromVersion != 0 || result.ToVersion != 1 || result.BackupChecksum == "" {
		t.Fatalf("fresh automatic result = %#v", result)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var kind string
	if err := db.QueryRow(`SELECT authorization_kind FROM migration_approvals WHERE approval_id=?`, "auto-backup-"+result.PlanID).Scan(&kind); err != nil {
		t.Fatal(err)
	}
	if kind != string(ports.MigrationAuthorizationAutomaticSafe) {
		t.Fatalf("fresh authorization kind = %q", kind)
	}
}

func TestApplyDoesNotRecreateRemovedCanonicalState(t *testing.T) {
	const project = "removed-before-apply"
	path, db := currentStoreWithoutProject(t, project)
	service := New(Dependencies{Resolver: resolverStub{resolved: ports.ResolvedStore{Path: path, Project: domain.ProjectID(project)}}, Open: sqliteOpener})
	ctx := context.Background()
	plan, err := service.Plan(ctx, Selection{})
	if err.Code != "" {
		t.Fatal(err)
	}
	backup, err := service.Backup(ctx, Selection{}, &plan)
	if err.Code != "" {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	approval := ApprovalFile{
		ApprovalID: "apply-" + project, ApprovedBy: "tester", EvidenceReference: "test",
		PlanID: plan.ID, Project: project, FromVersion: plan.FromVersion, ToVersion: plan.ToVersion,
		Checksums: plan.Checksums, BackupLocation: backup.Location, BackupChecksum: backup.Checksum,
		Command: "omg migration apply", Timestamp: now.Format(time.RFC3339Nano),
		ExpiresAtRaw: now.Add(time.Minute).Format(time.RFC3339Nano),
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	for _, artifact := range []string{path, path + "-wal", path + "-shm", path + "-journal"} {
		if err := os.Remove(artifact); err != nil && !os.IsNotExist(err) {
			t.Fatalf("remove %s: %v", artifact, err)
		}
	}

	if applyErr := service.Apply(ctx, Selection{}, plan, approval); applyErr.Code != domain.CodeUnavailable {
		t.Fatalf("apply error = %+v; want unavailable", applyErr)
	}
	for _, artifact := range []string{path, path + "-wal", path + "-shm", path + "-journal"} {
		if _, err := os.Lstat(artifact); !os.IsNotExist(err) {
			t.Fatalf("apply recreated SQLite artifact %s: %v", artifact, err)
		}
	}
}

func TestApplyRejectsDelegationTokensBeforeOpeningStore(t *testing.T) {
	token := "omgdt_v1_" + strings.Repeat("a", 43)
	now := time.Now().UTC()
	approval := ApprovalFile{
		ApprovalID: "approval", ApprovedBy: "tester", EvidenceReference: "evidence",
		PlanID: "plan", Project: "project", FromVersion: 4, ToVersion: 5,
		Checksums: []string{"checksum"}, BackupLocation: "backup", BackupChecksum: "backup-checksum",
		Command: "omg migration apply", Timestamp: now.Format(time.RFC3339Nano),
		ExpiresAtRaw: now.Add(time.Minute).Format(time.RFC3339Nano),
	}
	cases := []struct {
		name string
		set  func(*ApprovalFile)
	}{
		{"approval ID", func(file *ApprovalFile) { file.ApprovalID = token }},
		{"approved by", func(file *ApprovalFile) { file.ApprovedBy = token }},
		{"evidence reference", func(file *ApprovalFile) { file.EvidenceReference = token }},
		{"plan ID", func(file *ApprovalFile) { file.PlanID = token }},
		{"project", func(file *ApprovalFile) { file.Project = token }},
		{"checksums", func(file *ApprovalFile) { file.Checksums = []string{token} }},
		{"backup location", func(file *ApprovalFile) { file.BackupLocation = token }},
		{"backup checksum", func(file *ApprovalFile) { file.BackupChecksum = token }},
		{"command", func(file *ApprovalFile) { file.Command = token }},
		{"timestamp", func(file *ApprovalFile) { file.Timestamp = token }},
		{"expiration", func(file *ApprovalFile) { file.ExpiresAtRaw = token }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			candidate := approval
			test.set(&candidate)
			opens := 0
			service := New(Dependencies{
				Resolver: resolverStub{resolved: ports.ResolvedStore{Path: filepath.Join(t.TempDir(), "state.db"), Project: "project"}},
				Open: func(context.Context, string, ports.OpenOptions) (ports.FoundationStore, ports.OpenStatus, error) {
					opens++
					return nil, ports.OpenStatus{}, nil
				},
			})

			if err := service.Apply(context.Background(), Selection{}, Plan{}, candidate); err.Code != domain.CodeInvalidArgument {
				t.Fatalf("error = %+v; want invalid approval", err)
			}
			if opens != 0 {
				t.Fatalf("store opened %d times for a token-bearing approval", opens)
			}
		})
	}
}

func TestApplyRejectsInvalidApprovalWithoutCreatingState(t *testing.T) {
	const project = "migration-project"
	now := time.Now().UTC()
	basePlan := Plan{
		ID:             "plan-id",
		Project:        project,
		FromVersion:    4,
		ToVersion:      5,
		Checksums:      []string{"checksum"},
		BackupLocation: "backup.db",
	}
	baseApproval := ApprovalFile{
		ApprovalID:        "approval-id",
		ApprovedBy:        "tester",
		EvidenceReference: "evidence",
		PlanID:            basePlan.ID,
		Project:           project,
		FromVersion:       basePlan.FromVersion,
		ToVersion:         basePlan.ToVersion,
		Checksums:         basePlan.Checksums,
		BackupLocation:    basePlan.BackupLocation,
		BackupChecksum:    "backup-checksum",
		Command:           "omg migration apply",
		Timestamp:         now.Format(time.RFC3339Nano),
		ExpiresAtRaw:      now.Add(time.Minute).Format(time.RFC3339Nano),
	}
	cases := []struct {
		name string
		set  func(*ApprovalFile)
	}{
		{"plan mismatch", func(approval *ApprovalFile) { approval.PlanID = "other-plan" }},
		{"project mismatch", func(approval *ApprovalFile) { approval.Project = "other-project" }},
		{"expired", func(approval *ApprovalFile) {
			approval.Timestamp = now.Add(-2 * time.Minute).Format(time.RFC3339Nano)
			approval.ExpiresAtRaw = now.Add(-time.Minute).Format(time.RFC3339Nano)
		}},
		{"approval ID api key", func(approval *ApprovalFile) { approval.ApprovalID = "api_key=release" }},
		{"approved by api key", func(approval *ApprovalFile) { approval.ApprovedBy = "api-key=release" }},
		{"evidence reference private key", func(approval *ApprovalFile) { approval.EvidenceReference = "private_key=release" }},
		{"approval ID whitespace", func(approval *ApprovalFile) { approval.ApprovalID = " approval-id" }},
		{"approved by Windows path", func(approval *ApprovalFile) { approval.ApprovedBy = `C:\Users\alice\private` }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "state", "state.db")
			approval := baseApproval
			test.set(&approval)
			service := New(Dependencies{
				Resolver: resolverStub{resolved: ports.ResolvedStore{Path: path, Project: project, ProjectRoot: root}},
				Open:     sqliteOpener,
			})

			if err := service.Apply(context.Background(), Selection{}, basePlan, approval); err.Code != domain.CodeInvalidArgument {
				t.Fatalf("error = %+v; want invalid approval", err)
			}
			for _, candidate := range []string{path, path + "-wal", path + "-shm", filepath.Join(root, ".omg")} {
				if _, statErr := os.Lstat(candidate); !os.IsNotExist(statErr) {
					t.Fatalf("invalid approval mutated %s: %v", candidate, statErr)
				}
			}
		})
	}
}

func TestValidateApprovalPermitsCanonicalBackupPath(t *testing.T) {
	now := time.Now().UTC()
	plan := Plan{ID: "plan-id", Project: "project", FromVersion: 4, ToVersion: 5, Checksums: []string{"checksum"}, BackupLocation: `C:\private\backup.db`}
	file := ApprovalFile{
		ApprovalID: "approval-id", ApprovedBy: "tester", EvidenceReference: "evidence",
		PlanID: plan.ID, Project: plan.Project, FromVersion: plan.FromVersion, ToVersion: plan.ToVersion,
		Checksums: plan.Checksums, BackupLocation: plan.BackupLocation, BackupChecksum: "backup-checksum",
		Command: "omg migration apply", Timestamp: now.Format(time.RFC3339Nano), ExpiresAtRaw: now.Add(time.Minute).Format(time.RFC3339Nano),
	}
	if _, valid := validateApproval(plan, domain.ProjectID(plan.Project), file, now); !valid {
		t.Fatal("canonical approval metadata with required backup path rejected")
	}
}

func TestInitConfigFailureLeavesStateUninitialized(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "state.db")
	root := t.TempDir()
	service := New(Dependencies{
		Resolver: resolverStub{resolved: ports.ResolvedStore{Path: path, ProjectRoot: root}},
		Open:     sqliteOpener,
		ConfigInitializer: configInitializerStub(func(context.Context, string) error {
			return errors.New("config initialization failed")
		}),
	})

	if _, err := service.Init(context.Background(), Selection{}); err.Code != domain.CodeUnavailable {
		t.Fatalf("init error = %+v; want unavailable", err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatalf("config failure created state directory: %v", err)
	}
	status, err := service.Status(context.Background(), Selection{}, false)
	if err.Code != "" {
		t.Fatal(err)
	}
	if status.Initialized {
		t.Fatal("status reported failed initialization as initialized")
	}
}

func TestInitCreatesStateAfterConfigInitialization(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "state.db")
	root := t.TempDir()
	configured := false
	service := New(Dependencies{
		Resolver: resolverStub{resolved: ports.ResolvedStore{Path: path, ProjectRoot: root}},
		Open: func(ctx context.Context, path string, options ports.OpenOptions) (ports.FoundationStore, ports.OpenStatus, error) {
			if !configured {
				t.Fatal("store opened before project config initialization")
			}
			return sqliteOpener(ctx, path, options)
		},
		ConfigInitializer: configInitializerStub(func(_ context.Context, got string) error {
			if got != root {
				t.Fatalf("config initializer root = %q; want %q", got, root)
			}
			configured = true
			return nil
		}),
	})

	status, err := service.Init(context.Background(), Selection{})
	if err.Code != "" {
		t.Fatal(err)
	}
	if !status.Initialized {
		t.Fatal("init did not report initialized")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("init did not create state database: %v", err)
	}
}

func TestInitEnrollsProjectOnCurrentStore(t *testing.T) {
	const project = "init-enrollment"
	path, db := currentStoreWithoutProject(t, project)
	defer db.Close()
	root := t.TempDir()
	initialized := ""
	service := New(Dependencies{
		Resolver: resolverStub{resolved: ports.ResolvedStore{Path: path, Project: domain.ProjectID(project), ProjectRoot: root}},
		Open:     sqliteOpener,
		ConfigInitializer: configInitializerStub(func(_ context.Context, got string) error {
			initialized = got
			return nil
		}),
	})

	if _, err := service.Init(context.Background(), Selection{}); err.Code != "" {
		t.Fatal(err)
	}
	if initialized != root {
		t.Fatalf("config initializer root = %q; want %q", initialized, root)
	}
	if count := projectCount(t, db, project); count != 1 {
		t.Fatalf("init project count=%d", count)
	}
}

func currentStoreWithoutProject(t *testing.T, project string) (string, *sql.DB) {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	store, _, err := sqlite.Open(ctx, path, ports.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := store.PlanMigrations(ctx, project)
	if err != nil {
		t.Fatal(err)
	}
	backup, err := store.CreateMigrationBackup(ctx, plan)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	approval := ports.MigrationApproval{
		ApprovalID: "approval-" + project, ApprovedBy: "tester", EvidenceReference: "test",
		PlanID: plan.ID, Project: project, FromVersion: plan.FromVersion, ToVersion: plan.ToVersion,
		Checksums: plan.Checksums, BackupLocation: backup.Location, BackupChecksum: backup.Checksum,
		Command: "omg migration apply", Timestamp: now, ExpiresAt: now.Add(time.Minute),
	}
	if err := store.ApplyMigrations(ctx, plan, approval); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "DELETE FROM projects WHERE id=?", project); err != nil {
		db.Close()
		t.Fatal(err)
	}
	return path, db
}

func projectCount(t *testing.T, db *sql.DB, project string) int {
	t.Helper()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM projects WHERE id=?", project).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestWithCurrentStoreRejectsPendingSchemaWithoutCallingApplication(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	service := New(Dependencies{Resolver: resolverStub{resolved: ports.ResolvedStore{Path: path}}, Open: sqliteOpener})
	store, _, openErr := sqlite.Open(context.Background(), path, ports.OpenOptions{})
	if openErr != nil {
		t.Fatal(openErr)
	}
	if closeErr := store.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	called := false
	err := service.WithCurrentStore(context.Background(), Selection{}, func(ports.ResolvedStore, ports.Store) error {
		called = true
		return nil
	})
	if err.Code != "unavailable" || err.Message != "schema migration is required" {
		t.Fatalf("error = %+v", err)
	}
	if called {
		t.Fatal("application callback ran against a pending schema")
	}
}

func TestWithReadOnlyCurrentStoreRejectsPendingSchemaWithoutCallingApplication(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	service := New(Dependencies{Resolver: resolverStub{resolved: ports.ResolvedStore{Path: path}}, Open: sqliteOpener})
	store, _, openErr := sqlite.Open(context.Background(), path, ports.OpenOptions{})
	if openErr != nil {
		t.Fatal(openErr)
	}
	if closeErr := store.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	called := false
	err := service.WithReadOnlyCurrentStore(context.Background(), Selection{}, func(ports.ResolvedStore, ports.Store) error {
		called = true
		return nil
	})
	if err.Code != domain.CodeUnavailable || err.Message != "schema migration is required" {
		t.Fatalf("error = %+v", err)
	}
	if called {
		t.Fatal("application callback ran against a pending schema")
	}
}
func TestReadApprovalRejectsUnknownFields(t *testing.T) {
	if _, err := ReadApproval([]byte(`{"approved_by":"Ada","token":"secret"}`)); err.Code == "" {
		t.Fatal("accepted unexpected approval field")
	}
}

func TestUnavailableFromStatePathErrorsIsSafeAndActionable(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"sqlite: state ancestor extended ACL is not private", "ancestor grants another account write access"},
		{"sqlite: state path DACL is not private", "ancestor grants another account write access"},
		{"sqlite: unsafe writable state ancestor", "ancestor is writable by another account"},
		{"sqlite: state directory owner is not the current user", "not owned by the current user"},
		{"sqlite: reparse points are not permitted in state paths", "unsafe filesystem component"},
	}
	for _, test := range tests {
		err := unavailableFrom(errors.New(test.input))
		if err.Code != domain.CodeUnavailable || !strings.Contains(err.Message, test.want) {
			t.Errorf("unavailableFrom(%q) = %+v", test.input, err)
		}
		if strings.Contains(err.Message, "/") || strings.Contains(err.Message, "\\") {
			t.Errorf("safe error leaked a path: %q", err.Message)
		}
	}
	if err := unavailableFrom(errors.New("sqlite: configure failed at /private/path")); err.Message != "foundation service is unavailable" {
		t.Fatalf("unknown error detail escaped generic boundary: %+v", err)
	}
}
