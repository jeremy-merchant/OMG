package app

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/jeremy-merchant/oh-my-group/internal/app/foundation"
	"github.com/jeremy-merchant/oh-my-group/internal/app/query"
	"github.com/jeremy-merchant/oh-my-group/internal/domain"
	"github.com/jeremy-merchant/oh-my-group/internal/domain/coordination"
	"github.com/jeremy-merchant/oh-my-group/internal/ports"
)

const preflightInboxPreviewLimit = 5

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
	Healthy                     bool                           `json:"healthy"`
	MutationAllowed             bool                           `json:"mutation_allowed"`
	BlockingReasons             []string                       `json:"blocking_reasons"`
	PendingMigrations           int                            `json:"pending_migrations"`
	ActiveSessions              int                            `json:"active_sessions"`
	OpenSessions                int                            `json:"open_sessions"`
	AliveSessions               int                            `json:"alive_sessions"`
	IdleSessions                int                            `json:"idle_sessions"`
	StaleSessions               int                            `json:"stale_sessions"`
	RuntimeUnobservableSessions int                            `json:"runtime_unobservable_sessions"`
	FinishedUnclosedSessions    int                            `json:"finished_unclosed_sessions"`
	Conflicts                   int                            `json:"conflicts"`
	OwnershipConflicts          int                            `json:"ownership_conflicts"`
	GitRisks                    int                            `json:"git_risks"`
	IntegrationQueue            int                            `json:"integration_queue"`
	Housekeeping                query.HousekeepingView         `json:"housekeeping"`
	AutomaticMigration          *foundation.AutomaticMigration `json:"automatic_migration,omitempty"`
	InboxSummary                *PreflightInboxSummary         `json:"inbox_summary,omitempty"`
	Details                     *PreflightDetails              `json:"details,omitempty"`

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

// PreflightInboxSummary is the compact, body-free pull notification returned
// only for a selected session. Reading preflight never advances delivery,
// read, or acknowledgement state.
type PreflightInboxSummary struct {
	Pending    int                  `json:"pending"`
	Unread     int                  `json:"unread"`
	Actionable int                  `json:"actionable"`
	Items      []PreflightInboxItem `json:"items"`
}

type PreflightInboxItem struct {
	MessageID       string `json:"message_id"`
	Type            string `json:"type"`
	Subject         string `json:"subject"`
	SenderSessionID string `json:"sender_session_id"`
	RelatedTaskID   string `json:"related_task_id,omitempty"`
	NeedsRead       bool   `json:"needs_read"`
	NeedsAck        bool   `json:"needs_ack"`
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
		BlockingReasons:   []string{},
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

func applyPreflightDecision(result *PreflightView) {
	result.BlockingReasons = result.BlockingReasons[:0]
	result.MutationAllowed = result.Healthy && result.OwnershipConflicts == 0
	if !result.Healthy {
		result.BlockingReasons = append(result.BlockingReasons, "preflight_unhealthy")
	}
	if result.OwnershipConflicts != 0 {
		result.BlockingReasons = append(result.BlockingReasons, "ownership_conflict")
	}
}

func preflightDetails(snapshot query.BoardSnapshot) *PreflightDetails {
	return &PreflightDetails{Identity: snapshot.Identity, Sessions: snapshot.Sessions, Tasks: snapshot.Tasks, Inbox: snapshot.Inbox, Dependencies: snapshot.Dependencies, Reservations: snapshot.Reservations, Git: snapshot.Git, Warnings: snapshot.Warnings, SuggestedActions: snapshot.SuggestedActions}
}

func preflightMessageActionable(messageType string) bool {
	switch coordination.MessageType(messageType) {
	case coordination.MessageQuestion, coordination.MessageDependency, coordination.MessageConflict, coordination.MessageBlocked, coordination.MessageHandoff:
		return true
	default:
		return false
	}
}

func summarizePreflightInbox(inbox []query.InboxItemView) *PreflightInboxSummary {
	summary := &PreflightInboxSummary{Items: []PreflightInboxItem{}}
	pending := make([]query.InboxItemView, 0, len(inbox))
	for _, item := range inbox {
		if item.AcknowledgedAt != nil {
			continue
		}
		summary.Pending++
		if item.ReadAt == nil {
			summary.Unread++
		}
		if preflightMessageActionable(item.Type) {
			summary.Actionable++
		}
		pending = append(pending, item)
	}
	sort.SliceStable(pending, func(i, j int) bool {
		leftActionable := preflightMessageActionable(pending[i].Type)
		rightActionable := preflightMessageActionable(pending[j].Type)
		if leftActionable != rightActionable {
			return leftActionable
		}
		leftUnread := pending[i].ReadAt == nil
		rightUnread := pending[j].ReadAt == nil
		if leftUnread != rightUnread {
			return leftUnread
		}
		if !pending[i].CreatedAt.Equal(pending[j].CreatedAt) {
			return pending[i].CreatedAt.After(pending[j].CreatedAt)
		}
		return pending[i].MessageID < pending[j].MessageID
	})
	if len(pending) > preflightInboxPreviewLimit {
		pending = pending[:preflightInboxPreviewLimit]
	}
	for _, item := range pending {
		summary.Items = append(summary.Items, PreflightInboxItem{
			MessageID:       item.MessageID,
			Type:            item.Type,
			Subject:         item.Subject,
			SenderSessionID: item.SenderSessionID,
			RelatedTaskID:   item.RelatedTaskID,
			NeedsRead:       item.ReadAt == nil,
			NeedsAck:        true,
		})
	}
	return summary
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
	var automaticMigration *foundation.AutomaticMigration
	if status.Initialized && status.Pending != 0 {
		automatic, automaticErr := d.service.AutoMigrate(ctx, selection)
		if automaticErr.Code != "" {
			return Outcome{Error: automaticErr}
		}
		automaticMigration = &automatic
		if automatic.Applied {
			status, statusErr = d.service.Status(ctx, selection, false)
			if statusErr.Code != "" {
				return Outcome{Error: statusErr}
			}
		}
	}
	result := emptyPreflight(status)
	result.AutomaticMigration = automaticMigration
	if !status.Initialized || status.Pending != 0 {
		applyPreflightDecision(&result)
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
	result.OpenSessions = summary.OpenSessions
	result.AliveSessions = summary.AliveSessions
	result.IdleSessions = summary.IdleSessions
	result.StaleSessions = summary.StaleSessions
	result.RuntimeUnobservableSessions = summary.RuntimeUnobservableSessions
	result.FinishedUnclosedSessions = summary.FinishedUnclosedSessions
	result.Conflicts = summary.Conflicts
	result.OwnershipConflicts = summary.OwnershipConflicts
	result.GitRisks = summary.GitRisks
	result.IntegrationQueue = summary.IntegrationQueue
	result.Housekeeping = summary.Housekeeping
	applyPreflightDecision(&result)
	result.Identity = snapshot.Identity
	result.Sessions = snapshot.Sessions
	result.Tasks = snapshot.Tasks
	result.Inbox = snapshot.Inbox
	if payload.SessionID != "" {
		result.InboxSummary = summarizePreflightInbox(snapshot.Inbox)
	}
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
