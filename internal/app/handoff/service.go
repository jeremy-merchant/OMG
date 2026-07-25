// Package handoff coordinates immutable transfer, decision, and adoption facts.
package handoff

import (
	"context"
	"errors"
	"github.com/jeremy-merchant/OMG/internal/domain"
	coord "github.com/jeremy-merchant/OMG/internal/domain/coordination"
	"github.com/jeremy-merchant/OMG/internal/domain/lineage"
	"github.com/jeremy-merchant/OMG/internal/ports"
	"github.com/jeremy-merchant/OMG/internal/safety"
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
