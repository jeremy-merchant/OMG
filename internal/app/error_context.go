package app

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jeremy-merchant/oh-my-group/internal/app/foundation"
	"github.com/jeremy-merchant/oh-my-group/internal/domain"
	coord "github.com/jeremy-merchant/oh-my-group/internal/domain/coordination"
	"github.com/jeremy-merchant/oh-my-group/internal/domain/lineage"
	res "github.com/jeremy-merchant/oh-my-group/internal/domain/reservation"
	"github.com/jeremy-merchant/oh-my-group/internal/ports"
)

// enrichErrorOutcome first builds a deterministic contract from the request and
// error, then performs a best-effort read-only canonical lookup for current
// state and conflict owners. It never writes receipts, mutates Git, or invokes a
// canary verifier.
func (d *ServiceDispatcher) enrichErrorOutcome(ctx context.Context, request Request, result *Outcome) {
	prepareErrorOutcome(request, result)
	if ctx == nil || d == nil || d.service == nil || result == nil || result.Detail == nil || result.Error.Code == "" || !canonicalErrorContextCommand(request.Command) {
		return
	}
	selection := foundation.Selection{Project: request.Project, Workspace: request.Workspace, Store: request.Store}
	_ = d.service.WithReadOnlyCurrentStore(ctx, selection, func(resolved ports.ResolvedStore, store ports.Store) error {
		return store.Read(ctx, func(repositories ports.Repositories) error {
			hydrateCanonicalErrorContext(ctx, repositories, domain.ProjectID(resolved.Project), request, result.Detail)
			return nil
		})
	})
	normalizeErrorDetail(result.Detail, request)
}

func canonicalErrorContextCommand(command string) bool {
	switch command {
	case "candidate.close",
		"handoff.accept", "handoff.reject", "handoff.advance", "handoff.lifecycle",
		"canary.start", "canary.finish",
		"task.transition", "task.run-transition",
		"reserve.add", "reserve.batch-add", "worker.setup", "session.archive":
		return true
	default:
		return false
	}
}

func hydrateCanonicalErrorContext(ctx context.Context, repositories ports.Repositories, project domain.ProjectID, request Request, detail *ErrorDetail) {
	if detail == nil {
		return
	}
	detail.Entities.ProjectID = string(project)
	coordination := repositories.Coordination()
	switch request.Command {
	case "task.transition":
		if detail.Entities.TaskID == "" {
			return
		}
		if task, found, err := coordination.GetTask(ctx, lineage.ID(detail.Entities.TaskID)); err == nil && found {
			detail.CurrentState = string(task.State)
			detail.Entities.TaskID = string(task.ID)
			detail.AllowedTransitions = taskAllowedTransitions(detail.CurrentState)
		}
	case "task.run-transition":
		if detail.Entities.RunID == "" {
			return
		}
		if run, found, err := coordination.GetRun(ctx, lineage.ID(detail.Entities.RunID)); err == nil && found {
			detail.CurrentState = string(run.State)
			detail.Entities.RunID = string(run.ID)
			detail.Entities.TaskID = string(run.TaskID)
			detail.Entities.SessionID = string(run.SessionID)
			detail.AllowedTransitions = runAllowedTransitions(detail.CurrentState)
		}
	case "handoff.accept", "handoff.reject", "handoff.advance", "handoff.lifecycle", "candidate.close":
		hydrateHandoffErrorContext(ctx, coordination, detail)
	case "canary.start":
		hydrateHandoffErrorContext(ctx, coordination, detail)
		refineCanaryStartRecovery(request, detail)
	case "canary.finish":
		hydrateCanaryRunErrorContext(ctx, coordination, project, detail)
		refineCanaryFinishRecovery(request, detail)
	case "reserve.add", "reserve.batch-add":
		hydrateReservationConflictContext(ctx, repositories, project, request, detail)
	case "worker.setup":
		if detail.Entities.TaskID != "" {
			if task, found, err := coordination.GetTask(ctx, lineage.ID(detail.Entities.TaskID)); err == nil && found {
				detail.CurrentState = string(task.State)
				detail.Entities.TaskID = string(task.ID)
				detail.Entities.SessionID = string(task.ClaimedBySessionID)
				detail.AllowedTransitions = taskAllowedTransitions(detail.CurrentState)
			}
		}
		if detail.Entities.RunID != "" {
			if run, found, err := coordination.GetRun(ctx, lineage.ID(detail.Entities.RunID)); err == nil && found {
				detail.Entities.RunID = string(run.ID)
				detail.Entities.TaskID = string(run.TaskID)
				detail.Entities.SessionID = string(run.SessionID)
				if detail.CurrentState == "" {
					detail.CurrentState = string(run.State)
				}
			}
		}
		hydrateReservationConflictContext(ctx, repositories, project, request, detail)
	default:
		if detail.Entities.SessionID != "" {
			if session, found, err := coordination.GetSession(ctx, lineage.ID(detail.Entities.SessionID)); err == nil && found {
				detail.CurrentState = string(session.Liveness)
				detail.Entities.TaskID = string(session.TaskID)
			}
		}
	}
}

