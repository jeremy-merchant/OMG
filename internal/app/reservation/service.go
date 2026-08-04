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

	"github.com/jeremy-merchant/oh-my-group/internal/domain"
	"github.com/jeremy-merchant/oh-my-group/internal/domain/lineage"
	res "github.com/jeremy-merchant/oh-my-group/internal/domain/reservation"
	"github.com/jeremy-merchant/oh-my-group/internal/ports"
	"github.com/jeremy-merchant/oh-my-group/internal/safety"
)

const (
	maxTTL              = 24 * time.Hour
	maxReservationBatch = 128
)

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
type BatchCreateItem struct {
	ID      string
	Pattern res.Pattern
	Mode    res.Mode
	Intent  string
	TTL     time.Duration
}
type BatchCreateRequest struct {
	ProjectID domain.ProjectID
	Owner     res.Owner
	Items     []BatchCreateItem
}
type BatchCreateData struct {
	ReservationIDs []string `json:"reservation_ids"`
}
type PreparedBatch struct {
	ProjectID    domain.ProjectID
	Owner        res.Owner
	reservations []res.Reservation
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

// BatchCreate records a bounded set of reservations in one Store.Write
// transaction. Every item is validated and conflict-checked before the first
// repository insert, so strict conflicts and storage failures cannot leave a
// partial batch behind.
func (s *Service) BatchCreate(ctx context.Context, key domain.IdempotencyKey, q BatchCreateRequest) (domain.Result, error) {
	if !domain.IsSecretFreeStableMetadata(string(key)) || safety.RejectPrefixed(key, q) != nil || q.ProjectID == "" || len(q.Items) == 0 || len(q.Items) > maxReservationBatch {
		return domain.Result{}, invalid()
	}
	at := s.now().UTC()
	prepared, err := PrepareBatch(at, q)
	if err != nil {
		return domain.Result{}, err
	}
	_, out, err := s.store.Write(ctx, key, "reserve.batch-add", func(r ports.Repositories) (domain.Result, error) {
		data, warnings, err := CreatePreparedBatchInTransaction(ctx, r, at, s.strict, prepared)
		if err != nil {
			return domain.Result{}, err
		}
		return domain.Result{ID: domain.ResultID(data.ReservationIDs[0]), Outcome: domain.OutcomeOK, Data: data, Warnings: warnings}, nil
	})
	return out, mapErr(err)
}

// PrepareBatch normalizes and validates every reservation before a transaction
// mutates canonical state. The returned value is opaque outside this package and
// can be safely passed to CreatePreparedBatchInTransaction.
func PrepareBatch(at time.Time, q BatchCreateRequest) (PreparedBatch, error) {
	if q.ProjectID == "" || len(q.Items) == 0 || len(q.Items) > maxReservationBatch {
		return PreparedBatch{}, invalid()
	}
	prepared := make([]res.Reservation, 0, len(q.Items))
	seenIDs := make(map[string]struct{}, len(q.Items))
	seenPatterns := make(map[string]struct{}, len(q.Items))
	for _, item := range q.Items {
		if strings.TrimSpace(item.ID) == "" || !domain.IsSecretFreeStableMetadata(item.ID) || !validTTL(item.TTL) {
			return PreparedBatch{}, invalid()
		}
		if _, exists := seenIDs[item.ID]; exists {
			return PreparedBatch{}, invalid()
		}
		seenIDs[item.ID] = struct{}{}
		value, err := res.New(res.ReservationInput{ID: item.ID, Pattern: item.Pattern, Mode: item.Mode, Owner: q.Owner, Intent: item.Intent, ExpiresAt: at.Add(item.TTL)})
		if err != nil {
			return PreparedBatch{}, invalid()
		}
		patternKey := string(value.Pattern.Kind) + "\x00" + value.Pattern.Value + "\x00" + string(value.Pattern.CaseSensitivity) + "\x00" + string(value.Mode)
		if _, exists := seenPatterns[patternKey]; exists {
			return PreparedBatch{}, invalid()
		}
		seenPatterns[patternKey] = struct{}{}
		prepared = append(prepared, value)
	}
	return PreparedBatch{ProjectID: q.ProjectID, Owner: q.Owner, reservations: prepared}, nil
}

// CreatePreparedBatchInTransaction conflict-checks and persists a prepared
// batch inside an existing Store.Write callback. It performs no nested writes.
func CreatePreparedBatchInTransaction(ctx context.Context, repositories ports.Repositories, at time.Time, strict bool, prepared PreparedBatch) (BatchCreateData, []string, error) {
	return createPreparedBatchInTransaction(ctx, repositories, at, strict, false, prepared)
}

// EnsurePreparedBatchInTransaction is the worker-setup variant. An active
// reservation with the same ID and identical owner/pattern/mode/intent is reused
// instead of inserted again; every other ID collision fails closed.
func EnsurePreparedBatchInTransaction(ctx context.Context, repositories ports.Repositories, at time.Time, strict bool, prepared PreparedBatch) (BatchCreateData, []string, error) {
	return createPreparedBatchInTransaction(ctx, repositories, at, strict, true, prepared)
}

func createPreparedBatchInTransaction(ctx context.Context, repositories ports.Repositories, at time.Time, strict, reuseExact bool, prepared PreparedBatch) (BatchCreateData, []string, error) {
	if len(prepared.reservations) == 0 || prepared.ProjectID == "" {
		return BatchCreateData{}, nil, invalid()
	}
	if err := sameProject(ctx, repositories, prepared.ProjectID, prepared.Owner); err != nil {
		return BatchCreateData{}, nil, err
	}
	existing, err := repositories.Reservations().List(ctx, prepared.ProjectID)
	if err != nil {
		return BatchCreateData{}, nil, err
	}
	existingByID := make(map[string]res.Reservation, len(existing))
	for _, record := range existing {
		existingByID[record.ID] = record
	}
	reused := make(map[string]bool, len(prepared.reservations))
	warnings := make([]string, 0)
	for i, candidate := range prepared.reservations {
		if record, found := existingByID[candidate.ID]; found {
			if !reuseExact || record.LifecycleAt(at) != res.Active || record.Pattern != candidate.Pattern || record.Mode != candidate.Mode || record.Owner != candidate.Owner || record.Intent != candidate.Intent {
				return BatchCreateData{}, nil, conflict()
			}
			reused[candidate.ID] = true
		}
		for _, other := range existing {
			if reused[candidate.ID] && other.ID == candidate.ID {
				continue
			}
			decision := res.Decide(candidate, other, at)
			if !decision.Conflict {
				continue
			}
			warnings = append(warnings, candidate.ID+": "+warning(other.ID, decision.Overlap))
			if strict && other.LifecycleAt(at) != res.Overridden {
				return BatchCreateData{}, nil, conflict()
			}
		}
		for _, other := range prepared.reservations[:i] {
			decision := res.Decide(candidate, other, at)
			if !decision.Conflict {
				continue
			}
			warnings = append(warnings, candidate.ID+": "+warning(other.ID, decision.Overlap))
			if strict {
				return BatchCreateData{}, nil, conflict()
			}
		}
	}
	for _, candidate := range prepared.reservations {
		if reused[candidate.ID] {
			continue
		}
		if err := repositories.Reservations().Create(ctx, prepared.ProjectID, candidate, at); err != nil {
			return BatchCreateData{}, nil, err
		}
	}
	ids := make([]string, len(prepared.reservations))
	for i := range prepared.reservations {
		ids[i] = prepared.reservations[i].ID
	}
	return BatchCreateData{ReservationIDs: ids}, warnings, nil
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
