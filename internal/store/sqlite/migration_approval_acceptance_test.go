package sqlite

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// OMG-AC-016, SEC-T14, SEC-T15, PRIV-T05: an apply needs one current,
// plan-bound approval and preserves a complete forensic record only on success.
func TestMigrationApprovalAcceptanceRejectsAbsentMismatchedExpiredAndFutureApprovalsWithoutMutation(t *testing.T) {
	ctx := context.Background()
	store, _, err := Open(ctx, filepath.Join(t.TempDir(), "state.db"), OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	plan, backup, approval := migrationApproval(t, store, "approval-rejections")
	cases := []struct {
		name   string
		mutate func(*Approval)
	}{
		{name: "absent", mutate: func(a *Approval) { *a = Approval{} }},
		{name: "mismatched plan", mutate: func(a *Approval) { a.PlanID = "other-plan" }},
		{name: "expired", mutate: func(a *Approval) {
			now := time.Now().UTC()
			a.Timestamp = now.Add(-10 * time.Minute)
			a.ExpiresAt = now.Add(-5 * time.Minute)
		}},
		{name: "future issued", mutate: func(a *Approval) {
			a.Timestamp = time.Now().UTC().Add(time.Minute)
			a.ExpiresAt = a.Timestamp.Add(time.Minute)
		}},
		{name: "backup binding mismatch", mutate: func(a *Approval) { a.BackupLocation = backup.Location + ".other" }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			candidate := approval
			tc.mutate(&candidate)
			if err := store.ApplyMigrations(ctx, plan, candidate); !errors.Is(err, ErrMigrationApproval) {
				t.Fatalf("ApplyMigrations error = %v; want ErrMigrationApproval", err)
			}
			assertMigrationApplyState(t, store, 0, 0, 0)
		})
	}
}

// OMG-AC-016 and SEC-T14: a valid approval is consumed exactly once, bound to
// the verified backup, and yields both receipt and append-only audit evidence.
func TestMigrationApprovalAcceptanceConsumesExactlyOnceAndRejectsReplay(t *testing.T) {
	ctx := context.Background()
	store, _, err := Open(ctx, filepath.Join(t.TempDir(), "state.db"), OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	plan, backup, approval := migrationApproval(t, store, "approval-once")
	if err := store.ApplyMigrations(ctx, plan, approval); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(backup.Location); err != nil || info.Size() == 0 {
		t.Fatalf("verified backup unavailable: info=%v err=%v", info, err)
	}
	assertMigrationApplyState(t, store, len(store.migrations), 1, 1)

	var gotPlanID, gotBackupLocation, gotBackupChecksum, consumedAt string
	if err := store.db.QueryRowContext(ctx, `SELECT plan_id,backup_location,backup_checksum,consumed_at FROM migration_approvals WHERE approval_id=?`, approval.ApprovalID).Scan(&gotPlanID, &gotBackupLocation, &gotBackupChecksum, &consumedAt); err != nil {
		t.Fatal(err)
	}
	if gotPlanID != plan.ID || gotBackupLocation != backup.Location || gotBackupChecksum != backup.Checksum || consumedAt == "" {
		t.Fatalf("approval record = plan=%q backup=%q checksum=%q consumed=%q", gotPlanID, gotBackupLocation, gotBackupChecksum, consumedAt)
	}

	replaySQL := `CREATE TABLE replay_consumed_approval (id INTEGER PRIMARY KEY)`
	store.migrations = append(store.migrations, Migration{
		Version:  len(store.migrations) + 1,
		SQL:      replaySQL,
		Checksum: checksumSQL(replaySQL),
	})
	replayPlan, _, replayApproval := migrationApproval(t, store, "approval-once")
	replayApproval.ApprovalID = approval.ApprovalID
	if err := store.ApplyMigrations(ctx, replayPlan, replayApproval); !errors.Is(err, ErrMigrationApproval) {
		t.Fatalf("replayed ApplyMigrations error = %v; want ErrMigrationApproval", err)
	}
	assertMigrationApplyState(t, store, len(store.migrations)-1, 1, 1)
}

// PRIV-T05: a failed SQL migration leaves the original database untouched while
// retaining the independently created recovery backup.
func TestMigrationApprovalAcceptanceFailedApplyRetainsOriginalStateAndBackup(t *testing.T) {
	ctx := context.Background()
	store, _, err := Open(ctx, filepath.Join(t.TempDir(), "state.db"), OpenOptions{Migrations: []Migration{
		{Version: 1, SQL: foundationSQL},
		{Version: 2, SQL: "CREATE TABLE broken ("},
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	plan, backup, approval := migrationApproval(t, store, "failed-apply")
	if err := store.ApplyMigrations(ctx, plan, approval); err == nil {
		t.Fatal("ApplyMigrations unexpectedly succeeded")
	}
	assertMigrationApplyState(t, store, 0, 0, 0)
	if healthy, err := integrityPath(ctx, backup.Location); err != nil || !healthy {
		t.Fatalf("recovery backup integrity = %v, %v", healthy, err)
	}
}

func assertMigrationApplyState(t *testing.T, store *SQLiteStore, migrations, approvals, receipts int) {
	t.Helper()
	exists, err := tableExists(context.Background(), store.db, "schema_migrations")
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		if migrations != 0 || approvals != 0 || receipts != 0 {
			t.Fatalf("migration schema absent; want migrations=%d approvals=%d receipts=%d", migrations, approvals, receipts)
		}
		return
	}

	assertCount(t, store.db, "schema_migrations", migrations)
	assertCount(t, store.db, "migration_approvals", approvals)
	assertCount(t, store.db, "command_receipts", receipts)
	assertCount(t, store.db, "audit_events", receipts)
	if receipts == 1 {
		var evidence int
		if err := store.db.QueryRow(`SELECT COUNT(*) FROM command_receipts r JOIN audit_events a ON a.receipt_id=r.id WHERE r.idempotency_key LIKE 'migration-apply:%' AND a.event_type='migration_applied'`).Scan(&evidence); err != nil {
			t.Fatal(err)
		}
		if evidence != 1 {
			t.Fatalf("migration receipt/audit evidence count = %d; want 1", evidence)
		}
	}
}