func hydrateHandoffErrorContext(ctx context.Context, repository ports.CoordinationRepository, detail *ErrorDetail) {
	if detail.Entities.HandoffID == "" {
		return
	}
	handoff, found, err := repository.GetHandoff(ctx, detail.Entities.HandoffID)
	if err != nil || !found {
		return
	}
	detail.Entities.HandoffID = handoff.ID
	detail.Entities.TaskID = handoff.TaskID
	detail.Entities.RunID = handoff.RunID
	detail.Entities.SessionID = handoff.SourceSessionID
	events, err := repository.ListHandoffLifecycleEvents(ctx, handoff.ID)
	if err != nil {
		return
	}
	var decision *coord.HandoffDecision
	if value, ok, readErr := repository.GetHandoffDecision(ctx, handoff.ID); readErr == nil && ok {
		decision = &value
	}
	detail.CurrentState = string(coord.CurrentIntegrationState(events, decision))
	detail.AllowedTransitions = integrationAllowedTransitions(detail.CurrentState)
}

func hydrateCanaryRunErrorContext(ctx context.Context, repository ports.CoordinationRepository, project domain.ProjectID, detail *ErrorDetail) {
	if detail.Entities.CanaryRunID == "" {
		return
	}
	tasks, err := repository.ListTasks(ctx, project)
	if err != nil {
		return
	}
	for _, task := range tasks {
		handoffs, listErr := repository.ListHandoffs(ctx, string(task.ID))
		if listErr != nil {
			continue
		}
		for _, handoff := range handoffs {
			events, eventsErr := repository.ListHandoffLifecycleEvents(ctx, handoff.ID)
			if eventsErr != nil {
				continue
			}
			matched := false
			for _, event := range events {
				if event.CanaryRunID == detail.Entities.CanaryRunID {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
			detail.Entities.HandoffID = handoff.ID
			detail.Entities.TaskID = handoff.TaskID
			detail.Entities.RunID = handoff.RunID
			detail.Entities.SessionID = handoff.SourceSessionID
			var decision *coord.HandoffDecision
			if value, ok, readErr := repository.GetHandoffDecision(ctx, handoff.ID); readErr == nil && ok {
				decision = &value
			}
			detail.CurrentState = string(coord.CurrentIntegrationState(events, decision))
			detail.AllowedTransitions = integrationAllowedTransitions(detail.CurrentState)
			return
		}
	}
}

func refineCanaryStartRecovery(request Request, detail *ErrorDetail) {
	if detail == nil || detail.ReasonCode != "payload_validation" || !completeCanaryStartPayload(request.Payload) || detail.CurrentState == "" {
		return
	}
	if detail.CurrentState == string(coord.IntegrationAccepted) {
		var payload canaryStartPayload
		if decodePayload(request.Payload, &payload) && payload.Mode == "local_integration" {
			return
		}
		detail.ReasonCode = "missing_evidence"
		detail.MissingEvidence = []string{"integration_commit"}
		detail.Prerequisites = []string{"record an INTEGRATED lifecycle event with the exact integration commit before starting or retrying a strict canary"}
		detail.RecoveryActions = []RecoveryAction{inspectionAction(request, detail.Entities)}
		return
	}
	if !containsTransition(integrationAllowedTransitions(detail.CurrentState), string(coord.IntegrationCanaryRunning)) {
		detail.ReasonCode = "invalid_transition"
		detail.Prerequisites = []string{"the current handoff lifecycle must allow transition to CANARY_RUNNING"}
		detail.RecoveryActions = []RecoveryAction{inspectionAction(request, detail.Entities)}
	}
}

func refineCanaryFinishRecovery(request Request, detail *ErrorDetail) {
	if detail == nil || detail.ReasonCode != "payload_validation" || !completeCanaryFinishPayload(request.Payload) || detail.CurrentState == "" {
		return
	}
	if detail.CurrentState != string(coord.IntegrationCanaryRunning) {
		detail.ReasonCode = "invalid_transition"
		detail.Prerequisites = []string{"the matching canary run must still be the active CANARY_RUNNING lifecycle event"}
		detail.RecoveryActions = []RecoveryAction{inspectionAction(request, detail.Entities)}
	}
}

func completeCanaryStartPayload(raw json.RawMessage) bool {
	var payload canaryStartPayload
	if !decodePayload(raw, &payload) || payload.HandoffID == "" || payload.ActorSessionID == "" || payload.IntegrationRef == "" || payload.VerificationCommand == "" || payload.ExecutionKind == "" || payload.EnvironmentFingerprint == "" {
		return false
	}
	switch payload.Mode {
	case "", "release_or_production":
		return payload.CandidateSHA == ""
	case "local_integration":
		return payload.CandidateSHA != ""
	default:
		return false
	}
}

func completeCanaryFinishPayload(raw json.RawMessage) bool {
	var payload canaryFinishPayload
	return decodePayload(raw, &payload) && payload.CanaryRunID != "" && payload.ActorSessionID != ""
}

func containsTransition(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func hydrateReservationConflictContext(ctx context.Context, repositories ports.Repositories, project domain.ProjectID, request Request, detail *ErrorDetail) {
	if detail.ReasonCode != "reservation_conflict" && detail.ReasonCode != "state_conflict" {
		return
	}
	now := time.Now().UTC()
	candidates := reservationRecoveryCandidates(request, now)
	if len(candidates) == 0 {
		return
	}
	records, err := repositories.Reservations().List(ctx, project)
	if err != nil {
		return
	}
	seen := make(map[string]struct{})
	for _, candidate := range candidates {
		for _, other := range records {
			decision := res.Decide(candidate, other, now)
			if !decision.Conflict {
				continue
			}
			if _, exists := seen[other.ID]; exists {
				continue
			}
			seen[other.ID] = struct{}{}
			detail.Conflicts = append(detail.Conflicts, ErrorEntities{
				ProjectID: string(project), ReservationID: other.ID,
				TaskID: other.Owner.TaskID, RunID: other.Owner.RunID, SessionID: other.Owner.SessionID,
			})
		}
	}
	if len(detail.Conflicts) != 0 {
		detail.ReasonCode = "reservation_conflict"
	}
}

func reservationRecoveryCandidates(request Request, now time.Time) []res.Reservation {
	if request.Command == "worker.setup" {
		var payload workerSetupPayload
		if !decodePayload(request.Payload, &payload) {
			return nil
		}
		out := make([]res.Reservation, 0, len(payload.Reservations))
		owner := res.Owner{HumanID: payload.HumanID, SessionID: payload.SessionID, TaskID: payload.TaskID, RunID: payload.RunID}
		for _, item := range payload.Reservations {
			if candidate, ok := reservationRecoveryCandidate(item.ID, item.PatternKind, item.Pattern, item.CaseSensitivity, item.Mode, item.Intent, item.TTLSeconds, owner, now); ok {
				out = append(out, candidate)
			}
		}
		return out
	}
	if request.Command == "reserve.batch-add" {
		var payload reserveBatchAddPayload
		if !decodePayload(request.Payload, &payload) {
			return nil
		}
		out := make([]res.Reservation, 0, len(payload.Items))
		owner := res.Owner{HumanID: payload.HumanID, SessionID: payload.SessionID, TaskID: payload.TaskID, RunID: payload.RunID}
		for _, item := range payload.Items {
			if candidate, ok := reservationRecoveryCandidate(item.ID, item.PatternKind, item.Pattern, item.CaseSensitivity, item.Mode, item.Intent, item.TTLSeconds, owner, now); ok {
				out = append(out, candidate)
			}
		}
		return out
	}
	var payload reserveAddPayload
	if !decodePayload(request.Payload, &payload) {
		return nil
	}
	candidate, ok := reservationRecoveryCandidate(payload.ID, payload.PatternKind, payload.Pattern, payload.CaseSensitivity, payload.Mode, payload.Intent, payload.TTLSeconds, res.Owner{HumanID: payload.HumanID, SessionID: payload.SessionID, TaskID: payload.TaskID, RunID: payload.RunID}, now)
	if !ok {
		return nil
	}
	return []res.Reservation{candidate}
}

func reservationRecoveryCandidate(id, patternKind, patternValue, sensitivity, mode, intent string, ttlSeconds int64, owner res.Owner, now time.Time) (res.Reservation, bool) {
	pattern, err := res.NewPattern(res.PatternKind(patternKind), patternValue, res.CaseSensitivity(sensitivity))
	if err != nil {
		return res.Reservation{}, false
	}
	if id == "" {
		id = "recovery-candidate"
	}
	if intent == "" {
		intent = "recovery inspection"
	}
	ttl := durationFromSeconds(ttlSeconds)
	if ttl <= 0 {
		ttl = time.Hour
	}
	candidate, err := res.New(res.ReservationInput{ID: id, Pattern: pattern, Mode: res.Mode(mode), Owner: owner, Intent: intent, ExpiresAt: now.Add(ttl)})
	return candidate, err == nil
}
