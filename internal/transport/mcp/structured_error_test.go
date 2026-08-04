package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/jeremy-merchant/oh-my-group/internal/app"
	"github.com/jeremy-merchant/oh-my-group/internal/domain"
)

func TestMCPPayloadValidationPreservesStructuredRecovery(t *testing.T) {
	project, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	dispatcher := fakeDispatcher(func(context.Context, app.Request) app.Outcome {
		calls++
		return app.Outcome{}
	})
	invalidProject := project + string(filepath.Separator) + "."
	params := `{"name":"omg","arguments":{"request":{"version":1,"command":"canary.finish","project":` + strconv.Quote(invalidProject) + `,"payload":{}}}}`
	responses, serveErr := serve(t, context.Background(), rpcRequest(`"invalid"`, "tools/call", params), dispatcher)
	if serveErr != nil || len(responses) != 1 || responses[0].Error == nil || responses[0].Error.Code != invalidParamsCode || calls != 0 {
		t.Fatalf("responses=%+v calls=%d err=%v", responses, calls, serveErr)
	}
	encoded, err := json.Marshal(responses[0].Error.Data)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range [][]byte{
		[]byte(`"details"`), []byte(`"schema_version":1`), []byte(`"reason_code":"payload_validation"`),
		[]byte(`"operation":"canary.finish"`), []byte(`"recovery_actions"`), []byte(`"argv"`),
		[]byte(`"git_mutation":false`), []byte(`"executes_canary":false`), []byte(`"dangerous":false`),
	} {
		if !bytes.Contains(encoded, want) {
			t.Errorf("MCP validation error missing %s: %s", want, encoded)
		}
	}
}

func TestCandidateCloseAllowlistPreservesStructuredError(t *testing.T) {
	calls := 0
	detail := &app.ErrorDetail{
		SchemaVersion:      app.ErrorRecoverySchemaVersion,
		ReasonCode:         "invalid_transition",
		Operation:          "candidate.close",
		Cause:              "candidate lifecycle changed before closure",
		CurrentState:       "CANARY_RUNNING",
		MissingEvidence:    []string{"exact_real_canary_receipt"},
		Prerequisites:      []string{"record an exact real canary result before closure"},
		AllowedTransitions: []string{"CANARY_PASSED", "CANARY_FAILED"},
		RecoveryActions: []app.RecoveryAction{{
			Code: "inspect_handoff_lifecycle", Argv: []string{"omg", "handoff", "lifecycle", "--json"},
			Command: `"omg" "handoff" "lifecycle" "--json"`,
		}},
		Entities:  app.ErrorEntities{ProjectID: "/project", HandoffID: "handoff-1", TaskID: "task-1", RunID: "run-1", SessionID: "session-1"},
		Conflicts: []app.ErrorEntities{},
	}
	dispatcher := fakeDispatcher(func(_ context.Context, request app.Request) app.Outcome {
		calls++
		if request.Command != "candidate.close" {
			t.Fatalf("command=%q", request.Command)
		}
		return app.Outcome{Error: domain.NewError(domain.CodeConflict, "candidate lifecycle changed before closure", false), Detail: detail}
	})

	responses, err := serve(t, context.Background(), rpcRequest(`"call"`, "tools/call", toolRequest("candidate.close")), dispatcher)
	if err != nil || len(responses) != 1 || responses[0].Error != nil || calls != 1 {
		t.Fatalf("responses=%+v calls=%d err=%v", responses, calls, err)
	}
	result := responses[0].Result
	for _, want := range [][]byte{
		[]byte(`"structuredContent":{"ok":false,"error":{"code":"conflict","message":"candidate lifecycle changed before closure","retryable":false,"details":`),
		[]byte(`"schema_version":1`),
		[]byte(`"reason_code":"invalid_transition"`),
		[]byte(`"current_state":"CANARY_RUNNING"`),
		[]byte(`"missing_evidence":["exact_real_canary_receipt"]`),
		[]byte(`"argv":["omg","handoff","lifecycle","--json"]`),
		[]byte(`"handoff_id":"handoff-1"`),
		[]byte(`"git_mutation":false`),
		[]byte(`"executes_canary":false`),
		[]byte(`"dangerous":false`),
	} {
		if !bytes.Contains(result, want) {
			t.Errorf("MCP structured error missing %s: %s", want, result)
		}
	}
}
