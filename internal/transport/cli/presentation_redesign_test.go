package cli

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"example.invalid/coordledger/internal/app"
	"example.invalid/coordledger/internal/domain"
)

func TestOperatorHelpPaletteKeepsMeaningWithoutColor(t *testing.T) {
	plain := renderUsage("v-test", false)
	colored := renderUsage("v-test", true)
	for _, output := range []string{plain, colored} {
		for _, want := range []string{"OMG", "OPERATOR LEDGER", "Usage:", "START + VERIFY", "COORDINATE WORK", "INSPECT + INTEGRATE", "board", "GLOBAL OPTIONS"} {
			if !strings.Contains(output, want) {
				t.Errorf("help palette missing %q:\n%s", want, output)
			}
		}
	}
	if strings.Contains(plain, "\x1b[") {
		t.Fatalf("plain help contains ANSI controls: %q", plain)
	}
	if !strings.Contains(plain, "❯ board") {
		t.Fatalf("plain help does not retain the command-palette prompt: %q", plain)
	}
	if !strings.Contains(colored, "\x1b[") {
		t.Fatalf("color-enabled help does not contain ANSI styling: %q", colored)
	}
}

func TestOperatorErrorSeparatesCauseRetryAndNextCommand(t *testing.T) {
	var output bytes.Buffer
	renderError(&output, domain.NewError(domain.CodeConflict, "reservation conflict", true), ExitConflict)
	got := output.String()
	for _, want := range []string{"✘ OMG  ERROR", "code", "conflict", "cause", "reservation conflict", "retryable", "available", "next", "omg board all", "exit"} {
		if !strings.Contains(got, want) {
			t.Errorf("structured error missing %q: %q", want, got)
		}
	}
	if strings.Contains(got, "\x1b[") {
		t.Fatalf("non-TTY error output contains ANSI controls: %q", got)
	}
}

func TestHelpLayoutsRespectVisibleTerminalWidth(t *testing.T) {
	targets := []helpTarget{{}, {Command: "task"}, {Command: "task", Subcommand: "claim"}}
	for _, width := range []int{40, 60, 80, 120} {
		for _, color := range []bool{false, true} {
			for _, target := range targets {
				output, found := renderHelp("v-test", color, width, target)
				if !found {
					t.Fatalf("help target was not found: %+v", target)
				}
				for lineNumber, line := range strings.Split(strings.TrimSuffix(output, "\n"), "\n") {
					if visible := terminalDisplayWidth(line); visible > width {
						t.Fatalf("width=%d color=%t target=%+v line=%d visible=%d: %q", width, color, target, lineNumber+1, visible, line)
					}
				}
			}
		}
	}
}

func TestContextualHelpIsFocusedAndMachineStable(t *testing.T) {
	exit, taskHelp := run(t, "task", "--help")
	if exit != ExitSuccess {
		t.Fatalf("task help exit=%d: %s", exit, taskHelp)
	}
	for _, want := range []string{"OMG / TASK", "SUBCOMMANDS", "◆ create", "◇ get", "◆ claim", "--idempotency-key"} {
		if !strings.Contains(taskHelp, want) {
			t.Errorf("task help missing %q:\n%s", want, taskHelp)
		}
	}
	if strings.Contains(taskHelp, "omg backup") || strings.Contains(taskHelp, "INSPECT + INTEGRATE") {
		t.Fatalf("task help contains unrelated global palette content:\n%s", taskHelp)
	}

	exit, claimHelp := run(t, "help", "task", "claim")
	if exit != ExitSuccess {
		t.Fatalf("claim help exit=%d: %s", exit, claimHelp)
	}
	for _, want := range []string{"OMG / TASK / CLAIM", "Claim one ready task", "task_id", "session_id", "Parent help: omg task --help"} {
		if !strings.Contains(claimHelp, want) {
			t.Errorf("claim help missing %q:\n%s", want, claimHelp)
		}
	}
	if strings.Contains(claimHelp, "omg task create") || strings.Contains(claimHelp, "omg task get") {
		t.Fatalf("claim help contains unrelated task examples:\n%s", claimHelp)
	}

	exit, jsonHelp := run(t, "task", "--help", "--json")
	if exit != ExitSuccess {
		t.Fatalf("JSON task help exit=%d: %s", exit, jsonHelp)
	}
	var payload struct {
		Version string `json:"version"`
		Usage   string `json:"usage"`
	}
	decodeData(t, jsonHelp, &payload)
	if payload.Version != "test-version" || !strings.Contains(payload.Usage, "OMG / TASK") || strings.Contains(payload.Usage, "\x1b[") {
		t.Fatalf("unexpected JSON help payload: %+v", payload)
	}
}

