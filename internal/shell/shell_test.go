package shell

import (
	"bytes"
	"errors"
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
	"init", "version", "doctor", "backup", "migration", "export", "import", "preflight",
	"human", "session", "delegate", "task", "progress", "dependency", "message", "handoff", "checkpoint", "board",
	"reserve", "git", "integration", "run", "shell-init", "completion", "watch", "mcp", "release",
	"me", "tree", "all", "plan", "apply", "status", "remove", "create", "restore", "record", "get",
	"resume", "adopt", "issue", "register", "revoke", "claim", "transition", "run-create", "run-transition",
	"add", "history", "list", "active", "renew", "release", "override", "inbox", "thread", "send",
	"deliver", "read", "ack", "show", "supersede", "accept", "reject", "inventory", "current", "latest", "diff", "cleanup-plan",
	"html", "json", "markdown", "tty", "serve", "stdio",
}

var requiredFlags = []string{
	"--json", "--project", "--workspace", "--store", "--plan", "--plan-file", "--approval-file",
	"--idempotency-key", "--session", "--task", "--format", "--runtime", "--integrity", "--output", "--payload",
}

func assertUsefulHelpers(t *testing.T, target Shell, script string) {
	t.Helper()
	var required []string
	switch target {
	case Bash, Zsh:
		required = []string{"omg_preflight()", "command omg preflight --project \"$PWD\" \"$@\"", "omg_checkpoint()", "command omg checkpoint --project \"$PWD\" \"$@\""}
	case Fish:
		required = []string{"function omg_preflight", "command omg preflight --project \"$PWD\" $argv", "function omg_checkpoint", "command omg checkpoint --project \"$PWD\" $argv"}
	case PowerShell:
		required = []string{"function OMG-Preflight", "& omg preflight --project $PWD.Path @args", "function OMG-Checkpoint", "& omg checkpoint --project $PWD.Path @args"}
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
		required = "Where-Object { $_ -like \"$wordToComplete*\" }"
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
