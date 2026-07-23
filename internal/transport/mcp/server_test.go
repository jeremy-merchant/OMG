package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"example.invalid/coordledger/internal/app"
	"example.invalid/coordledger/internal/domain"
)

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error"`
}
type fakeDispatcher func(context.Context, app.Request) app.Outcome

func (f fakeDispatcher) Dispatch(ctx context.Context, request app.Request) app.Outcome {
	return f(ctx, request)
}

func serve(t *testing.T, ctx context.Context, input string, dispatcher app.Dispatcher) ([]rpcResponse, error) {
	t.Helper()
	var out bytes.Buffer
	err := Serve(ctx, strings.NewReader(input), &out, "test-version", dispatcher)
	if strings.TrimSpace(out.String()) == "" {
		return nil, err
	}
	var responses []rpcResponse
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var response rpcResponse
		if decodeErr := json.Unmarshal([]byte(line), &response); decodeErr != nil {
			t.Fatalf("invalid response %q: %v", line, decodeErr)
		}
		responses = append(responses, response)
	}
	return responses, err
}
func rpcRequest(id, method, params string) string {
	return `{"jsonrpc":"2.0","id":` + id + `,"method":"` + method + `","params":` + params + "}\n"
}
func toolRequest(command string) string {
	project, err := filepath.Abs(".")
	if err != nil {
		panic(err)
	}
	return `{"name":"omg","arguments":{"request":{"version":1,"command":"` + command + `","project":` + strconv.Quote(project) + `,"idempotency_key":"key","payload":{"title":"task","created_by_session_id":"session"}}}}`
}

func TestServeInitializeAndToolsAdvertiseTypedRequestSchema(t *testing.T) {
	responses, err := serve(t, context.Background(), rpcRequest(`"init"`, "initialize", `{}`)+rpcRequest("1", "tools/list", `{}`), nil)
	if err != nil || len(responses) != 2 || responses[0].Error != nil || responses[1].Error != nil || !bytes.Contains(responses[1].Result, []byte(`"request"`)) || !bytes.Contains(responses[1].Result, []byte(`"required":["version","command","project","payload"]`)) || !bytes.Contains(responses[1].Result, []byte(`"preflight.query"`)) || !bytes.Contains(responses[1].Result, []byte(`"session_id"`)) || bytes.Contains(responses[1].Result, []byte(`"args"`)) {
		t.Fatalf("responses=%+v err=%v", responses, err)
	}
}
func TestServeDispatchesTypedRequestAndReturnsStructuredOutcome(t *testing.T) {
	var calls atomic.Int32
	dispatcher := fakeDispatcher(func(_ context.Context, request app.Request) app.Outcome {
		calls.Add(1)
		if request.Command != "task.create" || request.Version != app.RequestVersion {
			t.Fatalf("request=%+v", request)
		}
		return app.Outcome{Data: map[string]string{"id": "task-1"}}
	})
	responses, err := serve(t, context.Background(), rpcRequest(`"call"`, "tools/call", toolRequest("task.create")), dispatcher)
	if err != nil || len(responses) != 1 || responses[0].Error != nil || calls.Load() != 1 || !bytes.Contains(responses[0].Result, []byte(`"structuredContent":{"ok":true,"data":{"id":"task-1"}}`)) || bytes.Contains(responses[0].Result, []byte(`"text"`)) {
		t.Fatalf("responses=%+v calls=%d err=%v", responses, calls.Load(), err)
	}
}

func TestServeProjectsRedactedReservationPatternsWithoutTextFallback(t *testing.T) {
	project, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	dispatcher := fakeDispatcher(func(_ context.Context, request app.Request) app.Outcome {
		if request.Command != "reserve.active" {
			t.Fatalf("command=%q", request.Command)
		}
		return app.Outcome{Data: []app.ReservationResult{{ID: "reservation-secret", Pattern: "[REDACTED:abcd]", Kind: "exact", Mode: "exclusive"}}}
	})
	input := `{"name":"omg","arguments":{"request":{"version":1,"command":"reserve.active","project":` + strconv.Quote(project) + `,"payload":{}}}}`
	responses, err := serve(t, context.Background(), rpcRequest(`"reservations"`, "tools/call", input), dispatcher)
	if err != nil || len(responses) != 1 || responses[0].Error != nil || !bytes.Contains(responses[0].Result, []byte(`"pattern":"[REDACTED:abcd]"`)) || bytes.Contains(responses[0].Result, []byte(`api_key=release`)) || bytes.Contains(responses[0].Result, []byte(`"text"`)) {
		t.Fatalf("responses=%+v err=%v", responses, err)
	}
}

