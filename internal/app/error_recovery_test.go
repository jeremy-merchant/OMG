package app

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/jeremy-merchant/oh-my-group/internal/app/foundation"
	"github.com/jeremy-merchant/oh-my-group/internal/domain"
	coord "github.com/jeremy-merchant/oh-my-group/internal/domain/coordination"
	"github.com/jeremy-merchant/oh-my-group/internal/domain/lineage"
	"github.com/jeremy-merchant/oh-my-group/internal/ports"
)

func TestStructuredRecoveryInvalidTaskTransitionSQLite(t *testing.T) {
	ctx, dispatcher, selection := lineageDispatcher(t)
	seedCandidateClose(t, ctx, dispatcher, selection, false)
	before := candidateReceiptCount(t, ctx, dispatcher, selection)

	outcome := dispatcher.Dispatch(ctx, projectRequest(selection, Request{
		Command:        "task.transition",
		IdempotencyKey: "structured-invalid-task-transition",
		Payload:        mustJSON(t, map[string]any{"task_id": "candidate-task", "state": string(lineage.TaskInProgress)}),
	}))
	if outcome.Error.Code != domain.CodeConflict || outcome.Error.Retryable {
		t.Fatalf("task transition error=%+v", outcome.Error)
	}
	assertStructuredDetail(t, outcome.Detail, "invalid_transition")
	if outcome.Detail.CurrentState != string(lineage.TaskWorkComplete) {
		t.Fatalf("current_state=%q", outcome.Detail.CurrentState)
	}
	if outcome.Detail.Entities.TaskID != "candidate-task" || !containsString(outcome.Detail.AllowedTransitions, string(lineage.TaskVerifiedDone)) {
		t.Fatalf("detail=%+v", outcome.Detail)
	}
	if after := candidateReceiptCount(t, ctx, dispatcher, selection); after != before {
		t.Fatalf("invalid transition created receipt: %d -> %d", before, after)
	}
}

func TestStructuredRecoveryInvalidHandoffTransitionSQLite(t *testing.T) {
	ctx, dispatcher, selection := lineageDispatcher(t)
	seedCandidateClose(t, ctx, dispatcher, selection, false)
	before := candidateReceiptCount(t, ctx, dispatcher, selection)

	outcome := dispatcher.Dispatch(ctx, projectRequest(selection, Request{
		Command:        "handoff.advance",
		IdempotencyKey: "structured-invalid-handoff-transition",
		Payload: mustJSON(t, map[string]any{
			"id": "structured-invalid-integration", "handoff_id": "candidate-handoff", "actor_session_id": "candidate-actor",
			"state": string(coord.IntegrationIntegrated), "integration_commit": "integration-commit",
		}),
	}))
	if outcome.Error.Code != domain.CodeInvalidArgument || outcome.Error.Retryable {
		t.Fatalf("handoff transition error=%+v", outcome.Error)
	}
	assertStructuredDetail(t, outcome.Detail, "invalid_transition")
	if outcome.Detail.CurrentState != string(coord.IntegrationSubmitted) || outcome.Detail.Entities.HandoffID != "candidate-handoff" || outcome.Detail.Entities.TaskID != "candidate-task" || outcome.Detail.Entities.ReservationID != "" {
		t.Fatalf("detail=%+v", outcome.Detail)
	}
	if len(outcome.Detail.AllowedTransitions) == 0 {
		t.Fatalf("allowed transitions missing: %+v", outcome.Detail)
	}
	if after := candidateReceiptCount(t, ctx, dispatcher, selection); after != before {
		t.Fatalf("invalid handoff transition created receipt: %d -> %d", before, after)
	}
}

