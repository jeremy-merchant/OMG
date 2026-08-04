// Package handoff coordinates immutable transfer, decision, and adoption facts.
package handoff

import (
	"context"
	"errors"
	"github.com/jeremy-merchant/oh-my-group/internal/domain"
	coord "github.com/jeremy-merchant/oh-my-group/internal/domain/coordination"
	"github.com/jeremy-merchant/oh-my-group/internal/domain/lineage"
	"github.com/jeremy-merchant/oh-my-group/internal/ports"
	"github.com/jeremy-merchant/oh-my-group/internal/safety"
	"time"
)

type Service struct {
	store ports.Store
	now   func() time.Time
}

func New(store ports.Store, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{store: store, now: now}
}
func invalid() error {
	return domain.NewError(domain.CodeInvalidArgument, "invalid handoff request", false)
}
func missing() error {
	return domain.NewError(domain.CodeNotFound, "coordination record not found", false)
}
func conflict() error {
	return domain.NewError(domain.CodeConflict, "handoff decision conflict", false)
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

type decisionOperation string

const (
	acceptOperation decisionOperation = "handoff.accept"
	rejectOperation decisionOperation = "handoff.reject"
)

func operationForDecision(decision string) (decisionOperation, bool) {
	switch coord.HandoffStatus(decision) {
	case coord.HandoffAccepted:
		return acceptOperation, true
	case coord.HandoffRejected:
		return rejectOperation, true
	default:
		return "", false
	}
}

type adoptionOperation string

const (
	handoffAdoptOperation adoptionOperation = "handoff.adopt"
	gitAdoptOperation     adoptionOperation = "git.adopt"
)

func operationForAdoption(a coord.Adoption) adoptionOperation {
	if a.GitAssetID != "" {
		return gitAdoptOperation
	}
	return handoffAdoptOperation
}
func (s *Service) Submit(ctx context.Context, key domain.IdempotencyKey, project string, h coord.Handoff) (coord.Handoff, error) {
	if !domain.IsSecretFreeStableMetadata(string(key)) || safety.RejectPrefixed(key, h, project) != nil {
		return coord.Handoff{}, invalid()
	}
	h.Status = coord.HandoffSubmitted
	h.CreatedAt = s.now().UTC()
	var out coord.Handoff
	_, result, e := s.store.Write(ctx, key, "handoff.create", func(r ports.Repositories) (domain.Result, error) {
		task, ok, e := r.Coordination().GetTask(ctx, lineage.ID(h.TaskID))
		if e != nil {
			return domain.Result{}, e
		}
		if !ok || string(task.ProjectID) != project {
			return domain.Result{}, missing()
		}
		run, ok, e := r.Coordination().GetRun(ctx, lineage.ID(h.RunID))
		if e != nil {
			return domain.Result{}, e
		}
		if !ok {
			return domain.Result{}, missing()
		}
		if run.TaskID != task.ID {
			return domain.Result{}, invalid()
		}
		source, ok, e := r.Coordination().GetSession(ctx, lineage.ID(h.SourceSessionID))
		if e != nil {
			return domain.Result{}, e
		} else if !ok {
			return domain.Result{}, missing()
		}
		if source.ProjectID != task.ProjectID || run.SessionID != source.ID {
			return domain.Result{}, invalid()
		}
		if h.TargetSessionID != "" {
			target, ok, e := r.Coordination().GetSession(ctx, lineage.ID(h.TargetSessionID))
			if e != nil {
				return domain.Result{}, e
			} else if !ok {
				return domain.Result{}, missing()
			}
			if target.ProjectID != task.ProjectID {
				return domain.Result{}, invalid()
			}
		}
		if h.TargetTaskID != "" {
			target, ok, e := r.Coordination().GetTask(ctx, lineage.ID(h.TargetTaskID))
			if e != nil {
				return domain.Result{}, e
			} else if !ok {
				return domain.Result{}, missing()
			}
			if target.ProjectID != task.ProjectID {
				return domain.Result{}, invalid()
			}
		}
		if h.Validate(run.State) != nil {
			return domain.Result{}, invalid()
		}
		e = r.Coordination().CreateHandoff(ctx, h)
		if e == nil && h.SourceCommit != "" {
			event := coord.HandoffLifecycleEvent{
				ID: "lifecycle-" + h.ID + "-submitted", HandoffID: h.ID,
				ActorSessionID: h.SourceSessionID, State: coord.IntegrationSubmitted,
				SourceCommit: h.SourceCommit, SourceTree: h.SourceTree, CreatedAt: h.CreatedAt,
			}
			if event.Validate() != nil {
				return domain.Result{}, invalid()
			}
			e = r.Coordination().CreateHandoffLifecycleEvent(ctx, event)
		}
		return domain.Result{ID: domain.ResultID(h.ID), Outcome: domain.OutcomeOK}, e
	})
	if e != nil {
		return out, mapErr(e)
	}
	return s.Get(ctx, string(result.ID))
}
func (s *Service) Get(ctx context.Context, id string) (coord.Handoff, error) {
	var out coord.Handoff
	e := s.store.Read(ctx, func(r ports.Repositories) error {
		var ok bool
		var e error
		out, ok, e = r.Coordination().GetHandoff(ctx, id)
		if e != nil {
			return e
		}
		if !ok {
			return missing()
		}
		return nil
	})
	if e != nil {
		return coord.Handoff{}, mapErr(e)
	}
	return out, nil
}
func (s *Service) History(ctx context.Context, task string) ([]coord.Handoff, error) {
	var out []coord.Handoff
	e := s.store.Read(ctx, func(r ports.Repositories) error {
		var e error
		out, e = r.Coordination().ListHandoffs(ctx, task)
		return e
	})
	if e != nil {
		return nil, mapErr(e)
	}
	return out, nil
}
func (s *Service) Supersede(ctx context.Context, key domain.IdempotencyKey, id, newID, summary string) (coord.Handoff, error) {
	var out coord.Handoff
	if !domain.IsSecretFreeStableMetadata(string(key)) || safety.RejectPrefixed(key, id, newID, summary) != nil {
		return out, invalid()
	}
	_, result, e := s.store.Write(ctx, key, "handoff.supersede", func(r ports.Repositories) (domain.Result, error) {
		old, ok, e := r.Coordination().GetHandoff(ctx, id)
		if e != nil {
			return domain.Result{}, e
		}
		if !ok {
			return domain.Result{}, missing()
		}
		out, e = coord.SupersedeHandoff(old, newID, summary, s.now().UTC())
		if e != nil {
			return domain.Result{}, invalid()
		}
		e = r.Coordination().CreateHandoff(ctx, out)
		if e == nil && out.SourceCommit != "" {
			event := coord.HandoffLifecycleEvent{ID: "lifecycle-" + out.ID + "-submitted", HandoffID: out.ID, ActorSessionID: out.SourceSessionID, State: coord.IntegrationSubmitted, SourceCommit: out.SourceCommit, SourceTree: out.SourceTree, CreatedAt: out.CreatedAt}
			e = r.Coordination().CreateHandoffLifecycleEvent(ctx, event)
		}
		return domain.Result{ID: domain.ResultID(out.ID), Outcome: domain.OutcomeOK}, e
	})
	if e != nil {
		return out, mapErr(e)
	}
	return s.Get(ctx, string(result.ID))
}
func (s *Service) Decide(ctx context.Context, key domain.IdempotencyKey, id, decision, decisionID, by string) (coord.HandoffDecision, error) {
	var out coord.HandoffDecision
	if !domain.IsSecretFreeStableMetadata(string(key)) || safety.RejectPrefixed(key, id, decision, decisionID, by) != nil {
		return coord.HandoffDecision{}, invalid()
	}
	operation, ok := operationForDecision(decision)
	if !ok {
		return coord.HandoffDecision{}, invalid()
	}
	_, result, e := s.store.Write(ctx, key, string(operation), func(r ports.Repositories) (domain.Result, error) {
		h, ok, e := r.Coordination().GetHandoff(ctx, id)
		if e != nil {
			return domain.Result{}, e
		}
		if !ok {
			return domain.Result{}, missing()
		}
		if _, ok, e = r.Coordination().GetSession(ctx, lineage.ID(by)); e != nil {
			return domain.Result{}, e
		} else if !ok {
			return domain.Result{}, missing()
		}
		if _, ok, e = r.Coordination().GetHandoffDecision(ctx, id); e != nil {
			return domain.Result{}, e
		} else if ok {
			return domain.Result{}, conflict()
		}
		out, e = coord.DecideHandoff(h, coord.HandoffStatus(decision), decisionID, by, s.now().UTC())
		if e != nil {
			return domain.Result{}, invalid()
		}
		e = r.Coordination().CreateHandoffDecision(ctx, out)
		if e == nil {
			events, listErr := r.Coordination().ListHandoffLifecycleEvents(ctx, h.ID)
			if listErr != nil {
				return domain.Result{}, listErr
			}
			state := coord.IntegrationAccepted
			note := ""
			if out.Decision == coord.HandoffRejected {
				state = coord.IntegrationRejected
				note = "handoff rejected"
			}
			if transitionErr := coord.ValidateIntegrationTransition(events, nil, state); transitionErr != nil {
				return domain.Result{}, invalid()
			}
			event := coord.HandoffLifecycleEvent{ID: "lifecycle-" + out.ID, HandoffID: h.ID, ActorSessionID: by, State: state, Note: note, CreatedAt: out.CreatedAt}
			if event.Validate() != nil {
				return domain.Result{}, invalid()
			}
			e = r.Coordination().CreateHandoffLifecycleEvent(ctx, event)
		}
		return domain.Result{ID: domain.ResultID(out.ID), Outcome: domain.OutcomeOK}, e
	})
	if e != nil {
		return out, mapErr(e)
	}
	e = s.store.Read(ctx, func(r ports.Repositories) error {
		var ok bool
		out, ok, e = r.Coordination().GetHandoffDecisionByID(ctx, string(result.ID))
		if e != nil {
			return e
		}
		if !ok {
			return missing()
		}
		return nil
	})
	if e != nil {
		return coord.HandoffDecision{}, mapErr(e)
	}
	return out, nil
}

func (s *Service) Advance(ctx context.Context, key domain.IdempotencyKey, event coord.HandoffLifecycleEvent) (coord.HandoffLifecycleEvent, error) {
	return s.advance(ctx, key, event, false)
}

// AdvanceLocalCanary preserves the global strict lifecycle graph while
// permitting one narrow transition for a Git-verified local rolling canary.
// Acceptance remains mandatory; only the missing INTEGRATED ledger event is
// tolerated by this entry point.
func (s *Service) AdvanceLocalCanary(ctx context.Context, key domain.IdempotencyKey, event coord.HandoffLifecycleEvent) (coord.HandoffLifecycleEvent, error) {
	if event.State != coord.IntegrationCanaryRunning {
		return coord.HandoffLifecycleEvent{}, invalid()
	}
	return s.advance(ctx, key, event, true)
}

func (s *Service) advance(ctx context.Context, key domain.IdempotencyKey, event coord.HandoffLifecycleEvent, allowLocalCanary bool) (coord.HandoffLifecycleEvent, error) {
	if !domain.IsSecretFreeStableMetadata(string(key)) || safety.RejectPrefixed(key, event) != nil {
		return coord.HandoffLifecycleEvent{}, invalid()
	}
	if event.State == coord.IntegrationAccepted || event.State == coord.IntegrationRejected {
		return coord.HandoffLifecycleEvent{}, invalid()
	}
	event.CreatedAt = s.now().UTC()
	if event.Validate() != nil {
		return coord.HandoffLifecycleEvent{}, invalid()
	}
	_, result, err := s.store.Write(ctx, key, "handoff.lifecycle", func(r ports.Repositories) (domain.Result, error) {
		handoff, ok, readErr := r.Coordination().GetHandoff(ctx, event.HandoffID)
		if readErr != nil {
			return domain.Result{}, readErr
		}
		if !ok {
			return domain.Result{}, missing()
		}
		if _, ok, readErr = r.Coordination().GetSession(ctx, lineage.ID(event.ActorSessionID)); readErr != nil {
			return domain.Result{}, readErr
		} else if !ok {
			return domain.Result{}, missing()
		}
		events, readErr := r.Coordination().ListHandoffLifecycleEvents(ctx, handoff.ID)
		if readErr != nil {
			return domain.Result{}, readErr
		}
		decision, hasDecision, readErr := r.Coordination().GetHandoffDecision(ctx, handoff.ID)
		if readErr != nil {
			return domain.Result{}, readErr
		}
		var decisionPtr *coord.HandoffDecision
		if hasDecision {
			decisionPtr = &decision
		}
		if coord.ValidateIntegrationTransition(events, decisionPtr, event.State) != nil && !validLocalCanaryTransition(allowLocalCanary, events, decisionPtr, event) {
			return domain.Result{}, invalid()
		}
		if createErr := r.Coordination().CreateHandoffLifecycleEvent(ctx, event); createErr != nil {
			return domain.Result{}, createErr
		}
		return domain.Result{ID: domain.ResultID(event.ID), Outcome: domain.OutcomeOK}, nil
	})
	if err != nil {
		return coord.HandoffLifecycleEvent{}, mapErr(err)
	}
	var out coord.HandoffLifecycleEvent
	err = s.store.Read(ctx, func(r ports.Repositories) error {
		var ok bool
		var readErr error
		out, ok, readErr = r.Coordination().GetHandoffLifecycleEventByID(ctx, string(result.ID))
		if readErr != nil {
			return readErr
		}
		if !ok {
			return missing()
		}
		return nil
	})
	if err != nil {
		return coord.HandoffLifecycleEvent{}, mapErr(err)
	}
	return out, nil
}

func validLocalCanaryTransition(allowed bool, events []coord.HandoffLifecycleEvent, decision *coord.HandoffDecision, event coord.HandoffLifecycleEvent) bool {
	return allowed && event.State == coord.IntegrationCanaryRunning && decision != nil && decision.Decision == coord.HandoffAccepted && coord.CurrentIntegrationState(events, decision) == coord.IntegrationAccepted
}

func (s *Service) Lifecycle(ctx context.Context, handoffID string) ([]coord.HandoffLifecycleEvent, error) {
	var events []coord.HandoffLifecycleEvent
	err := s.store.Read(ctx, func(r ports.Repositories) error {
		var readErr error
		events, readErr = r.Coordination().ListHandoffLifecycleEvents(ctx, handoffID)
		return readErr
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return events, nil
}
func (s *Service) Adopt(ctx context.Context, key domain.IdempotencyKey, a coord.Adoption) (coord.Adoption, error) {
	if !domain.IsSecretFreeStableMetadata(string(key)) || safety.RejectPrefixed(key, a) != nil {
		return a, invalid()
	}
	a.CreatedAt = s.now().UTC()
	if a.Validate() != nil || a.GrantsRestrictedAuthority() {
		return a, invalid()
	}
	operation := operationForAdoption(a)
	_, result, e := s.store.Write(ctx, key, string(operation), func(r ports.Repositories) (domain.Result, error) {
		owner, ok, e := r.Coordination().GetSession(ctx, lineage.ID(a.NewOwnerSessionID))
		if e != nil {
			return domain.Result{}, e
		}
		if !ok || string(owner.ProjectID) != a.ProjectID {
			return domain.Result{}, missing()
		}
		if a.GitAssetID != "" {
			if owner.TaskID == "" {
				return domain.Result{}, missing()
			}
			task, ok, e := r.Coordination().GetTask(ctx, owner.TaskID)
			if e != nil {
				return domain.Result{}, e
			}
			if !ok || string(task.ProjectID) != a.ProjectID {
				return domain.Result{}, missing()
			}
			runs, e := r.Coordination().ListRunsForSession(ctx, lineage.ID(a.ProjectID), owner.ID)
			if e != nil {
				return domain.Result{}, e
			}
			hasRun := false
			for _, run := range runs {
				if run.TaskID == task.ID && run.SessionID == owner.ID {
					hasRun = true
					break
				}
			}
			if !hasRun {
				return domain.Result{}, missing()
			}
		}
		if a.SessionID != "" {
			x, ok, e := r.Coordination().GetSession(ctx, lineage.ID(a.SessionID))
			if e != nil {
				return domain.Result{}, e
			}
			if !ok || string(x.ProjectID) != a.ProjectID {
				return domain.Result{}, missing()
			}
		}
		if a.TaskID != "" {
			x, ok, e := r.Coordination().GetTask(ctx, lineage.ID(a.TaskID))
			if e != nil {
				return domain.Result{}, e
			}
			if !ok || string(x.ProjectID) != a.ProjectID {
				return domain.Result{}, missing()
			}
		}
		if a.HandoffID != "" {
			x, ok, e := r.Coordination().GetHandoff(ctx, a.HandoffID)
			if e != nil {
				return domain.Result{}, e
			}
			if !ok {
				return domain.Result{}, missing()
			}
			t, ok, e := r.Coordination().GetTask(ctx, lineage.ID(x.TaskID))
			if e != nil {
				return domain.Result{}, e
			}
			if !ok || string(t.ProjectID) != a.ProjectID {
				return domain.Result{}, missing()
			}
		}
		if a.GitAssetID != "" {
			snapshot, ok, e := r.Git().LatestSnapshot(ctx, domain.ProjectID(a.ProjectID))
			if e != nil {
				return domain.Result{}, e
			}
			if !ok || snapshot.ObservedAt.After(a.CreatedAt) {
				return domain.Result{}, missing()
			}
			found := false
			for _, asset := range snapshot.Assets {
				if asset.Fingerprint == a.GitAssetID {
					found = true
					break
				}
			}
			if !found {
				return domain.Result{}, missing()
			}
		}
		e = r.Coordination().CreateAdoption(ctx, a)
		return domain.Result{ID: domain.ResultID(a.ID), Outcome: domain.OutcomeOK}, e
	})
	if e != nil {
		return a, mapErr(e)
	}
	e = s.store.Read(ctx, func(r ports.Repositories) error {
		var ok bool
		a, ok, e = r.Coordination().GetAdoptionByID(ctx, string(result.ID))
		if e != nil {
			return e
		}
		if !ok {
			return missing()
		}
		return nil
	})
	if e != nil {
		return coord.Adoption{}, mapErr(e)
	}
	return a, nil
}
