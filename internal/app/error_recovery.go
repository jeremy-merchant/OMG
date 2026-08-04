package app

import (
	"encoding/json"
	"strings"

	"github.com/jeremy-merchant/oh-my-group/internal/domain"
)

const ErrorRecoverySchemaVersion = 1

// RecoveryAction is an advisory, explicit next step. Merely constructing or
// serializing an action never executes it.
type RecoveryAction struct {
	Code           string   `json:"code"`
	Description    string   `json:"description,omitempty"`
	Argv           []string `json:"argv"`
	Command        string   `json:"command"`
	GitMutation    bool     `json:"git_mutation"`
	ExecutesCanary bool     `json:"executes_canary"`
	Dangerous      bool     `json:"dangerous"`
}

// ErrorEntities contains only identifiers that are present and relevant to the
// failed request. It intentionally excludes free-form payload data.
type ErrorEntities struct {
	ProjectID     string `json:"project_id,omitempty"`
	TaskID        string `json:"task_id,omitempty"`
	RunID         string `json:"run_id,omitempty"`
	SessionID     string `json:"session_id,omitempty"`
	HandoffID     string `json:"handoff_id,omitempty"`
	CanaryRunID   string `json:"canary_run_id,omitempty"`
	ReservationID string `json:"reservation_id,omitempty"`
}

// IdempotencyRecovery describes whether the failure concerns replay or key
// ownership. A canonical replay is represented by a successful Outcome, not an
// error with Replay=true.
type IdempotencyRecovery struct {
	Key      string `json:"key,omitempty"`
	Replay   bool   `json:"replay"`
	Conflict bool   `json:"conflict"`
	Guidance string `json:"guidance,omitempty"`
}

// ErrorDetail is the transport-neutral structured recovery contract. The
// legacy DomainError remains the stable compatibility surface while ReasonCode
// provides a more specific, stable machine classification.
type ErrorDetail struct {
	SchemaVersion      int                 `json:"schema_version"`
	ReasonCode         string              `json:"reason_code"`
	Operation          string              `json:"operation,omitempty"`
	Cause              string              `json:"cause,omitempty"`
	CurrentState       string              `json:"current_state,omitempty"`
	MissingEvidence    []string            `json:"missing_evidence"`
	Prerequisites      []string            `json:"prerequisites"`
	AllowedTransitions []string            `json:"allowed_transitions"`
	RecoveryActions    []RecoveryAction    `json:"recovery_actions"`
	Entities           ErrorEntities       `json:"entities"`
	Conflicts          []ErrorEntities     `json:"conflicts"`
	Idempotency        IdempotencyRecovery `json:"idempotency"`
	GitMutation        bool                `json:"git_mutation"`
	ExecutesCanary     bool                `json:"executes_canary"`
	Dangerous          bool                `json:"dangerous"`
}

func newErrorDetail(operation, reasonCode string) *ErrorDetail {
	return &ErrorDetail{
		SchemaVersion:      ErrorRecoverySchemaVersion,
		ReasonCode:         reasonCode,
		Operation:          operation,
		MissingEvidence:    []string{},
		Prerequisites:      []string{},
		AllowedTransitions: []string{},
		RecoveryActions:    []RecoveryAction{},
		Conflicts:          []ErrorEntities{},
	}
}

// NewErrorDetail converts a legacy DomainError into the common structured
// recovery contract without consulting a store. Transports use it for
// validation failures that happen before dispatcher execution.
func NewErrorDetail(request Request, err domain.DomainError) *ErrorDetail {
	if err.Code == "" {
		return nil
	}
	detail := errorDetailFor(request, err)
	normalizeErrorDetail(detail, request)
	return detail
}

func prepareErrorOutcome(request Request, result *Outcome) {
	if result == nil || result.Error.Code == "" {
		return
	}
	if result.Detail == nil {
		result.Detail = NewErrorDetail(request, result.Error)
	}
	normalizeErrorDetail(result.Detail, request)
}

func normalizeErrorDetail(detail *ErrorDetail, request Request) {
	if detail == nil {
		return
	}
	if detail.SchemaVersion == 0 {
		detail.SchemaVersion = ErrorRecoverySchemaVersion
	}
	if detail.Operation == "" {
		detail.Operation = request.Command
	}
	if detail.MissingEvidence == nil {
		detail.MissingEvidence = []string{}
	}
	if detail.Prerequisites == nil {
		detail.Prerequisites = []string{}
	}
	if detail.AllowedTransitions == nil {
		detail.AllowedTransitions = []string{}
	}
	if detail.RecoveryActions == nil {
		detail.RecoveryActions = []RecoveryAction{}
	}
	if detail.Conflicts == nil {
		detail.Conflicts = []ErrorEntities{}
	}
	for i := range detail.RecoveryActions {
		if detail.RecoveryActions[i].Argv == nil {
			detail.RecoveryActions[i].Argv = []string{}
		}
		if detail.RecoveryActions[i].Command == "" && len(detail.RecoveryActions[i].Argv) != 0 {
			detail.RecoveryActions[i].Command = formatRecoveryCommand(detail.RecoveryActions[i].Argv)
		}
	}
	if detail.Entities.ProjectID == "" {
		detail.Entities.ProjectID = request.Project
	}
	if detail.Idempotency.Key == "" && detail.Idempotency.Conflict {
		detail.Idempotency.Key = request.IdempotencyKey
	}
}

