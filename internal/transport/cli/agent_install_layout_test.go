package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestAgentInstallNarrowTTYIsWidthBounded(t *testing.T) {
	home := projectConfinedAgentHome(t)
	t.Setenv("OMG_AGENT_HOME", home)
	t.Setenv("NO_COLOR", "1")
	t.Setenv("COLUMNS", "36")

	var output bytes.Buffer
	exit := Run([]string{"agent", "install"}, "test-version", &output)
	if exit != ExitSuccess {
		t.Fatalf("agent install exit=%d output=%q", exit, output.String())
	}
	for lineNumber, line := range strings.Split(strings.TrimSuffix(output.String(), "\n"), "\n") {
		if width := terminalDisplayWidth(line); width > 36 {
			t.Fatalf("line %d exceeds 36 cells (%d): %q\n%s", lineNumber+1, width, line, output.String())
		}
	}
	for _, want := range []string{"Claude", "Codex", "OMP", "~/.agents/skills/omg/", "SKILL.md"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("narrow output missing %q:\n%s", want, output.String())
		}
	}
}