func TestStructuredRecoveryMissingEvidenceSQLite(t *testing.T) {
	ctx, dispatcher, selection := lineageDispatcher(t)
	seedCandidateClose(t, ctx, dispatcher, selection, false)
	before := candidateReceiptCount(t, ctx, dispatcher, selection)

	outcome := dispatcher.Dispatch(ctx, projectRequest(selection, Request{
		Command:        "task.run-transition",
		IdempotencyKey: "structured-missing-evidence",
		Payload:        mustJSON(t, map[string]any{"run_id": "candidate-run", "state": string(lineage.RunVerifiedDone)}),
	}))
	if outcome.Error.Code != domain.CodeInvalidArgument || outcome.Error.Retryable {
		t.Fatalf("run transition error=%+v", outcome.Error)
	}
	assertStructuredDetail(t, outcome.Detail, "missing_evidence")
	if outcome.Detail.CurrentState != string(lineage.RunWorkComplete) || !containsString(outcome.Detail.MissingEvidence, "verification_evidence") {
		t.Fatalf("detail=%+v", outcome.Detail)
	}
	if outcome.Detail.Entities.RunID != "candidate-run" || outcome.Detail.Entities.TaskID != "candidate-task" || outcome.Detail.Entities.SessionID != "candidate-source" {
		t.Fatalf("entities=%+v", outcome.Detail.Entities)
	}
	if after := candidateReceiptCount(t, ctx, dispatcher, selection); after != before {
		t.Fatalf("missing evidence created receipt: %d -> %d", before, after)
	}
}