func errorDetailFor(request Request, err domain.DomainError) *ErrorDetail {
	payload := recoveryPayload(request.Payload)
	reason := classifyErrorReason(request, payload, err)
	detail := newErrorDetail(request.Command, reason)
	detail.Cause = err.Message
	detail.CurrentState = firstPayloadString(payload, "current_state", "from_state")
	detail.Entities = ErrorEntities{
		ProjectID:     request.Project,
		TaskID:        firstPayloadString(payload, "task_id", "dependent_task_id", "prerequisite_task_id"),
		RunID:         firstPayloadString(payload, "run_id"),
		SessionID:     firstPayloadString(payload, "session_id", "actor_session_id", "source_session_id", "owner_session_id", "new_owner_session_id"),
		HandoffID:     firstPayloadString(payload, "handoff_id"),
		CanaryRunID:   firstPayloadString(payload, "canary_run_id"),
		ReservationID: reservationEntityID(request.Command, payload),
	}

	switch reason {
	case "payload_validation":
		detail.Prerequisites = []string{"provide a payload that matches the public command contract"}
		detail.RecoveryActions = append(detail.RecoveryActions, exampleAction(request.Command))
	case "invalid_transition":
		applyTransitionRecovery(detail, request, payload)
	case "missing_evidence":
		applyEvidenceRecovery(detail, request, payload)
	case "reservation_conflict":
		detail.Prerequisites = []string{"inspect active reservations and coordinate with the current owner before retrying"}
		detail.RecoveryActions = append(detail.RecoveryActions, readOnlyAction("inspect_reservations", "Inspect active reservations without changing them.", append(selectionArgv(request, "reserve", "active"), "--json")...))
	case "exact_revision_conflict":
		detail.Prerequisites = []string{"the integration ref must still resolve to the exact recorded integration commit before a real canary can start or retry"}
		detail.RecoveryActions = append(detail.RecoveryActions,
			inspectionAction(request, detail.Entities),
			readOnlyAction("inspect_git_revision", "Inspect the current integration ref without recording a new inventory.", append(selectionArgv(request, "git", "current"), "--json")...),
		)
	case "idempotency_conflict":
		detail.Idempotency = IdempotencyRecovery{
			Key:      request.IdempotencyKey,
			Conflict: true,
			Guidance: "Do not reuse this key for different input or a different operation. Inspect the canonical receipt, then retry changed intent with a new key.",
		}
		detail.Prerequisites = []string{"confirm whether the canonical receipt already represents the intended mutation"}
		detail.RecoveryActions = append(detail.RecoveryActions, readOnlyAction("inspect_receipts", "Inspect canonical receipts before choosing a new key.", append(selectionArgv(request, "receipt", "list"), "--json")...))
	case "runtime_unobservable", "finished_unclosed":
		detail.Prerequisites = []string{"inspect session classification before changing lifecycle state"}
		detail.RecoveryActions = append(detail.RecoveryActions, readOnlyAction("inspect_session_hygiene", "Inspect runtime-unobservable and finished-unclosed sessions.", append(selectionArgv(request, "stale"), "--json")...))
	case "entity_not_found":
		detail.RecoveryActions = append(detail.RecoveryActions, inspectionAction(request, detail.Entities))
	case "temporarily_unavailable":
		detail.RecoveryActions = append(detail.RecoveryActions, readOnlyAction("retry_preflight", "Re-check current store and lifecycle health before retrying.", append(selectionArgv(request, "preflight"), "--json")...))
	default:
		detail.RecoveryActions = append(detail.RecoveryActions, inspectionAction(request, detail.Entities))
	}
	return detail
}

