package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jeremy-merchant/OMG/internal/domain"
	"github.com/jeremy-merchant/OMG/internal/ports"
)

func TestOpenReportsPendingWithoutSchemaMutation(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	store, status, err := Open(ctx, path, OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if len(status.Pending) != 11 || status.Pending[0].Version != 1 || status.Pending[1].Version != 2 || status.Pending[2].Version != 3 || status.Pending[3].Version != 4 || status.Pending[4].Version != 5 || status.Pending[5].Version != 6 || status.Pending[6].Version != 7 || status.Pending[7].Version != 8 || status.Pending[8].Version != 9 || status.Pending[9].Version != 10 || status.Pending[10].Version != 11 {
		t.Fatalf("pending = %#v", status.Pending)
	}
	exists, err := tableExists(ctx, store.db, "schema_migrations")
	if err != nil {
		t.Fatal(err)
	}
	sqlSource, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "0001_foundation.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if string(sqlSource) != foundationSQL {
		t.Fatal("embedded migration differs from migration file")
	}
	if status.Pending[1].SQL != coordinationSQL {
		t.Fatal("embedded coordination migration differs from source")
	}
	if status.Pending[2].SQL != reservationSQL {
		t.Fatal("embedded reservation migration differs from source")
	}
	if status.Pending[5].SQL != scopedHumansSQL {
		t.Fatal("embedded scoped humans migration differs from source")
	}
	if status.Pending[7].SQL != legacyHumanAssociationsSQL {
		t.Fatal("embedded legacy human associations migration differs from source")
	}
	handoffLifecycleSource, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "0009_handoff_lifecycle.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if status.Pending[8].SQL != handoffLifecycleSQL || status.Pending[8].SQL != string(handoffLifecycleSource) {
		t.Fatal("embedded handoff lifecycle migration differs from source")
	}
	exactSHACanarySource, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "0010_exact_sha_canary.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if status.Pending[9].SQL != exactSHACanarySQL || status.Pending[9].SQL != string(exactSHACanarySource) {
		t.Fatal("embedded exact-SHA canary migration differs from source")
	}
	automaticAuthorizationSource, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "0011_automatic_migration_authorization.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if status.Pending[10].SQL != automaticMigrationAuthorizationSQL || status.Pending[10].SQL != string(automaticAuthorizationSource) || !status.Pending[10].AutomaticSafe {
		t.Fatal("embedded automatic migration authorization differs from source or is not declared safe")
	}
	if exists {
		t.Fatal("open applied schema migration")
	}
}

