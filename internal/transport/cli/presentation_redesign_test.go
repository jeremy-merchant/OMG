package cli

import (
	"bytes"
	"strings"
	"testing"

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
	for _, want := range []string{"✘ OMG  ERROR", "code", "conflict", "cause", "reservation conflict", "retryable", "available", "next", "omg board all", "exit="} {
		if !strings.Contains(got, want) {
			t.Errorf("structured error missing %q: %q", want, got)
		}
	}
	if strings.Contains(got, "\x1b[") {
		t.Fatalf("non-TTY error output contains ANSI controls: %q", got)
	}
}
