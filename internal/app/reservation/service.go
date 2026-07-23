// Package reservation applies advisory path reservation policy over canonical storage.
package reservation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"example.invalid/coordledger/internal/domain"
	"example.invalid/coordledger/internal/domain/lineage"
	res "example.invalid/coordledger/internal/domain/reservation"
	"example.invalid/coordledger/internal/ports"
	"example.invalid/coordledger/internal/safety"
)

const maxTTL = 24 * time.Hour

type Options struct{ StrictConflicts bool }
type Service struct {
	store  ports.Store
	now    func() time.Time
	strict bool
}

func New(store ports.Store, now func() time.Time) *Service {
	return NewWithOptions(store, now, Options{})
}
func NewWithOptions(store ports.Store, now func() time.Time, o Options) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{store: store, now: now, strict: o.StrictConflicts}
}

type CreateRequest struct {
	ProjectID domain.ProjectID
	ID        string
	Pattern   res.Pattern
	Mode      res.Mode
	Owner     res.Owner
	Intent    string
	TTL       time.Duration
}
type RenewRequest struct {
	ProjectID                   domain.ProjectID
	ReservationID, CheckpointID string
	TTL                         time.Duration
}
type ReleaseRequest struct {
	ProjectID             domain.ProjectID
	ReservationID, Reason string
}
type OverrideRequest struct {
	ProjectID                      domain.ProjectID
	ReservationID, HumanID, Reason string
}
type resultData struct {
	ReservationID string `json:"reservation_id,omitempty"`
	ReleasedCount int    `json:"released_count,omitempty"`
}

