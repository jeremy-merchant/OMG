// Package progress coordinates immutable task progress reports.
package progress

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
type progressSummary struct {
	ProgressID string `json:"progress_id"`
	TaskID     string `json:"task_id"`
}

func New(store ports.Store, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{store: store, now: now}
}
func invalid() error {
	return domain.NewError(domain.CodeInvalidArgument, "invalid progress request", false)
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
func (s *Service) Append(ctx context.Context, key domain.IdempotencyKey, p coord.Progress) (coord.Progress, error) {
	if !domain.IsSecretFreeStableMetadata(string(key)) || safety.RejectPrefixed(key, p) != nil {
		return p, invalid()
	}
	p.CreatedAt = s.now().UTC()
	if p.Validate() != nil {
		return p, invalid()
	}
	_, result, e := s.store.Write(ctx, key, "progress.add", func(r ports.Repositories) (domain.Result, error) {
		task, ok, e := r.Coordination().GetTask(ctx, coordID(p.TaskID))
		if e != nil {
			return domain.Result{}, e
		} else if !ok {
			return domain.Result{}, missing()
		}
		session, ok, e := r.Coordination().GetSession(ctx, coordID(p.SessionID))
		if e != nil {
			return domain.Result{}, e
		} else if !ok {
			return domain.Result{}, missing()
		}
		if session.ProjectID != task.ProjectID {
			return domain.Result{}, invalid()
		}
		if p.RunID != "" {
			run, ok, e := r.Coordination().GetRun(ctx, coordID(p.RunID))
			if e != nil {
				return domain.Result{}, e
			} else if !ok {
				return domain.Result{}, missing()
			}
			if run.TaskID != task.ID || run.SessionID != session.ID {
				return domain.Result{}, invalid()
			}
		}
		e = r.Coordination().CreateProgress(ctx, p)
		return domain.Result{ID: domain.ResultID(p.ID), Outcome: domain.OutcomeOK, Data: progressSummary{ProgressID: p.ID, TaskID: p.TaskID}}, e
	})
	if e != nil {
		return p, mapErr(e)
	}
	var out coord.Progress
	e = s.store.Read(ctx, func(r ports.Repositories) error {
		var ok bool
		out, ok, e = r.Coordination().GetProgress(ctx, string(result.ID))
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
func (s *Service) History(ctx context.Context, taskID string) ([]coord.Progress, error) {
	var out []coord.Progress
	e := s.store.Read(ctx, func(r ports.Repositories) error {
		var e error
		out, e = r.Coordination().ListProgress(ctx, taskID)
		return e
	})
	return out, mapErr(e)
}
func coordID(v string) lineage.ID { return lineage.ID(v) }