func TestHumanCommandRecoverySuggestsClosestValidPath(t *testing.T) {
	exit, output := run(t, "prefligth")
	if exit != ExitUnavailable {
		t.Fatalf("top-level typo exit=%d: %s", exit, output)
	}
	for _, want := range []string{"unknown command \"prefligth\"", "Did you mean \"preflight\"?", "omg preflight --help"} {
		if !strings.Contains(output, want) {
			t.Errorf("top-level typo output missing %q: %s", want, output)
		}
	}

	exit, output = run(t, "task", "cliam")
	if exit != ExitUnavailable {
		t.Fatalf("subcommand typo exit=%d: %s", exit, output)
	}
	for _, want := range []string{"unknown task subcommand \"cliam\"", "Did you mean \"claim\"?", "omg task claim --help"} {
		if !strings.Contains(output, want) {
			t.Errorf("subcommand typo output missing %q: %s", want, output)
		}
	}

	exit, output = run(t, "task")
	if exit != ExitSuccess || !strings.Contains(output, "OMG / TASK") || !strings.Contains(output, "SUBCOMMANDS") || !strings.Contains(output, "claim") || strings.Contains(output, "task subcommand is required") {
		t.Fatalf("parent command discovery exit=%d: %s", exit, output)
	}
}

func TestStructuredSuccessAndRuntimeResultsAreWidthSafe(t *testing.T) {
	t.Setenv("COLUMNS", "40")
	var output bytes.Buffer
	renderSuccess(&output, struct {
		Status  string   `json:"status"`
		Message string   `json:"message"`
		Items   []string `json:"items"`
	}{
		Status:  "verified",
		Message: "한글 협업 상태와 a-very-long-identifier-without-breaks-0123456789",
		Items:   []string{"first", "second"},
	})
	got := output.String()
	for _, want := range []string{"✔ OMG  VERIFIED", "status", "verified", "한글 협업 상태", "items", "first · second"} {
		if !strings.Contains(got, want) {
			t.Errorf("structured success missing %q: %s", want, got)
		}
	}
	if strings.Contains(got, "{verified ") || strings.Contains(got, "[first second]") {
		t.Fatalf("structured success fell back to a Go value dump: %s", got)
	}
	for _, line := range strings.Split(strings.TrimSuffix(got, "\n"), "\n") {
		if visible := terminalDisplayWidth(line); visible > 40 {
			t.Fatalf("success line exceeds width: visible=%d line=%q", visible, line)
		}
	}

	output.Reset()
	renderRuntimeResult(&output, app.CLIRuntimeResult{Runtime: "shell", Executable: "a-very-long-executable-name-without-spaces", Resolution: "configured", Status: "succeeded", ExitCode: 0})
	got = output.String()
	for _, want := range []string{"RUN COMPLETE", "runtime", "shell", "resolution", "configured", "exit"} {
		if !strings.Contains(got, want) {
			t.Errorf("runtime result missing %q: %s", want, got)
		}
	}
	for _, line := range strings.Split(strings.TrimSuffix(got, "\n"), "\n") {
		if visible := terminalDisplayWidth(line); visible > 40 {
			t.Fatalf("runtime line exceeds width: visible=%d line=%q", visible, line)
		}
	}
}

func TestCLITokenSplittingPrefersSemanticDelimiters(t *testing.T) {
	tests := []struct {
		value string
		width int
		want  []string
	}{
		{"instruction_source=delegation_token", 32, []string{"instruction_source=", "delegation_token"}},
		{"omg/task/with-a-very-long-id", 16, []string{"omg/task/with-a-", "very-long-id"}},
		{"0123456789abcdef", 8, []string{"01234567", "89abcdef"}},
	}
	for _, test := range tests {
		got := splitTerminalToken(test.value, test.width)
		if !reflect.DeepEqual(got, test.want) {
			t.Errorf("splitTerminalToken(%q, %d) = %#v, want %#v", test.value, test.width, got, test.want)
		}
		for _, part := range got {
			if width := terminalDisplayWidth(part); width > test.width {
				t.Errorf("split part %q width=%d exceeds %d", part, width, test.width)
			}
		}
	}
}
