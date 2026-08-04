// Package dependency coordinates immutable task dependency facts.
package dependency

import (
	"context"
	"errors"
	"time"

	"github.com/jeremy-merchant/oh-my-group/internal/domain"
	coord "github.com/jeremy-merchant/oh-my-group/internal/domain/coordination"
	"github.com/jeremy-merchant/oh-my-group/internal/domain/lineage"
	"github.com/jeremy-merchant/oh-my-group/internal/ports"
	"github.com/jeremy-merchant/oh-my-group/internal/safety"
)

type Service struct {
	store ports.Store
	now   func() time.Time
}
type dependencySummary struct {
	DependencyID string `json:"dependency_id"`
}

func New(store ports.Store, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{store: store, now: now}
}
func invalid() error {
	return domain.NewError(domain.CodeInvalidArgument, "invalid dependency request", false)
}
func conflict() error {
	return domain.NewError(domain.CodeConflict, "dependency graph conflict", false)
}
func missing() error {
	return domain.NewError(domain.CodeNotFound, "coordination record not found", false)
}
func mapErr(e error) error {
	if e == nil {
		return nil
	}
	var d domain.DomainError
	if errors.As(e, &d) {
		return d
	}
	return domain.NewError(domain.CodeUnavailable, "coordination store unavailable", true)
}
func (s *Service) Add(ctx context.Context, key domain.IdempotencyKey, d coord.Dependency) (coord.Dependency, error) {
	if !domain.IsSecretFreeStableMetadata(string(key)) || safety.RejectPrefixed(key, d) != nil {
		return d, invalid()
	}
	if d.Validate() != nil {
		return d, invalid()
	}
	_, result, e := s.store.Write(ctx, key, "dependency.add", func(r ports.Repositories) (domain.Result, error) {
		pre, ok, e := r.Coordination().GetTask(ctx, lineage.ID(d.PrerequisiteTaskID))
		if e != nil {
			return domain.Result{}, e
		}
		if !ok {
			return domain.Result{}, missing()
		}
		dep, ok, e := r.Coordination().GetTask(ctx, lineage.ID(d.DependentTaskID))
		if e != nil {
			return domain.Result{}, e
		}
		if !ok {
			return domain.Result{}, missing()
		}
		if pre.ProjectID != dep.ProjectID {
			return domain.Result{}, invalid()
		}
		edges, e := r.Coordination().ListDependencies(ctx, string(pre.ProjectID))
		if e != nil {
			return domain.Result{}, e
		}
		if _, e = coord.AddDependency(edges, d); e != nil {
			return domain.Result{}, conflict()
		}
		now := s.now().UTC()
		if e = r.Coordination().CreateDependency(ctx, d, now); e != nil {
			return domain.Result{}, e
		}
		fact := coord.DecideSatisfaction(d, pre.State)
		if fact.Satisfied {
			msg := coord.MailMessage{ID: fact.NotificationKey, Type: coord.MessageDependency, ThreadID: "dependency:" + d.ID, SenderSessionID: string(pre.CreatedBySessionID), Recipients: []coord.RecipientTarget{{TaskID: d.DependentTaskID}}, Subject: "dependency satisfied", Body: "dependency satisfied", RelatedTaskID: d.DependentTaskID, CreatedAt: now}
			if msg.Validate() != nil {
				return domain.Result{}, invalid()
			}
			won, markErr := r.Coordination().MarkDependencySatisfied(ctx, d.ID, now, nil, msg.ID)
			if markErr != nil {
				return domain.Result{}, markErr
			}
			if won {
				if e = r.Coordination().CreateMessage(ctx, string(pre.ProjectID), msg); e != nil {
					return domain.Result{}, e
				}
			}
		} else if d.Kind == coord.DependencyHard {
			switch dep.State {
			case lineage.TaskReady, lineage.TaskClaimed, lineage.TaskInProgress, lineage.TaskWaiting, lineage.TaskRework, lineage.TaskInterrupted, lineage.TaskStale:
				if _, e = r.Coordination().TransitionTask(ctx, dep.ID, lineage.TaskBlocked, nil, now); e != nil {
					return domain.Result{}, e
				}
			case lineage.TaskBlocked:
				// Already blocked by another active prerequisite.
			default:
				return domain.Result{}, conflict()
			}
		}
		return domain.Result{ID: domain.ResultID(d.ID), Outcome: domain.OutcomeOK, Data: dependencySummary{DependencyID: d.ID}}, nil
	})
	if e != nil {
		return d, mapErr(e)
	}
	var out coord.Dependency
	e = s.store.Read(ctx, func(r ports.Repositories) error {
		var ok bool
		out, ok, e = r.Coordination().GetDependency(ctx, string(result.ID))
		if e != nil {
			return e
		}
		if !ok {
			return missing()
		}
		return nil
	})
	return out, mapErr(e)
}
func (s *Service) List(ctx context.Context, project string) ([]coord.Dependency, error) {
	var out []coord.Dependency
	e := s.store.Read(ctx, func(r ports.Repositories) error {
		var e error
		out, e = r.Coordination().ListDependencies(ctx, project)
		return e
	})
	return out, mapErr(e)
}

