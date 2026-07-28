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
	Verbose   bool   `json:"verbose,omitempty"`
}

// PreflightView is intentionally small by default. Details are returned only
// when the caller explicitly asks for a verbose startup projection.
type PreflightView struct {
	Healthy           bool              `json:"healthy"`
	PendingMigrations int               `json:"pending_migrations"`
	ActiveSessions    int               `json:"active_sessions"`
	StaleSessions     int               `json:"stale_sessions"`
	Conflicts         int               `json:"conflicts"`
	IntegrationQueue  int               `json:"integration_queue"`
	Details           *PreflightDetails `json:"details,omitempty"`

	// Deprecated in-process compatibility fields. Public transports omit them;
	// callers request Details with verbose instead.
	Initialized      bool                        `json:"-"`
	Identity         *query.IdentityView         `json:"-"`
	Sessions         []query.IdentityView        `json:"-"`
	Tasks            []query.TaskView            `json:"-"`
	Inbox            []query.InboxItemView       `json:"-"`
	Dependencies     []query.DependencyView      `json:"-"`
	Reservations     []query.ReservationView     `json:"-"`
	Git              *query.GitView              `json:"-"`
	Warnings         []string                    `json:"-"`
	SuggestedActions []query.SuggestedActionView `json:"-"`
}

type PreflightDetails struct {
	Identity         *query.IdentityView         `json:"identity,omitempty"`
	Sessions         []query.IdentityView        `json:"sessions"`
	Tasks            []query.TaskView            `json:"tasks"`
	Inbox            []query.InboxItemView       `json:"inbox"`
	Dependencies     []query.DependencyView      `json:"dependencies"`
	Reservations     []query.ReservationView     `json:"reservations"`
	Git              *query.GitView              `json:"git,omitempty"`
	Warnings         []string                    `json:"warnings"`
	SuggestedActions []query.SuggestedActionView `json:"suggested_actions"`
}

func emptyPreflight(status foundation.Status) PreflightView {
	return PreflightView{
		Healthy:           status.Initialized && status.Pending == 0,
		PendingMigrations: status.Pending,
		Initialized:       status.Initialized,
		Sessions:          []query.IdentityView{},
		Tasks:             []query.TaskView{},
		Inbox:             []query.InboxItemView{},
		Dependencies:      []query.DependencyView{},
		Reservations:      []query.ReservationView{},
		Warnings:          []string{},
		SuggestedActions:  []query.SuggestedActionView{},
	}
}

func preflightDetails(snapshot query.BoardSnapshot) *PreflightDetails {
	return &PreflightDetails{Identity: snapshot.Identity, Sessions: snapshot.Sessions, Tasks: snapshot.Tasks, Inbox: snapshot.Inbox, Dependencies: snapshot.Dependencies, Reservations: snapshot.Reservations, Git: snapshot.Git, Warnings: snapshot.Warnings, SuggestedActions: snapshot.SuggestedActions}
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
	summary := query.Summarize(snapshot)
	result.ActiveSessions = summary.ActiveSessions
	result.StaleSessions = summary.StaleSessions
	result.Conflicts = summary.Conflicts
	result.IntegrationQueue = summary.IntegrationQueue
	result.Identity = snapshot.Identity
	result.Sessions = snapshot.Sessions
	result.Tasks = snapshot.Tasks
	result.Inbox = snapshot.Inbox
	result.Dependencies = snapshot.Dependencies
	result.Reservations = snapshot.Reservations
	result.Git = snapshot.Git
	result.Warnings = snapshot.Warnings
	result.SuggestedActions = snapshot.SuggestedActions
	if payload.Verbose {
		result.Details = preflightDetails(snapshot)
	}
	return Outcome{Data: result}
}
func decodePreflightSnapshot(model query.ViewModel, snapshot *query.BoardSnapshot) error {
	return json.Unmarshal(model.Data(), snapshot)
}