func TestServeDispatchesStrictPreflightApplicationQuery(t *testing.T) {
	project, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	var got app.Request
	dispatcher := fakeDispatcher(func(_ context.Context, request app.Request) app.Outcome {
		got = request
		return app.Outcome{Data: map[string]any{"initialized": false, "sessions": []any{}}}
	})
	input := `{"name":"omg","arguments":{"request":{"version":1,"command":"preflight.query","project":` + strconv.Quote(project) + `,"payload":{"session_id":"selected-session"}}}}`
	responses, err := serve(t, context.Background(), rpcRequest(`"preflight"`, "tools/call", input), dispatcher)
	if err != nil || len(responses) != 1 || responses[0].Error != nil {
		t.Fatalf("responses=%+v err=%v", responses, err)
	}
	if got.Command != "preflight.query" || got.IdempotencyKey != "" || string(got.Payload) != `{"session_id":"selected-session"}` {
		t.Fatalf("preflight request=%+v", got)
	}
	if !bytes.Contains(responses[0].Result, []byte(`"initialized":false`)) {
		t.Fatalf("preflight result=%s", responses[0].Result)
	}
}
func TestServeRejectsMalformedUnknownAndInvalidIDs(t *testing.T) {
	input := "{bad}\n" + `{"jsonrpc":"2.0","id":1,"method":"tools/list","extra":true}` + "\n" + `{"jsonrpc":"2.0","id":true,"method":"tools/list"}` + "\n" + `{"jsonrpc":"2.0","id":null,"method":"tools/list"}` + "\n"
	responses, err := serve(t, context.Background(), input, nil)
	if err != nil || len(responses) != 4 || responses[0].Error.Code != parseErrorCode || responses[1].Error.Code != invalidRequestCode || responses[2].Error.Code != invalidRequestCode || responses[3].Error.Code != invalidRequestCode {
		t.Fatalf("responses=%+v err=%v", responses, err)
	}
}

func TestServeRejectsHostileFramesWithoutDispatching(t *testing.T) {
	var calls atomic.Int32
	dispatcher := fakeDispatcher(func(context.Context, app.Request) app.Outcome {
		calls.Add(1)
		return app.Outcome{}
	})
	deepPayload := strings.Repeat(`{"nested":`, 64) + `"leaf"` + strings.Repeat(`}`, 64)
	deep := `{"jsonrpc":"2.0","id":"deep","method":"tools/call","params":{"name":"omg","arguments":{"request":{"version":1,"command":"task.create","idempotency_key":"deep-key","payload":` + deepPayload + `}}}}` + "\n"
	unknown := `{"jsonrpc":"2.0","id":"unknown","method":"tools/call","params":{"name":"omg","arguments":{"request":{"version":1,"command":"task.create","idempotency_key":"unknown-key","payload":{"title":"task","created_by_session_id":"session"},"untrusted":true}}}}` + "\n"
	oversized := strings.Repeat("x", maxFrameSize+1) + "\n"

	responses, err := serve(t, context.Background(), deep+unknown+oversized, dispatcher)
	if err != nil || len(responses) != 3 || responses[0].Error == nil || responses[0].Error.Code != invalidParamsCode || responses[1].Error == nil || responses[1].Error.Code != invalidParamsCode || responses[2].Error == nil || responses[2].Error.Code != parseErrorCode || calls.Load() != 0 {
		t.Fatalf("responses=%+v calls=%d err=%v", responses, calls.Load(), err)
	}
}