// Reconcile records each satisfied edge once and atomically emits its one dependency message.
func (s *Service) Reconcile(ctx context.Context, key domain.IdempotencyKey, project, prerequisite string) (lineage.Task, error) {
	var out lineage.Task
	if !domain.IsSecretFreeStableMetadata(string(key)) || safety.RejectPrefixed(key, project, prerequisite) != nil {
		return out, invalid()
	}
	_, result, e := s.store.Write(ctx, key, "dependency.reconcile", func(r ports.Repositories) (domain.Result, error) {
		pre, ok, e := r.Coordination().GetTask(ctx, lineage.ID(prerequisite))
		if e != nil {
			return domain.Result{}, e
		}
		if !ok || string(pre.ProjectID) != project {
			return domain.Result{}, missing()
		}
		edges, e := r.Coordination().ListDependencies(ctx, project)
		if e != nil {
			return domain.Result{}, e
		}
		for _, d := range edges {
			if d.PrerequisiteTaskID != prerequisite {
				continue
			}
			f := coord.DecideSatisfaction(d, pre.State)
			if !f.Satisfied {
				continue
			}
			msg := coord.MailMessage{ID: f.NotificationKey, Type: coord.MessageDependency, ThreadID: "dependency:" + d.ID, SenderSessionID: string(pre.CreatedBySessionID), Recipients: []coord.RecipientTarget{{TaskID: d.DependentTaskID}}, Subject: "dependency satisfied", Body: "dependency satisfied", RelatedTaskID: d.DependentTaskID, CreatedAt: s.now().UTC()}
			if msg.Validate() != nil {
				return domain.Result{}, invalid()
			}
			won, e := r.Coordination().MarkDependencySatisfied(ctx, d.ID, msg.CreatedAt, nil, msg.ID)
			if e != nil {
				return domain.Result{}, e
			}
			if won {
				if e = r.Coordination().CreateMessage(ctx, project, msg); e != nil {
					return domain.Result{}, e
				}
				ready, e := r.Coordination().HardDependenciesSatisfied(ctx, d.DependentTaskID)
				if e != nil {
					return domain.Result{}, e
				}
				dependent, ok, e := r.Coordination().GetTask(ctx, lineage.ID(d.DependentTaskID))
				if e != nil {
					return domain.Result{}, e
				}
				if !ok {
					return domain.Result{}, missing()
				}
				if ready && (dependent.State == lineage.TaskBlocked || dependent.State == lineage.TaskWaiting) {
					out, e = r.Coordination().TransitionTask(ctx, dependent.ID, resumeState(dependent), nil, msg.CreatedAt)
					if e != nil {
						return domain.Result{}, e
					}
				}
			}
		}
		if out.ID == "" {
			out = pre
		}
		return domain.Result{ID: domain.ResultID(out.ID), Outcome: domain.OutcomeOK}, nil
	})
	if e != nil {
		return out, mapErr(e)
	}
	return s.task(ctx, lineage.ID(result.ID))
}

// TransitionAndReconcile atomically records the prerequisite transition and all resulting dependency facts.
func (s *Service) TransitionAndReconcile(ctx context.Context, key domain.IdempotencyKey, project, task, actorSession string, to lineage.TaskState, evidence []byte) (lineage.Task, error) {
	var out lineage.Task
	if !domain.IsSecretFreeStableMetadata(string(key)) || safety.RejectPrefixed(key, project, task, actorSession, evidence) != nil {
		return out, invalid()
	}
	_, result, e := s.store.Write(ctx, key, "task.transition", func(r ports.Repositories) (domain.Result, error) {
		var err error
		out, err = TransitionAndReconcileRepositories(ctx, r, s.now().UTC(), project, task, actorSession, to, evidence)
		return domain.Result{ID: domain.ResultID(out.ID), Outcome: domain.OutcomeOK}, err
	})
	if e != nil {
		return out, mapErr(e)
	}
	return s.task(ctx, lineage.ID(result.ID))
}

// TransitionAndReconcileRepositories performs the task transition inside an
// existing store transaction. It is shared by compound lifecycle operations so
// dependency notifications cannot be committed separately from completion.
// TransitionAndReconcileInTransaction applies a task transition and dependency
// reconciliation inside an existing store transaction. Compound lifecycle
// commands use it so task completion, reservation release, and archival commit
// together or not at all.
func TransitionAndReconcileInTransaction(ctx context.Context, r ports.Repositories, now time.Time, project, task, actorSession string, to lineage.TaskState, evidence []byte) (lineage.Task, error) {
	return TransitionAndReconcileRepositories(ctx, r, now, project, task, actorSession, to, evidence)
}

