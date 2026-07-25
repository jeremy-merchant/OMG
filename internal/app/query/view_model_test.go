package query

import (
	"encoding/json"
	"testing"
)

func TestViewModelCopiesPayload(t *testing.T) {
	payload := json.RawMessage(`{"items":["one"]}`)
	view, err := NewViewModel("board", "cursor-1", payload)
	if err != nil {
		t.Fatal(err)
	}
	payload[0] = '['

	got, err := view.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"view_version":1,"kind":"board","snapshot_cursor":"cursor-1","data":{"items":["one"]}}` {
		t.Fatalf("unexpected view JSON: %s", got)
	}
}

func TestAbsenceValuesAreExplicit(t *testing.T) {
	if Unknown == Missing || Missing == NotApplicable || NotApplicable == Unknown {
		t.Fatalf("absence values must remain distinct")
	}
}
