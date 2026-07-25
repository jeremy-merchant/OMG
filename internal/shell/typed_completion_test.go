package shell

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGeneratedCompletionsRouteTypedOptionValues(t *testing.T) {
	tests := []struct {
		target Shell
		want   []string
	}{
		{Bash, []string{
			`local previous="${COMP_WORDS[COMP_CWORD-1]}"`,
			`--project|--workspace)`,
			`compgen -d -- "$current"`,
			`--store|--output|--plan-file|--approval-file|--payload-file)`,
			`compgen -f -- "$current"`,
			`compgen -W 'tty markdown html json'`,
		}},
		{Zsh, []string{
			`local previous="${words[CURRENT-1]}"`,
			`_files -/`,
			`_files`,
			`_describe 'OMG output format' choices`,
		}},
		{Fish, []string{
			`complete -c omg -l 'project' -r -f -a '(__fish_complete_directories)'`,
			`complete -c omg -l 'store' -r -F`,
			`complete -c omg -l 'format' -x -a 'tty markdown html json'`,
		}},
		{PowerShell, []string{
			`$previous -in @('--project', '--workspace')`,
			`Get-ChildItem -LiteralPath . -Force -Directory`,
			`$previous -in @('--store', '--output', '--plan-file', '--approval-file', '--payload-file')`,
			`$previous -eq '--format'`,
			`'html|Render the self-contained operator board.'`,
		}},
	}
	for _, test := range tests {
		result, err := Completion(test.target)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range test.want {
			if !strings.Contains(result.Content, want) {
				t.Errorf("%s completion missing typed-value marker %q", test.target, want)
			}
		}
	}
}

func TestBashCompletionUsesDirectoryFileAndFormatCandidates(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is unavailable")
	}
	result, err := Completion(Bash)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "project-alpha"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "payload.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		words  string
		cursor int
		want   []string
		forbid []string
	}{
		{words: "omg task --project pro", cursor: 3, want: []string{"project-alpha"}, forbid: []string{"payload.json", "claim"}},
		{words: "omg task --payload-file pay", cursor: 3, want: []string{"payload.json"}, forbid: []string{"claim", "apply"}},
		{words: "omg export html --format h", cursor: 4, want: []string{"html"}, forbid: []string{"handoff", "history"}},
		{words: "omg export html --output pay", cursor: 4, want: []string{"payload.json"}, forbid: []string{"markdown", "claim"}},
	}
	for _, test := range tests {
		script := result.Content + "\n" +
			"COMP_WORDS=(" + test.words + ")\n" +
			"COMP_CWORD=" + fmt.Sprint(test.cursor) + "\n" +
			"_omg_completion\n" +
			"printf '%s\\n' \"${COMPREPLY[@]}\"\n"
		command := exec.Command(bash, "--noprofile", "--norc")
		command.Dir = root
		command.Stdin = strings.NewReader(script)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("bash completion %q failed: %v\n%s", test.words, err, output)
		}
		seen := make(map[string]bool)
		for _, candidate := range strings.Fields(string(output)) {
			seen[candidate] = true
		}
		for _, want := range test.want {
			if !seen[want] {
				t.Errorf("bash completion %q missing %q: %q", test.words, want, output)
			}
		}
		for _, forbidden := range test.forbid {
			if seen[forbidden] {
				t.Errorf("bash completion %q contains unrelated %q: %q", test.words, forbidden, output)
			}
		}
	}
}