func TransitionAndReconcileRepositories(ctx context.Context, r ports.Repositories, now time.Time, project, task, actorSession string, to lineage.TaskState, evidence []byte) (lineage.Task, error) {
	if actorSession != "" {
		actor, ok, err := r.Coordination().GetSession(ctx, lineage.ID(actorSession))
		if err != nil {
			return lineage.Task{}, err
		}
		if !ok || string(actor.ProjectID) != project {
			return lineage.Task{}, missing()
		}
		if (to == lineage.TaskWorkComplete || to == lineage.TaskVerifiedDone) && (actor.Liveness == lineage.Stale || actor.Liveness == lineage.Interrupted) {
			return lineage.Task{}, domain.NewError(domain.CodeConflict, "completion actor is not live", false)
		}
	}
	pre, ok, err := r.Coordination().GetTask(ctx, lineage.ID(task))
	if err != nil {
		return lineage.Task{}, err
	}
	if !ok || string(pre.ProjectID) != project {
		return lineage.Task{}, missing()
	}
	if !lineage.CanTransitionTask(pre.State, to, evidence) {
		return lineage.Task{}, domain.NewError(domain.CodeConflict, "invalid task transition", false)
	}
	if to == lineage.TaskVerifiedDone {
		if err := requiredChildrenVerified(ctx, r.Coordination(), project, pre); err != nil {
			return lineage.Task{}, err
		}
	}
	pre, err = r.Coordination().TransitionTask(ctx, pre.ID, to, evidence, now)
	if err != nil {
		return lineage.Task{}, err
	}
	edges, err := r.Coordination().ListDependencies(ctx, project)
	if err != nil {
		return lineage.Task{}, err
	}
	for _, dependency := range edges {
		if dependency.PrerequisiteTaskID != task {
			continue
		}
		fact := coord.DecideSatisfaction(dependency, pre.State)
		if !fact.Satisfied {
			continue
		}
		sender := actorSession
		if sender == "" {
			sender = string(pre.CreatedBySessionID)
		}
		message := coord.MailMessage{ID: fact.NotificationKey, Type: coord.MessageDependency, ThreadID: "dependency:" + dependency.ID, SenderSessionID: sender, Recipients: []coord.RecipientTarget{{TaskID: dependency.DependentTaskID}}, Subject: "dependency satisfied", Body: "dependency satisfied", RelatedTaskID: dependency.DependentTaskID, CreatedAt: now}
		won, err := r.Coordination().MarkDependencySatisfied(ctx, dependency.ID, now, nil, message.ID)
		if err != nil {
			return lineage.Task{}, err
		}
		if !won {
			continue
		}
		if err = r.Coordination().CreateMessage(ctx, project, message); err != nil {
			return lineage.Task{}, err
		}
		ready, err := r.Coordination().HardDependenciesSatisfied(ctx, dependency.DependentTaskID)
		if err != nil {
			return lineage.Task{}, err
		}
		dependent, ok, err := r.Coordination().GetTask(ctx, lineage.ID(dependency.DependentTaskID))
		if err != nil {
			return lineage.Task{}, err
		}
		if !ok {
			return lineage.Task{}, missing()
		}
		if ready && (dependent.State == lineage.TaskBlocked || dependent.State == lineage.TaskWaiting) {
			if _, err = r.Coordination().TransitionTask(ctx, dependent.ID, resumeState(dependent), nil, now); err != nil {
				return lineage.Task{}, err
			}
		}
	}
	return pre, nil
}

func requiredChildrenVerified(ctx context.Context, repository ports.CoordinationRepository, project string, task lineage.Task) error {
	if lineage.EffectiveTaskCompletionPolicy(task.CompletionPolicy) != lineage.TaskCompletionAllRequiredChildrenVerified {
		return nil
	}
	tasks, err := repository.ListTasks(ctx, domain.ProjectID(project))
	if err != nil {
		return err
	}
	for _, child := range tasks {
		if child.ParentTaskID != task.ID || lineage.EffectiveTaskParentRequirement(child.ParentRequirement) != lineage.TaskParentRequired {
			continue
		}
		if child.State != lineage.TaskVerifiedDone {
			return domain.NewError(domain.CodeConflict, "required child tasks are not verified", false)
		}
	}
	return nil
}

func resumeState(task lineage.Task) lineage.TaskState {
	if task.ClaimedBySessionID == "" {
		return lineage.TaskReady
	}
	return lineage.TaskInProgress
}

func (s *Service) task(ctx context.Context, id lineage.ID) (lineage.Task, error) {
	var out lineage.Task
	err := s.store.Read(ctx, func(r ports.Repositories) error {
		var ok bool
		var e error
		out, ok, e = r.Coordination().GetTask(ctx, id)
		if e != nil {
			return e
		}
		if !ok {
			return missing()
		}
		return nil
	})
	return out, mapErr(err)
}
