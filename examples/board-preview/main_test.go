package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jeremy-merchant/OMG/internal/app/query"
	"github.com/jeremy-merchant/OMG/internal/view"
)

func TestParsePreviewFormat(t *testing.T) {
	tests := []struct {
		input     string
		format    view.Format
		extension string
	}{
		{"html", view.FormatHTML, "html"},
		{"TTY", view.FormatTTY, "txt"},
		{"markdown", view.FormatMarkdown, "md"},
		{"md", view.FormatMarkdown, "md"},
		{"json", view.FormatJSON, "json"},
	}
	for _, test := range tests {
		format, extension, err := parsePreviewFormat(test.input)
		if err != nil {
			t.Fatalf("parsePreviewFormat(%q): %v", test.input, err)
		}
		if format != test.format || extension != test.extension {
			t.Errorf("parsePreviewFormat(%q) = %q, %q; want %q, %q", test.input, format, extension, test.format, test.extension)
		}
	}
	if _, _, err := parsePreviewFormat("pdf"); err == nil || !strings.Contains(err.Error(), "unsupported format") {
		t.Fatalf("unsupported format error = %v", err)
	}
}

func TestPreviewRendersEverySupportedPresentationToStdout(t *testing.T) {
	model, err := previewViewModel()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		format view.Format
		want   []string
	}{
		{view.FormatHTML, []string{"<!doctype html>", `class="signal-deck"`, "Clear the constraint before work moves.", "omg board task --task task-preview"}},
		{view.FormatTTY, []string{"OMG  OPERATOR LEDGER", "WORK GRAPH", "Review </td><script>globalThis.pwned=1</script> & 협업", "omg board task --task task-preview"}},
		{view.FormatMarkdown, []string{"# Board", "## Current tasks/runs", "globalThis.pwned=1", "omg board task --task task-preview"}},
	}
	for _, test := range tests {
		var output bytes.Buffer
		if err := writePreview("-", test.format, model, &output); err != nil {
			t.Fatalf("writePreview(%q): %v", test.format, err)
		}
		for _, want := range test.want {
			if !strings.Contains(output.String(), want) {
				t.Errorf("%s preview missing %q:\n%s", test.format, want, output.String())
			}
		}
	}

	var output bytes.Buffer
	if err := writePreview("-", view.FormatJSON, model, &output); err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		ViewVersion    int                 `json:"view_version"`
		Kind           string              `json:"kind"`
		SnapshotCursor string              `json:"snapshot_cursor"`
		Data           query.BoardSnapshot `json:"data"`
	}
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatalf("JSON preview is invalid: %v\n%s", err, output.String())
	}
	if decoded.ViewVersion != query.ViewVersion || decoded.Kind != "board" || decoded.SnapshotCursor != "preview-cursor" ||
		decoded.Data.ProjectID != "preview-project" || len(decoded.Data.SuggestedActions) != 3 {
		t.Fatalf("unexpected JSON preview: %+v", decoded)
	}
}

func TestPreviewFileIsOwnerOnlyAndNeverOverwritten(t *testing.T) {
	model, err := previewViewModel()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "preview.html")
	if err := writePreview(path, view.FormatHTML, model, nil); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("preview mode = %#o, want owner-only", info.Mode().Perm())
	}
	if err := writePreview(path, view.FormatHTML, model, nil); err == nil || !strings.Contains(err.Error(), "create output") {
		t.Fatalf("second write error = %v, want no-overwrite failure", err)
	}
}

func TestPreviewFixtureUsesARealSafeBoardCommand(t *testing.T) {
	snapshot := previewSnapshot()
	if len(snapshot.SuggestedActions) != 3 {
		t.Fatalf("suggested actions = %+v", snapshot.SuggestedActions)
	}
	want := map[string]string{
		"inspect-task":        "omg board task --task task-preview",
		"reservation_history": `omg reserve history --payload '{"reservation_id":"reservation-preview"}'`,
		"git_cleanup_plan":    "omg git cleanup-plan",
	}
	for _, action := range snapshot.SuggestedActions {
		command, ok := want[action.Code]
		if !ok || action.Command != command {
			t.Errorf("suggested action = %+v", action)
		}
		if strings.Contains(action.Command, "task show") || strings.Contains(action.Command, "--reservation ") {
			t.Errorf("preview suggests an unsupported command: %q", action.Command)
		}
	}
	payload, err := json.Marshal(snapshot.Progress)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"👩‍💻", "ㄱ", "🇰🇷"} {
		if !strings.Contains(string(payload), value) {
			t.Errorf("preview progress fixture missing grapheme %q", value)
		}
	}
}

func TestWritePreviewRequiresStdoutForDash(t *testing.T) {
	model, err := previewViewModel()
	if err != nil {
		t.Fatal(err)
	}
	if err := writePreview("-", view.FormatTTY, model, nil); err == nil || !strings.Contains(err.Error(), "stdout is unavailable") {
		t.Fatalf("stdout error = %v", err)
	}
}