func TestStructuredRecoveryReservationConflictIncludesOwnerSQLite(t *testing.T) {
	ctx, dispatcher, selection := lineageDispatcher(t)
	seedCandidateClose(t, ctx, dispatcher, selection, false)
	mapped := dispatcher.service.WithCurrentStore(ctx, selection, func(resolved ports.ResolvedStore, store ports.Store) error {
		_, _, err := store.Write(ctx, domain.IdempotencyKey("seed-reservation-conflict-owner"), "test.write", func(repositories ports.Repositories) (domain.Result, error) {
			now := time.Date(2026, time.July, 30, 12, 30, 0, 0, time.UTC)
			coordination := repositories.Coordination()
			if err := coordination.CreateSession(ctx, lineage.AgentSession{
				ID: "reservation-owner-session", ProjectID: lineage.ID(resolved.Project), HumanID: "candidate-human", Kind: lineage.HumanDirect,
				Runtime: "test", Role: "worker", Source: lineage.SourceHuman, SourceRef: "test", RootID: "reservation-owner-session",
				TaskID: "reservation-owner-task", StartedAt: now,
			}); err != nil {
				return domain.Result{}, err
			}
			task, err := coordination.CreateTask(ctx, lineage.Task{
				ID: "reservation-owner-task", ProjectID: lineage.ID(resolved.Project), DisplayNumber: 2, Title: "reservation owner task",
				State: lineage.TaskReady, CreatedBySessionID: "reservation-owner-session", CreatedAt: now, UpdatedAt: now,
			})
			if err != nil {
				return domain.Result{}, err
			}
			if _, won, err := coordination.ClaimTask(ctx, task.ID, "reservation-owner-session", now); err != nil {
				return domain.Result{}, err
			} else if !won {
				return domain.Result{}, domain.NewError(domain.CodeConflict, "reservation owner task claim failed", false)
			}
			if err := coordination.CreateRun(ctx, lineage.TaskRun{
				ID: "reservation-owner-run", TaskID: task.ID, SessionID: "reservation-owner-session", State: lineage.RunRunning, StartedAt: now,
			}); err != nil {
				return domain.Result{}, err
			}
			return domain.Result{ID: "seed-reservation-conflict-owner", Outcome: domain.OutcomeOK}, nil
		})
		return err
	})
	if mapped.Code != "" {
		t.Fatalf("seed reservation conflict owner: %+v", mapped)
	}

	request := projectRequest(selection, Request{
		Command:        "reserve.add",
		IdempotencyKey: "structured-reservation-conflict",
		Payload: mustJSON(t, reserveAddPayload{
			ID: "conflicting-reservation", PatternKind: "exact", Pattern: "internal/candidate.go", CaseSensitivity: "sensitive", Mode: "exclusive",
			HumanID: "candidate-human", SessionID: "reservation-owner-session", TaskID: "reservation-owner-task", RunID: "reservation-owner-run", Intent: "conflicting edit", TTLSeconds: 3600,
		}),
	})
	strictDispatcher := NewDispatcherWithOptions(dispatcher.service, DispatcherOptions{StrictReservationConflicts: true})
	before := candidateReceiptCount(t, ctx, strictDispatcher, selection)
	outcome := strictDispatcher.Dispatch(ctx, request)
	if outcome.Error.Code != domain.CodeConflict || outcome.Error.Retryable {
		t.Fatalf("strict reservation conflict error=%+v", outcome.Error)
	}
	assertStructuredDetail(t, outcome.Detail, "reservation_conflict")
	if outcome.Detail.Entities.ReservationID != "conflicting-reservation" || outcome.Detail.Entities.TaskID != "reservation-owner-task" || outcome.Detail.Entities.RunID != "reservation-owner-run" || outcome.Detail.Entities.SessionID != "reservation-owner-session" {
		t.Fatalf("request entities=%+v", outcome.Detail.Entities)
	}
	if len(outcome.Detail.Conflicts) != 1 {
		t.Fatalf("conflicts=%+v", outcome.Detail.Conflicts)
	}
	conflict := outcome.Detail.Conflicts[0]
	if conflict.ReservationID != "candidate-reservation" || conflict.TaskID != "candidate-task" || conflict.RunID != "candidate-run" || conflict.SessionID != "candidate-source" {
		t.Fatalf("conflict owner=%+v", conflict)
	}
	if len(outcome.Detail.RecoveryActions) != 1 || outcome.Detail.RecoveryActions[0].Code != "inspect_reservations" {
		t.Fatalf("reservation recovery=%+v", outcome.Detail.RecoveryActions)
	}
	if after := candidateReceiptCount(t, ctx, strictDispatcher, selection); after != before {
		t.Fatalf("strict reservation conflict created receipt: %d -> %d", before, after)
	}

	advisory := request
	advisory.IdempotencyKey = "structured-reservation-advisory"
	advisoryOutcome := dispatcher.Dispatch(ctx, advisory)
	if advisoryOutcome.Error.Code != "" {
		t.Fatalf("default advisory reservation changed compatibility: %+v", advisoryOutcome)
	}
	result, ok := advisoryOutcome.Data.(ReservationMutationResult)
	if !ok || result.ReservationID != "conflicting-reservation" || len(result.Warnings) == 0 {
		t.Fatalf("default advisory result=%#v", advisoryOutcome.Data)
	}

	batchRequest := projectRequest(selection, Request{
		Command:        "reserve.batch-add",
		IdempotencyKey: "structured-reservation-batch-conflict",
		Payload: mustJSON(t, reserveBatchAddPayload{
			HumanID: "candidate-human", SessionID: "reservation-owner-session", TaskID: "reservation-owner-task", RunID: "reservation-owner-run",
			Items: []reserveBatchAddItemPayload{
				{ID: "batch-clean", PatternKind: "exact", Pattern: "internal/batch-clean.go", CaseSensitivity: "sensitive", Mode: "exclusive", Intent: "clean batch edit", TTLSeconds: 3600},
				{ID: "batch-conflict", PatternKind: "exact", Pattern: "internal/candidate.go", CaseSensitivity: "sensitive", Mode: "exclusive", Intent: "conflicting batch edit", TTLSeconds: 3600},
			},
		}),
	})
	beforeBatch := candidateReceiptCount(t, ctx, strictDispatcher, selection)
	batchOutcome := strictDispatcher.Dispatch(ctx, batchRequest)
	if batchOutcome.Error.Code != domain.CodeConflict || batchOutcome.Error.Retryable {
		t.Fatalf("strict batch conflict error=%+v", batchOutcome.Error)
	}
	assertStructuredDetail(t, batchOutcome.Detail, "reservation_conflict")
	if !conflictContainsReservation(batchOutcome.Detail.Conflicts, "candidate-reservation") {
		t.Fatalf("batch conflicts=%+v", batchOutcome.Detail.Conflicts)
	}
	if reservationExists(t, ctx, strictDispatcher, selection, "batch-clean") || reservationExists(t, ctx, strictDispatcher, selection, "batch-conflict") {
		t.Fatal("strict batch conflict persisted partial reservations")
	}
	if after := candidateReceiptCount(t, ctx, strictDispatcher, selection); after != beforeBatch {
		t.Fatalf("strict batch conflict created receipt: %d -> %d", beforeBatch, after)
	}

	advisoryBatch := batchRequest
	advisoryBatch.IdempotencyKey = "structured-reservation-batch-advisory"
	advisoryBatchOutcome := dispatcher.Dispatch(ctx, advisoryBatch)
	if advisoryBatchOutcome.Error.Code != "" {
		t.Fatalf("default advisory batch changed compatibility: %+v", advisoryBatchOutcome)
	}
	batchResult, ok := advisoryBatchOutcome.Data.(ReservationBatchMutationResult)
	if !ok || !reflect.DeepEqual(batchResult.ReservationIDs, []string{"batch-clean", "batch-conflict"}) || len(batchResult.Warnings) == 0 {
		t.Fatalf("default advisory batch result=%#v", advisoryBatchOutcome.Data)
	}
}

