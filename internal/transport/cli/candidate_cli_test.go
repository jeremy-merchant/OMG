package cli

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/jeremy-merchant/oh-my-group/internal/app"
	"github.com/jeremy-merchant/oh-my-group/internal/bootstrap"
)

func TestCandidateCloseCLIForwardsStrictApplicationRequest(t *testing.T) {
	dispatcher := &recordingDispatcher{outcome: app.Outcome{Data: map[string]any{"closed": false, "current_state": "SUBMITTED"}}}
	service := bootstrap.CLIService(bootstrap.Foundation())
	service.Dispatcher = dispatcher
	payload := `{"handoff_id":"handoff-1","actor_session_id":"actor-1","archive_event_id":"archive-1","evidence":"verification evidence"}`
	var output bytes.Buffer

	exit := RunWithApplication(context.Background(), []string{"candidate", "close", "--project", "/project", "--idempotency-key", "candidate-close-1", "--payload", payload, "--json"}, "test-version", strings.NewReader(""), &output, io.Discard, service)
	if exit != ExitSuccess {
		t.Fatalf("candidate close exit=%d output=%s", exit, output.String())
	}
	if len(dispatcher.requests) != 1 {
		t.Fatalf("candidate close requests=%+v", dispatcher.requests)
	}
	request := dispatcher.requests[0]
	if request.Command != "candidate.close" || request.Project != "/project" || request.IdempotencyKey != "candidate-close-1" || string(request.Payload) != payload {
		t.Fatalf("candidate close request=%+v", request)
	}
	if !strings.Contains(output.String(), `"closed":false`) || !strings.Contains(output.String(), `"current_state":"SUBMITTED"`) {
		t.Fatalf("candidate close output=%s", output.String())
	}
}
