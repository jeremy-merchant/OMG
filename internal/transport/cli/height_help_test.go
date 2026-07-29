package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestShortTerminalGlobalHelpUsesProgressiveDisclosure(t *testing.T) {
	compact, ok := renderHelpWithHeight("test", false, 100, 24, helpTarget{})
	if !ok {
		t.Fatal("compact global help was not found")
	}
	expanded, ok := renderHelpWithHeight("test", false, 100, 80, helpTarget{})
	if !ok {
		t.Fatal("expanded global help was not found")
	}
	for _, want := range []string{
		"WORKFLOWS", "Choose scope", "Start work", "Share state", "Recover safely",
		"COMMAND FAMILIES", "START + VERIFY", "COORDINATE WORK", "INSPECT + INTEGRATE",
		"COMMON OPTIONS", "--project <path> · --json · --help", "Short terminal view",
	} {
		if !strings.Contains(compact, want) {
			t.Errorf("compact help missing %q:\n%s", want, compact)
		}
	}
	for _, command := range helpCommands {
		if !strings.Contains(compact, command.Name) {
			t.Errorf("compact help omitted command %q", command.Name)
		}
	}
	for _, forbidden := range []string{
		"Create local canonical state.",
		"Supply one strict inline payload when allowed.",
		"GLOBAL OPTIONS",
	} {
		if strings.Contains(compact, forbidden) {
			t.Errorf("compact help retained expanded-only text %q", forbidden)
		}
	}
	compactLines := strings.Count(compact, "\n")
	expandedLines := strings.Count(expanded, "\n")
	if compactLines >= expandedLines {
		t.Fatalf("compact help has %d lines; expanded has %d", compactLines, expandedLines)
	}
	if compactLines > 32 {
		t.Fatalf("compact 100-column help is still too tall: %d lines\n%s", compactLines, compact)
	}
}

func TestTerminalHeightDoesNotTruncateContextualCommandHelp(t *testing.T) {
	short, ok := renderHelpWithHeight("test", false, 80, 20, helpTarget{Command: "task"})
	if !ok {
		t.Fatal("short terminal task help was not found")
	}
	tall, ok := renderHelpWithHeight("test", false, 80, 80, helpTarget{Command: "task"})
	if !ok {
		t.Fatal("tall terminal task help was not found")
	}
	if short != tall {
		t.Fatal("terminal height changed the complete contextual command contract")
	}
	for _, want := range []string{"SUBCOMMANDS", "claim", "OPTIONS", "EXAMPLES", "RELATED PATHS"} {
		if !strings.Contains(short, want) {
			t.Errorf("task help missing %q", want)
		}
	}
}

func TestConfiguredTerminalHeightNormalizesLines(t *testing.T) {
	tests := []struct {
		value string
		want  int
		ok    bool
	}{
		{"", 0, false},
		{"invalid", 0, false},
		{"0", 0, false},
		{"5", minimumTerminalHeight, true},
		{"24", 24, true},
		{"999", maximumTerminalHeight, true},
	}
	for _, test := range tests {
		t.Run(test.value, func(t *testing.T) {
			t.Setenv("LINES", test.value)
			got, ok := configuredTerminalHeight()
			if got != test.want || ok != test.ok {
				t.Fatalf("configuredTerminalHeight() = (%d, %v), want (%d, %v)", got, ok, test.want, test.ok)
			}
		})
	}
}

func TestNonTTYHelpIgnoresLinesForDeterministicFullOutput(t *testing.T) {
	t.Setenv("LINES", "20")
	if height := cliTerminalHeight(&bytes.Buffer{}); height != 0 {
		t.Fatalf("non-TTY height = %d, want unknown/full output", height)
	}
}