func conflictContainsReservation(values []ErrorEntities, reservationID string) bool {
	for _, value := range values {
		if value.ReservationID == reservationID {
			return true
		}
	}
	return false
}

func reservationExists(t *testing.T, ctx context.Context, dispatcher *ServiceDispatcher, selection foundation.Selection, reservationID string) bool {
	t.Helper()
	found := false
	mapped := dispatcher.service.WithReadOnlyCurrentStore(ctx, selection, func(resolved ports.ResolvedStore, store ports.Store) error {
		return store.Read(ctx, func(repositories ports.Repositories) error {
			_, found, _ = repositories.Reservations().Get(ctx, domain.ProjectID(resolved.Project), reservationID)
			return nil
		})
	})
	if mapped.Code != "" {
		t.Fatalf("read reservation %s: %+v", reservationID, mapped)
	}
	return found
}

func TestCandidateCloseReplayAndChangedPayloadConflictSQLite(t *testing.T) {
	ctx, dispatcher, selection := lineageDispatcher(t)
	seedCandidateClose(t, ctx, dispatcher, selection, true)
	before := candidateReceiptCount(t, ctx, dispatcher, selection)
	request := projectRequest(selection, candidateCloseRequest("structured-candidate-idempotency"))

	first := dispatcher.Dispatch(ctx, request)
	if first.Error.Code != "" {
		t.Fatalf("first candidate.close=%+v", first)
	}
	if after := candidateReceiptCount(t, ctx, dispatcher, selection); after != before+1 {
		t.Fatalf("first receipt count=%d want %d", after, before+1)
	}
	replay := dispatcher.Dispatch(ctx, request)
	if replay.Error.Code != "" || replay.Detail != nil || !reflect.DeepEqual(replay.Data, first.Data) {
		t.Fatalf("canonical replay=%+v first=%+v", replay, first)
	}

	var changed candidateClosePayload
	if err := json.Unmarshal(request.Payload, &changed); err != nil {
		t.Fatal(err)
	}
	changed.Evidence = "different closure evidence"
	conflictRequest := request
	conflictRequest.Payload = mustJSON(t, changed)
	conflict := dispatcher.Dispatch(ctx, conflictRequest)
	if conflict.Error.Code != domain.CodeConflict || conflict.Error.Retryable {
		t.Fatalf("changed payload error=%+v", conflict.Error)
	}
	assertStructuredDetail(t, conflict.Detail, "idempotency_conflict")
	if !conflict.Detail.Idempotency.Conflict || conflict.Detail.Idempotency.Replay || conflict.Detail.Idempotency.Key != request.IdempotencyKey {
		t.Fatalf("idempotency=%+v", conflict.Detail.Idempotency)
	}
	if after := candidateReceiptCount(t, ctx, dispatcher, selection); after != before+1 {
		t.Fatalf("conflict created receipt: %d", after)
	}
}

func TestCandidateCloseUnreadyRemainsReadOnlyWithConsistentSchema(t *testing.T) {
	ctx, dispatcher, selection := lineageDispatcher(t)
	seedCandidateClose(t, ctx, dispatcher, selection, false)
	before := candidateReceiptCount(t, ctx, dispatcher, selection)

	outcome := dispatcher.Dispatch(ctx, projectRequest(selection, candidateCloseRequest("structured-candidate-read-only")))
	if outcome.Error.Code != "" || outcome.Detail != nil {
		t.Fatalf("unready candidate.close=%+v", outcome)
	}
	result, ok := outcome.Data.(candidateCloseResult)
	if !ok || result.ReadyToClose || result.Closed || result.GitMutated {
		t.Fatalf("candidate result=%#v", outcome.Data)
	}
	if result.MissingEvidence == nil || result.AllowedTransitions == nil || result.NextArgv == nil {
		t.Fatalf("candidate success schema has nil arrays: %+v", result)
	}
	if after := candidateReceiptCount(t, ctx, dispatcher, selection); after != before {
		t.Fatalf("unready candidate created receipt: %d -> %d", before, after)
	}

	invalid := dispatcher.Dispatch(ctx, projectRequest(selection, Request{Command: "candidate.close", IdempotencyKey: "structured-candidate-invalid", Payload: []byte(`{}`)}))
	assertStructuredDetail(t, invalid.Detail, "payload_validation")
	if invalid.Detail.MissingEvidence == nil || invalid.Detail.AllowedTransitions == nil || invalid.Detail.RecoveryActions == nil || invalid.Detail.Conflicts == nil {
		t.Fatalf("candidate error schema has nil arrays: %+v", invalid.Detail)
	}
	if len(invalid.Detail.RecoveryActions) != 1 || !reflect.DeepEqual(invalid.Detail.RecoveryActions[0].Argv, []string{"omg", "candidate", "close", "--help"}) {
		t.Fatalf("candidate payload recovery=%+v", invalid.Detail.RecoveryActions)
	}
}

