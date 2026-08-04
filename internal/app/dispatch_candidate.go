package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	dependencyapp "github.com/jeremy-merchant/oh-my-group/internal/app/dependency"
	"github.com/jeremy-merchant/oh-my-group/internal/app/foundation"
	"github.com/jeremy-merchant/oh-my-group/internal/domain"
	coord "github.com/jeremy-merchant/oh-my-group/internal/domain/coordination"
	"github.com/jeremy-merchant/oh-my-group/internal/domain/lineage"
	"github.com/jeremy-merchant/oh-my-group/internal/ports"
	"github.com/jeremy-merchant/oh-my-group/internal/safety"
)

type candidateClosePayload struct {
	HandoffID      string `json:"handoff_id"`
	ActorSessionID string `json:"actor_session_id"`
	ArchiveEventID string `json:"archive_event_id"`
	Evidence       string `json:"evidence"`
}

type candidateCloseResult struct {
	HandoffID            string   `json:"handoff_id"`
	TaskID               string   `json:"task_id,omitempty"`
	RunID                string   `json:"run_id,omitempty"`
	SourceSessionID      string   `json:"source_session_id,omitempty"`
	CurrentState         string   `json:"current_state"`
	CompletedSteps       []string `json:"completed_steps"`
	MissingEvidence      []string `json:"missing_evidence"`
	AllowedTransitions   []string `json:"allowed_transitions"`
	NextCommand          string   `json:"next_command,omitempty"`
	NextArgv             []string `json:"next_argv"`
	ReadyToClose         bool     `json:"ready_to_close"`
	Closed               bool     `json:"closed"`
	TaskState            string   `json:"task_state,omitempty"`
	RunState             string   `json:"run_state,omitempty"`
	ReservationsReleased int      `json:"reservations_released"`
	SessionArchived      bool     `json:"session_archived"`
	GitMutated           bool     `json:"git_mutated"`
}

type candidateCloseReceipt struct {
	candidateCloseResult
	PayloadSHA256 string `json:"_payload_sha256,omitempty"`
}

type candidateFacts struct {
	result   candidateCloseResult
	handoff  coord.Handoff
	decision *coord.HandoffDecision
	events   []coord.HandoffLifecycleEvent
}

