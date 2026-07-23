package app

import (
	"context"
	"encoding/json"
	"testing"

	"example.invalid/coordledger/internal/app/query"
	"example.invalid/coordledger/internal/ports"
)

func TestDispatchBoardQueryReturnsCanonicalViewModel(t *testing.T) {
	ctx, dispatcher, selection := lineageDispatcher(t)

	result := dispatcher.Dispatch(ctx, Request{
		Version: RequestVersion,
		Command: "board.query",
		Project: selection.Project,
		Payload: json.RawMessage(`{"mode":"all"}`),
	})
	if result.Error.Code != "" {
		t.Fatalf("board.query error=%+v", result.Error)
	}
	model, ok := result.Data.(query.ViewModel)
	if !ok || model.Kind() != "board" || model.Version() != query.ViewVersion {
		t.Fatalf("board.query model=%#v", result.Data)
	}
}

func TestDispatchBoardQueryUsesExistingReadOnlyStore(t *testing.T) {
	var opened []ports.OpenOptions
	opener := func(ctx context.Context, path string, options ports.OpenOptions) (ports.FoundationStore, ports.OpenStatus, error) {
		opened = append(opened, options)
		return lineageSQLiteOpener(ctx, path, options)
	}
	ctx, dispatcher, selection := lineageDispatcherWithOpener(t, opener)
	before := len(opened)

	result := dispatcher.Dispatch(ctx, Request{
		Version: RequestVersion,
		Command: "board.query",
		Project: selection.Project,
		Payload: json.RawMessage(`{"mode":"all"}`),
	})
	if result.Error.Code != "" {
		t.Fatalf("board.query error=%+v", result.Error)
	}
	if len(opened) != before+1 {
		t.Fatalf("query opened store %d times; want one", len(opened)-before)
	}
	options := opened[len(opened)-1]
	if !options.ReadOnly || !options.ExistingOnly {
		t.Fatalf("board.query open options = %+v; want existing read-only", options)
	}
}

func TestReadOnlyDispatchCommandsUseExistingReadOnlyStore(t *testing.T) {
	tests := []struct {
		name    string
		command string
		payload string
	}{
		{name: "receipt list", command: "receipt.list", payload: `{}`},
		{name: "progress history", command: "progress.history", payload: `{"task_id":"task"}`},
		{name: "dependency list", command: "dependency.list", payload: `{}`},
		{name: "handoff history", command: "handoff.history", payload: `{"task_id":"task"}`},
		{name: "human get", command: "human.get", payload: `{"id":"human"}`},
		{name: "reservation list", command: "reserve.list", payload: `{}`},
		{name: "git history", command: "git.history", payload: `{}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var opened []ports.OpenOptions
			opener := func(ctx context.Context, path string, options ports.OpenOptions) (ports.FoundationStore, ports.OpenStatus, error) {
				opened = append(opened, options)
				return lineageSQLiteOpener(ctx, path, options)
			}
			ctx, dispatcher, selection := lineageDispatcherWithOpener(t, opener)
			before := len(opened)

			dispatcher.Dispatch(ctx, Request{
				Version: RequestVersion,
				Command: test.command,
				Project: selection.Project,
				Payload: json.RawMessage(test.payload),
			})

			if len(opened) != before+1 {
				t.Fatalf("%s opened store %d times; want one", test.command, len(opened)-before)
			}
			options := opened[len(opened)-1]
			if !options.ReadOnly || !options.ExistingOnly {
				t.Fatalf("%s open options = %+v; want existing read-only", test.command, options)
			}
		})
	}
}

func TestDispatchBoardQueryRejectsMalformedSelector(t *testing.T) {
	ctx, dispatcher, selection := lineageDispatcher(t)
	result := dispatcher.Dispatch(ctx, Request{
		Version: RequestVersion,
		Command: "board.query",
		Project: selection.Project,
		Payload: json.RawMessage(`{"mode":"all","unknown":true}`),
	})
	if result.Error.Code != "invalid_argument" || result.Error.Message != "application request is invalid" {
		t.Fatalf("board.query error=%+v", result.Error)
	}
}
