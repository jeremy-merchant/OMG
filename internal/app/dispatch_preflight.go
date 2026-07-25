package app

import (
	"context"
	"encoding/json"

	"github.com/jeremy-merchant/OMG/internal/app/foundation"
	"github.com/jeremy-merchant/OMG/internal/app/query"
	"github.com/jeremy-merchant/OMG/internal/domain"
	"github.com/jeremy-merchant/OMG/internal/ports"
)

// PreflightRequest selects the session whose identity should be projected.
// An empty SessionID deliberately returns the project-wide view without guessing
// a current session.
type PreflightRequest struct {
	SessionID string `json:"session_id,omitempty"`
}

// PreflightView is the canonical read-only startup projection shared by all
// public transports.
type PreflightView struct {
	Initialized       bool                        `json:"initialized"`
	PendingMigrations int                         `json:"pending_migrations"`
	Identity          *query.IdentityView         `json:"identity,omitempty"`
	Sessions          []query.IdentityView        `json:"sessions"`
	Tasks             []query.TaskView            `json:"tasks"`
	Inbox             []query.InboxItemView       `json:"inbox"`
	Dependencies      []query.DependencyView      `json:"dependencies"`
	Reservations      []query.ReservationView     `json:"reservations"`
	Git               *query.GitView              `json:"git,omitempty"`
	Warnings          []string                    `json:"warnings"`
	SuggestedActions  []query.SuggestedActionView `json:"suggested_actions"`
}

func emptyPreflight(status foundation.Status) PreflightView {
	return PreflightView{
		Initialized:       status.Initialized,
		PendingMigrations: status.Pending,
		Sessions:          []query.IdentityView{},
		Tasks:             []query.TaskView{},
		Inbox:             []query.InboxItemView{},
		Dependencies:      []query.DependencyView{},
		Reservations:      []query.ReservationView{},
		Warnings:          []string{},
		SuggestedActions:  []query.SuggestedActionView{},
	}
}

func (d *ServiceDispatcher) dispatchPreflight(ctx context.Context, request Request, selection foundation.Selection) Outcome {
	var payload PreflightRequest
	if request.IdempotencyKey != "" || !decodePayload(request.Payload, &payload) {
		return Outcome{Error: invalidRequest()}
	}
	status, statusErr := d.service.Status(ctx, selection, false)
	if statusErr.Code != "" {
		return Outcome{Error: statusErr}
	}
	result := emptyPreflight(status)
	if !status.Initialized || status.Pending != 0 {
		return Outcome{Data: result}
	}

	boardRequest := query.BoardRequest{Mode: query.BoardAll}
	if payload.SessionID != "" {
		boardRequest.Mode = query.BoardMe
		boardRequest.SessionID = payload.SessionID
	}
	var snapshot query.BoardSnapshot
	err := d.service.WithReadOnlyCurrentStore(ctx, selection, func(resolved ports.ResolvedStore, store ports.Store) error {
		actor := domain.NewActorContext(domain.ScopeID(resolved.Project), resolved.Project, resolved.Workspace, domain.InvocationCLI, []domain.Capability{domain.CapabilityRead})
		model, queryErr := query.NewWithNativeResolver(store, d.service.NativeSessionResolver()).Query(ctx, actor, boardRequest)
		if queryErr != nil {
			return queryErr
		}
		return decodePreflightSnapshot(model, &snapshot)
	})
	if err.Code != "" {
		return Outcome{Error: err}
	}
	result.Identity = snapshot.Identity
	result.Sessions = snapshot.Sessions
	result.Tasks = snapshot.Tasks
	result.Inbox = snapshot.Inbox
	result.Dependencies = snapshot.Dependencies
	result.Reservations = snapshot.Reservations
	result.Git = snapshot.Git
	result.Warnings = snapshot.Warnings
	result.SuggestedActions = snapshot.SuggestedActions
	return Outcome{Data: result}
}
func decodePreflightSnapshot(model query.ViewModel, snapshot *query.BoardSnapshot) error {
	return json.Unmarshal(model.Data(), snapshot)
}