func (d *ServiceDispatcher) dispatchCandidate(ctx context.Context, request Request, selection foundation.Selection) (Outcome, bool) {
	if request.Command != "candidate.close" {
		return Outcome{}, false
	}
	if request.IdempotencyKey == "" || !domain.IsSecretFreeStableMetadata(request.IdempotencyKey) {
		return Outcome{Error: invalidRequest()}, true
	}
	var payload candidateClosePayload
	if !decodePayload(request.Payload, &payload) || payload.HandoffID == "" || payload.ActorSessionID == "" || payload.ArchiveEventID == "" || payload.Evidence == "" || safety.RejectPrefixed(request.IdempotencyKey, payload) != nil {
		return Outcome{Error: invalidRequest()}, true
	}

	payloadSHA256 := candidatePayloadSHA256(payload)
	var result candidateCloseResult
	err := d.service.WithCurrentStore(ctx, selection, func(resolved ports.ResolvedStore, store ports.Store) error {
		project := lineage.ID(resolved.Project)
		facts, readErr := inspectCandidate(ctx, store, project, payload)
		if readErr != nil {
			return readErr
		}
		result = facts.result
		if !result.ReadyToClose {
			return nil
		}

		_, recorded, writeErr := store.Write(ctx, domain.IdempotencyKey(request.IdempotencyKey), "candidate.close", func(repositories ports.Repositories) (domain.Result, error) {
			fresh, inspectErr := inspectCandidateRepositories(ctx, repositories, project, payload)
			if inspectErr != nil {
				return domain.Result{}, inspectErr
			}
			if !fresh.result.ReadyToClose {
				return domain.Result{}, domain.NewError(domain.CodeConflict, "candidate lifecycle changed before closure", false)
			}

			now := time.Now().UTC()
			coordination := repositories.Coordination()
			task, found, repositoryErr := coordination.GetTask(ctx, lineage.ID(fresh.handoff.TaskID))
			if repositoryErr != nil {
				return domain.Result{}, repositoryErr
			}
			if !found || task.ProjectID != project {
				return domain.Result{}, domain.NewError(domain.CodeNotFound, "candidate task was not found in project", false)
			}
			run, found, repositoryErr := coordination.GetRun(ctx, lineage.ID(fresh.handoff.RunID))
			if repositoryErr != nil {
				return domain.Result{}, repositoryErr
			}
			if !found || run.TaskID != task.ID || string(run.SessionID) != fresh.handoff.SourceSessionID {
				return domain.Result{}, domain.NewError(domain.CodeConflict, "candidate run lineage does not match the handoff", false)
			}
			source, found, repositoryErr := coordination.GetSession(ctx, lineage.ID(fresh.handoff.SourceSessionID))
			if repositoryErr != nil {
				return domain.Result{}, repositoryErr
			}
			if !found || source.ProjectID != project || run.SessionID != source.ID {
				return domain.Result{}, domain.NewError(domain.CodeConflict, "candidate source session does not match the handoff", false)
			}
			if task.ClaimedBySessionID != "" && task.ClaimedBySessionID != source.ID {
				return domain.Result{}, domain.NewError(domain.CodeConflict, "candidate task is owned by another session", false)
			}
			actor, found, repositoryErr := coordination.GetSession(ctx, lineage.ID(payload.ActorSessionID))
			if repositoryErr != nil {
				return domain.Result{}, repositoryErr
			}
			if !found || actor.ProjectID != project {
				return domain.Result{}, domain.NewError(domain.CodeNotFound, "candidate close actor session was not found in project", false)
			}

			evidence := []byte(payload.Evidence)
			switch run.State {
			case lineage.RunWorkComplete:
				if !lineage.CanTransitionRun(run.State, lineage.RunVerifiedDone, evidence) {
					return domain.Result{}, domain.NewError(domain.CodeConflict, "candidate run cannot transition to VERIFIED_DONE", false)
				}
				run, repositoryErr = coordination.TransitionRun(ctx, run.ID, lineage.RunVerifiedDone, evidence, now)
				if repositoryErr != nil {
					return domain.Result{}, repositoryErr
				}
			case lineage.RunVerifiedDone:
			default:
				return domain.Result{}, domain.NewError(domain.CodeConflict, "candidate run must be WORK_COMPLETE before closure", false)
			}

			switch task.State {
			case lineage.TaskWorkComplete:
				task, repositoryErr = dependencyapp.TransitionAndReconcileInTransaction(ctx, repositories, now, string(project), string(task.ID), payload.ActorSessionID, lineage.TaskVerifiedDone, evidence)
				if repositoryErr != nil {
					return domain.Result{}, repositoryErr
				}
			case lineage.TaskVerifiedDone:
			default:
				return domain.Result{}, domain.NewError(domain.CodeConflict, "candidate task must be WORK_COMPLETE before closure", false)
			}

			runs, repositoryErr := coordination.ListRunsForSession(ctx, project, source.ID)
			if repositoryErr != nil {
				return domain.Result{}, repositoryErr
			}
			for _, ownedRun := range runs {
				if !lineage.RunHasEnded(ownedRun.State) {
					return domain.Result{}, domain.NewError(domain.CodeConflict, "candidate source session still owns a non-terminal run", false)
				}
			}

			released, repositoryErr := repositories.Reservations().ReleaseForTask(ctx, domain.ProjectID(resolved.Project), task.ID, now, "candidate lifecycle closed")
			if repositoryErr != nil {
				return domain.Result{}, repositoryErr
			}
			sessionArchived := source.EndedAt != nil || source.InterruptedAt != nil || source.Liveness == lineage.Interrupted
			if !sessionArchived {
				heartbeat := lineage.Heartbeat{ID: lineage.ID(payload.ArchiveEventID), SessionID: source.ID, ObservedAt: now, Liveness: lineage.Interrupted, Detail: []byte(`{"archived":true,"mode":"FULL","candidate_closed":true}`)}
				if heartbeat.Validate() != nil {
					return domain.Result{}, invalidRequest()
				}
				if repositoryErr = coordination.RecordHeartbeat(ctx, heartbeat); repositoryErr != nil {
					return domain.Result{}, repositoryErr
				}
				sessionArchived = true
			}

			value := fresh.result
			value.Closed = true
			value.ReadyToClose = true
			value.TaskState = string(task.State)
			value.RunState = string(run.State)
			value.ReservationsReleased = len(released)
			value.SessionArchived = sessionArchived
			value.GitMutated = false
			value.NextCommand = ""
			value.NextArgv = []string{}
			return domain.Result{ID: domain.ResultID(fresh.handoff.ID), Outcome: domain.OutcomeOK, Data: candidateCloseReceipt{candidateCloseResult: value, PayloadSHA256: payloadSHA256}}, nil
		})
		if writeErr != nil {
			return writeErr
		}
		encoded, marshalErr := json.Marshal(recorded.Data)
		var persisted candidateCloseReceipt
		if marshalErr != nil || json.Unmarshal(encoded, &persisted) != nil {
			return domain.NewError(domain.CodeInternal, "candidate close receipt is invalid", false)
		}
		if persisted.PayloadSHA256 != "" && persisted.PayloadSHA256 != payloadSHA256 {
			return domain.NewError(domain.CodeConflict, "idempotency key was reused with a different candidate.close payload", false)
		}
		result = persisted.candidateCloseResult
		return nil
	})
	return outcome(result, err), true
}

