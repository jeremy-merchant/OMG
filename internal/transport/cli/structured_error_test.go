package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jeremy-merchant/oh-my-group/internal/app"
	"github.com/jeremy-merchant/oh-my-group/internal/domain"
)

func TestStructuredErrorJSONPreservesMachineRecovery(t *testing.T) {
	detail := structuredErrorFixture()
	var output bytes.Buffer
	exit := writeErrorWithContextAndDetail(&output, true, domain.NewError(domain.CodeConflict, "invalid task transition", false), terminalErrorContext{}, detail)
	if exit != ExitConflict {
		t.Fatalf("exit=%d output=%s", exit, output.String())
	}
	var envelope ErrorEnvelope
	if err := json.Unmarshal(output.Bytes(), &envelope); err != nil {
		t.Fatalf("decode structured error: %v; output=%s", err, output.String())
	}
	if envelope.Error.Code != string(domain.CodeConflict) || envelope.Error.Message != "invalid task transition" || envelope.Error.Retryable || envelope.Error.ExitCode != ExitConflict {
		t.Fatalf("legacy fields=%+v", envelope.Error)
	}
	if envelope.Error.Details == nil || envelope.Error.Details.ReasonCode != "invalid_transition" || envelope.Error.Details.CurrentState != "WORK_COMPLETE" {
		t.Fatalf("details=%+v", envelope.Error.Details)
	}
	if len(envelope.Error.Details.RecoveryActions) != 1 || len(envelope.Error.Details.RecoveryActions[0].Argv) == 0 || envelope.Error.Details.RecoveryActions[0].Command == "" {
		t.Fatalf("recovery action=%+v", envelope.Error.Details.RecoveryActions)
	}
	if envelope.Error.Recovery == nil || envelope.Error.Recovery.NextCommand != detail.RecoveryActions[0].Command {
		t.Fatalf("legacy recovery=%+v", envelope.Error.Recovery)
	}
	if len(envelope.Error.Details.Conflicts) != 1 || envelope.Error.Details.Conflicts[0].SessionID != "owner-session" {
		t.Fatalf("conflicts=%+v", envelope.Error.Details.Conflicts)
	}
}

func TestStructuredErrorHumanOutputIsReadableAndComplete(t *testing.T) {
	detail := structuredErrorFixture()
	var output bytes.Buffer
	exit := writeErrorWithContextAndDetail(&output, false, domain.NewError(domain.CodeConflict, "invalid task transition", false), terminalErrorContext{}, detail)
	if exit != ExitConflict {
		t.Fatalf("exit=%d", exit)
	}
	rendered := output.String()
	for _, want := range []string{
		"invalid task transition",
		"invalid_transition",
		"WORK_COMPLETE",
		"task=task-1",
		"session=owner-session",
		"VERIFIED_DONE",
		"inspect_task",
		detail.RecoveryActions[0].Command,
		"git_mutation=false",
		"executes_canary=false",
		"dangerous=false",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("human output missing %q:\n%s", want, rendered)
		}
	}
}

func TestCLIValidationErrorUsesStructuredRecovery(t *testing.T) {
	request := Request{
		Name: "canary", Subcommand: "finish", JSON: true, Project: "/project", SessionID: "actor-session",
		CanaryRunID: "canary-1", IdempotencyKey: "canary-finish-key",
	}
	var output bytes.Buffer
	exit := writeInvalidRequest(&output, request, "canary finish request is invalid")
	if exit != ExitUsage {
		t.Fatalf("exit=%d output=%s", exit, output.String())
	}
	var envelope ErrorEnvelope
	if err := json.Unmarshal(output.Bytes(), &envelope); err != nil {
		t.Fatalf("decode validation error: %v; output=%s", err, output.String())
	}
	detail := envelope.Error.Details
	if detail == nil || detail.ReasonCode != "payload_validation" || detail.Operation != "canary.finish" {
		t.Fatalf("details=%+v", detail)
	}
	if detail.Entities.ProjectID != "/project" || detail.Entities.SessionID != "actor-session" || detail.Entities.CanaryRunID != "canary-1" {
		t.Fatalf("entities=%+v", detail.Entities)
	}
	if len(detail.RecoveryActions) != 1 || detail.RecoveryActions[0].Code != "inspect_command_help" || len(detail.RecoveryActions[0].Argv) == 0 || detail.RecoveryActions[0].Command == "" {
		t.Fatalf("actions=%+v", detail.RecoveryActions)
	}
	if detail.RecoveryActions[0].GitMutation || detail.RecoveryActions[0].ExecutesCanary || detail.RecoveryActions[0].Dangerous {
		t.Fatalf("validation recovery has side effects: %+v", detail.RecoveryActions[0])
	}
}

func structuredErrorFixture() *app.ErrorDetail {
	action := app.RecoveryAction{
		Code:        "inspect_task",
		Description: "Inspect current task and run state before retrying.",
		Argv:        []string{"omg", "board", "task", "--project", "/project", "--task", "task-1", "--json"},
		Command:     `"omg" "board" "task" "--project" "/project" "--task" "task-1" "--json"`,
	}
	return &app.ErrorDetail{
		SchemaVersion:      app.ErrorRecoverySchemaVersion,
		ReasonCode:         "invalid_transition",
		Operation:          "task.transition",
		Cause:              "invalid task transition",
		CurrentState:       "WORK_COMPLETE",
		MissingEvidence:    []string{},
		Prerequisites:      []string{"the current entity state must allow transition to IN_PROGRESS"},
		AllowedTransitions: []string{"VERIFIED_DONE"},
		RecoveryActions:    []app.RecoveryAction{action},
		Entities:           app.ErrorEntities{ProjectID: "/project", TaskID: "task-1"},
		Conflicts:          []app.ErrorEntities{{ProjectID: "/project", ReservationID: "reservation-1", SessionID: "owner-session"}},
	}
}
