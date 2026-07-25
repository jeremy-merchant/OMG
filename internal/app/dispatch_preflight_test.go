package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jeremy-merchant/OMG/internal/app/query"
)

func TestDispatchPreflightQueryReturnsCanonicalProjection(t *testing.T) {
	ctx, dispatcher, selection := lineageDispatcher(t)

	payload, err := json.Marshal(PreflightRequest{})
	if err != nil {
		t.Fatal(err)
	}
	result := dispatcher.Dispatch(ctx, Request{
		Version: RequestVersion,
		Command: "preflight.query",
		Project: selection.Project,
		Payload: payload,
	})
	if result.Error.Code != "" {
		t.Fatalf("preflight.query error=%+v", result.Error)
	}
	preflight, ok := result.Data.(PreflightView)
	if !ok {
		t.Fatalf("preflight.query result=%T; want PreflightView", result.Data)
	}
	if !preflight.Initialized || preflight.PendingMigrations != 0 || preflight.Identity != nil {
		t.Fatalf("preflight state=%+v", preflight)
	}
	if preflight.Sessions == nil || preflight.Tasks == nil || preflight.Inbox == nil || preflight.Dependencies == nil || preflight.Reservations == nil || preflight.Warnings == nil || preflight.SuggestedActions == nil {
		t.Fatalf("preflight must return canonical non-nil collections: %+v", preflight)
	}

	boardPayload, err := json.Marshal(query.BoardRequest{Mode: query.BoardAll})
	if err != nil {
		t.Fatal(err)
	}
	board := dispatcher.Dispatch(ctx, Request{Version: RequestVersion, Command: "board.query", Project: selection.Project, Payload: boardPayload})
	if board.Error.Code != "" {
		t.Fatalf("board.query error=%+v", board.Error)
	}
	model := board.Data.(query.ViewModel)
	var snapshot query.BoardSnapshot
	if err := json.Unmarshal(model.Data(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(preflight.Sessions) != len(snapshot.Sessions) || len(preflight.Tasks) != len(snapshot.Tasks) || len(preflight.Inbox) != len(snapshot.Inbox) || len(preflight.Dependencies) != len(snapshot.Dependencies) || len(preflight.Reservations) != len(snapshot.Reservations) {
		t.Fatalf("preflight projection differs from canonical board: preflight=%+v board=%+v", preflight, snapshot)
	}
}

func TestDispatchPreflightQueryUsesOnlyExplicitSessionSelection(t *testing.T) {
	ctx, dispatcher, selection := lineageDispatcher(t)
	if err := seedCheckpointRefresh(ctx, dispatcher, selection); err != nil {
		t.Fatal(err)
	}

	payload, err := json.Marshal(PreflightRequest{SessionID: "checkpoint-session"})
	if err != nil {
		t.Fatal(err)
	}
	selected := dispatcher.Dispatch(ctx, Request{Version: RequestVersion, Command: "preflight.query", Project: selection.Project, Payload: payload})
	if selected.Error.Code != "" {
		t.Fatalf("selected preflight.query error=%+v", selected.Error)
	}
	view := selected.Data.(PreflightView)
	if view.Identity == nil || view.Identity.ID != "checkpoint-session" {
		t.Fatalf("selected preflight identity=%+v", view.Identity)
	}

	unselected := dispatcher.Dispatch(ctx, Request{Version: RequestVersion, Command: "preflight.query", Project: selection.Project, Payload: json.RawMessage(`{}`)})
	if unselected.Error.Code != "" {
		t.Fatalf("unselected preflight.query error=%+v", unselected.Error)
	}
	withoutSelection := unselected.Data.(PreflightView)
	if withoutSelection.Identity != nil || len(withoutSelection.Sessions) != 2 {
		t.Fatalf("unselected preflight must not guess identity: %+v", withoutSelection)
	}
}
func TestDispatchPreflightQueryRejectsUnknownPayloadFields(t *testing.T) {
	ctx, dispatcher, selection := lineageDispatcher(t)
	result := dispatcher.Dispatch(ctx, Request{
		Version: RequestVersion,
		Command: "preflight.query",
		Project: selection.Project,
		Payload: json.RawMessage(`{"unexpected":true}`),
	})
	if result.Error.Code == "" {
		t.Fatal("preflight.query accepted unknown payload field")
	}
}

func TestDispatchPreflightQueryRejectsNilContext(t *testing.T) {
	_, dispatcher, selection := lineageDispatcher(t)
	result := dispatcher.Dispatch(nil, Request{
		Version: RequestVersion,
		Command: "preflight.query",
		Project: selection.Project,
		Payload: json.RawMessage(`{}`),
	})
	if result.Error.Code == "" {
		t.Fatal("preflight.query accepted nil context")
	}
}

func TestDispatchPreflightQueryTerminatesCanceledWork(t *testing.T) {
	_, dispatcher, selection := lineageDispatcher(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result := dispatcher.Dispatch(ctx, Request{
		Version: RequestVersion,
		Command: "preflight.query",
		Project: selection.Project,
		Payload: json.RawMessage(`{}`),
	})
	if result.Error.Code == "" || result.Data != nil {
		t.Fatalf("canceled preflight result = %#v", result)
	}
}
