package bootstrap

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jeremy-merchant/oh-my-group/internal/app"
	appfoundation "github.com/jeremy-merchant/oh-my-group/internal/app/foundation"
	"github.com/jeremy-merchant/oh-my-group/internal/domain"
	instructions "github.com/jeremy-merchant/oh-my-group/internal/integration/instructions"
	"github.com/jeremy-merchant/oh-my-group/internal/ports"
	"github.com/jeremy-merchant/oh-my-group/internal/store/sqlite"
	watchmode "github.com/jeremy-merchant/oh-my-group/internal/watch"
)

type bootstrapResolverStub struct{ resolved ports.ResolvedStore }

func (r bootstrapResolverStub) Resolve(context.Context, ports.ResolveRequest) (ports.ResolvedStore, error) {
	return r.resolved, nil
}

func TestWatchStatusDoesNotAppendWALFallbackAuditEvent(t *testing.T) {
	ctx := context.Background()
	path, db := initializedBootstrapStore(t, "watch-status")
	defer db.Close()

	service := appfoundation.New(appfoundation.Dependencies{
		Resolver: bootstrapResolverStub{resolved: ports.ResolvedStore{Path: path, Project: "watch-status"}},
		Open: func(ctx context.Context, path string, options ports.OpenOptions) (ports.FoundationStore, ports.OpenStatus, error) {
			options.WALEligible = func(string) bool { return false }
			store, status, err := sqlite.Open(ctx, path, options)
			if err != nil {
				return nil, ports.OpenStatus{}, err
			}
			return store, status, nil
		},
	})
	application := CLIService(service)

	var auditBefore int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM audit_events").Scan(&auditBefore); err != nil {
		t.Fatal(err)
	}

	result, err := application.Watch(ctx, app.CLIWatchRequest{Status: true})
	if err.Code != "" {
		t.Fatal(err)
	}
	status, ok := result.Status.(watchmode.Status)
	if !ok || status.Code != watchmode.StatusStopped {
		t.Fatalf("watch status = %#v; want stopped", result.Status)
	}

	var auditAfter int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM audit_events").Scan(&auditAfter); err != nil {
		t.Fatal(err)
	}
	if auditAfter != auditBefore {
		t.Fatalf("watch status appended WAL fallback audit event: %d -> %d", auditBefore, auditAfter)
	}
}

func TestIntegrationUsesConfiguredInstructionTargets(t *testing.T) {
	root := t.TempDir()
	target := ".github/instructions/OMG.md"
	if err := os.MkdirAll(filepath.Join(root, ".omg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".github", "instructions"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".omg", "project.toml"), []byte("[integrations]\ninstruction_targets = [\""+target+"\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	application := integrationApplication(root)

	planned := integrationPlans(t, application, root, "plan")
	if len(planned) != 1 || planned[0].Target.Path != target || planned[0].Action != instructions.ActionCreate {
		t.Fatalf("configured plan = %#v", planned)
	}
	if _, err := application.Integration(context.Background(), app.CLIIntegrationRequest{Project: root, Subcommand: "apply"}); err.Code != "" {
		t.Fatal(err)
	}
	configuredPath := filepath.Join(root, filepath.FromSlash(target))
	content, err := os.ReadFile(configuredPath)
	if err != nil || !strings.Contains(string(content), "<!-- OMG BEGIN v1 -->") {
		t.Fatalf("configured target was not applied: content=%q err=%v", content, err)
	}
	if _, err := os.Stat(filepath.Join(root, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("default target was changed: %v", err)
	}
	if _, err := application.Integration(context.Background(), app.CLIIntegrationRequest{Project: root, Subcommand: "remove"}); err.Code != "" {
		t.Fatal(err)
	}
	if _, err := os.Stat(configuredPath); !os.IsNotExist(err) {
		t.Fatalf("configured target was not removed: %v", err)
	}
	status, statusErr := application.Integration(context.Background(), app.CLIIntegrationRequest{Project: root, Subcommand: "status"})
	if statusErr.Code != "" {
		t.Fatal(statusErr)
	}
	statuses, ok := status.([]instructions.Status)
	if !ok || len(statuses) != 1 || statuses[0].Target.Path != target {
		t.Fatalf("configured status = %#v", status)
	}
}

func TestIntegrationDefaultsAndRejectsUnsafeConfiguredTargets(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		root := t.TempDir()
		plans := integrationPlans(t, integrationApplication(root), root, "plan")
		if len(plans) != 2 || plans[0].Target.Path != "AGENTS.md" || plans[1].Target.Path != "CLAUDE.md" {
			t.Fatalf("default plan = %#v", plans)
		}
	})
	t.Run("out of project", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, ".omg"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, ".omg", "project.toml"), []byte("[integrations]\ninstruction_targets = [\"../outside.md\"]\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := integrationApplication(root).Integration(context.Background(), app.CLIIntegrationRequest{Project: root, Subcommand: "plan"})
		if err.Code != domain.CodeInvalidArgument {
			t.Fatalf("unsafe target error = %#v", err)
		}
	})
}

func integrationApplication(root string) app.CLIService {
	service := appfoundation.New(appfoundation.Dependencies{
		Resolver: bootstrapResolverStub{resolved: ports.ResolvedStore{ProjectRoot: root}},
	})
	return CLIService(service)
}

func integrationPlans(t *testing.T, application app.CLIService, root, subcommand string) []instructions.Plan {
	t.Helper()
	result, err := application.Integration(context.Background(), app.CLIIntegrationRequest{Project: root, Subcommand: subcommand})
	if err.Code != "" {
		t.Fatal(err)
	}
	plans, ok := result.([]instructions.Plan)
	if !ok {
		t.Fatalf("plans = %#v", result)
	}
	return plans
}

func initializedBootstrapStore(t *testing.T, project string) (string, *sql.DB) {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	store, _, err := sqlite.Open(ctx, path, ports.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := store.PlanMigrations(ctx, project)
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	backup, err := store.CreateMigrationBackup(ctx, plan)
	if err != nil {
		store.Close()
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
		store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	return path, db
}