func TestServeRejectsOmittedRelativeOrUncleanProjectWithoutDispatching(t *testing.T) {
	projectRoot, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	dispatcher := fakeDispatcher(func(context.Context, app.Request) app.Outcome {
		calls.Add(1)
		return app.Outcome{}
	})
	for _, project := range []string{"", "relative", projectRoot + string(filepath.Separator) + "."} {
		request := `{"name":"omg","arguments":{"request":{"version":1,"command":"task.create","project":` + strconv.Quote(project) + `,"idempotency_key":"key","payload":{"title":"task","created_by_session_id":"session"}}}}`
		responses, err := serve(t, context.Background(), rpcRequest(`"call"`, "tools/call", request), dispatcher)
		if err != nil || len(responses) != 1 || responses[0].Error == nil || responses[0].Error.Code != invalidParamsCode {
			t.Fatalf("project=%q responses=%+v err=%v", project, responses, err)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("invalid projects reached dispatcher: calls=%d", calls.Load())
	}
}

func TestServeRecoversAfterOversizedFrames(t *testing.T) {
	var calls atomic.Int32
	dispatcher := fakeDispatcher(func(_ context.Context, request app.Request) app.Outcome {
		calls.Add(1)
		if request.Command != "task.create" {
			t.Fatalf("command=%q", request.Command)
		}
		return app.Outcome{Data: map[string]string{"id": "task-1"}}
	})
	oversized := strings.Repeat("x", maxFrameSize+1) + "\n"

	responses, err := serve(t, context.Background(), oversized+oversized+rpcRequest(`"call"`, "tools/call", toolRequest("task.create")), dispatcher)
	if err != nil || len(responses) != 3 || responses[0].Error == nil || responses[0].Error.Code != parseErrorCode || responses[1].Error == nil || responses[1].Error.Code != parseErrorCode || responses[2].Error != nil || calls.Load() != 1 || !bytes.Contains(responses[2].Result, []byte(`"id":"task-1"`)) {
		t.Fatalf("responses=%+v calls=%d err=%v", responses, calls.Load(), err)
	}
}

func TestServeAcceptsFinalFrameAtEOF(t *testing.T) {
	input := strings.TrimSuffix(rpcRequest("1", "tools/list", `{}`), "\n")

	responses, err := serve(t, context.Background(), input, nil)
	if err != nil || len(responses) != 1 || responses[0].Error != nil {
		t.Fatalf("responses=%+v err=%v", responses, err)
	}
}
func TestServeSerializesApplicationErrorsWithoutCLIOutput(t *testing.T) {
	dispatcher := fakeDispatcher(func(context.Context, app.Request) app.Outcome {
		return app.Outcome{Error: domain.NewError(domain.CodeConflict, "conflict", false)}
	})
	responses, err := serve(t, context.Background(), rpcRequest("2", "tools/call", toolRequest("message.send")), dispatcher)
	if err != nil || len(responses) != 1 || responses[0].Error != nil || !bytes.Contains(responses[0].Result, []byte(`"isError":true`)) || !bytes.Contains(responses[0].Result, []byte(`"code":"conflict"`)) || bytes.Contains(responses[0].Result, []byte(`"text"`)) {
		t.Fatalf("responses=%+v err=%v", responses, err)
	}
}
func TestServeBoundsFramesAndStructuredOutput(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":"` + strings.Repeat("x", maxFrameSize) + `"}` + "\n"
	responses, err := serve(t, context.Background(), input, nil)
	if err != nil || len(responses) != 1 || responses[0].Error == nil || responses[0].Error.Code != parseErrorCode {
		t.Fatalf("responses=%+v err=%v", responses, err)
	}
	oversized := fakeDispatcher(func(context.Context, app.Request) app.Outcome {
		return app.Outcome{Data: strings.Repeat("x", maxOutputSize)}
	})
	responses, err = serve(t, context.Background(), rpcRequest("2", "tools/call", toolRequest("handoff.create")), oversized)
	if err != nil || len(responses) != 1 || responses[0].Error == nil || responses[0].Error.Code != internalErrorCode {
		t.Fatalf("responses=%+v err=%v", responses, err)
	}
}
func TestServePassesCancellationToDispatcher(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dispatcher := fakeDispatcher(func(got context.Context, _ app.Request) app.Outcome {
		if got.Err() != context.Canceled {
			t.Fatalf("context error=%v", got.Err())
		}
		return app.Outcome{}
	})
	responses, err := serve(t, ctx, rpcRequest("1", "tools/call", toolRequest("task.create")), dispatcher)
	if err != nil || len(responses) != 1 || responses[0].Error != nil {
		t.Fatalf("responses=%+v err=%v", responses, err)
	}
}