func invalid() error {
	return domain.NewError(domain.CodeInvalidArgument, "invalid reservation request", false)
}
func missing() error {
	return domain.NewError(domain.CodeNotFound, "reservation or lineage record not found", false)
}
func conflict() error {
	return domain.NewError(domain.CodeConflict, "advisory reservation conflict", false)
}
func mapErr(e error) error {
	if e == nil {
		return nil
	}
	var d domain.DomainError
	if errors.As(e, &d) {
		return d
	}
	return domain.NewError(domain.CodeUnavailable, "reservation store unavailable", true)
}
func newID() (string, error) {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		return "", e
	}
	return "reservation-" + hex.EncodeToString(b[:]), nil
}
func validTTL(v time.Duration) bool { return v > 0 && v <= maxTTL }
func warning(id string, o res.Overlap) string {
	return fmt.Sprintf("advisory reservation conflict: %s (%s)", id, o)
}
func (s *Service) Create(ctx context.Context, key domain.IdempotencyKey, q CreateRequest) (domain.Result, error) {
	if !domain.IsSecretFreeStableMetadata(string(key)) || safety.RejectPrefixed(key, q) != nil {
		return domain.Result{}, invalid()
	}
	at := s.now().UTC()
	if q.ProjectID == "" || !domain.IsSecretFreeStableMetadata(q.ID) || !validTTL(q.TTL) {
		return domain.Result{}, invalid()
	}
	if q.ID == "" {
		var e error
		q.ID, e = newID()
		if e != nil {
			return domain.Result{}, domain.NewError(domain.CodeUnavailable, "reservation identifier unavailable", true)
		}
	}
	v, e := res.New(res.ReservationInput{ID: q.ID, Pattern: q.Pattern, Mode: q.Mode, Owner: q.Owner, Intent: q.Intent, ExpiresAt: at.Add(q.TTL)})
	if e != nil {
		return domain.Result{}, invalid()
	}
	_, out, e := s.store.Write(ctx, key, "reserve.add", func(r ports.Repositories) (domain.Result, error) {
		if e := sameProject(ctx, r, q.ProjectID, v.Owner); e != nil {
			return domain.Result{}, e
		}
		all, e := r.Reservations().List(ctx, q.ProjectID)
		if e != nil {
			return domain.Result{}, e
		}
		w := []string{}
		for _, other := range all {
			d := res.Decide(v, other, at)
			if !d.Conflict {
				continue
			}
			w = append(w, warning(other.ID, d.Overlap))
			if s.strict && other.LifecycleAt(at) != res.Overridden {
				return domain.Result{}, conflict()
			}
		}
		if e = r.Reservations().Create(ctx, q.ProjectID, v, at); e != nil {
			return domain.Result{}, e
		}
		return domain.Result{ID: domain.ResultID(v.ID), Outcome: domain.OutcomeOK, Data: resultData{ReservationID: v.ID}, Warnings: w}, nil
	})
	return out, mapErr(e)
}
func sameProject(ctx context.Context, r ports.Repositories, p domain.ProjectID, o res.Owner) error {
	c := r.Coordination()
	se, ok, e := c.GetSession(ctx, lineage.ID(o.SessionID))
	if e != nil {
		return e
	}
	if !ok || se.ProjectID != lineage.ID(p) || se.HumanID != lineage.ID(o.HumanID) {
		return missing()
	}
	ta, ok, e := c.GetTask(ctx, lineage.ID(o.TaskID))
	if e != nil {
		return e
	}
	if !ok || ta.ProjectID != lineage.ID(p) {
		return missing()
	}
	ru, ok, e := c.GetRun(ctx, lineage.ID(o.RunID))
	if e != nil {
		return e
	}
	if !ok || ru.TaskID != lineage.ID(o.TaskID) || ru.SessionID != lineage.ID(o.SessionID) {
		return missing()
	}
	return nil
}
func (s *Service) List(ctx context.Context, p domain.ProjectID) ([]res.Reservation, error) {
	if p == "" {
		return nil, invalid()
	}
	var out []res.Reservation
	e := s.store.Read(ctx, func(r ports.Repositories) error { var x error; out, x = r.Reservations().List(ctx, p); return x })
	return out, mapErr(e)
}
func (s *Service) History(ctx context.Context, p domain.ProjectID, id string) (res.ReservationHistory, error) {
	if p == "" || strings.TrimSpace(id) == "" || !domain.IsSecretFreeStableMetadata(id) {
		return res.ReservationHistory{}, invalid()
	}
	var out res.ReservationHistory
	e := s.store.Read(ctx, func(r ports.Repositories) error {
		v, ok, e := r.Reservations().History(ctx, p, id)
		if e != nil {
			return e
		}
		if !ok {
			return missing()
		}
		out = v
		return nil
	})
	return out, mapErr(e)
}
func (s *Service) Active(ctx context.Context, p domain.ProjectID) ([]res.Reservation, error) {
	all, e := s.List(ctx, p)
	if e != nil {
		return nil, e
	}
	at := s.now().UTC()
	out := []res.Reservation{}
	for _, v := range all {
		if v.LifecycleAt(at) != res.Expired && v.LifecycleAt(at) != res.Released {
			out = append(out, v)
		}
	}
	return out, nil
}
func (s *Service) Renew(ctx context.Context, key domain.IdempotencyKey, q RenewRequest) (domain.Result, error) {
	if !domain.IsSecretFreeStableMetadata(string(key)) || safety.RejectPrefixed(key, q) != nil {
		return domain.Result{}, invalid()
	}
	at := s.now().UTC()
	if q.ProjectID == "" || strings.TrimSpace(q.ReservationID) == "" || !domain.IsSecretFreeStableMetadata(q.ReservationID) || !domain.IsSecretFreeStableMetadata(q.CheckpointID) || !validTTL(q.TTL) {
		return domain.Result{}, invalid()
	}
	_, out, e := s.store.Write(ctx, key, "reserve.renew", func(r ports.Repositories) (domain.Result, error) {
		v, ok, e := r.Reservations().Get(ctx, q.ProjectID, q.ReservationID)
		if e != nil {
			return domain.Result{}, e
		}
		if !ok {
			return domain.Result{}, missing()
		}
		next, f, e := v.Renew(at, at.Add(q.TTL))
		if e != nil {
			return domain.Result{}, invalid()
		}
		f.CheckpointID = q.CheckpointID
		if e = r.Reservations().Renew(ctx, q.ProjectID, v.ID, f, at); e != nil {
			return domain.Result{}, e
		}
		return domain.Result{ID: domain.ResultID(next.ID), Outcome: domain.OutcomeOK, Data: resultData{ReservationID: next.ID}}, nil
	})
	return out, mapErr(e)
}
func (s *Service) Release(ctx context.Context, key domain.IdempotencyKey, q ReleaseRequest) (domain.Result, error) {
	if !domain.IsSecretFreeStableMetadata(string(key)) || safety.RejectPrefixed(key, q) != nil {
		return domain.Result{}, invalid()
	}
	return s.release(ctx, key, q.ProjectID, q.ReservationID, q.Reason)
}
func (s *Service) release(ctx context.Context, key domain.IdempotencyKey, p domain.ProjectID, id, reason string) (domain.Result, error) {
	at := s.now().UTC()
	if p == "" || strings.TrimSpace(id) == "" || !domain.IsSecretFreeStableMetadata(id) || strings.TrimSpace(reason) == "" {
		return domain.Result{}, invalid()
	}
	_, out, e := s.store.Write(ctx, key, "reserve.release", func(r ports.Repositories) (domain.Result, error) {
		v, ok, e := r.Reservations().Get(ctx, p, id)
		if e != nil {
			return domain.Result{}, e
		}
		if !ok {
			return domain.Result{}, missing()
		}
		next, f, e := v.Release(at, reason)
		if e != nil {
			return domain.Result{}, invalid()
		}
		if e = r.Reservations().Release(ctx, p, id, f); e != nil {
			return domain.Result{}, e
		}
		return domain.Result{ID: domain.ResultID(next.ID), Outcome: domain.OutcomeOK, Data: resultData{ReservationID: next.ID}}, nil
	})
	return out, mapErr(e)
}
func (s *Service) Override(ctx context.Context, key domain.IdempotencyKey, q OverrideRequest) (domain.Result, error) {
	if !domain.IsSecretFreeStableMetadata(string(key)) || safety.RejectPrefixed(key, q) != nil {
		return domain.Result{}, invalid()
	}
	at := s.now().UTC()
	if q.ProjectID == "" || strings.TrimSpace(q.ReservationID) == "" || !domain.IsSecretFreeStableMetadata(q.ReservationID) || strings.TrimSpace(q.HumanID) == "" || strings.TrimSpace(q.Reason) == "" {
		return domain.Result{}, invalid()
	}
	_, out, e := s.store.Write(ctx, key, "reserve.override", func(r ports.Repositories) (domain.Result, error) {
		if _, ok, e := r.Coordination().GetHuman(ctx, lineage.ID(q.HumanID)); e != nil {
			return domain.Result{}, e
		} else if !ok {
			return domain.Result{}, missing()
		}
		v, ok, e := r.Reservations().Get(ctx, q.ProjectID, q.ReservationID)
		if e != nil {
			return domain.Result{}, e
		}
		if !ok {
			return domain.Result{}, missing()
		}
		next, f, e := v.Override(at, res.OverrideRecord{HumanID: q.HumanID, Reason: q.Reason})
		if e != nil {
			return domain.Result{}, invalid()
		}
		if e = r.Reservations().Override(ctx, q.ProjectID, v.ID, f); e != nil {
			return domain.Result{}, e
		}
		return domain.Result{ID: domain.ResultID(next.ID), Outcome: domain.OutcomeOK, Data: resultData{ReservationID: next.ID}}, nil
	})
	return out, mapErr(e)
}
func (s *Service) ReleaseForWaitingTask(ctx context.Context, key domain.IdempotencyKey, p domain.ProjectID, task lineage.ID) (domain.Result, error) {
	if !domain.IsSecretFreeStableMetadata(string(key)) || safety.RejectPrefixed(key, p, task) != nil || p == "" || task == "" {
		return domain.Result{}, invalid()
	}
	at := s.now().UTC()
	_, out, e := s.store.Write(ctx, key, "reserve.release", func(r ports.Repositories) (domain.Result, error) {
		v, ok, e := r.Coordination().GetTask(ctx, task)
		if e != nil {
			return domain.Result{}, e
		}
		if !ok || v.ProjectID != lineage.ID(p) || (v.State != lineage.TaskWaiting && v.State != lineage.TaskBlocked) {
			return domain.Result{}, invalid()
		}
		xs, e := r.Reservations().ReleaseForTask(ctx, p, task, at, "task waiting or blocked")
		if e != nil {
			return domain.Result{}, e
		}
		return domain.Result{Outcome: domain.OutcomeOK, Data: resultData{ReleasedCount: len(xs)}}, nil
	})
	return out, mapErr(e)
}
