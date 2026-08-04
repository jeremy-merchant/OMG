// Package message coordinates typed inert mailbox records.
package message

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
	return domain.NewError(domain.CodeInvalidArgument, "invalid message request", false)
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

type deliveryResultSummary struct {
	RecipientRowID string `json:"recipient_row_id"`
}

func deliveryRowID(data any) (string, bool) {
	if x, ok := data.(deliveryResultSummary); ok {
		return x.RecipientRowID, x.RecipientRowID != ""
	}
	x, ok := data.(map[string]any)
	if !ok {
		return "", false
	}
	id, ok := x["recipient_row_id"].(string)
	return id, ok && id != ""
}

type deliveryOperation string

const (
	deliverOperation     deliveryOperation = "message.deliver"
	readOperation        deliveryOperation = "message.read"
	acknowledgeOperation deliveryOperation = "message.ack"
)

func (s *Service) Send(ctx context.Context, key domain.IdempotencyKey, project string, m coord.MailMessage) (coord.MailMessage, error) {
	if !domain.IsSecretFreeStableMetadata(string(key)) || safety.RejectPrefixed(key, m, project) != nil {
		return m, invalid()
	}
	m.CreatedAt = s.now().UTC()
	if m.Validate() != nil {
		return m, invalid()
	}
	_, result, e := s.store.Write(ctx, key, "message.send", func(r ports.Repositories) (domain.Result, error) {
		sender, ok, e := r.Coordination().GetSession(ctx, lineage.ID(m.SenderSessionID))
		if e != nil {
			return domain.Result{}, e
		}
		if !ok || string(sender.ProjectID) != project {
			return domain.Result{}, missing()
		}
		if m.RelatedTaskID != "" {
			t, ok, e := r.Coordination().GetTask(ctx, lineage.ID(m.RelatedTaskID))
			if e != nil {
				return domain.Result{}, e
			}
			if !ok || string(t.ProjectID) != project {
				return domain.Result{}, missing()
			}
		}
		for _, to := range m.Recipients {
			if to.SessionID != "" {
				x, ok, e := r.Coordination().GetSession(ctx, lineage.ID(to.SessionID))
				if e != nil {
					return domain.Result{}, e
				}
				if !ok || string(x.ProjectID) != project {
					return domain.Result{}, missing()
				}
			}
			if to.HumanID != "" {
				if _, ok, e := r.Coordination().GetHuman(ctx, lineage.ID(to.HumanID)); e != nil {
					return domain.Result{}, e
				} else if !ok {
					return domain.Result{}, missing()
				}
			}
			if to.TaskID != "" {
				x, ok, e := r.Coordination().GetTask(ctx, lineage.ID(to.TaskID))
				if e != nil {
					return domain.Result{}, e
				}
				if !ok || string(x.ProjectID) != project {
					return domain.Result{}, missing()
				}
			}
		}
		e = r.Coordination().CreateMessage(ctx, project, m)
		return domain.Result{ID: domain.ResultID(m.ID), Outcome: domain.OutcomeOK}, e
	})
	if e != nil {
		return m, mapErr(e)
	}
	e = s.store.Read(ctx, func(r ports.Repositories) error {
		var ok bool
		m, ok, e = r.Coordination().GetMessage(ctx, string(result.ID))
		if e != nil {
			return e
		}
		if !ok {
			return missing()
		}
		return nil
	})
	if e != nil {
		return coord.MailMessage{}, mapErr(e)
	}
	return m, nil
}
func (s *Service) Thread(ctx context.Context, id string) ([]coord.MailMessage, error) {
	var out []coord.MailMessage
	e := s.store.Read(ctx, func(r ports.Repositories) error { var e error; out, e = r.Coordination().ListThread(ctx, id); return e })
	return out, mapErr(e)
}
func (s *Service) Inbox(ctx context.Context, project string, to coord.RecipientTarget) ([]coord.MailMessage, error) {
	if to.Validate() != nil {
		return nil, invalid()
	}
	var out []coord.MailMessage
	e := s.store.Read(ctx, func(r ports.Repositories) error {
		var e error
		out, e = r.Coordination().ListInbox(ctx, project, to)
		return e
	})
	return out, mapErr(e)
}
func (s *Service) Deliver(ctx context.Context, key domain.IdempotencyKey, id string, to coord.RecipientTarget) (coord.RecipientDelivery, error) {
	return s.advance(ctx, key, id, to, deliverOperation, func(d coord.RecipientDelivery, at time.Time) (coord.RecipientDelivery, error) {
		if !d.DeliveredAt.IsZero() {
			return d, nil
		}
		return coord.DeliverRecipient(id, to, at)
	})
}
func (s *Service) Read(ctx context.Context, key domain.IdempotencyKey, id string, to coord.RecipientTarget) (coord.RecipientDelivery, error) {
	return s.advance(ctx, key, id, to, readOperation, coord.MarkRecipientRead)
}
func (s *Service) Acknowledge(ctx context.Context, key domain.IdempotencyKey, id string, to coord.RecipientTarget) (coord.RecipientDelivery, error) {
	return s.advance(ctx, key, id, to, acknowledgeOperation, coord.AcknowledgeRecipient)
}
func (s *Service) advance(ctx context.Context, key domain.IdempotencyKey, id string, to coord.RecipientTarget, operation deliveryOperation, f func(coord.RecipientDelivery, time.Time) (coord.RecipientDelivery, error)) (coord.RecipientDelivery, error) {
	var out coord.RecipientDelivery
	var rowID string
	if !domain.IsSecretFreeStableMetadata(string(key)) || safety.RejectPrefixed(key, to, id) != nil || to.Validate() != nil {
		return out, invalid()
	}
	_, result, e := s.store.Write(ctx, key, string(operation), func(r ports.Repositories) (domain.Result, error) {
		d, ok, e := r.Coordination().GetDelivery(ctx, id, to)
		if e != nil {
			return domain.Result{}, e
		}
		if !ok {
			return domain.Result{}, missing()
		}
		rowID, ok, e = r.Coordination().GetDeliveryRowID(ctx, id, to)
		if e != nil {
			return domain.Result{}, e
		}
		if !ok {
			return domain.Result{}, missing()
		}
		out, e = f(d, s.now().UTC())
		if e != nil {
			return domain.Result{}, invalid()
		}
		e = r.Coordination().SetDelivery(ctx, out)
		return domain.Result{ID: domain.ResultID(id), Outcome: domain.OutcomeOK, Data: deliveryResultSummary{RecipientRowID: rowID}}, e
	})
	if e != nil {
		return out, mapErr(e)
	}
	if rowID == "" {
		var ok bool
		rowID, ok = deliveryRowID(result.Data)
		if !ok {
			return coord.RecipientDelivery{}, mapErr(domain.NewError(domain.CodeUnavailable, "receipt delivery summary unavailable", false))
		}
	}
	e = s.store.Read(ctx, func(r ports.Repositories) error {
		var ok bool
		out, ok, e = r.Coordination().GetDeliveryByID(ctx, rowID)
		if e != nil {
			return e
		}
		if !ok {
			return missing()
		}
		return nil
	})
	if e != nil {
		return coord.RecipientDelivery{}, mapErr(e)
	}
	return out, nil
}
