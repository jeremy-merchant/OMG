package view

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jeremy-merchant/oh-my-group/internal/app"
	"github.com/jeremy-merchant/oh-my-group/internal/app/query"
)

func TestTTYBoardRespectsNarrowVisibleWidths(t *testing.T) {
	model := viewModel(t, `{"schema_version":1,"view_version":1,"mode":"all","project_id":"프로젝트-운영-환경","snapshot_cursor":"cursor-with-a-very-long-canonical-identifier-0123456789","scope":{"project_id":"프로젝트-운영-환경","workspace_id":"workspace-with-a-very-long-identifier-0123456789","mode":"all"},"identity":{"id":"session-with-a-very-long-canonical-identifier-0123456789","kind":"agent","role":"검증 담당자","runtime":"native","liveness":"alive","native_access_state":"available","task_id":"task-long-0123456789"},"sessions":[],"tasks":[{"id":"task-with-a-very-long-canonical-identifier-0123456789","display_number":17,"title":"한글과 CJK가 포함된 매우 긴 작업 상태를 좁은 터미널에서 확인","state":"active","claimed_by_session_id":"session-with-a-very-long-canonical-identifier-0123456789"}],"runs":[{"id":"run-with-a-very-long-identifier-0123456789","task_id":"task-with-a-very-long-canonical-identifier-0123456789","session_id":"session-with-a-very-long-canonical-identifier-0123456789","state":"working"}],"progress":[{"id":"progress-long-0123456789","task_id":"task-with-a-very-long-canonical-identifier-0123456789","run_id":"run-with-a-very-long-identifier-0123456789","phase":"implementation","done":["폭 측정 완료"],"doing":["a-very-long-token-without-breaks-01234567890123456789"],"next":["회귀 테스트와 실제 TTY 검증"]}],"dependencies":[{"id":"dependency-long-0123456789","dependent_task_id":"task-with-a-very-long-canonical-identifier-0123456789","blocker_task_id":"blocker-with-a-very-long-identifier-0123456789","type":"blocks","unblock_on":"verified_done","satisfied":false}],"inbox":[],"handoffs":[],"reservations":[],"warnings":["advisory warning with a very long explanation that must wrap without hiding the operational meaning"],"suggested_actions":[{"code":"inspect-long-task","command":"omg board task --task task-with-a-very-long-canonical-identifier-0123456789 --format tty"}]}`)
	board, err := decodeBoard(model)
	if err != nil {
		t.Fatal(err)
	}

	for _, width := range []int{40, 60, 80} {
		for _, color := range []bool{false, true} {
			output := renderTTYWidth(board, color, width)
			for lineNumber, line := range strings.Split(strings.TrimSuffix(output, "\n"), "\n") {
				if visible := ttyDisplayWidth(line); visible > width {
					t.Fatalf("width=%d color=%t line=%d visible=%d: %q", width, color, lineNumber+1, visible, line)
				}
			}
			normalizedWords := strings.Join(strings.Fields(stripTTYANSI(output)), " ")
			normalizedTokenStream := strings.Map(func(character rune) rune {
				if character == ' ' || character == '\n' || character == '\r' || character == '\t' {
					return -1
				}
				return character
			}, stripTTYANSI(output))
			for _, want := range []string{"OPERATOR LEDGER", "⦸ BLOCKED", "WORK GRAPH", "한글과 CJK"} {
				if !strings.Contains(normalizedWords, want) {
					t.Errorf("width=%d color=%t output missing %q:\n%s", width, color, want, output)
				}
			}
			for _, want := range []string{"session-with-a-very-long-canonical-identifier", "inspect-long-task"} {
				if !strings.Contains(normalizedTokenStream, want) {
					t.Errorf("width=%d color=%t output lost token %q:\n%s", width, color, want, output)
				}
			}
			if color && !strings.Contains(output, "\x1b[") {
				t.Fatalf("width=%d colored output contains no ANSI styling", width)
			}
			if !color && strings.Contains(output, "\x1b[") {
				t.Fatalf("width=%d plain output contains ANSI styling", width)
			}
		}
	}
}

func TestPreflightSharesWidthAndColorSemantics(t *testing.T) {
	now := time.Date(2026, time.July, 24, 12, 0, 0, 0, time.UTC)
	preflight := app.PreflightView{
		Healthy:           true,
		MutationAllowed:   true,
		PendingMigrations: 0,
		Details: &app.PreflightDetails{Identity: &query.IdentityView{
			ID:                "session-with-a-very-long-canonical-identifier-0123456789",
			Kind:              "agent",
			Role:              "검증 담당자",
			Runtime:           "native",
			NativeAccessState: "available",
			StartedAt:         now,
		}, Tasks: []query.TaskView{{
			ID:            "task-with-a-very-long-canonical-identifier-0123456789",
			DisplayNumber: 18,
			Title:         "좁은 터미널에서도 시작 준비 상태와 긴 식별자를 모두 확인",
			State:         "active",
			CreatedAt:     now,
			UpdatedAt:     now,
		}}, Warnings: []string{"long advisory warning that remains readable and does not overflow the terminal"}},
	}

	for _, width := range []int{40, 60, 80} {
		for _, color := range []bool{false, true} {
			output := RenderPreflightTTYWithOptions(preflight, width, color)
			for lineNumber, line := range strings.Split(strings.TrimSuffix(output, "\n"), "\n") {
				if visible := ttyDisplayWidth(line); visible > width {
					t.Fatalf("width=%d color=%t line=%d visible=%d: %q", width, color, lineNumber+1, visible, line)
				}
			}
			normalizedWords := strings.Join(strings.Fields(stripTTYANSI(output)), " ")
			normalizedTokenStream := strings.Map(func(character rune) rune {
				if character == ' ' || character == '\n' || character == '\r' || character == '\t' {
					return -1
				}
				return character
			}, stripTTYANSI(output))
			for _, want := range []string{"OPERATOR LEDGER", "PREFLIGHT", "VERIFIED", "검증 담당자"} {
				if !strings.Contains(normalizedWords, want) {
					t.Errorf("width=%d color=%t preflight missing %q:\n%s", width, color, want, output)
				}
			}
			if !strings.Contains(normalizedTokenStream, "task-with-a-very-long-canonical-identifier") {
				t.Errorf("width=%d color=%t preflight lost canonical task ID:\n%s", width, color, output)
			}
		}
	}
}

