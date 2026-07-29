package app

import (
	"strings"
	"testing"
)

func TestMessageInboxPayloadErrorsNameExactRecovery(t *testing.T) {
	for _, test := range []struct {
		name    string
		payload string
		want    string
	}{
		{name: "legacy singular selector", payload: `{"recipient_session_id":"worker-1"}`, want: "unknown field recipient_session_id; expected recipient.session_id"},
		{name: "missing recipient", payload: `{}`, want: "missing required field recipient; expected object"},
		{name: "wrong nested type", payload: `{"recipient":{"session_id":7}}`, want: "field recipient.session_id has type number; expected string"},
		{name: "multiple selectors", payload: `{"recipient":{"session_id":"worker-1","role":"worker"}}`, want: "exactly one recipient selector is required"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validatePublicPayload("message.inbox", []byte(test.payload))
			if err.Code == "" || !strings.Contains(err.Message, test.want) {
				t.Fatalf("error=%+v, want %q", err, test.want)
			}
		})
	}
	if err := validatePublicPayload("message.inbox", []byte(`{"recipient":{"session_id":"worker-1"}}`)); err.Code != "" {
		t.Fatalf("valid inbox payload rejected: %+v", err)
	}
}

func TestTaskLifecyclePayloadContractsRejectMissingFieldsPrecisely(t *testing.T) {
	for _, test := range []struct {
		command string
		payload string
		want    string
	}{
		{command: "task.create", payload: `{"title":"work"}`, want: "missing required field created_by_session_id; expected string"},
		{command: "task.get", payload: `{}`, want: "missing required field task_id; expected string"},
		{command: "task.claim", payload: `{"task_id":"task-1"}`, want: "missing required field session_id; expected string"},
		{command: "task.transition", payload: `{"task_id":"task-1"}`, want: "missing required field state; expected string"},
		{command: "task.run-create", payload: `{"task_id":"task-1"}`, want: "missing required field session_id; expected string"},
		{command: "task.run-transition", payload: `{"run_id":"run-1"}`, want: "missing required field state; expected string"},
		{command: "task.finish-lite", payload: `{"task_id":"task-1","run_id":"run-1","session_id":"session-1","actor_session_id":"session-1","archive_event_id":"archive-1"}`, want: "missing required field evidence; expected string"},
	} {
		t.Run(test.command, func(t *testing.T) {
			err := validatePublicPayload(test.command, []byte(test.payload))
			if err.Code == "" || err.Message != test.want {
				t.Fatalf("error=%+v, want %q", err, test.want)
			}
		})
	}
}