func candidatePayloadSHA256(payload candidateClosePayload) string {
	encoded, _ := json.Marshal(payload)
	digest := sha256.Sum256(encoded)
	return fmt.Sprintf("%x", digest[:])
}

func inspectCandidate(ctx context.Context, store ports.Store, project lineage.ID, payload candidateClosePayload) (candidateFacts, error) {
	var facts candidateFacts
	err := store.Read(ctx, func(repositories ports.Repositories) error {
		var inspectErr error
		facts, inspectErr = inspectCandidateRepositories(ctx, repositories, project, payload)
		return inspectErr
	})
	return facts, err
}

func inspectCandidateRepositories(ctx context.Context, repositories ports.Repositories, project lineage.ID, payload candidateClosePayload) (candidateFacts, error) {
	coordination := repositories.Coordination()
	handoff, found, err := coordination.GetHandoff(ctx, payload.HandoffID)
	if err != nil {
		return candidateFacts{}, err
	}
	if !found {
		return candidateFacts{}, domain.NewError(domain.CodeNotFound, "candidate handoff was not found", false)
	}
	task, found, err := coordination.GetTask(ctx, lineage.ID(handoff.TaskID))
	if err != nil {
		return candidateFacts{}, err
	}
	if !found || task.ProjectID != project {
		return candidateFacts{}, domain.NewError(domain.CodeNotFound, "candidate task was not found in project", false)
	}
	if handoff.Status != coord.HandoffSubmitted {
		return candidateFacts{}, domain.NewError(domain.CodeConflict, "candidate handoff is not an immutable submitted handoff", false)
	}
	events, err := coordination.ListHandoffLifecycleEvents(ctx, handoff.ID)
	if err != nil {
		return candidateFacts{}, err
	}
	decision, hasDecision, err := coordination.GetHandoffDecision(ctx, handoff.ID)
	if err != nil {
		return candidateFacts{}, err
	}
	var decisionPtr *coord.HandoffDecision
	if hasDecision {
		decisionPtr = &decision
	}

	current := coord.CurrentIntegrationState(events, decisionPtr)
	result := candidateCloseResult{
		HandoffID:          handoff.ID,
		TaskID:             handoff.TaskID,
		RunID:              handoff.RunID,
		SourceSessionID:    handoff.SourceSessionID,
		CurrentState:       string(current),
		CompletedSteps:     []string{"handoff_validated"},
		MissingEvidence:    []string{},
		AllowedTransitions: candidateAllowedTransitions(current),
		NextArgv:           []string{},
		GitMutated:         false,
	}

	accepted := hasDecision && decision.Decision == coord.HandoffAccepted
	if accepted {
		result.CompletedSteps = append(result.CompletedSteps, "review_accepted")
	} else {
		result.MissingEvidence = append(result.MissingEvidence, "accepted_review")
	}
	var integrated *coord.HandoffLifecycleEvent
	var exactCanary *coord.HandoffLifecycleEvent
	var cleaned *coord.HandoffLifecycleEvent
	for i := range events {
		event := &events[i]
		switch event.State {
		case coord.IntegrationIntegrated:
			integrated = event
		case coord.IntegrationCanaryPassed:
			if exactRealCanary(event, integrated) {
				exactCanary = event
			}
		case coord.IntegrationSourceCleaned:
			if event.SourceWorktreeCleaned && event.SourceBranchCleaned {
				cleaned = event
			}
		}
	}
	if integrated != nil {
		result.CompletedSteps = append(result.CompletedSteps, "integration_recorded")
	} else {
		result.MissingEvidence = append(result.MissingEvidence, "integration_commit")
	}
	if exactCanary != nil {
		result.CompletedSteps = append(result.CompletedSteps, "exact_real_canary_passed")
	} else {
		result.MissingEvidence = append(result.MissingEvidence, "exact_real_canary_receipt")
	}
	if cleaned != nil {
		result.CompletedSteps = append(result.CompletedSteps, "source_cleanup_recorded")
	} else {
		result.MissingEvidence = append(result.MissingEvidence, "source_cleanup_receipt")
	}
	result.ReadyToClose = current == coord.IntegrationSourceCleaned && accepted && integrated != nil && exactCanary != nil && cleaned != nil
	result.NextArgv, result.NextCommand = candidateNextAction(payload, current, result)
	return candidateFacts{result: result, handoff: handoff, decision: decisionPtr, events: events}, nil
}

