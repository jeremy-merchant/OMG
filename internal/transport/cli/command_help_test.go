package cli

import (
	"reflect"
	"strings"
	"testing"

	"example.invalid/coordledger/internal/app/foundation"
	"example.invalid/coordledger/internal/domain"
)

func TestCommandHelpCatalogIsUniqueAndComplete(t *testing.T) {
	wantCommands := []string{
		"init", "preflight", "doctor", "migration", "backup", "release", "version",
		"human", "session", "delegate", "checkpoint", "task", "progress", "dependency", "message", "handoff", "reserve",
		"board", "git", "export", "import", "integration", "watch", "run", "shell-init", "completion", "mcp", "receipt",
	}
	if got := commandNames(); !reflect.DeepEqual(got, wantCommands) {
		t.Fatalf("help command catalog = %v, want %v", got, wantCommands)
	}

	seen := make(map[string]struct{}, len(helpCommands))
	for _, command := range helpCommands {
		if command.Name == "" || command.Group == "" || command.Summary == "" || len(command.Usage) == 0 {
			t.Errorf("incomplete help command: %+v", command)
		}
		if _, duplicate := seen[command.Name]; duplicate {
			t.Errorf("duplicate command help entry %q", command.Name)
		}
		seen[command.Name] = struct{}{}
		subcommands := make(map[string]struct{}, len(command.Subcommands))
		for _, subcommand := range command.Subcommands {
			if subcommand.Name == "" || subcommand.Summary == "" {
				t.Errorf("incomplete %s subcommand help: %+v", command.Name, subcommand)
			}
			if _, duplicate := subcommands[subcommand.Name]; duplicate {
				t.Errorf("duplicate %s subcommand help %q", command.Name, subcommand.Name)
			}
			subcommands[subcommand.Name] = struct{}{}
		}
	}

	for _, target := range []helpTarget{
		{Command: "shell-init", Subcommand: "powershell"},
		{Command: "completion", Subcommand: "powershell"},
		{Command: "receipt", Subcommand: "get"},
		{Command: "receipt", Subcommand: "list"},
	} {
		if _, found := renderHelp("v-test", false, 80, target); !found {
			t.Errorf("supported help target missing: %+v", target)
		}
	}
}

func TestSubcommandHelpShowsOnlyApplicableOptions(t *testing.T) {
	_, runHelp := run(t, "run", "--help")
	if strings.Contains(runHelp, "--json") {
		t.Fatalf("run help advertises unsupported JSON output:\n%s", runHelp)
	}
	for _, want := range []string{"--runtime <name>", "omg run --runtime shell -- go test ./..."} {
		if !strings.Contains(runHelp, want) {
			t.Errorf("run help missing %q:\n%s", want, runHelp)
		}
	}

	_, getHelp := run(t, "task", "get", "--help")
	if strings.Contains(getHelp, "--idempotency-key") {
		t.Fatalf("read-only task get help advertises an idempotency key:\n%s", getHelp)
	}
	_, claimHelp := run(t, "task", "claim", "--help")
	if !strings.Contains(claimHelp, "--idempotency-key") {
		t.Fatalf("mutating task claim help omits its idempotency key:\n%s", claimHelp)
	}
}

func TestInvalidRequestRecoveryUsesValidHelpTargets(t *testing.T) {
	exit, output := run(t, "board")
	if exit != ExitUsage || !strings.Contains(output, "omg board --help") {
		t.Fatalf("missing board mode recovery exit=%d output=%s", exit, output)
	}

	exit, output = run(t, "export", "xml")
	if exit != ExitUsage {
		t.Fatalf("invalid export format exit=%d output=%s", exit, output)
	}
	for _, want := range []string{"Did you mean \"html\"?", "omg export html --help"} {
		if !strings.Contains(output, want) {
			t.Errorf("invalid export recovery missing %q: %s", want, output)
		}
	}

	exit, output = run(t, "integration", "unknown")
	if exit != ExitUsage || !strings.Contains(output, "omg integration --help") {
		t.Fatalf("invalid integration subcommand recovery exit=%d output=%s", exit, output)
	}
}

func TestStatePathSecurityErrorExplainsSafeRecovery(t *testing.T) {
	var output strings.Builder
	exit := statusResult(&output, Request{Name: "init"}, foundation.Status{}, domain.NewError(
		domain.CodeUnavailable,
		"state path is not owner-only because an ancestor grants another account write access",
		false,
	))
	if exit != ExitUnavailable {
		t.Fatalf("state path error exit=%d: %s", exit, output.String())
	}
	for _, want := range []string{
		"state path is not owner-only",
		"Choose an absolute store path",
		"omg init --help",
	} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("state path recovery missing %q: %s", want, output.String())
		}
	}
}
