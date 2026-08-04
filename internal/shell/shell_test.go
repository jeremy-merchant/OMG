package shell

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

func TestGeneratorsReturnDeterministicNewlineTerminatedStaticScripts(t *testing.T) {
	for _, target := range []Shell{Bash, Zsh, Fish, PowerShell} {
		t.Run(string(target), func(t *testing.T) {
			initFirst, err := Init(target)
			if err != nil {
				t.Fatal(err)
			}
			initSecond, err := Init(target)
			if err != nil {
				t.Fatal(err)
			}
			completionFirst, err := Completion(target)
			if err != nil {
				t.Fatal(err)
			}
			completionSecond, err := Completion(target)
			if err != nil {
				t.Fatal(err)
			}

			if initFirst != initSecond || completionFirst != completionSecond {
				t.Fatal("generator output is not deterministic")
			}
			for name, result := range map[string]Result{"init": initFirst, "completion": completionFirst} {
				if result.Shell != target || !strings.HasSuffix(result.Content, "\n") {
					t.Fatalf("%s result = %#v", name, result)
				}
				assertSafeStaticScript(t, result.Content)
			}

			for _, command := range requiredCommands {
				if !strings.Contains(completionFirst.Content, command) {
					t.Fatalf("completion omitted command family %q", command)
				}
			}
			for _, flag := range requiredFlags {
				if !strings.Contains(completionFirst.Content, flag) {
					t.Fatalf("completion omitted documented flag %q", flag)
				}
			}
			assertUsefulHelpers(t, target, initFirst.Content)
			assertNativePrefixFiltering(t, target, completionFirst.Content)
			validateShellSyntaxWhenAvailable(t, target, initFirst.Content)
			validateShellSyntaxWhenAvailable(t, target, completionFirst.Content)
		})
	}
}

func TestGeneratorsRejectUnknownShellWithStableError(t *testing.T) {
	for _, generate := range []func(Shell) (Result, error){Init, Completion} {
		result, err := generate(Shell("unsupported-shell"))
		if !errors.Is(err, ErrUnsupportedShell) {
			t.Fatalf("error = %v, want ErrUnsupportedShell", err)
		}
		if result != (Result{}) {
			t.Fatalf("result = %#v, want zero value", result)
		}
		if err.Error() != ErrUnsupportedShell.Error() {
			t.Fatalf("error = %q, want stable %q", err, ErrUnsupportedShell)
		}
	}
}

var requiredCommands = []string{
	"help", "init", "version", "doctor", "backup", "migration", "export", "import", "preflight",
	"human", "session", "delegate", "task", "progress", "dependency", "message", "handoff", "checkpoint", "mode", "board",
	"reserve", "git", "integration", "run", "shell-init", "completion", "watch", "mcp", "release", "receipt",
	"me", "tree", "all", "plan", "apply", "status", "remove", "create", "restore", "record", "get",
	"resume", "adopt", "issue", "register", "revoke", "claim", "transition", "run-create", "run-transition", "finish-lite",
	"add", "batch-add", "history", "list", "active", "renew", "release", "override", "inbox", "thread", "send",
	"deliver", "read", "ack", "show", "lifecycle", "advance", "observe", "work-lite", "full", "classify", "actionable", "backlog", "hygiene", "history", "summary", "queue", "supersede", "accept", "reject", "inventory", "current", "latest", "diff", "cleanup-plan",
	"html", "json", "markdown", "tty", "serve", "bash", "zsh", "fish", "powershell",
}

var requiredFlags = []string{
	"--help", "-h", "--json", "--integrity", "--status", "--stdio", "--payload-stdin",
	"--project", "--workspace", "--store", "--output", "--plan-file", "--approval-file",
	"--idempotency-key", "--format", "--session", "--task", "--runtime", "--payload", "--payload-file",
}

func assertUsefulHelpers(t *testing.T, target Shell, script string) {
	t.Helper()
	var required []string
	switch target {
	case Bash, Zsh:
		required = []string{"omg_preflight()", "command omg preflight --project \"$PWD\" \"$@\"", "omg_board()", "command omg board actionable --project \"$PWD\" \"$@\"", "omg_checkpoint()", "command omg checkpoint --project \"$PWD\" \"$@\""}
	case Fish:
		required = []string{"function omg_preflight", "command omg preflight --project \"$PWD\" $argv", "function omg_board", "command omg board actionable --project \"$PWD\" $argv", "function omg_checkpoint", "command omg checkpoint --project \"$PWD\" $argv"}
	case PowerShell:
		required = []string{"function OMG-Preflight", "& omg preflight --project $PWD.Path @args", "function OMG-Board", "& omg board actionable --project $PWD.Path @args", "function OMG-Checkpoint", "& omg checkpoint --project $PWD.Path @args"}
	}
	for _, fragment := range required {
		if !strings.Contains(script, fragment) {
			t.Fatalf("init omitted useful helper fragment %q", fragment)
		}
	}
}

