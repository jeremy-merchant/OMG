package dependency

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"example.invalid/coordledger/internal/app/testsupport"
	"example.invalid/coordledger/internal/domain"
	coord "example.invalid/coordledger/internal/domain/coordination"
	core "example.invalid/coordledger/internal/domain/lineage"
	"example.invalid/coordledger/internal/ports"
)

func TestDependencyCyclesCriteriaUnblockingAndExactlyOnceNotifications(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	s, db := testsupport.Store(t, now)
	testsupport.Seed(t, s, now)
	svc := New(s, func() time.Time { return now })
	for _, d := range []coord.Dependency{{ID: "ab-work", PrerequisiteTaskID: "a", DependentTaskID: "b", Kind: coord.DependencyHard, Criterion: coord.UnblockWorkComplete}, {ID: "ac-verified", PrerequisiteTaskID: "a", DependentTaskID: "c", Kind: coord.DependencyHard, Criterion: coord.UnblockVerifiedDone}, {ID: "cb-work", PrerequisiteTaskID: "c", DependentTaskID: "b", Kind: coord.DependencyHard, Criterion: coord.UnblockWorkComplete}} {
		if _, err := svc.Add(ctx, domain.IdempotencyKey(d.ID), d); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.Add(ctx, "cycle", coord.Dependency{ID: "ba", PrerequisiteTaskID: "b", DependentTaskID: "a", Kind: coord.DependencyHard, Criterion: coord.UnblockWorkComplete}); err == nil {
		t.Fatal("cycle accepted")
	}
	_, _, err := s.Write(ctx, "block-b", "test.write", func(r ports.Repositories) (domain.Result, error) {
		_, e := r.Coordination().TransitionTask(ctx, "b", core.TaskBlocked, nil, now)
		return domain.Result{ID: "b", Outcome: domain.OutcomeOK}, e
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TransitionAndReconcile(ctx, "a-work", testsupport.Project, "a", "", core.TaskWorkComplete, nil); err != nil {
		t.Fatalf("completion without actor: %v", err)
	}
	var state string
	if err := db.QueryRow("SELECT state FROM tasks WHERE id='b'").Scan(&state); err != nil || state != "BLOCKED" {
		t.Fatalf("b state=%s err=%v", state, err)
	}
	if _, err := svc.Reconcile(ctx, "a-repeat", testsupport.Project, "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TransitionAndReconcile(ctx, "a-verified", testsupport.Project, "a", "source", core.TaskVerifiedDone, []byte("evidence")); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TransitionAndReconcile(ctx, "c-work", testsupport.Project, "c", "source", core.TaskWorkComplete, nil); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT state FROM tasks WHERE id='b'").Scan(&state); err != nil || state != "IN_PROGRESS" {
		t.Fatalf("b did not unblock: %s %v", state, err)
	}
	var messages int
	if err := db.QueryRow("SELECT COUNT(*) FROM messages WHERE type='DEPENDENCY'").Scan(&messages); err != nil || messages != 3 {
		t.Fatalf("dependency messages=%d err=%v", messages, err)
	}
}

func TestDependencyRejectsDelegationTokensBeforeAnyPersistence(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	raw := "omgdt_v1_" + strings.Repeat("a", 43)
	for _, token := range []string{raw, raw + "-recoverable"} {
		t.Run("add/"+token[len(raw):], func(t *testing.T) {
			store, db := testsupport.Store(t, now)
			testsupport.Seed(t, store, now)
			svc := New(store, func() time.Time { return now })
			beforeReceipts, beforeAudit := dependencyReceiptCounts(t, db)
			for _, test := range []struct {
				name string
				key  domain.IdempotencyKey
				dep  coord.Dependency
			}{
				{name: "key", key: domain.IdempotencyKey(token), dep: dependencyForTokenTest()},
				{name: "id", key: "add-id", dep: coord.Dependency{ID: token, PrerequisiteTaskID: "a", DependentTaskID: "b", Kind: coord.DependencyHard, Criterion: coord.UnblockWorkComplete}},
				{name: "prerequisite", key: "add-prerequisite", dep: coord.Dependency{ID: "ab", PrerequisiteTaskID: token, DependentTaskID: "b", Kind: coord.DependencyHard, Criterion: coord.UnblockWorkComplete}},
				{name: "dependent", key: "add-dependent", dep: coord.Dependency{ID: "ab", PrerequisiteTaskID: "a", DependentTaskID: token, Kind: coord.DependencyHard, Criterion: coord.UnblockWorkComplete}},
			} {
				t.Run(test.name, func(t *testing.T) {
					if _, err := svc.Add(ctx, test.key, test.dep); err == nil {
						t.Fatal("dependency request containing a delegation token was accepted")
					} else if strings.Contains(err.Error(), token) {
						t.Fatal("delegation token was surfaced in the add error")
					}
					assertDependencyCount(t, db, "task_dependencies", 0)
					receipts, audit := dependencyReceiptCounts(t, db)
					if receipts != beforeReceipts || audit != beforeAudit {
						t.Fatalf("rejected add mutated receipts/audit: got %d/%d, want %d/%d", receipts, audit, beforeReceipts, beforeAudit)
					}
					assertDependencyPrivateAbsent(t, db, token)
				})
			}
		})
		t.Run("transition/"+token[len(raw):], func(t *testing.T) {
			store, db := testsupport.Store(t, now)
			testsupport.Seed(t, store, now)
			svc := New(store, func() time.Time { return now })
			beforeReceipts, beforeAudit := dependencyReceiptCounts(t, db)
			for _, test := range []struct {
				name     string
				key      domain.IdempotencyKey
				project  string
				task     string
				actor    string
				evidence []byte
			}{
				{name: "key", key: domain.IdempotencyKey(token), project: testsupport.Project, task: "a"},
				{name: "project", key: "transition-project", project: token, task: "a"},
				{name: "task", key: "transition-task", project: testsupport.Project, task: token},
				{name: "actor", key: "transition-actor", project: testsupport.Project, task: "a", actor: token},
				{name: "evidence", key: "transition-evidence", project: testsupport.Project, task: "a", evidence: []byte("evidence " + token)},
			} {
				t.Run(test.name, func(t *testing.T) {
					if _, err := svc.TransitionAndReconcile(ctx, test.key, test.project, test.task, test.actor, core.TaskWorkComplete, test.evidence); err == nil {
						t.Fatal("transition request containing a delegation token was accepted")
					} else if strings.Contains(err.Error(), token) {
						t.Fatal("delegation token was surfaced in the transition error")
					}
					assertDependencyState(t, db, "a", core.TaskClaimed)
					assertDependencyCount(t, db, "messages", 0)
					receipts, audit := dependencyReceiptCounts(t, db)
					if receipts != beforeReceipts || audit != beforeAudit {
						t.Fatalf("rejected transition mutated receipts/audit: got %d/%d, want %d/%d", receipts, audit, beforeReceipts, beforeAudit)
					}
					assertDependencyPrivateAbsent(t, db, token)
				})
			}
		})
	}
}

func TestDependencyRejectsSecretBearingStableMetadataBeforePersistence(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	store, db := testsupport.Store(t, now)
	testsupport.Seed(t, store, now)
	svc := New(store, func() time.Time { return now })
	beforeReceipts, beforeAudit := dependencyReceiptCounts(t, db)
	for _, test := range []struct {
		name string
		key  domain.IdempotencyKey
		dep  coord.Dependency
	}{
		{name: "key", key: "password=release-secret", dep: dependencyForTokenTest()},
		{name: "dependency id", key: "dependency-id", dep: coord.Dependency{ID: "password=release-secret", PrerequisiteTaskID: "a", DependentTaskID: "b", Kind: coord.DependencyHard, Criterion: coord.UnblockWorkComplete}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := svc.Add(ctx, test.key, test.dep); err == nil {
				t.Fatal("secret-bearing stable metadata accepted")
			}
			assertDependencyCount(t, db, "task_dependencies", 0)
			receipts, audit := dependencyReceiptCounts(t, db)
			if receipts != beforeReceipts || audit != beforeAudit {
				t.Fatalf("rejected dependency mutated receipts/audit: got %d/%d, want %d/%d", receipts, audit, beforeReceipts, beforeAudit)
			}
		})
	}
}

func TestTransitionAndReconcileRejectsStaleOrInterruptedCompletionActorWithoutMutation(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	for _, liveness := range []core.Liveness{core.Stale, core.Interrupted} {
		t.Run(string(liveness), func(t *testing.T) {
			store, db := testsupport.Store(t, now)
			testsupport.Seed(t, store, now)
			if _, _, err := store.Write(ctx, domain.IdempotencyKey("actor-"+string(liveness)), "test.write", func(r ports.Repositories) (domain.Result, error) {
				err := r.Coordination().RecordHeartbeat(ctx, core.Heartbeat{ID: core.ID("heartbeat-" + string(liveness)), SessionID: "source", ObservedAt: now, Liveness: liveness, Detail: []byte("{}")})
				return domain.Result{ID: "source", Outcome: domain.OutcomeOK}, err
			}); err != nil {
				t.Fatal(err)
			}
			beforeReceipts, beforeAudit := dependencyReceiptCounts(t, db)
			service := New(store, func() time.Time { return now })
			if _, err := service.TransitionAndReconcile(ctx, domain.IdempotencyKey("reject-"+string(liveness)), testsupport.Project, "a", "source", core.TaskWorkComplete, nil); err == nil {
				t.Fatal("completion by non-live actor was accepted")
			}
			assertDependencyState(t, db, "a", core.TaskClaimed)
			assertDependencyCount(t, db, "messages", 0)
			receipts, audit := dependencyReceiptCounts(t, db)
			if receipts != beforeReceipts || audit != beforeAudit {
				t.Fatalf("rejected completion mutated receipts/audit: got %d/%d, want %d/%d", receipts, audit, beforeReceipts, beforeAudit)
			}
		})
	}
}

func TestHardDependencyBlocksReadyClaimAndReleasesAfterCriterion(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	s, _ := testsupport.Store(t, now)
	testsupport.Seed(t, s, now)
	_, _, err := s.Write(ctx, "seed-ready-dependency", "test.write", func(r ports.Repositories) (domain.Result, error) {
		c := r.Coordination()
		for _, task := range []core.Task{
			{ID: "ready-prerequisite", ProjectID: testsupport.Project, Title: "ready prerequisite", State: core.TaskReady, CreatedBySessionID: "source", CreatedAt: now, UpdatedAt: now},
			{ID: "ready-dependent", ProjectID: testsupport.Project, Title: "ready dependent", State: core.TaskReady, CreatedBySessionID: "source", CreatedAt: now, UpdatedAt: now},
		} {
			if _, createErr := c.CreateTask(ctx, task); createErr != nil {
				return domain.Result{}, createErr
			}
		}
		if _, won, claimErr := c.ClaimTask(ctx, "ready-prerequisite", "source", now); claimErr != nil || !won {
			return domain.Result{}, claimErr
		}
		return domain.Result{ID: "seed-ready-dependency", Outcome: domain.OutcomeOK}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	svc := New(s, func() time.Time { return now })
	dependency := coord.Dependency{ID: "ready-edge", PrerequisiteTaskID: "ready-prerequisite", DependentTaskID: "ready-dependent", Kind: coord.DependencyHard, Criterion: coord.UnblockWorkComplete}
	if _, err = svc.Add(ctx, "ready-edge-add", dependency); err != nil {
		t.Fatal(err)
	}
	dependent, err := dependencyTask(ctx, s, "ready-dependent")
	if err != nil || dependent.State != core.TaskBlocked {
		t.Fatalf("dependent state after add=%s err=%v", dependent.State, err)
	}
	_, _, err = s.Write(ctx, "blocked-claim", "test.write", func(r ports.Repositories) (domain.Result, error) {
		_, won, claimErr := r.Coordination().ClaimTask(ctx, "ready-dependent", "target", now)
		if claimErr != nil {
			return domain.Result{}, claimErr
		}
		if won {
			return domain.Result{}, errors.New("blocked dependent task was claimed")
		}
		return domain.Result{ID: "blocked-claim", Outcome: domain.OutcomeOK}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.TransitionAndReconcile(ctx, "ready-prerequisite-complete", testsupport.Project, "ready-prerequisite", "source", core.TaskWorkComplete, nil); err != nil {
		t.Fatal(err)
	}
	dependent, err = dependencyTask(ctx, s, "ready-dependent")
	if err != nil || dependent.State != core.TaskReady {
		t.Fatalf("dependent state after release=%s err=%v", dependent.State, err)
	}
}

func dependencyTask(ctx context.Context, store ports.Store, id core.ID) (core.Task, error) {
	var task core.Task
	err := store.Read(ctx, func(r ports.Repositories) error {
		var ok bool
		var readErr error
		task, ok, readErr = r.Coordination().GetTask(ctx, id)
		if readErr != nil {
			return readErr
		}
		if !ok {
			return errors.New("task not found")
		}
		return nil
	})
	return task, err
}

func dependencyForTokenTest() coord.Dependency {
	return coord.Dependency{ID: "ab", PrerequisiteTaskID: "a", DependentTaskID: "b", Kind: coord.DependencyHard, Criterion: coord.UnblockWorkComplete}
}