func exactRealCanary(event, integrated *coord.HandoffLifecycleEvent) bool {
	if event == nil || integrated == nil || event.CanaryExitCode == nil {
		return false
	}
	return event.State == coord.IntegrationCanaryPassed &&
		event.CanaryExecutionKind == "real" && event.CanaryResult == "PASS_REAL" && *event.CanaryExitCode == 0 && event.CanaryFailedCount == 0 &&
		event.CanaryTargetSHA == integrated.IntegrationCommit && event.CanaryTargetTree != "" &&
		event.CanaryHeadBefore == event.CanaryTargetSHA && event.CanaryHeadAfter == event.CanaryTargetSHA &&
		event.CanaryRefFingerprintBefore != "" && event.CanaryRefFingerprintBefore == event.CanaryRefFingerprintAfter
}

func candidateAllowedTransitions(current coord.IntegrationState) []string {
	switch current {
	case coord.IntegrationSubmitted:
		return []string{string(coord.IntegrationReviewing), string(coord.IntegrationAccepted), string(coord.IntegrationRejected)}
	case coord.IntegrationReviewing:
		return []string{string(coord.IntegrationAccepted), string(coord.IntegrationRejected)}
	case coord.IntegrationAccepted:
		return []string{string(coord.IntegrationIntegrated)}
	case coord.IntegrationIntegrated:
		return []string{string(coord.IntegrationCanaryRunning)}
	case coord.IntegrationCanaryRunning:
		return []string{string(coord.IntegrationCanaryPassed), string(coord.IntegrationCanaryMock), string(coord.IntegrationCanaryFailed), string(coord.IntegrationCanarySkipped), string(coord.IntegrationCanaryInvalid)}
	case coord.IntegrationCanaryMock, coord.IntegrationCanaryFailed, coord.IntegrationCanarySkipped, coord.IntegrationCanaryInvalid:
		return []string{string(coord.IntegrationCanaryRunning)}
	case coord.IntegrationCanaryPassed:
		return []string{string(coord.IntegrationSourceCleaned)}
	default:
		return []string{}
	}
}

