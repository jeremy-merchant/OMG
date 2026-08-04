package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/jeremy-merchant/oh-my-group/internal/app"
	"github.com/jeremy-merchant/oh-my-group/internal/bootstrap"
)

func TestWorkerSetupRoutesOneStrictPayloadToApplicationDispatcher(t *testing.T) {
	dispatcher := &recordingDispatcher{outcome: app.Outcome{Data: map[string]any{"run_id": "run-1", "reservation_ids": []string{}}}}
	service := bootstrap.CLIService(bootstrap.Foundation())
	service.Dispatcher = dispatcher
	payload := `{"human_id":"human-1","controller_session_id":"controller-1","session_id":"worker-1","runtime":"runtime-1","role":"worker","task_id":"task-1","task_title":"Bounded change","run_id":"run-1","reservations":[]}`
	var output bytes.Buffer
	exit := RunWithApplication(context.Background(), []string{"worker", "setup", "--project", "/project", "--idempotency-key", "setup-1", "--payload", payload, "--json"}, "test-version", strings.NewReader(""), &output, io.Discard, service)
	if exit != ExitSuccess || len(dispatcher.requests) != 1 {
		t.Fatalf("exit=%d requests=%+v output=%s", exit, dispatcher.requests, output.String())
	}
	request := dispatcher.requests[0]
	if request.Command != "worker.setup" || request.IdempotencyKey != "setup-1" || request.Project != "/project" {
		t.Fatalf("request=%+v", request)
	}
	var decoded map[string]any
	if err := json.Unmarshal(request.Payload, &decoded); err != nil || decoded["session_id"] != "worker-1" || decoded["reservations"] == nil {
		t.Fatalf("payload=%s decoded=%#v err=%v", request.Payload, decoded, err)
	}
}

func TestWorkerSetupRejectsBootstrapIdentityFlags(t *testing.T) {
	dispatcher := &recordingDispatcher{}
	service := bootstrap.CLIService(bootstrap.Foundation())
	service.Dispatcher = dispatcher
	var output bytes.Buffer
	exit := RunWithApplication(context.Background(), []string{"worker", "setup", "--project", "/project", "--session", "worker-1", "--idempotency-key", "setup-1", "--payload", `{}`, "--json"}, "test-version", strings.NewReader(""), &output, io.Discard, service)
	if exit != ExitUsage || len(dispatcher.requests) != 0 {
		t.Fatalf("exit=%d requests=%+v output=%s", exit, dispatcher.requests, output.String())
	}
}

func TestWorkerSetupHelpAndExampleExposeAtomicContract(t *testing.T) {
	exit, help := run(t, "worker", "setup", "--help")
	if exit != ExitSuccess {
		t.Fatalf("help exit=%d output=%s", exit, help)
	}
	for _, want := range []string{"one transaction", "0-128 initial reservations", "omg worker setup", "--payload-file"} {
		if !strings.Contains(help, want) {
			t.Errorf("help missing %q:\n%s", want, help)
		}
	}
	exit, example := run(t, "example", "show", "worker-setup", "--json")
	if exit != ExitSuccess || !strings.Contains(example, `"command":"omg worker setup"`) || !strings.Contains(example, `"controller_session_id"`) || !strings.Contains(example, `"reservations"`) {
		t.Fatalf("example exit=%d output=%s", exit, example)
	}
}
