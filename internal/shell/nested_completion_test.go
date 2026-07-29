package shell

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

func TestCompletionPathCandidatesTraverseOneCommandLevel(t *testing.T) {
	tests := []struct {
		command     string
		selected    string
		contains    []string
		excludes    []string
		description map[string]string
	}{
		{
			command: "task", selected: "cl",
			contains: []string{"create", "get", "claim", "transition", "--project", "--payload-file"},
			excludes: []string{"apply", "powershell"},
		},
		{
			command: "task", selected: "claim",
			contains:    []string{"--project", "--workspace", "--store", "--payload-file", "--idempotency-key", "--help"},
			excludes:    []string{"create", "get", "claim", "transition", "run-create", "run-transition"},
			description: map[string]string{"--idempotency-key": "Provide the required mutation replay key."},
		},
		{
			command: "help", selected: "task",
			contains:    []string{"create", "get", "claim", "transition", "--project", "--payload-file"},
			excludes:    []string{"migration", "backup", "powershell"},
			description: map[string]string{"claim": "Claim one ready task for exactly one session."},
		},
		{
			command: "help", selected: "unknown",
			contains: []string{"task", "migration", "receipt", "--help"},
			excludes: []string{"claim", "transition"},
		},
	}
	for _, test := range tests {
		t.Run(test.command+"/"+test.selected, func(t *testing.T) {
			candidates := CompletionPathCandidates(test.command, test.selected)
			seen := make(map[string]bool, len(candidates))
			for _, candidate := range candidates {
				seen[candidate] = true
			}
			for _, want := range test.contains {
				if !seen[want] {
					t.Errorf("CompletionPathCandidates(%q, %q) missing %q: %v", test.command, test.selected, want, candidates)
				}
			}
			for _, forbidden := range test.excludes {
				if seen[forbidden] {
					t.Errorf("CompletionPathCandidates(%q, %q) contains unrelated %q: %v", test.command, test.selected, forbidden, candidates)
				}
			}
			items := CompletionPathItems(test.command, test.selected)
			itemDescriptions := make(map[string]string, len(items))
			for _, item := range items {
				itemDescriptions[item.Value] = item.Description
			}
			for value, want := range test.description {
				if got := itemDescriptions[value]; got != want {
					t.Errorf("CompletionPathItems(%q, %q) description for %q = %q, want %q", test.command, test.selected, value, got, want)
				}
			}
			if len(candidates) > 0 {
				candidates[0] = "mutated"
				if CompletionPathCandidates(test.command, test.selected)[0] == "mutated" {
					t.Fatal("CompletionPathCandidates returned shared mutable storage")
				}
			}
			if len(items) > 0 {
				items[0].Value = "mutated"
				items[0].Description = "mutated"
				fresh := CompletionPathItems(test.command, test.selected)
				if fresh[0].Value == "mutated" || fresh[0].Description == "mutated" {
					t.Fatal("CompletionPathItems returned shared mutable storage")
				}
			}
		})
	}
}

func TestNestedCompletionGeneratorsContainLeafOptionRouting(t *testing.T) {
	tests := []struct {
		target Shell
		want   []string
	}{
		{Bash, []string{
			`local selected="${COMP_WORDS[2]}"`,
			`case "$selected" in`,
			`create|get|claim|transition|run-create|run-transition|finish-lite) candidates='--help -h --json`,
		}},
		{Zsh, []string{
			`local selected="${words[3]}"`,
			`case "$selected" in`,
			`create|get|claim|transition|run-create|run-transition|finish-lite) choices=(`,
		}},
		{Fish, []string{
			`__fish_seen_subcommand_from task; and not __fish_seen_subcommand_from create get claim transition run-create run-transition finish-lite`,
			`__fish_seen_subcommand_from task`,
		}},
		{PowerShell, []string{
			`$selected = if ($elements.Count -gt 2)`,
			`if ($selected -in @('create', 'get', 'claim', 'transition', 'run-create', 'run-transition', 'finish-lite'))`,
		}},
	}
	for _, test := range tests {
		result, err := Completion(test.target)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range test.want {
			if !strings.Contains(result.Content, want) {
				t.Errorf("%s completion missing nested routing marker %q", test.target, want)
			}
		}
	}
}

func TestBashCompletionTraversesCommandSubcommandPaths(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is unavailable")
	}
	result, err := Completion(Bash)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		words  []string
		cursor int
		want   []string
		forbid []string
	}{
		{words: []string{"omg", "task", "cl"}, cursor: 2, want: []string{"claim"}, forbid: []string{"apply", "powershell"}},
		{words: []string{"omg", "task", "claim", "--"}, cursor: 3, want: []string{"--project", "--payload-file", "--idempotency-key"}, forbid: []string{"create", "transition", "run-create"}},
		{words: []string{"omg", "help", "task", "cl"}, cursor: 3, want: []string{"claim"}, forbid: []string{"migration", "backup", "powershell"}},
	}
	for _, test := range tests {
		t.Run(strings.Join(test.words, "_"), func(t *testing.T) {
			quoted := make([]string, len(test.words))
			for index, word := range test.words {
				quoted[index] = fmt.Sprintf("%q", word)
			}
			script := result.Content + "\n" +
				"COMP_WORDS=(" + strings.Join(quoted, " ") + ")\n" +
				"COMP_CWORD=" + fmt.Sprint(test.cursor) + "\n" +
				"_omg_completion\n" +
				"printf '%s\\n' \"${COMPREPLY[@]}\"\n"
			command := exec.Command(bash, "--noprofile", "--norc")
			command.Stdin = strings.NewReader(script)
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("bash completion %v failed: %v\n%s", test.words, err, output)
			}
			seen := make(map[string]bool)
			for _, candidate := range strings.Fields(string(output)) {
				seen[candidate] = true
			}
			for _, want := range test.want {
				if !seen[want] {
					t.Errorf("bash completion %v missing %q: %q", test.words, want, output)
				}
			}
			for _, forbidden := range test.forbid {
				if seen[forbidden] {
					t.Errorf("bash completion %v contains sibling/unrelated %q: %q", test.words, forbidden, output)
				}
			}
		})
	}
}