func TestTTYTokenSplittingPrefersSemanticDelimiters(t *testing.T) {
	tests := []struct {
		value string
		width int
		want  []string
	}{
		{"instruction_source=delegation_token", 32, []string{"instruction_source=", "delegation_token"}},
		{"claimed_by_session=agt-preview", 24, []string{"claimed_by_session=", "agt-preview"}},
		{"0123456789abcdef", 8, []string{"01234567", "89abcdef"}},
		{"한글협업상태확인", 8, []string{"한글협업", "상태확인"}},
	}
	for _, test := range tests {
		got := splitTTYToken(test.value, test.width)
		if !reflect.DeepEqual(got, test.want) {
			t.Errorf("splitTTYToken(%q, %d) = %#v, want %#v", test.value, test.width, got, test.want)
		}
		for _, part := range got {
			if width := ttyDisplayWidth(part); width > test.width {
				t.Errorf("split part %q width=%d exceeds %d", part, width, test.width)
			}
		}
	}
}

func TestNarrowTTYSeparatesStatusFromCanonicalMetadataWithoutDuplication(t *testing.T) {
	model := viewModel(t, `{"schema_version":1,"view_version":1,"mode":"all","project_id":"project-compact","snapshot_cursor":"cursor-compact","scope":{"project_id":"project-compact","mode":"all"},"identity":{"id":"session-with-a-very-long-canonical-id","kind":"agent_delegated","role":"reviewer","runtime":"generic","instruction_source":"delegation_token","provenance_confidence":"verified","task_id":"task-with-a-very-long-id","native_access_state":"available","started_at":"2026-07-24T01:02:03Z"},"sessions":[],"tasks":[{"id":"task-with-a-very-long-id","display_number":8,"title":"Review narrow terminal metadata","state":"WAITING","created_by_session_id":"session-owner","claimed_by_session_id":"session-with-a-very-long-canonical-id","created_at":"2026-07-24T01:02:03Z","updated_at":"2026-07-24T02:03:04Z"}],"runs":[],"progress":[{"id":"progress-with-a-very-long-id","task_id":"task-with-a-very-long-id","run_id":"run-with-a-very-long-id","session_id":"session-with-a-very-long-canonical-id","phase":"review","done":["inspected"],"doing":["verify"],"next":["handoff"],"created_at":"2026-07-24T02:04:05Z"}],"dependencies":[],"inbox":[],"handoffs":[],"reservations":[],"warnings":[],"suggested_actions":[]}`)
	board, err := decodeBoard(model)
	if err != nil {
		t.Fatal(err)
	}
	output := renderTTYWidth(board, false, 40)
	canonical := strings.Map(func(character rune) rune {
		switch character {
		case ' ', '\n', '\r', '\t', '│', '├', '└', '─', '·':
			return -1
		default:
			return character
		}
	}, output)
	for _, want := range []string{
		"instruction_source=delegation_token",
		"task=task-with-a-very-long-id",
		"created_by_session=session-owner",
		"claimed_by_session=session-with-a-very-long-canonical-id",
		"created=2026-07-24T01:02:03Z",
		"updated=2026-07-24T02:03:04Z",
		"progress_id=progress-with-a-very-long-id",
		"session=session-with-a-very-long-canonical-id",
	} {
		if !strings.Contains(canonical, want) {
			t.Errorf("narrow TTY lost canonical fact %q:\n%s", want, output)
		}
	}
	if strings.Count(output, "kind=agent_delegated") != 1 {
		t.Fatalf("session kind is duplicated in narrow TTY:\n%s", output)
	}
	if strings.Contains(output, "Task task-with-a-very-long-id") || strings.Contains(output, "Progress progress-with-a-very-long-id") {
		t.Fatalf("narrow TTY retained duplicate prose rows:\n%s", output)
	}
	if lines := len(strings.Split(strings.TrimSuffix(output, "\n"), "\n")); lines > 105 {
		t.Fatalf("narrow TTY expanded to %d lines, want at most 105:\n%s", lines, output)
	}
}

func TestTTYProgressMetadataMarksEmptyLanes(t *testing.T) {
	metadata := ttyProgressMetadata(query.ProgressView{ID: "progress-1", TaskID: "task-1", RunID: "run-1", SessionID: "session-1"})
	for _, want := range []string{"progress_id=progress-1", "session=session-1", "done=none", "doing=none", "next=none"} {
		if !strings.Contains(metadata, want) {
			t.Errorf("progress metadata missing %q: %s", want, metadata)
		}
	}
}