func TestStructuredRecoveryCanaryStartFinishAndRetryContextSQLite(t *testing.T) {
	ctx, dispatcher, selection := lineageDispatcher(t)
	seedCandidateClose(t, ctx, dispatcher, selection, true)
	before := candidateReceiptCount(t, ctx, dispatcher, selection)

	startRequest := projectRequest(selection, Request{
		Command:        "canary.start",
		IdempotencyKey: "structured-canary-retry",
		Payload: mustJSON(t, map[string]any{
			"handoff_id": "candidate-handoff", "actor_session_id": "candidate-actor", "integration_ref": "refs/heads/main",
			"verification_command": "go test ./...", "execution_kind": "real", "environment_fingerprint": "environment-fingerprint",
		}),
	})
	retryConflict := Outcome{Error: domain.NewError(domain.CodeConflict, "integration ref does not match recorded exact SHA", false)}
	dispatcher.enrichErrorOutcome(ctx, startRequest, &retryConflict)
	assertStructuredDetail(t, retryConflict.Detail, "exact_revision_conflict")
	if retryConflict.Detail.CurrentState != string(coord.IntegrationSourceCleaned) || retryConflict.Detail.Entities.HandoffID != "candidate-handoff" || retryConflict.Detail.Entities.CanaryRunID != "" {
		t.Fatalf("canary retry detail=%+v", retryConflict.Detail)
	}
	if len(retryConflict.Detail.RecoveryActions) != 2 || retryConflict.Detail.RecoveryActions[1].Code != "inspect_git_revision" {
		t.Fatalf("canary retry actions=%+v", retryConflict.Detail.RecoveryActions)
	}

	finishRequest := projectRequest(selection, Request{
		Command:        "canary.finish",
		IdempotencyKey: "structured-canary-finish",
		Payload: mustJSON(t, map[string]any{
			"canary_run_id": "candidate-canary", "actor_session_id": "candidate-actor", "exit_code": 0,
			"passed_count": 1, "failed_count": 0, "skipped_count": 0,
		}),
	})
	finishConflict := Outcome{Error: domain.NewError(domain.CodeInvalidArgument, "invalid handoff request", false)}
	dispatcher.enrichErrorOutcome(ctx, finishRequest, &finishConflict)
	assertStructuredDetail(t, finishConflict.Detail, "invalid_transition")
	if finishConflict.Detail.CurrentState != string(coord.IntegrationSourceCleaned) || finishConflict.Detail.Entities.CanaryRunID != "candidate-canary" || finishConflict.Detail.Entities.HandoffID != "candidate-handoff" || finishConflict.Detail.Entities.TaskID != "candidate-task" || finishConflict.Detail.Entities.RunID != "candidate-run" || finishConflict.Detail.Entities.SessionID != "candidate-source" {
		t.Fatalf("canary finish detail=%+v", finishConflict.Detail)
	}

	malformedFinish := dispatcher.Dispatch(ctx, projectRequest(selection, Request{
		Command:        "canary.finish",
		IdempotencyKey: "structured-canary-malformed-finish",
		Payload: mustJSON(t, map[string]any{
			"canary_run_id": "candidate-canary", "actor_session_id": "candidate-actor", "exit_code": "zero",
			"passed_count": 1, "failed_count": 0, "skipped_count": 0,
		}),
	}))
	assertStructuredDetail(t, malformedFinish.Detail, "payload_validation")
	if malformedFinish.Detail.CurrentState != string(coord.IntegrationSourceCleaned) {
		t.Fatalf("malformed canary finish lost canonical context: %+v", malformedFinish.Detail)
	}
	if after := candidateReceiptCount(t, ctx, dispatcher, selection); after != before {
		t.Fatalf("canary error enrichment created receipt: %d -> %d", before, after)
	}
}