func candidateNextAction(payload candidateClosePayload, current coord.IntegrationState, result candidateCloseResult) ([]string, string) {
	if result.ReadyToClose {
		return []string{}, ""
	}
	var argv []string
	switch current {
	case coord.IntegrationSubmitted, coord.IntegrationReviewing:
		body, _ := json.Marshal(map[string]string{"handoff_id": payload.HandoffID, "actor_session_id": payload.ActorSessionID})
		argv = []string{"omg", "handoff", "accept", "--idempotency-key", "ACCEPT_KEY", "--payload", string(body), "--json"}
	case coord.IntegrationAccepted:
		body, _ := json.Marshal(map[string]any{
			"id":                 "INTEGRATION_EVENT_ID",
			"handoff_id":         payload.HandoffID,
			"actor_session_id":   payload.ActorSessionID,
			"state":              string(coord.IntegrationIntegrated),
			"integration_commit": "INTEGRATION_COMMIT",
		})
		argv = []string{"omg", "handoff", "advance", "--idempotency-key", "INTEGRATION_EVENT_KEY", "--payload", string(body), "--json"}
	case coord.IntegrationIntegrated:
		argv = []string{"omg", "canary", "start", "--handoff", payload.HandoffID, "--session", payload.ActorSessionID, "--integration-ref", "INTEGRATION_REF", "--verification-command", "VERIFICATION_COMMAND", "--execution-kind", "real", "--environment-fingerprint", "ENVIRONMENT_FINGERPRINT", "--idempotency-key", "CANARY_START_KEY", "--json"}
	case coord.IntegrationCanaryRunning:
		argv = []string{"omg", "canary", "finish", "--canary", "CANARY_ID", "--session", payload.ActorSessionID, "--exit-code", "0", "--passed", "PASS_COUNT", "--failed", "0", "--skipped", "SKIP_COUNT", "--idempotency-key", "CANARY_FINISH_KEY", "--json"}
	case coord.IntegrationCanaryMock, coord.IntegrationCanaryFailed, coord.IntegrationCanarySkipped, coord.IntegrationCanaryInvalid:
		argv = []string{"omg", "canary", "start", "--handoff", payload.HandoffID, "--session", payload.ActorSessionID, "--integration-ref", "INTEGRATION_REF", "--verification-command", "VERIFICATION_COMMAND", "--execution-kind", "real", "--environment-fingerprint", "ENVIRONMENT_FINGERPRINT", "--idempotency-key", "CANARY_RETRY_KEY", "--json"}
	case coord.IntegrationCanaryPassed:
		body, _ := json.Marshal(map[string]any{
			"id":                      "SOURCE_CLEANUP_EVENT_ID",
			"handoff_id":              payload.HandoffID,
			"actor_session_id":        payload.ActorSessionID,
			"state":                   string(coord.IntegrationSourceCleaned),
			"source_worktree_cleaned": true,
			"source_branch_cleaned":   true,
			"note":                    "source Git cleanup completed after an advisory cleanup plan",
		})
		argv = []string{"omg", "handoff", "advance", "--idempotency-key", "SOURCE_CLEANUP_EVENT_KEY", "--payload", string(body), "--json"}
	case coord.IntegrationBlocked:
		argv = []string{"omg", "handoff", "lifecycle", "--payload", fmt.Sprintf(`{"handoff_id":%q}`, payload.HandoffID), "--json"}
	}
	if len(argv) == 0 {
		return []string{}, ""
	}
	return argv, formatCandidateCommand(argv)
}

func formatCandidateCommand(argv []string) string {
	encoded := make([]string, len(argv))
	for i, value := range argv {
		if value == "" {
			encoded[i] = "''"
			continue
		}
		quoted, _ := json.Marshal(value)
		encoded[i] = string(quoted)
	}
	return fmt.Sprintf("%s", joinCandidateArgs(encoded))
}

func joinCandidateArgs(values []string) string {
	if len(values) == 0 {
		return ""
	}
	result := values[0]
	for _, value := range values[1:] {
		result += " " + value
	}
	return result
}
