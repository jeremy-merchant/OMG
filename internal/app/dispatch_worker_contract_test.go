package app

import (
	"testing"
	"time"

	"github.com/jeremy-merchant/oh-my-group/internal/domain"
)

func workerSetupContractPayload(t *testing.T) []byte {
	t.Helper()
	return mustJSON(t, map[string]any{
		"human_id": "human-1", "controller_session_id": "controller-1", "session_id": "worker-1",
		"runtime": "runtime-1", "role": "worker", "task_id": "task-1", "task_title": "Bounded change",
		"run_id": "run-1", "reservations": []any{},
	})
}

func TestWorkerSetupChangedPayloadProducesStructuredIdempotencyConflict(t *testing.T) {
	request := Request{Version: RequestVersion, Command: "worker.setup", Project: "/project", IdempotencyKey: "setup-key", Payload: workerSetupContractPayload(t)}
	outcome := Outcome{Error: domain.NewError(domain.CodeConflict, "idempotency key was reused with a different worker.setup payload", false)}
	prepareErrorOutcome(request, &outcome)
	if outcome.Detail == nil || outcome.Detail.ReasonCode != "idempotency_conflict" || outcome.Detail.Operation != "worker.setup" {
		t.Fatalf("detail = %+v", outcome.Detail)
	}
	if outcome.Detail.Entities.TaskID != "task-1" || outcome.Detail.Entities.RunID != "run-1" || outcome.Detail.Entities.SessionID != "worker-1" {
		t.Fatalf("entities = %+v", outcome.Detail.Entities)
	}
	if !outcome.Detail.Idempotency.Conflict || outcome.Detail.Idempotency.Key != "setup-key" || outcome.Detail.Idempotency.Replay {
		t.Fatalf("idempotency = %+v", outcome.Detail.Idempotency)
	}
}

func TestWorkerSetupReservationRecoveryCandidatesUseCommonOwner(t *testing.T) {
	payload := map[string]any{
		"human_id": "human-1", "controller_session_id": "controller-1", "session_id": "worker-1",
		"runtime": "runtime-1", "role": "worker", "task_id": "task-1", "task_title": "Bounded change", "run_id": "run-1",
		"reservations": []any{map[string]any{
			"id": "reservation-1", "pattern_kind": "exact", "pattern": "internal/app/setup.go",
			"case_sensitivity": "sensitive", "mode": "exclusive", "intent": "edit setup", "ttl_seconds": 3600,
		}},
	}
	candidates := reservationRecoveryCandidates(Request{Command: "worker.setup", Payload: mustJSON(t, payload)}, time.Date(2026, 7, 31, 10, 0, 0, 0, time.UTC))
	if len(candidates) != 1 {
		t.Fatalf("candidates = %#v", candidates)
	}
	candidate := candidates[0]
	if candidate.ID != "reservation-1" || candidate.Pattern.Value != "internal/app/setup.go" || candidate.Owner.HumanID != "human-1" || candidate.Owner.SessionID != "worker-1" || candidate.Owner.TaskID != "task-1" || candidate.Owner.RunID != "run-1" {
		t.Fatalf("candidate = %+v", candidate)
	}
}

func TestWorkerSetupPublicPayloadContract(t *testing.T) {
	valid := workerSetupContractPayload(t)
	if err := validatePublicPayload("worker.setup", valid); err.Code != "" {
		t.Fatalf("valid payload rejected: %+v", err)
	}
	missing := map[string]any{
		"human_id": "human-1", "controller_session_id": "controller-1", "session_id": "worker-1",
		"runtime": "runtime-1", "role": "worker", "task_id": "task-1", "task_title": "Bounded change", "run_id": "run-1",
	}
	if err := validatePublicPayload("worker.setup", mustJSON(t, missing)); err.Code != domain.CodeInvalidArgument {
		t.Fatalf("missing reservations error = %+v", err)
	}
}