func classifyErrorReason(request Request, payload map[string]any, err domain.DomainError) string {
	message := strings.ToLower(err.Message)
	targetState := firstPayloadString(payload, "to_state", "state")
	switch {
	case strings.Contains(message, "idempotency"):
		return "idempotency_conflict"
	case strings.Contains(message, "integration ref") && strings.Contains(message, "exact sha"):
		return "exact_revision_conflict"
	case strings.Contains(message, "reservation") && strings.Contains(message, "conflict"):
		return "reservation_conflict"
	case strings.Contains(message, "runtime") && strings.Contains(message, "unobservable"):
		return "runtime_unobservable"
	case strings.Contains(message, "finished") && strings.Contains(message, "unclosed"):
		return "finished_unclosed"
	case targetState == "VERIFIED_DONE" && firstPayloadString(payload, "evidence", "verification_evidence") == "":
		return "missing_evidence"
	case err.Code != domain.CodeInvalidArgument && strings.Contains(message, "evidence"):
		return "missing_evidence"
	case strings.Contains(message, "transition") || strings.Contains(message, "cannot transition") || strings.Contains(message, "must be") || (err.Code == domain.CodeConflict && lifecycleTransitionCommand(request.Command)) || (err.Code == domain.CodeInvalidArgument && request.Command == "handoff.advance" && completeKnownHandoffTransition(payload)):
		return "invalid_transition"
	case err.Code == domain.CodeInvalidArgument:
		return "payload_validation"
	case err.Code == domain.CodeNotFound:
		return "entity_not_found"
	case err.Code == domain.CodeUnavailable || err.Retryable:
		return "temporarily_unavailable"
	case err.Code == domain.CodeConflict:
		return "state_conflict"
	case err.Code == domain.CodeInternal:
		return "internal_failure"
	default:
		return string(err.Code)
	}
}

func completeKnownHandoffTransition(payload map[string]any) bool {
	if firstPayloadString(payload, "id") == "" || firstPayloadString(payload, "handoff_id") == "" || firstPayloadString(payload, "actor_session_id") == "" {
		return false
	}
	switch firstPayloadString(payload, "state") {
	case "SUBMITTED", "REVIEWING", "INTEGRATED", "CANARY_RUNNING", "CANARY_PASSED", "CANARY_MOCK_PASSED", "CANARY_FAILED", "CANARY_SKIPPED", "CANARY_INVALIDATED", "SOURCE_CLEANED", "BLOCKED":
		return true
	default:
		return false
	}
}

func lifecycleTransitionCommand(command string) bool {
	switch command {
	case "handoff.advance", "canary.start", "canary.finish", "task.transition", "task.run-transition", "candidate.close":
		return true
	default:
		return false
	}
}

func applyTransitionRecovery(detail *ErrorDetail, request Request, payload map[string]any) {
	target := firstPayloadString(payload, "to_state", "state")
	if target != "" {
		detail.Prerequisites = append(detail.Prerequisites, "the current entity state must allow transition to "+target)
	}
	switch request.Command {
	case "handoff.advance":
		detail.AllowedTransitions = integrationAllowedTransitions(detail.CurrentState)
		detail.RecoveryActions = append(detail.RecoveryActions, inspectionAction(request, detail.Entities))
	case "task.transition":
		detail.AllowedTransitions = taskAllowedTransitions(detail.CurrentState)
		detail.RecoveryActions = append(detail.RecoveryActions, inspectionAction(request, detail.Entities))
	case "task.run-transition":
		detail.AllowedTransitions = runAllowedTransitions(detail.CurrentState)
		detail.RecoveryActions = append(detail.RecoveryActions, inspectionAction(request, detail.Entities))
	case "canary.start", "canary.finish":
		detail.AllowedTransitions = canaryAllowedTransitions(detail.CurrentState)
		detail.RecoveryActions = append(detail.RecoveryActions, inspectionAction(request, detail.Entities))
	default:
		detail.RecoveryActions = append(detail.RecoveryActions, inspectionAction(request, detail.Entities))
	}
}

func applyEvidenceRecovery(detail *ErrorDetail, request Request, payload map[string]any) {
	switch request.Command {
	case "task.transition", "task.run-transition", "candidate.close":
		detail.MissingEvidence = append(detail.MissingEvidence, "verification_evidence")
		detail.Prerequisites = append(detail.Prerequisites, "provide non-empty verification evidence for VERIFIED_DONE")
	default:
		detail.MissingEvidence = append(detail.MissingEvidence, "required_evidence")
	}
	detail.RecoveryActions = append(detail.RecoveryActions, inspectionAction(request, detail.Entities))
}

func recoveryPayload(raw json.RawMessage) map[string]any {
	var payload map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &payload) != nil {
		return map[string]any{}
	}
	return payload
}

func firstPayloadString(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := payload[key].(string); ok && value != "" {
			return value
		}
	}
	return ""
}

func reservationEntityID(command string, payload map[string]any) string {
	if value := firstPayloadString(payload, "reservation_id"); value != "" {
		return value
	}
	if strings.HasPrefix(command, "reserve.") {
		return firstPayloadString(payload, "id")
	}
	return ""
}

