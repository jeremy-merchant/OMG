package mcp

import (
	"encoding/json"
	"testing"

	"github.com/jeremy-merchant/oh-my-group/internal/app"
)

func TestReceiptCommandsRequireSafeQueryShape(t *testing.T) {
	for _, request := range []app.Request{
		{Version: app.RequestVersion, Command: "receipt.get", Project: "/project", Payload: json.RawMessage(`{"id":"receipt-1"}`)},
		{Version: app.RequestVersion, Command: "receipt.list", Project: "/project", Payload: json.RawMessage(`{}`)},
	} {
		if !validRequest(request) {
			t.Fatalf("validRequest(%+v) = false", request)
		}
	}
	if validRequest(app.Request{Version: app.RequestVersion, Command: "receipt.get", Project: "/project", IdempotencyKey: "must-not-be-present", Payload: json.RawMessage(`{"id":"receipt-1"}`)}) {
		t.Fatal("keyed receipt query accepted")
	}
	if validRequest(app.Request{Version: app.RequestVersion, Command: "receipt.list", Project: "/project", Payload: json.RawMessage(`{"raw":"request"}`)}) {
		t.Fatal("receipt list accepted non-empty payload")
	}
}
