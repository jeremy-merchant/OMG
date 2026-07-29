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
