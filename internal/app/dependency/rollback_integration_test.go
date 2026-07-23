package dependency

import (
	"context"
	"testing"
	"time"

	"example.invalid/coordledger/internal/app/testsupport"
	coord "example.invalid/coordledger/internal/domain/coordination"
	core "example.invalid/coordledger/internal/domain/lineage"
)

func TestTransitionAndNotificationRollbackTogetherOnStoreFailure(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	s, db := testsupport.Store(t, now)
	testsupport.Seed(t, s, now)
	svc := New(s, func() time.Time { return now })
	if _, err := svc.Add(ctx, "edge", coord.Dependency{ID: "edge", PrerequisiteTaskID: "a", DependentTaskID: "b", Kind: coord.DependencyHard, Criterion: coord.UnblockWorkComplete}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TRIGGER fail_dependency_message BEFORE INSERT ON messages WHEN NEW.type = 'DEPENDENCY' BEGIN SELECT RAISE(ABORT, 'injected failure'); END"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TransitionAndReconcile(ctx, "transition", testsupport.Project, "a", "source", core.TaskWorkComplete, nil); err == nil {
		t.Fatal("injected notification failure unexpectedly succeeded")
	}
	var state string
	var facts int
	var messages int
	if err := db.QueryRow("SELECT state FROM tasks WHERE id='a'").Scan(&state); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM task_dependencies WHERE satisfied_at IS NOT NULL").Scan(&facts); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM messages WHERE type='DEPENDENCY'").Scan(&messages); err != nil {
		t.Fatal(err)
	}
	if state != "CLAIMED" || facts != 0 || messages != 0 {
		t.Fatalf("rollback failed: state=%s facts=%d messages=%d", state, facts, messages)
	}
}
