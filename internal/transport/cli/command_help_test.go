package cli

import (
	"reflect"
	"strings"
	"testing"

	"github.com/jeremy-merchant/OMG/internal/app/foundation"
	"github.com/jeremy-merchant/OMG/internal/domain"
	shellgen "github.com/jeremy-merchant/OMG/internal/shell"
)

func TestCommandHelpCatalogIsUniqueAndComplete(t *testing.T) {
	wantCommands := []string{
		"init", "preflight", "status", "stale", "doctor", "migration", "backup", "release", "version", "agent", "worker",
		"human", "session", "delegate", "checkpoint", "task", "progress", "dependency", "message", "handoff", "reserve",
		"board", "git", "orphan", "canary", "export", "import", "integration", "watch", "run", "example", "shell-init", "completion", "mcp", "receipt",
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

func TestReserveAddHelpExposesRequiredLineageAndCopyablePayload(t *testing.T) {
	exit, output := run(t, "reserve", "add", "--help")
	if exit != ExitSuccess {
		t.Fatalf("reserve add help exit=%d: %s", exit, output)
	}
	for _, want := range []string{
		"Requires human_id, session_id, task_id, and run_id.",
		"omg reserve add",
		`"pattern_kind":"exact"`,
		`"human_id":"HUMAN_ID"`,
		`"run_id":"RUN_ID"`,
	} {
		if !strings.Contains(output, want) {
			t.Errorf("reserve add help missing %q:\n%s", want, output)
		}
	}
}

func TestSessionAndMessageHelpExposeCopyableAgentPayloads(t *testing.T) {
	for _, test := range []struct {
		args []string
		want []string
	}{
		{
			args: []string{"session", "create", "--help"},
			want: []string{
				"omg session create",
				`"source_ref":"human:task-summary"`,
				"instruction_source and provenance_confidence are derived output fields",
			},
		},
		{
			args: []string{"message", "send", "--help"},
			want: []string{
				"omg message send",
				`"type":"QUESTION"`,
				`"session_id":"PEER_SESSION_ID"`,
				`"related_task_id":"TASK_ID"`,
			},
		},
		{
			args: []string{"git", "latest", "--help"},
			want: []string{
				"omg git latest --project /project --json",
				"No payload is required",
				"change project scope.",
			},
		},
		{
			args: []string{"git", "diff", "--help"},
			want: []string{
				"omg git diff --project /project --json",
				"latest observations",
				"before and after",
			},
		},
	} {
		exit, output := run(t, test.args...)
		if exit != ExitSuccess {
			t.Fatalf("%v help exit=%d: %s", test.args, exit, output)
		}
		for _, want := range test.want {
			if !strings.Contains(output, want) {
				t.Errorf("%v help missing %q:\n%s", test.args, want, output)
			}
		}
	}
}

func TestInvalidRequestRecoveryUsesValidHelpTargets(t *testing.T) {
	exit, output := run(t, "board")
	if exit != ExitSuccess || !strings.Contains(output, "OMG / BOARD") || !strings.Contains(output, "SUBCOMMANDS") || !strings.Contains(output, "board task") || strings.Contains(output, "missing board mode") {
		t.Fatalf("bare board discovery exit=%d output=%s", exit, output)
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

func TestGlobalHelpIsWorkflowFirstAndCompact(t *testing.T) {
	exit, output := run(t, "--help")
	if exit != ExitSuccess {
		t.Fatalf("global help exit=%d: %s", exit, output)
	}
	for _, want := range []string{
		"35 commands",
		"WORKFLOWS",
		"First run",
		"Start work",
		"Share state",
		"Recover safely",
		"omg init → omg preflight → omg board all",
		"START + VERIFY · 11",
		"COORDINATE WORK · 10",
		"INSPECT + INTEGRATE · 14",
		"Record or inspect done / doing / next.",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("workflow-first global help missing %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, "Transfer work, evidence, risks, and ownership decisions without treating self-report as verification.") {
		t.Fatalf("global command palette still uses verbose contextual copy:\n%s", output)
	}
}

func TestContextualHelpShowsBoundedRelatedPaths(t *testing.T) {
	for _, test := range []struct {
		args []string
		want []string
	}{
		{args: []string{"task", "--help"}, want: []string{"RELATED PATHS", "omg progress --help", "omg dependency --help", "omg handoff --help", "omg board --help"}},
		{args: []string{"task", "claim", "--help"}, want: []string{"RELATED PATHS", "omg progress --help", "Parent help: omg task --help"}},
		{args: []string{"doctor", "--help"}, want: []string{"omg preflight --help", "omg migration --help", "omg backup --help"}},
	} {
		exit, output := run(t, test.args...)
		if exit != ExitSuccess {
			t.Fatalf("help %v exit=%d: %s", test.args, exit, output)
		}
		for _, want := range test.want {
			if !strings.Contains(output, want) {
				t.Errorf("help %v missing related path %q:\n%s", test.args, want, output)
			}
		}
		if strings.Count(output, "RELATED PATHS") != 1 {
			t.Errorf("help %v renders related paths more than once:\n%s", test.args, output)
		}
	}
}

func TestWorkflowAndRelatedCatalogReferencesKnownCommands(t *testing.T) {
	for _, workflow := range helpWorkflows {
		if workflow.Marker == "" || workflow.Name == "" || workflow.Command == "" || workflow.Summary == "" {
			t.Errorf("incomplete workflow: %+v", workflow)
		}
	}
	for _, command := range helpCommands {
		if summary := globalCommandSummary(command.Name); summary == "" {
			t.Errorf("global summary missing for %q", command.Name)
		}
		seen := map[string]bool{}
		for _, related := range relatedCommandNames(command.Name) {
			if !knownCommand(related) {
				t.Errorf("%s related path references unknown command %q", command.Name, related)
			}
			if related == command.Name || seen[related] {
				t.Errorf("%s has invalid duplicate/self related path %q", command.Name, related)
			}
			seen[related] = true
		}
	}
}

func TestNarrowGlobalHelpUsesCompactCommandGrid(t *testing.T) {
	output, found := renderHelp("v-test", false, 40, helpTarget{})
	if !found {
		t.Fatal("global help was not rendered")
	}
	for _, want := range []string{
		"init · preflight · status · stale",
		"human · session · delegate",
		"board · git · orphan · canary · export",
		"Open one family with: omg <command>",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("narrow command grid missing %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, "Create or inspect canonical human identities.") {
		t.Fatalf("narrow command grid retained verbose descriptions:\n%s", output)
	}
	if lines := len(strings.Split(strings.TrimSuffix(output, "\n"), "\n")); lines > 100 {
		t.Fatalf("narrow global help expanded to %d lines, want at most 100", lines)
	}
}

func TestExamplesUseHangingIndentAtNarrowWidths(t *testing.T) {
	output, found := renderHelp("v-test", false, 48, helpTarget{Command: "task"})
	if !found {
		t.Fatal("task help was not rendered")
	}
	lines := strings.Split(output, "\n")
	for index, line := range lines {
		if strings.Contains(line, "omg task create") && index+1 < len(lines) {
			if !strings.HasPrefix(lines[index+1], "    ") {
				t.Fatalf("wrapped example continuation lacks hanging indent:\n%s", output)
			}
			return
		}
	}
	t.Fatalf("wrapped task example not found:\n%s", output)
}

func TestShellCompletionVocabularyCoversHelpAndDecoderContracts(t *testing.T) {
	words := make(map[string]bool)
	for _, word := range shellgen.CompletionWords() {
		words[word] = true
	}
	for _, command := range helpCommands {
		if !words[command.Name] {
			t.Errorf("shell completion omitted command %q", command.Name)
		}
		contextual := make(map[string]bool)
		for _, candidate := range shellgen.CompletionCandidates(command.Name) {
			contextual[candidate] = true
		}
		for _, subcommand := range command.Subcommands {
			if !words[subcommand.Name] {
				t.Errorf("shell completion omitted %s subcommand %q", command.Name, subcommand.Name)
			}
			if !contextual[subcommand.Name] {
				t.Errorf("shell completion does not offer %s subcommand %q in its command context", command.Name, subcommand.Name)
			}
		}
	}
	for _, flag := range []string{
		"--help", "-h", "--json", "--integrity", "--status", "--stdio", "--payload-stdin",
		"--project", "--workspace", "--store", "--output", "--plan-file", "--approval-file", "--idempotency-key",
		"--format", "--session", "--task", "--runtime", "--payload", "--payload-file",
	} {
		if !words[flag] {
			t.Errorf("shell completion omitted decoder/help option %q", flag)
		}
	}
	for _, value := range []string{"help", "html", "markdown", "tty", "json", "bash", "zsh", "fish", "powershell"} {
		if !words[value] {
			t.Errorf("shell completion omitted presentation value %q", value)
		}
	}
	if words["--plan"] {
		t.Fatal("shell completion exposes unsupported --plan")
	}
}

func TestShellCompletionDescriptionsStayBoundToHelpCatalog(t *testing.T) {
	for _, command := range helpCommands {
		if got, want := shellgen.CompletionDescription("", command.Name), globalCommandSummary(command.Name); got != want {
			t.Errorf("completion description for %q = %q, want help summary %q", command.Name, got, want)
		}
		for _, subcommand := range command.Subcommands {
			description := shellgen.CompletionDescription(command.Name, subcommand.Name)
			if description == "" || description == "OMG "+command.Name+" value." {
				t.Errorf("completion description for %s %s is generic: %q", command.Name, subcommand.Name, description)
			}
		}
	}
	if got := shellgen.CompletionDescription("", "help"); got != "Explore command families and contextual help." {
		t.Errorf("completion description for help = %q", got)
	}
}
