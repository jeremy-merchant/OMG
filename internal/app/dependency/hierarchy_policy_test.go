package dependency

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jeremy-merchant/oh-my-group/internal/app/testsupport"
	"github.com/jeremy-merchant/oh-my-group/internal/domain"
	core "github.com/jeremy-merchant/oh-my-group/internal/domain/lineage"
	"github.com/jeremy-merchant/oh-my-group/internal/ports"
)

func TestVerifiedDoneRequiresOnlyDirectRequiredChildrenForGatedParent(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 31, 8, 0, 0, 0, time.UTC)
	store, _ := testsupport.Store(t, now)
	testsupport.Seed(t, store, now)
	if _, _, err := store.Write(ctx, "seed-gated-hierarchy", "test.write", func(repositories ports.Repositories) (domain.Result, error) {
		for _, task := range []core.Task{
			{ID: "gated-parent", ProjectID: testsupport.Project, Title: "parent", State: core.TaskWorkComplete, CreatedBySessionID: "source", CompletionPolicy: core.TaskCompletionAllRequiredChildrenVerified, ParentRequirement: core.TaskParentOptional, CreatedAt: now, UpdatedAt: now},
			{ID: "required-child", ProjectID: testsupport.Project, Title: "required", State: core.TaskWorkComplete, CreatedBySessionID: "source", ParentTaskID: "gated-parent", ParentRequirement: core.TaskParentRequired, CreatedAt: now, UpdatedAt: now},
			{ID: "optional-child", ProjectID: testsupport.Project, Title: "optional", State: core.TaskWorkComplete, CreatedBySessionID: "source", ParentTaskID: "gated-parent", ParentRequirement: core.TaskParentOptional, CreatedAt: now, UpdatedAt: now},
		} {
			if _, err := repositories.Coordination().CreateTask(ctx, task); err != nil {
				return domain.Result{}, err
			}
		}
		return domain.Result{ID: "gated-parent", Outcome: domain.OutcomeOK}, nil
	}); err != nil {
		t.Fatal(err)
	}

	if _, _, err := store.Write(ctx, "verify-parent-too-early", "test.write", func(repositories ports.Repositories) (domain.Result, error) {
		_, err := TransitionAndReconcileRepositories(ctx, repositories, now, testsupport.Project, "gated-parent", "source", core.TaskVerifiedDone, []byte("parent evidence"))
		return domain.Result{}, err
	}); !hasDomainCode(err, domain.CodeConflict) {
		t.Fatalf("early parent verification error = %v", err)
	}
	assertTaskState(t, store, "gated-parent", core.TaskWorkComplete)

	if _, _, err := store.Write(ctx, "verify-required-child", "test.write", func(repositories ports.Repositories) (domain.Result, error) {
		child, err := TransitionAndReconcileRepositories(ctx, repositories, now, testsupport.Project, "required-child", "source", core.TaskVerifiedDone, []byte("child evidence"))
		return domain.Result{ID: domain.ResultID(child.ID), Outcome: domain.OutcomeOK}, err
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Write(ctx, "verify-gated-parent", "test.write", func(repositories ports.Repositories) (domain.Result, error) {
		parent, err := TransitionAndReconcileRepositories(ctx, repositories, now, testsupport.Project, "gated-parent", "source", core.TaskVerifiedDone, []byte("parent evidence"))
		return domain.Result{ID: domain.ResultID(parent.ID), Outcome: domain.OutcomeOK}, err
	}); err != nil {
		t.Fatal(err)
	}
	assertTaskState(t, store, "gated-parent", core.TaskVerifiedDone)
	assertTaskState(t, store, "optional-child", core.TaskWorkComplete)
}

func TestIndependentLegacyParentIsNotRetroactivelyChildGated(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 31, 8, 0, 0, 0, time.UTC)
	store, _ := testsupport.Store(t, now)
	testsupport.Seed(t, store, now)
	if _, _, err := store.Write(ctx, "seed-independent-hierarchy", "test.write", func(repositories ports.Repositories) (domain.Result, error) {
		for _, task := range []core.Task{
			{ID: "independent-parent", ProjectID: testsupport.Project, Title: "legacy parent", State: core.TaskWorkComplete, CreatedBySessionID: "source", CreatedAt: now, UpdatedAt: now},
			{ID: "legacy-required-child", ProjectID: testsupport.Project, Title: "legacy child", State: core.TaskWorkComplete, CreatedBySessionID: "source", ParentTaskID: "independent-parent", ParentRequirement: core.TaskParentRequired, CreatedAt: now, UpdatedAt: now},
		} {
			if _, err := repositories.Coordination().CreateTask(ctx, task); err != nil {
				return domain.Result{}, err
			}
		}
		return domain.Result{ID: "independent-parent", Outcome: domain.OutcomeOK}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Write(ctx, "verify-independent-parent", "test.write", func(repositories ports.Repositories) (domain.Result, error) {
		parent, err := TransitionAndReconcileRepositories(ctx, repositories, now, testsupport.Project, "independent-parent", "source", core.TaskVerifiedDone, []byte("legacy parent evidence"))
		return domain.Result{ID: domain.ResultID(parent.ID), Outcome: domain.OutcomeOK}, err
	}); err != nil {
		t.Fatal(err)
	}
	assertTaskState(t, store, "independent-parent", core.TaskVerifiedDone)
	assertTaskState(t, store, "legacy-required-child", core.TaskWorkComplete)
}

func assertTaskState(t *testing.T, store ports.Store, taskID string, want core.TaskState) {
	t.Helper()
	var got core.TaskState
	if err := store.Read(context.Background(), func(repositories ports.Repositories) error {
		task, found, err := repositories.Coordination().GetTask(context.Background(), core.ID(taskID))
		if err != nil {
			return err
		}
		if !found {
			t.Fatalf("task %s missing", taskID)
		}
		got = task.State
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("task %s state = %s; want %s", taskID, got, want)
	}
}

func hasDomainCode(err error, code domain.ErrorCode) bool {
	var value domain.DomainError
	return errors.As(err, &value) && value.Code == code && !value.Retryable
}
