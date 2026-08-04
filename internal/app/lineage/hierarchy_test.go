package lineage

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

func TestCreateTaskAppliesCompatibleHierarchyDefaults(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 31, 7, 0, 0, 0, time.UTC)
	store, _ := testsupport.Store(t, now)
	testsupport.Seed(t, store, now)
	service := New(store, func() time.Time { return now })

	root, err := service.CreateTask(ctx, "hierarchy-root", core.Task{ProjectID: testsupport.Project, Title: "root", CreatedBySessionID: "source"})
	if err != nil {
		t.Fatal(err)
	}
	if root.CompletionPolicy != core.TaskCompletionIndependent || root.ParentRequirement != core.TaskParentOptional {
		t.Fatalf("root policies = %q/%q", root.CompletionPolicy, root.ParentRequirement)
	}
	child, err := service.CreateTask(ctx, "hierarchy-child", core.Task{ProjectID: testsupport.Project, Title: "child", CreatedBySessionID: "source", ParentTaskID: root.ID})
	if err != nil {
		t.Fatal(err)
	}
	if child.ParentTaskID != root.ID || child.CompletionPolicy != core.TaskCompletionIndependent || child.ParentRequirement != core.TaskParentRequired {
		t.Fatalf("child = %#v", child)
	}
}

func TestCreateTaskRejectsMissingClosedAndCyclicParents(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 31, 7, 0, 0, 0, time.UTC)
	store, _ := testsupport.Store(t, now)
	testsupport.Seed(t, store, now)
	service := New(store, func() time.Time { return now })

	if _, err := service.CreateTask(ctx, "missing-parent", core.Task{ProjectID: testsupport.Project, Title: "missing", CreatedBySessionID: "source", ParentTaskID: "does-not-exist"}); !domainCode(err, domain.CodeNotFound) {
		t.Fatalf("missing parent error = %v", err)
	}
	if _, _, err := store.Write(ctx, "close-parent", "test.write", func(repositories ports.Repositories) (domain.Result, error) {
		_, err := repositories.Coordination().TransitionTask(ctx, "a", core.TaskWorkComplete, []byte("complete"), now)
		return domain.Result{ID: "a", Outcome: domain.OutcomeOK}, err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateTask(ctx, "closed-parent", core.Task{ProjectID: testsupport.Project, Title: "closed", CreatedBySessionID: "source", ParentTaskID: "a"}); !domainCode(err, domain.CodeConflict) {
		t.Fatalf("closed parent error = %v", err)
	}
	if _, err := service.CreateTask(ctx, "self-cycle", core.Task{ID: "self-cycle-task", ProjectID: testsupport.Project, Title: "cycle", CreatedBySessionID: "source", ParentTaskID: "self-cycle-task"}); !domainCode(err, domain.CodeConflict) {
		t.Fatalf("self cycle error = %v", err)
	}
}

func TestCreateTaskRejectsHierarchyDeeperThanLimit(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 31, 7, 0, 0, 0, time.UTC)
	store, _ := testsupport.Store(t, now)
	testsupport.Seed(t, store, now)
	service := New(store, func() time.Time { return now })
	if _, _, err := store.Write(ctx, "seed-deep-hierarchy", "test.write", func(repositories ports.Repositories) (domain.Result, error) {
		parent := core.ID("")
		for i := 1; i <= maxTaskHierarchyDepth; i++ {
			id := core.ID("depth-" + twoDigits(i))
			if _, err := repositories.Coordination().CreateTask(ctx, core.Task{ID: id, ProjectID: testsupport.Project, DisplayNumber: int64(i), Title: string(id), State: core.TaskReady, ParentTaskID: parent, CreatedAt: now, UpdatedAt: now}); err != nil {
				return domain.Result{}, err
			}
			parent = id
		}
		return domain.Result{ID: "depth-seed", Outcome: domain.OutcomeOK}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateTask(ctx, "too-deep", core.Task{ProjectID: testsupport.Project, Title: "too deep", ParentTaskID: "depth-16"}); !domainCode(err, domain.CodeConflict) {
		t.Fatalf("depth error = %v", err)
	}
}

func domainCode(err error, code domain.ErrorCode) bool {
	var value domain.DomainError
	return errors.As(err, &value) && value.Code == code && !value.Retryable
}

func twoDigits(value int) string {
	if value < 10 {
		return "0" + string(rune('0'+value))
	}
	return string([]byte{byte('0' + value/10), byte('0' + value%10)})
}
