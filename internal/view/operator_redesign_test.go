package view

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestOperatorTTYKeepsSemanticHierarchyWithAndWithoutANSI(t *testing.T) {
	model := viewModel(t, `{"schema_version":1,"view_version":1,"mode":"all","project_id":"프로젝트-1","snapshot_cursor":"cursor-1","scope":{"project_id":"프로젝트-1","mode":"all"},"identity":{"id":"session-abcdefghijklmnopqrstuvwxyz-0123456789","kind":"agent","role":"검증자","runtime":"native","liveness":"alive","native_access_state":"available"},"sessions":[],"tasks":[{"id":"task-1","display_number":1,"title":"한글 작업 상태 확인","state":"active"}],"runs":[],"progress":[],"dependencies":[{"id":"dep-1","dependent_task_id":"task-1","blocker_task_id":"task-2","type":"blocks","unblock_on":"verified_done","satisfied":false}],"inbox":[],"handoffs":[],"reservations":[],"warnings":[],"suggested_actions":[]}`)
	board, err := decodeBoard(model)
	if err != nil {
		t.Fatal(err)
	}

	plain := renderTTY(board, false)
	colored := renderTTY(board, true)
	for _, output := range []string{plain, colored} {
		for _, want := range []string{"OMG", "OPERATOR LEDGER", "NOW", "SESSIONS", "WORK GRAPH", "⦸ BLOCKED", "프로젝트-1", "한글 작업 상태 확인"} {
			if !strings.Contains(output, want) {
				t.Errorf("TTY output missing %q:\n%s", want, output)
			}
		}
		if !strings.Contains(output, shortID("session-abcdefghijklmnopqrstuvwxyz-0123456789")) {
			t.Errorf("TTY primary view does not abbreviate the long session ID: %s", output)
		}
		if !strings.Contains(output, "session-abcdefghijklmnopqrstuvwxyz-0123456789") {
			t.Errorf("TTY secondary metadata lost the canonical session ID: %s", output)
		}
	}
	if strings.Contains(plain, "\x1b[") {
		t.Fatalf("plain TTY fallback contains ANSI controls: %q", plain)
	}
	if !strings.Contains(colored, "\x1b[") {
		t.Fatalf("color-enabled TTY output does not contain ANSI styling: %q", colored)
	}

	var nonTTY bytes.Buffer
	if err := Render(FormatTTY, model, &nonTTY); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(nonTTY.String(), "\x1b[") {
		t.Fatalf("non-TTY writer received ANSI controls: %q", nonTTY.String())
	}
}

func TestOperatorHTMLUsesDomainComponentsWithoutGenericTables(t *testing.T) {
	model := representativeViewModel(t)
	var output bytes.Buffer
	if err := Render(FormatHTML, model, &output); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	for _, want := range []string{
		`class="now-grid"`,
		`class="tree"`,
		`class="timeline"`,
		`class="dependency-map"`,
		`class="message-stack"`,
		`class="handoff-flow"`,
		`class="reservation-list"`,
		`class="git-console"`,
		`class="command-palette"`,
		`<details class="meta-disclosure">`,
		`prefers-color-scheme:light`,
		`@media print`,
		`:focus-visible`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("HTML operator board missing %q", want)
		}
	}
	for _, forbidden := range []string{"<table", "<script", "<link", "http://", "https://"} {
		if strings.Contains(strings.ToLower(got), forbidden) {
			t.Errorf("HTML operator board contains forbidden generic or external markup %q", forbidden)
		}
	}
}

func TestOperatorHTMLPreservesCJKAndCanonicalLongIDs(t *testing.T) {
	longID := "session-abcdefghijklmnopqrstuvwxyz-0123456789"
	model := viewModel(t, `{"schema_version":1,"view_version":1,"mode":"me","project_id":"프로젝트-1","snapshot_cursor":"cursor-1","scope":{"project_id":"프로젝트-1","mode":"me"},"identity":{"id":"`+longID+`","kind":"agent","role":"운영자","runtime":"native","liveness":"alive","native_access_state":"available"},"sessions":[],"tasks":[{"id":"task-1","display_number":1,"title":"현재 누가 무엇을 하는지 확인","state":"active"}],"runs":[],"progress":[],"dependencies":[],"inbox":[],"handoffs":[],"reservations":[],"warnings":[],"suggested_actions":[]}`)
	var output bytes.Buffer
	if err := Render(FormatHTML, model, &output); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	for _, want := range []string{"프로젝트-1", "운영자", "현재 누가 무엇을 하는지 확인", shortID(longID), longID} {
		if !strings.Contains(got, want) {
			t.Errorf("HTML output lost CJK or canonical identifier %q", want)
		}
	}
}

func TestOperatorNowUsesLivenessBeforeNativeAccess(t *testing.T) {
	model := viewModel(t, `{"schema_version":1,"view_version":1,"mode":"all","project_id":"project-1","snapshot_cursor":"cursor-1","scope":{"project_id":"project-1","mode":"all"},"identity":{"id":"session-stale","kind":"agent","runtime":"native","liveness":"stale","native_access_state":"available"},"sessions":[],"tasks":[],"runs":[],"progress":[],"dependencies":[],"inbox":[],"handoffs":[],"reservations":[],"warnings":[],"suggested_actions":[]}`)
	board, err := decodeBoard(model)
	if err != nil {
		t.Fatal(err)
	}

	if got := renderTTY(board, false); !strings.Contains(got, "0 active session(s)") {
		t.Fatalf("TTY Now summary counted a stale native session as active:\n%s", got)
	}
	var output bytes.Buffer
	if err := Render(FormatHTML, model, &output); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); !strings.Contains(got, "<strong>0</strong> active sessions") {
		t.Fatalf("HTML Now summary counted a stale native session as active:\n%s", got)
	}
}

func TestShortIDPreservesUnicodeRuneBoundaries(t *testing.T) {
	short := strings.Repeat("界", 9)
	if got := shortID(short); got != short {
		t.Fatalf("short Unicode ID = %q, want unchanged %q", got, short)
	}

	long := strings.Repeat("界", 30)
	got := shortID(long)
	if !utf8.ValidString(got) {
		t.Fatalf("shortID produced invalid UTF-8: %q", got)
	}
	want := strings.Repeat("界", 11) + "…" + strings.Repeat("界", 8)
	if got != want {
		t.Fatalf("shortID = %q, want %q", got, want)
	}
}