func TestStructuredRecoveryCanaryVerifierUnavailable(t *testing.T) {
	ctx, dispatcher, selection := lineageDispatcher(t)
	seedCandidateClose(t, ctx, dispatcher, selection, true)
	before := candidateReceiptCount(t, ctx, dispatcher, selection)
	outcome := dispatcher.Dispatch(ctx, projectRequest(selection, Request{
		Command:        "canary.start",
		IdempotencyKey: "structured-canary-verifier-unavailable",
		Payload: mustJSON(t, map[string]any{
			"handoff_id": "candidate-handoff", "actor_session_id": "candidate-actor", "integration_ref": "refs/heads/main",
			"verification_command": "go test ./...", "execution_kind": "real", "environment_fingerprint": "environment-fingerprint",
		}),
	}))
	if outcome.Error.Code != domain.CodeUnavailable || !outcome.Error.Retryable {
		t.Fatalf("canary verifier error=%+v", outcome.Error)
	}
	assertStructuredDetail(t, outcome.Detail, "temporarily_unavailable")
	if outcome.Detail.CurrentState != string(coord.IntegrationSourceCleaned) || outcome.Detail.Entities.HandoffID != "candidate-handoff" {
		t.Fatalf("canary unavailable detail=%+v", outcome.Detail)
	}
	if after := candidateReceiptCount(t, ctx, dispatcher, selection); after != before {
		t.Fatalf("unavailable verifier created receipt: %d -> %d", before, after)
	}
}

func TestStructuredRecoveryDoesNotTreatTargetAsCurrentState(t *testing.T) {
	request := Request{
		Command: "task.transition", Project: "/project",
		Payload: mustJSON(t, map[string]any{"task_id": "task-1", "state": string(lineage.TaskInProgress)}),
	}
	outcome := Outcome{Error: domain.NewError(domain.CodeConflict, "invalid task transition", false)}
	prepareErrorOutcome(request, &outcome)
	assertStructuredDetail(t, outcome.Detail, "invalid_transition")
	if outcome.Detail.CurrentState != "" {
		t.Fatalf("target state was reported as current state: %+v", outcome.Detail)
	}
	if outcome.Detail.Entities.ReservationID != "" {
		t.Fatalf("generic id leaked as reservation entity: %+v", outcome.Detail.Entities)
	}
}

func TestStructuredRecoverySessionClassifications(t *testing.T) {
	for _, test := range []struct {
		message string
		reason  string
	}{
		{"session runtime is unobservable", "runtime_unobservable"},
		{"session is finished but unclosed", "finished_unclosed"},
	} {
		t.Run(test.reason, func(t *testing.T) {
			request := Request{Command: "session.heartbeat", Project: "/project", Payload: []byte(`{"session_id":"session-1"}`)}
			outcome := Outcome{Error: domain.NewError(domain.CodeConflict, test.message, false)}
			prepareErrorOutcome(request, &outcome)
			assertStructuredDetail(t, outcome.Detail, test.reason)
			if outcome.Detail.Entities.SessionID != "session-1" {
				t.Fatalf("entities=%+v", outcome.Detail.Entities)
			}
		})
	}
}

func projectRequest(selection foundation.Selection, request Request) Request {
	request.Version = RequestVersion
	request.Project = selection.Project
	return request
}

func assertStructuredDetail(t *testing.T, detail *ErrorDetail, reason string) {
	t.Helper()
	if detail == nil || detail.SchemaVersion != ErrorRecoverySchemaVersion || detail.ReasonCode != reason {
		t.Fatalf("detail=%+v want reason=%q", detail, reason)
	}
	if detail.RecoveryActions == nil || len(detail.RecoveryActions) == 0 {
		t.Fatalf("recovery actions missing: %+v", detail)
	}
	for _, action := range detail.RecoveryActions {
		if len(action.Argv) == 0 || action.Command == "" {
			t.Fatalf("action is not executable: %+v", action)
		}
		if action.GitMutation || action.ExecutesCanary || action.Dangerous {
			t.Fatalf("error recovery action has implicit side effects: %+v", action)
		}
	}
	if detail.GitMutation || detail.ExecutesCanary || detail.Dangerous {
		t.Fatalf("error detail has implicit side effects: %+v", detail)
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