func exampleAction(command string) RecoveryAction {
	argv := []string{"omg"}
	for _, segment := range strings.Split(command, ".") {
		if segment != "" {
			argv = append(argv, segment)
		}
	}
	argv = append(argv, "--help")
	return readOnlyAction("inspect_command_help", "Inspect contextual command help and the public payload contract before retrying.", argv...)
}

func inspectionAction(request Request, entities ErrorEntities) RecoveryAction {
	switch {
	case entities.HandoffID != "":
		payload, _ := json.Marshal(map[string]string{"handoff_id": entities.HandoffID})
		return readOnlyAction("inspect_handoff_lifecycle", "Inspect the handoff lifecycle before choosing a transition.", append(selectionArgv(request, "handoff", "lifecycle"), "--payload", string(payload), "--json")...)
	case entities.TaskID != "":
		return readOnlyAction("inspect_task", "Inspect current task and run state before retrying.", append(selectionArgv(request, "board", "task"), "--task", entities.TaskID, "--json")...)
	case entities.SessionID != "":
		return readOnlyAction("inspect_session", "Inspect current session classification before retrying.", append(selectionArgv(request, "board", "me"), "--session", entities.SessionID, "--json")...)
	default:
		return readOnlyAction("inspect_preflight", "Inspect current project lifecycle state before retrying.", append(selectionArgv(request, "preflight"), "--json")...)
	}
}

func readOnlyAction(code, description string, argv ...string) RecoveryAction {
	return RecoveryAction{Code: code, Description: description, Argv: argv, Command: formatRecoveryCommand(argv)}
}

func selectionArgv(request Request, command ...string) []string {
	argv := append([]string{"omg"}, command...)
	switch {
	case request.Project != "":
		argv = append(argv, "--project", request.Project)
	case request.Workspace != "":
		argv = append(argv, "--workspace", request.Workspace)
	case request.Store != "":
		argv = append(argv, "--store", request.Store)
	}
	return argv
}

func formatRecoveryCommand(argv []string) string {
	encoded := make([]string, len(argv))
	for i, value := range argv {
		if value == "" {
			encoded[i] = "''"
			continue
		}
		quoted, _ := json.Marshal(value)
		encoded[i] = string(quoted)
	}
	return strings.Join(encoded, " ")
}

func integrationAllowedTransitions(current string) []string {
	switch current {
	case "SUBMITTED":
		return []string{"REVIEWING", "ACCEPTED", "REJECTED", "BLOCKED"}
	case "REVIEWING":
		return []string{"ACCEPTED", "REJECTED", "BLOCKED"}
	case "ACCEPTED":
		return []string{"INTEGRATED", "BLOCKED"}
	case "INTEGRATED":
		return []string{"CANARY_RUNNING", "BLOCKED"}
	case "CANARY_RUNNING":
		return []string{"CANARY_PASSED", "CANARY_MOCK_PASSED", "CANARY_FAILED", "CANARY_SKIPPED", "CANARY_INVALIDATED", "BLOCKED"}
	case "CANARY_MOCK_PASSED", "CANARY_FAILED", "CANARY_SKIPPED", "CANARY_INVALIDATED":
		return []string{"CANARY_RUNNING", "BLOCKED"}
	case "CANARY_PASSED":
		return []string{"SOURCE_CLEANED", "BLOCKED"}
	default:
		return []string{}
	}
}

func canaryAllowedTransitions(current string) []string {
	if current == "" {
		return []string{"CANARY_RUNNING", "CANARY_PASSED", "CANARY_MOCK_PASSED", "CANARY_FAILED", "CANARY_SKIPPED", "CANARY_INVALIDATED"}
	}
	return integrationAllowedTransitions(current)
}

func taskAllowedTransitions(current string) []string {
	switch current {
	case "READY":
		return []string{"CLAIMED", "BLOCKED", "CANCELLED"}
	case "CLAIMED", "IN_PROGRESS", "WAITING", "BLOCKED", "REWORK", "INTERRUPTED", "STALE":
		return []string{"READY", "IN_PROGRESS", "WAITING", "BLOCKED", "REWORK", "WORK_COMPLETE", "VERIFIED_DONE", "FAILED", "ABANDONED", "CANCELLED"}
	case "WORK_COMPLETE":
		return []string{"VERIFIED_DONE"}
	default:
		return []string{}
	}
}

func runAllowedTransitions(current string) []string {
	switch current {
	case "RUNNING", "WAITING", "BLOCKED", "REWORK", "INTERRUPTED", "STALE", "WORK_COMPLETE":
		return []string{"RUNNING", "WAITING", "BLOCKED", "REWORK", "WORK_COMPLETE", "VERIFIED_DONE", "INTERRUPTED", "STALE", "FAILED", "ABANDONED", "CANCELLED"}
	default:
		return []string{}
	}
}
