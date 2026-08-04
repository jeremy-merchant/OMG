package sqlite

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jeremy-merchant/oh-my-group/internal/ports"
)

func TestApplyMigrationsRejectsFutureIssuedApprovalWithoutMutation(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, time.July, 23, 12, 0, 0, 0, time.UTC)
	store, _, err := Open(ctx, filepath.Join(canonicalTempDir(t), "state.db"), OpenOptions{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	plan, err := store.PlanMigrations(ctx, "future-approval")
	if err != nil {
		t.Fatal(err)
	}
	backup, err := store.CreateMigrationBackup(ctx, plan)
	if err != nil {
		t.Fatal(err)
	}
	approval := approvalForPlan(plan, backup, now.Add(time.Second), now.Add(5*time.Minute))

	if err := store.ApplyMigrations(ctx, plan, approval); !errors.Is(err, ErrMigrationApproval) {
		t.Fatalf("ApplyMigrations error = %v; want ErrMigrationApproval", err)
	}
	exists, err := tableExists(ctx, store.db, "schema_migrations")
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("future-issued approval mutated schema")
	}
}

func TestApplyMigrationsAcceptsApprovalIssuedAtStoreClock(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, time.July, 23, 12, 0, 0, 0, time.UTC)
	store, _, err := Open(ctx, filepath.Join(canonicalTempDir(t), "state.db"), OpenOptions{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	plan, err := store.PlanMigrations(ctx, "current-approval")
	if err != nil {
		t.Fatal(err)
	}
	backup, err := store.CreateMigrationBackup(ctx, plan)
	if err != nil {
		t.Fatal(err)
	}
	approval := approvalForPlan(plan, backup, now, now.Add(15*time.Minute))

	if err := store.ApplyMigrations(ctx, plan, approval); err != nil {
		t.Fatalf("ApplyMigrations error = %v", err)
	}
}

func TestBackupIntegrityUsesExactURISignificantPath(t *testing.T) {
	ctx := context.Background()
	store, _, err := Open(ctx, filepath.Join(canonicalTempDir(t), "state.db"), OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	backupPath := filepath.Join(canonicalTempDir(t), "backup?exact#%.db")
	truncatedNeighbor := backupPath[:len(backupPath)-len("?exact#%.db")]
	if err := os.WriteFile(truncatedNeighbor, []byte("not a SQLite database"), 0o600); err != nil {
		t.Fatal(err)
	}

	backup, err := store.Backup(ctx, ports.BackupDestination(backupPath))
	if err != nil {
		t.Fatalf("Backup error = %v", err)
	}
	if backup.Location != backupPath {
		t.Fatalf("backup location = %q; want %q", backup.Location, backupPath)
	}
	checksum, err := fileChecksum(backupPath)
	if err != nil {
		t.Fatal(err)
	}
	if backup.Checksum != checksum {
		t.Fatalf("backup checksum = %q; want checksum of exact path %q", backup.Checksum, checksum)
	}
}

func canonicalTempDir(t *testing.T) string {
	t.Helper()
	path, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func approvalForPlan(plan MigrationPlan, backup ports.BackupMetadata, issued, expires time.Time) Approval {
	return Approval{
		ApprovalID:        "approval-" + plan.Project,
		ApprovedBy:        "test",
		EvidenceReference: "test",
		PlanID:            plan.ID,
		Project:           plan.Project,
		FromVersion:       plan.FromVersion,
		ToVersion:         plan.ToVersion,
		Checksums:         plan.Checksums,
		BackupLocation:    backup.Location,
		BackupChecksum:    backup.Checksum,
		Command:           migrationApplyCommand,
		Timestamp:         issued,
		ExpiresAt:         expires,
	}
}