func assertNativePrefixFiltering(t *testing.T, target Shell, script string) {
	t.Helper()
	var required string
	switch target {
	case Bash:
		required = "compgen -W"
	case Zsh:
		required = "_describe"
	case Fish:
		required = "complete -c omg"
	case PowerShell:
		required = "if ($parts[0] -like \"$wordToComplete*\")"
	}
	if !strings.Contains(script, required) {
		t.Fatalf("completion omitted native prefix filtering %q", required)
	}
}

func assertSafeStaticScript(t *testing.T, script string) {
	t.Helper()
	lowerScript := strings.ToLower(script)
	for _, forbidden := range []string{"eval", "path=", "watch start", "alias "} {
		if strings.Contains(lowerScript, forbidden) {
			t.Fatalf("script contains forbidden %q", forbidden)
		}
	}
	for _, agent := range []string{"codex", "claude", "opencode", "gjc", "aider", "cursor", "gemini", "copilot", "goose"} {
		for _, declaration := range []string{"alias " + agent, "function " + agent, agent + "()"} {
			if strings.Contains(lowerScript, declaration) {
				t.Fatalf("script shadows agent binary with %q", declaration)
			}
		}
	}
}

func validateShellSyntaxWhenAvailable(t *testing.T, target Shell, script string) {
	t.Helper()
	binary, args := shellSyntaxCommand(target)
	path, err := exec.LookPath(binary)
	if err != nil {
		return
	}
	command := exec.Command(path, args...)
	command.Stdin = strings.NewReader(script)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("%s syntax validation failed: %v\n%s", target, err, output)
	}
}

func shellSyntaxCommand(target Shell) (string, []string) {
	switch target {
	case Bash:
		return "bash", []string{"-n"}
	case Zsh:
		return "zsh", []string{"-n"}
	case Fish:
		return "fish", []string{"--no-execute", "-"}
	case PowerShell:
		return powerShellSyntaxCommand()
	default:
		return "", nil
	}
}

func powerShellSyntaxCommand() (string, []string) {
	parser := "$tokens=$null;$errors=$null;[System.Management.Automation.Language.Parser]::ParseInput([Console]::In.ReadToEnd(),[ref]$tokens,[ref]$errors)|Out-Null;if($errors.Count){exit 1}"
	if _, err := exec.LookPath("pwsh"); err == nil {
		return "pwsh", []string{"-NoProfile", "-NonInteractive", "-Command", parser}
	}
	return "powershell", []string{"-NoProfile", "-NonInteractive", "-Command", parser}
}

func TestShellSyntaxCommandConsumesScriptFromStandardInput(t *testing.T) {
	binary, args := shellSyntaxCommand(Bash)
	if binary != "bash" || !bytes.Equal([]byte(strings.Join(args, " ")), []byte("-n")) {
		t.Fatalf("bash syntax command = %q %q", binary, args)
	}
}

func TestCompletionVocabularyIsUniqueDefensiveAndContainsNoInventedFlag(t *testing.T) {
	words := CompletionWords()
	seen := make(map[string]bool, len(words))
	for _, word := range words {
		if word == "" {
			t.Fatal("completion vocabulary contains an empty word")
		}
		if seen[word] {
			t.Fatalf("completion vocabulary contains duplicate %q", word)
		}
		seen[word] = true
	}
	if seen["--plan"] {
		t.Fatal("completion vocabulary advertises unsupported --plan instead of --plan-file")
	}
	if !seen["receipt"] || !seen["--payload-file"] || !seen["--payload-stdin"] || !seen["--stdio"] {
		t.Fatalf("completion vocabulary lacks current contract words: %v", words)
	}
	if len(words) == 0 {
		t.Fatal("completion vocabulary is empty")
	}
	words[0] = "mutated"
	if CompletionWords()[0] == "mutated" {
		t.Fatal("CompletionWords returned shared mutable storage")
	}
}

func TestCompletionCandidatesAreContextualAndDefensive(t *testing.T) {
	tests := []struct {
		command  string
		contains []string
		excludes []string
	}{
		{"", []string{"task", "migration", "--help"}, []string{"claim", "apply", "--payload"}},
		{"task", []string{"create", "claim", "run-transition", "--payload-file", "--idempotency-key"}, []string{"apply", "restore", "powershell"}},
		{"migration", []string{"plan", "apply", "--plan-file", "--approval-file"}, []string{"claim", "run-transition"}},
		{"shell-init", []string{"bash", "zsh", "fish", "powershell", "--help"}, []string{"claim", "apply"}},
		{"help", []string{"task", "migration", "receipt", "--help"}, []string{"claim", "apply"}},
		{"not-a-command", []string{"task", "migration", "--help"}, []string{"claim", "apply"}},
	}
	for _, test := range tests {
		candidates := CompletionCandidates(test.command)
		seen := make(map[string]bool, len(candidates))
		for _, candidate := range candidates {
			seen[candidate] = true
		}
		for _, want := range test.contains {
			if !seen[want] {
				t.Errorf("CompletionCandidates(%q) missing %q: %v", test.command, want, candidates)
			}
		}
		for _, forbidden := range test.excludes {
			if seen[forbidden] {
				t.Errorf("CompletionCandidates(%q) contains unrelated %q: %v", test.command, forbidden, candidates)
			}
		}
		if len(candidates) > 0 {
			candidates[0] = "mutated"
			if CompletionCandidates(test.command)[0] == "mutated" {
				t.Fatalf("CompletionCandidates(%q) returned shared mutable storage", test.command)
			}
		}
	}
}