func TestForeignKeysAndIdempotentReceiptEvent(t *testing.T) {
	store := migratedStore(t, OpenOptions{})
	ctx := context.Background()
	if _, err := store.db.ExecContext(ctx, `INSERT INTO audit_events(id,receipt_id,event_type,payload_json,occurred_at) VALUES('bad','missing','x','{}','now')`); err == nil {
		t.Fatal("foreign key violation succeeded")
	}
	var calls atomic.Int32
	callback := func(_ ports.Repositories) (domain.Result, error) {
		calls.Add(1)
		return domain.Result{ID: "result-1", Outcome: domain.OutcomeOK, Data: map[string]string{"value": "first"}}, nil
	}
	firstReceipt, firstResult, err := store.Write(ctx, "same-key", "test.write", callback)
	if err != nil {
		t.Fatal(err)
	}
	secondReceipt, secondResult, err := store.Write(ctx, "same-key", "test.write", callback)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("callback calls = %d; want 1", calls.Load())
	}
	if firstReceipt != secondReceipt || firstResult.ID != secondResult.ID || firstResult.Receipt != secondResult.Receipt {
		t.Fatal("duplicate command did not replay original result")
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE audit_events SET event_type='changed' WHERE receipt_id=?`, firstReceipt.ID); err == nil {
		t.Fatal("audit event update succeeded")
	}
	assertCount(t, store.db, "audit_events", 2)
}

func TestWriteConflictsWhenIdempotencyKeyOperationDiffers(t *testing.T) {
	store := migratedStore(t, OpenOptions{})
	ctx := context.Background()
	var baselineReceipts, baselineEvents int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM command_receipts").Scan(&baselineReceipts); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow("SELECT COUNT(*) FROM audit_events").Scan(&baselineEvents); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	callback := func(_ ports.Repositories) (domain.Result, error) {
		calls.Add(1)
		return domain.Result{ID: "created", Outcome: domain.OutcomeOK}, nil
	}
	if _, _, err := store.Write(ctx, "shared-key", "task.create", callback); err != nil {
		t.Fatal(err)
	}
	_, _, err := store.Write(ctx, "shared-key", "task.transition", callback)
	var domainErr domain.DomainError
	if !errors.As(err, &domainErr) || domainErr.Code != domain.CodeConflict {
		t.Fatalf("cross-operation replay error = %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("callback calls = %d; want 1", calls.Load())
	}
	assertCount(t, store.db, "command_receipts", baselineReceipts+1)
	assertCount(t, store.db, "audit_events", baselineEvents+1)
}

func TestWriteRejectsSecretLikeIdempotencyKeysBeforeAnyMutation(t *testing.T) {
	store := migratedStore(t, OpenOptions{})
	ctx := context.Background()
	var baselineReceipts, baselineEvents int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM command_receipts").Scan(&baselineReceipts); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow("SELECT COUNT(*) FROM audit_events").Scan(&baselineEvents); err != nil {
		t.Fatal(err)
	}
	for _, key := range []domain.IdempotencyKey{"password-key", "token-key", "/private/key"} {
		var calls int
		_, _, err := store.Write(ctx, key, "test.write", func(_ ports.Repositories) (domain.Result, error) {
			calls++
			return domain.Result{Outcome: domain.OutcomeOK}, nil
		})
		var domainErr domain.DomainError
		if !errors.As(err, &domainErr) || domainErr.Code != domain.CodeInvalidArgument {
			t.Fatalf("Write(%q) error = %v", key, err)
		}
		if calls != 0 {
			t.Fatalf("Write(%q) invoked callback", key)
		}
		assertCount(t, store.db, "command_receipts", baselineReceipts)
		assertCount(t, store.db, "audit_events", baselineEvents)
	}
	if _, _, err := store.Write(ctx, "benign-write-1", "test.write", func(_ ports.Repositories) (domain.Result, error) {
		return domain.Result{Outcome: domain.OutcomeOK}, nil
	}); err != nil {
		t.Fatalf("benign key rejected: %v", err)
	}
	assertCount(t, store.db, "command_receipts", baselineReceipts+1)
	assertCount(t, store.db, "audit_events", baselineEvents+1)
}

func TestApprovalMismatchBlocksApply(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	store, _, err := Open(ctx, path, OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	plan, backup, approval := migrationApproval(t, store, "fixture")
	approval.PlanID = "other-plan"
	if err := store.ApplyMigrations(ctx, plan, approval); !errors.Is(err, ErrMigrationApproval) {
		t.Fatalf("ApplyMigrations error = %v", err)
	}
	exists, err := tableExists(ctx, store.db, "schema_migrations")
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("mismatch mutated schema")
	}
	if backup.Checksum == "" {
		t.Fatal("backup was not verified")
	}
}

func TestApprovedMigrationAndFailurePreserveBackup(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	failing := []Migration{{Version: 1, SQL: foundationSQL}, {Version: 2, SQL: "CREATE TABLE broken ("}}
	store, _, err := Open(ctx, path, OpenOptions{Migrations: failing})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	plan, backup, approval := migrationApproval(t, store, "failure-fixture")
	if err := store.ApplyMigrations(ctx, plan, approval); err == nil {
		t.Fatal("failing migration succeeded")
	}
	exists, err := tableExists(ctx, store.db, "schema_migrations")
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("failed migration partially applied foundation schema")
	}
	healthy, err := integrityPath(ctx, backup.Location)
	if err != nil || !healthy {
		t.Fatalf("backup integrity = %v, %v", healthy, err)
	}

	good := migratedStore(t, OpenOptions{})
	report, err := good.CheckIntegrity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Healthy {
		t.Fatal("integrity report is unhealthy")
	}
}

func TestAutomaticSafeMigrationRequiresIncrementalAllSafePlanAndRecordsEvidence(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	seedSchemaThrough(t, path, 10)
	store, status, err := Open(ctx, path, OpenOptions{ExistingOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if len(status.Pending) != 1 || status.Pending[0].Version != 11 || !status.Pending[0].AutomaticSafe {
		t.Fatalf("pending = %#v; want only auto-safe v11", status.Pending)
	}
	plan, err := store.PlanMigrations(ctx, "auto-safe-project")
	if err != nil {
		t.Fatal(err)
	}
	if !plan.AutomaticEligible || plan.FromVersion != 10 || plan.ToVersion != 11 {
		t.Fatalf("plan = %#v; want eligible v10 to v11", plan)
	}
	backup, err := store.CreateMigrationBackup(ctx, plan)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	approval := Approval{
		ApprovalID: "auto-safe-" + plan.ID, ApprovedBy: automaticMigrationPolicyActor, EvidenceReference: automaticMigrationPolicyEvidence,
		PlanID: plan.ID, Project: plan.Project, FromVersion: plan.FromVersion, ToVersion: plan.ToVersion, Checksums: plan.Checksums,
		BackupLocation: backup.Location, BackupChecksum: backup.Checksum, Command: automaticMigrationApplyCommand,
		Timestamp: now, ExpiresAt: now.Add(5 * time.Minute), AuthorizationKind: ports.MigrationAuthorizationAutomaticSafe,
	}
	if err := store.ApplyMigrations(ctx, plan, approval); err != nil {
		t.Fatal(err)
	}
	var kind, operation, eventType string
	if err := store.db.QueryRowContext(ctx, `SELECT authorization_kind FROM migration_approvals WHERE approval_id=?`, approval.ApprovalID).Scan(&kind); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT operation FROM command_receipts WHERE idempotency_key=?`, "migration-auto-apply:"+approval.ApprovalID).Scan(&operation); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT event_type FROM audit_events WHERE receipt_id=(SELECT id FROM command_receipts WHERE idempotency_key=?)`, "migration-auto-apply:"+approval.ApprovalID).Scan(&eventType); err != nil {
		t.Fatal(err)
	}
	if kind != string(ports.MigrationAuthorizationAutomaticSafe) || operation != "migration.auto_apply" || eventType != "migration_auto_applied" {
		t.Fatalf("automatic evidence kind=%q operation=%q event=%q", kind, operation, eventType)
	}
	if healthy, err := integrityPath(ctx, backup.Location); err != nil || !healthy {
		t.Fatalf("backup integrity = %t, %v", healthy, err)
	}
}

func TestAutomaticSafeMigrationRejectsFreshAndMixedRiskPlans(t *testing.T) {
	ctx := context.Background()
	fresh, _, err := Open(ctx, filepath.Join(t.TempDir(), "fresh.db"), OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	freshPlan, err := fresh.PlanMigrations(ctx, "fresh-project")
	if err != nil {
		t.Fatal(err)
	}
	if freshPlan.AutomaticEligible {
		t.Fatal("fresh initialization was marked automatic eligible")
	}

	path := filepath.Join(t.TempDir(), "mixed.db")
	seedSchemaThrough(t, path, 9)
	mixed, _, err := Open(ctx, path, OpenOptions{ExistingOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer mixed.Close()
	mixedPlan, err := mixed.PlanMigrations(ctx, "mixed-project")
	if err != nil {
		t.Fatal(err)
	}
	if mixedPlan.AutomaticEligible || mixedPlan.FromVersion != 9 || mixedPlan.ToVersion != 11 {
		t.Fatalf("mixed plan = %#v; want ineligible v9 to v11", mixedPlan)
	}
}

func TestConcurrentShortWritesRemainConsistent(t *testing.T) {
	store := migratedStore(t, OpenOptions{})
	ctx := context.Background()
	var group sync.WaitGroup
	errorsCh := make(chan error, 32)
	for i := range 32 {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			_, _, err := store.Write(ctx, domain.IdempotencyKey(fmt.Sprintf("key-%d", i)), "test.write", func(_ ports.Repositories) (domain.Result, error) {
				return domain.Result{ID: domain.ResultID(fmt.Sprintf("result-%d", i)), Outcome: domain.OutcomeOK}, nil
			})
			if err != nil {
				errorsCh <- err
			}
		}(i)
	}
	group.Wait()
	close(errorsCh)
	for err := range errorsCh {
		t.Error(err)
	}
	assertCount(t, store.db, "command_receipts", 33)
	assertCount(t, store.db, "audit_events", 33)
}

func migratedStore(t *testing.T, options OpenOptions) *SQLiteStore {
	t.Helper()
	ctx := context.Background()
	store, _, err := Open(ctx, filepath.Join(t.TempDir(), "state.db"), options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	plan, _, approval := migrationApproval(t, store, "test-project")
	if err := store.ApplyMigrations(ctx, plan, approval); err != nil {
		t.Fatal(err)
	}
	return store
}

func seedSchemaThrough(t *testing.T, path string, version int) {
	t.Helper()
	ctx := context.Background()
	store, _, err := Open(ctx, path, OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	migrations := append([]Migration(nil), store.migrations...)
	for _, migration := range migrations {
		if migration.Version > version {
			break
		}
		if _, err := store.db.ExecContext(ctx, migration.SQL); err != nil {
			_ = store.Close()
			t.Fatalf("apply fixture migration %d: %v", migration.Version, err)
		}
		if _, err := store.db.ExecContext(ctx, `INSERT INTO schema_migrations(version,checksum,applied_at) VALUES(?,?,?)`, migration.Version, migration.Checksum, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			_ = store.Close()
			t.Fatalf("record fixture migration %d: %v", migration.Version, err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}

func migrationApproval(t *testing.T, store *SQLiteStore, project string) (MigrationPlan, ports.BackupMetadata, Approval) {
	t.Helper()
	plan, err := store.PlanMigrations(context.Background(), project)
	if err != nil {
		t.Fatal(err)
	}
	backup, err := store.CreateMigrationBackup(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	return plan, backup, Approval{ApprovalID: "approval-" + project, ApprovedBy: "test", EvidenceReference: "test", PlanID: plan.ID, Project: project, FromVersion: plan.FromVersion, ToVersion: plan.ToVersion, Checksums: plan.Checksums, BackupLocation: backup.Location, BackupChecksum: backup.Checksum, Command: migrationApplyCommand, Timestamp: now, ExpiresAt: now.Add(5 * time.Minute)}
}

func assertCount(t *testing.T, db *sql.DB, table string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("%s count = %d; want %d", table, got, want)
	}
}
