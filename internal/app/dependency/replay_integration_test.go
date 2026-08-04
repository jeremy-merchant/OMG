package dependency

import (
	"context"
	"database/sql"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jeremy-merchant/oh-my-group/internal/app/testsupport"
	"github.com/jeremy-merchant/oh-my-group/internal/domain"
	coord "github.com/jeremy-merchant/oh-my-group/internal/domain/coordination"
	core "github.com/jeremy-merchant/oh-my-group/internal/domain/lineage"
	"github.com/jeremy-merchant/oh-my-group/internal/ports"
)

func TestDependencyReplayReconcileAndCriteriaProgressionAreExactOnce(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	store, db := testsupport.Store(t, now)
	testsupport.Seed(t, store, now)
	svc := New(store, func() time.Time { return now })

	work := coord.Dependency{ID: "work-canonical", PrerequisiteTaskID: "a", DependentTaskID: "b", Kind: coord.DependencyHard, Criterion: coord.UnblockWorkComplete}
	firstDependency, err := svc.Add(ctx, "dependency-replay", work)
	if err != nil {
		t.Fatal(err)
	}
	secondDependency, err := svc.Add(ctx, "dependency-replay", coord.Dependency{ID: "dependency-altered", PrerequisiteTaskID: "a", DependentTaskID: "c", Kind: coord.DependencySoft, Criterion: coord.UnblockVerifiedDone})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(secondDependency, firstDependency) {
		t.Fatalf("replay returned %#v; want canonical %#v", secondDependency, firstDependency)
	}
	assertDependencyCount(t, db, "task_dependencies", 1)
	assertDependencyCount(t, db, "messages WHERE type='DEPENDENCY'", 0)
	receipts, audit := dependencyReceiptCounts(t, db)
	if receipts != 3 || audit != 3 {
		t.Fatalf("dependency replay produced receipts=%d audit=%d; want migration, seed, and one dependency command record", receipts, audit)
	}
	verified := coord.Dependency{ID: "verified-canonical", PrerequisiteTaskID: "a", DependentTaskID: "c", Kind: coord.DependencyHard, Criterion: coord.UnblockVerifiedDone}
	if _, err := svc.Add(ctx, "dependency-verified", verified); err != nil {
		t.Fatal(err)
	}
	_, _, err = store.Write(ctx, "block-dependents", "test.write", func(r ports.Repositories) (domain.Result, error) {
		for _, id := range []core.ID{"b", "c"} {
			if _, err := r.Coordination().TransitionTask(ctx, id, core.TaskBlocked, nil, now); err != nil {
				return domain.Result{}, err
			}
		}
		return domain.Result{ID: "blocked", Outcome: domain.OutcomeOK}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	beforeReceipts, beforeAudit := dependencyReceiptCounts(t, db)

	firstTransition, err := svc.TransitionAndReconcile(ctx, "transition-work", testsupport.Project, "a", "source", core.TaskWorkComplete, []byte("private work evidence: customer-secret"))
	if err != nil {
		t.Fatal(err)
	}
	secondTransition, err := svc.TransitionAndReconcile(ctx, "transition-work", testsupport.Project, "a", "source", core.TaskVerifiedDone, []byte("hostile replay evidence: rotate-token"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(secondTransition, firstTransition) || firstTransition.State != core.TaskWorkComplete {
		t.Fatalf("transition replay=%#v; want work-complete canonical %#v", secondTransition, firstTransition)
	}
	assertDependencyState(t, db, "b", core.TaskInProgress)
	assertDependencyPrivateAbsent(t, db, "private work evidence: customer-secret", "hostile replay evidence: rotate-token", "hard", "work_complete", "soft", "verified_done")
	assertDependencyCount(t, db, "task_dependencies WHERE satisfied_at IS NOT NULL", 1)
	assertDependencyCount(t, db, "messages WHERE type='DEPENDENCY'", 1)
	assertDependencyPrivateAbsent(t, db, "private work evidence: customer-secret", "hostile replay evidence: rotate-token")

	firstReconcile, err := svc.Reconcile(ctx, "reconcile-replay", testsupport.Project, "a")
	if err != nil {
		t.Fatal(err)
	}
	secondReconcile, err := svc.Reconcile(ctx, "reconcile-replay", "foreign", "foreign-task")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(secondReconcile, firstReconcile) || firstReconcile.ID != "a" || firstReconcile.State != core.TaskWorkComplete {
		t.Fatalf("reconcile replay=%#v; want canonical %#v", secondReconcile, firstReconcile)
	}
	assertDependencyCount(t, db, "task_dependencies WHERE satisfied_at IS NOT NULL", 1)
	assertDependencyCount(t, db, "messages WHERE type='DEPENDENCY'", 1)

	if _, err := svc.TransitionAndReconcile(ctx, "transition-verified", testsupport.Project, "a", "source", core.TaskVerifiedDone, []byte("private verification evidence")); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Reconcile(ctx, "reconcile-repeat", testsupport.Project, "a"); err != nil {
		t.Fatal(err)
	}
	assertDependencyState(t, db, "c", core.TaskInProgress)
	assertDependencyCount(t, db, "task_dependencies WHERE satisfied_at IS NOT NULL", 2)
	assertDependencyCount(t, db, "messages WHERE type='DEPENDENCY'", 2)
	assertDependencyPrivateAbsent(t, db, "private verification evidence")

	_, err = svc.TransitionAndReconcile(ctx, "transition-cross-project", "foreign", "a", "source", core.TaskVerifiedDone, []byte("private failed evidence"))
	if err == nil {
		t.Fatal("cross-project transition succeeded")
	}
	afterReceipts, afterAudit := dependencyReceiptCounts(t, db)
	if afterReceipts != beforeReceipts+4 || afterAudit != beforeAudit+4 {
		t.Fatalf("failed transition left successful receipt/audit effects: receipts %d→%d audit %d→%d", beforeReceipts, afterReceipts, beforeAudit, afterAudit)
	}
	assertDependencyCount(t, db, "task_dependencies WHERE satisfied_at IS NOT NULL", 2)
	assertDependencyCount(t, db, "messages WHERE type='DEPENDENCY'", 2)
}

func dependencyReceiptCounts(t *testing.T, db *sql.DB) (int, int) {
	t.Helper()
	var receipts, audit int
	if err := db.QueryRow("SELECT COUNT(*) FROM command_receipts").Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM audit_events").Scan(&audit); err != nil {
		t.Fatal(err)
	}
	return receipts, audit
}

func assertDependencyState(t *testing.T, db *sql.DB, id string, want core.TaskState) {
	t.Helper()
	var got core.TaskState
	if err := db.QueryRow("SELECT state FROM tasks WHERE id=?", id).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("task %s state=%s; want %s", id, got, want)
	}
}

func assertDependencyCount(t *testing.T, db *sql.DB, from string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + from).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("%s count=%d; want %d", from, got, want)
	}
}

func assertDependencyPrivateAbsent(t *testing.T, db *sql.DB, private ...string) {
	t.Helper()
	for table, column := range map[string]string{"command_receipts": "result_json", "audit_events": "payload_json"} {
		rows, err := db.Query("SELECT " + column + " FROM " + table)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var value []byte
			if err := rows.Scan(&value); err != nil {
				t.Fatal(err)
			}
			for _, secret := range private {
				if strings.Contains(string(value), secret) {
					t.Fatalf("%s leaked %q", table, secret)
				}
			}
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