func TestCompletionItemsCarryDefensiveDescriptions(t *testing.T) {
	tests := []struct {
		command     string
		value       string
		description string
	}{
		{"", "task", "Create, inspect, claim, or transition tasks and runs."},
		{"task", "claim", "Claim one ready task for exactly one session."},
		{"task", "--payload-file", "Read one strict owner-only payload file."},
		{"migration", "apply", "Apply the exact approved migration plan."},
	}
	for _, test := range tests {
		items := CompletionItems(test.command)
		found := false
		for _, item := range items {
			if item.Value != test.value {
				continue
			}
			found = true
			if item.Description != test.description {
				t.Errorf("CompletionItems(%q) description for %q = %q, want %q", test.command, test.value, item.Description, test.description)
			}
		}
		if !found {
			t.Errorf("CompletionItems(%q) missing %q: %#v", test.command, test.value, items)
		}
		if got := CompletionDescription(test.command, test.value); got != test.description {
			t.Errorf("CompletionDescription(%q, %q) = %q, want %q", test.command, test.value, got, test.description)
		}
		if len(items) > 0 {
			items[0].Value = "mutated"
			items[0].Description = "mutated"
			fresh := CompletionItems(test.command)
			if fresh[0].Value == "mutated" || fresh[0].Description == "mutated" {
				t.Fatalf("CompletionItems(%q) returned shared mutable storage", test.command)
			}
		}
	}
}

func TestRichCompletionGeneratorsExposeDescriptions(t *testing.T) {
	tests := []struct {
		target    Shell
		want      string
		forbidden string
	}{
		{Bash, "task) candidates='create get claim", "Claim one ready task for exactly one session."},
		{Zsh, "'claim:Claim one ready task for exactly one session.'", ""},
		{Fish, "-a 'claim' -d 'Claim one ready task for exactly one session.'", ""},
		{PowerShell, "'claim|Claim one ready task for exactly one session.'", ""},
	}
	for _, test := range tests {
		result, err := Completion(test.target)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(result.Content, test.want) {
			t.Errorf("%s completion missing described candidate %q", test.target, test.want)
		}
		if test.forbidden != "" && strings.Contains(result.Content, test.forbidden) {
			t.Errorf("%s completion unexpectedly includes rich descriptions", test.target)
		}
	}
}

func TestGeneratedCompletionScriptsInspectCommandContext(t *testing.T) {
	tests := []struct {
		target Shell
		want   []string
	}{
		{Bash, []string{`local command="${COMP_WORDS[1]}"`, `task) candidates='create get claim`, `migration) candidates='plan apply`}},
		{Zsh, []string{`local command="${words[2]}"`, `task) choices=(`, `migration) choices=(`}},
		{Fish, []string{`__fish_use_subcommand`, `__fish_seen_subcommand_from task`, `__fish_seen_subcommand_from migration`}},
		{PowerShell, []string{`$commandAst.CommandElements`, `'task' { @(`, `'migration' { @(`}},
	}
	for _, test := range tests {
		result, err := Completion(test.target)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range test.want {
			if !strings.Contains(result.Content, want) {
				t.Errorf("%s completion lacks contextual marker %q", test.target, want)
			}
		}
	}
}

func TestBashCompletionExecutesContextually(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is unavailable")
	}
	result, err := Completion(Bash)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		words  string
		cursor int
		want   []string
		forbid []string
	}{
		{words: "omg ta", cursor: 1, want: []string{"task"}, forbid: []string{"claim", "apply"}},
		{words: "omg task cl", cursor: 2, want: []string{"claim"}, forbid: []string{"cleanup-plan", "apply", "restore"}},
		{words: "omg migration ap", cursor: 2, want: []string{"apply"}, forbid: []string{"--approval-file", "claim", "run-transition"}},
		{words: "omg shell-init po", cursor: 2, want: []string{"powershell"}, forbid: []string{"claim", "apply"}},
	}
	for _, test := range tests {
		script := result.Content + "\n" +
			"COMP_WORDS=(" + test.words + ")\n" +
			"COMP_CWORD=" + fmt.Sprint(test.cursor) + "\n" +
			"_omg_completion\n" +
			"printf '%s\\n' \"${COMPREPLY[@]}\"\n"
		command := exec.Command(bash, "--noprofile", "--norc")
		command.Stdin = strings.NewReader(script)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("bash completion %q failed: %v\n%s", test.words, err, output)
		}
		lines := strings.Fields(string(output))
		seen := make(map[string]bool, len(lines))
		for _, line := range lines {
			seen[line] = true
		}
		for _, want := range test.want {
			if !seen[want] {
				t.Errorf("bash completion %q missing %q: %q", test.words, want, output)
			}
		}
		for _, forbidden := range test.forbid {
			if seen[forbidden] {
				t.Errorf("bash completion %q exposed unrelated %q: %q", test.words, forbidden, output)
			}
		}
	}
}
