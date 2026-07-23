// Package shell generates optional, static OMG shell adapter scripts.
package shell

import "errors"

// Shell identifies a supported target shell.
type Shell string

const (
	Bash       Shell = "bash"
	Zsh        Shell = "zsh"
	Fish       Shell = "fish"
	PowerShell Shell = "powershell"
)

// Result is a generated, newline-terminated script for a target shell.
type Result struct {
	Shell   Shell  `json:"shell"`
	Content string `json:"content"`
}

// ErrUnsupportedShell is returned for every unsupported shell without
// reflecting the untrusted shell name in the error.
var ErrUnsupportedShell = errors.New("unsupported shell")

// Init returns an optional static shell initialization script. Generating the
// script does not execute OMG or modify the invoking shell or filesystem.
func Init(target Shell) (Result, error) {
	content, ok := initScripts[target]
	if !ok {
		return Result{}, ErrUnsupportedShell
	}
	return Result{Shell: target, Content: content}, nil
}

// Completion returns an optional static OMG completion script. Generating the
// script does not execute OMG or modify the invoking shell or filesystem.
func Completion(target Shell) (Result, error) {
	content, ok := completionScripts[target]
	if !ok {
		return Result{}, ErrUnsupportedShell
	}
	return Result{Shell: target, Content: content}, nil
}

var initScripts = map[Shell]string{
	Bash: `# OMG shell initialization.
omg_preflight() {
    command omg preflight --project "$PWD" "$@"
}
omg_checkpoint() {
    command omg checkpoint --project "$PWD" "$@"
}
`,
	Zsh: `# OMG shell initialization.
omg_preflight() {
    command omg preflight --project "$PWD" "$@"
}
omg_checkpoint() {
    command omg checkpoint --project "$PWD" "$@"
}
`,
	Fish: `# OMG shell initialization.
function omg_preflight
    command omg preflight --project "$PWD" $argv
end
function omg_checkpoint
    command omg checkpoint --project "$PWD" $argv
end
`,
	PowerShell: `# OMG shell initialization.
function OMG-Preflight {
    & omg preflight --project $PWD.Path @args
}
function OMG-Checkpoint {
    & omg checkpoint --project $PWD.Path @args
}
`,
}

var completionScripts = map[Shell]string{
	Bash: `# OMG bash completion.
_omg_completion() {
    local current="${COMP_WORDS[COMP_CWORD]}"
    COMPREPLY=( $(compgen -W 'init version doctor backup migration export import preflight human session delegate task progress dependency message handoff checkpoint board reserve git integration run shell-init completion watch mcp release me tree all plan apply status remove create restore record get resume adopt issue register revoke claim transition run-create run-transition add history list active renew release override inbox thread send deliver read ack show supersede accept reject inventory current latest diff cleanup-plan html json markdown tty serve stdio --json --project --workspace --store --plan --plan-file --approval-file --idempotency-key --session --task --format --runtime --integrity --output --payload' -- "$current") )
}
complete -F _omg_completion omg
`,
	Zsh: `#compdef omg
# OMG zsh completion.
_omg_completion() {
    local -a choices
    choices=(
        'init' 'version' 'doctor' 'backup' 'migration' 'export' 'import' 'preflight'
        'human' 'session' 'delegate' 'task' 'progress' 'dependency' 'message' 'handoff' 'checkpoint' 'board'
        'reserve' 'git' 'integration' 'run' 'shell-init' 'completion' 'watch' 'mcp' 'release'
        'me' 'tree' 'all' 'plan' 'apply' 'status' 'remove' 'create' 'restore' 'record' 'get'
        'resume' 'adopt' 'issue' 'register' 'revoke' 'claim' 'transition' 'run-create' 'run-transition'
        'add' 'history' 'list' 'active' 'renew' 'release' 'override' 'inbox' 'thread' 'send'
        'deliver' 'read' 'ack' 'show' 'supersede' 'accept' 'reject' 'inventory' 'current' 'latest' 'diff' 'cleanup-plan'
        'html' 'json' 'markdown' 'tty' 'serve' 'stdio'
        '--json' '--project' '--workspace' '--store' '--plan' '--plan-file' '--approval-file'
        '--idempotency-key' '--session' '--task' '--format' '--runtime' '--integrity' '--output' '--payload'
    )
    _describe 'OMG command or flag' choices
}
compdef _omg_completion omg
`,
	Fish: `# OMG fish completion.
complete -c omg -f -a 'init' -a 'version' -a 'doctor' -a 'backup' -a 'migration' -a 'export' -a 'import' -a 'preflight'
complete -c omg -f -a 'human' -a 'session' -a 'delegate' -a 'task' -a 'progress' -a 'dependency' -a 'message' -a 'handoff' -a 'checkpoint' -a 'board'
complete -c omg -f -a 'reserve' -a 'git' -a 'integration' -a 'run' -a 'shell-init' -a 'completion' -a 'watch' -a 'mcp' -a 'release'
complete -c omg -f -a 'me' -a 'tree' -a 'all' -a 'plan' -a 'apply' -a 'status' -a 'remove' -a 'create' -a 'restore' -a 'record' -a 'get'
complete -c omg -f -a 'resume' -a 'adopt' -a 'issue' -a 'register' -a 'revoke' -a 'claim' -a 'transition' -a 'run-create' -a 'run-transition'
complete -c omg -f -a 'add' -a 'history' -a 'list' -a 'active' -a 'renew' -a 'release' -a 'override' -a 'inbox' -a 'thread' -a 'send'
complete -c omg -f -a 'deliver' -a 'read' -a 'ack' -a 'show' -a 'supersede' -a 'accept' -a 'reject' -a 'inventory' -a 'current' -a 'latest' -a 'diff' -a 'cleanup-plan'
complete -c omg -f -a 'html' -a 'json' -a 'markdown' -a 'tty' -a 'serve' -a 'stdio'
complete -c omg -f -a '--json' -a '--project' -a '--workspace' -a '--store' -a '--plan' -a '--plan-file' -a '--approval-file'
complete -c omg -f -a '--idempotency-key' -a '--session' -a '--task' -a '--format' -a '--runtime' -a '--integrity' -a '--output' -a '--payload'
`,
	PowerShell: `# OMG PowerShell completion.
Register-ArgumentCompleter -CommandName omg -ScriptBlock {
    param($commandName, $wordToComplete, $cursorPosition)
    @(
        'init', 'version', 'doctor', 'backup', 'migration', 'export', 'import', 'preflight',
        'human', 'session', 'delegate', 'task', 'progress', 'dependency', 'message', 'handoff', 'checkpoint', 'board',
        'reserve', 'git', 'integration', 'run', 'shell-init', 'completion', 'watch', 'mcp', 'release',
        'me', 'tree', 'all', 'plan', 'apply', 'status', 'remove', 'create', 'restore', 'record', 'get',
        'resume', 'adopt', 'issue', 'register', 'revoke', 'claim', 'transition', 'run-create', 'run-transition',
        'add', 'history', 'list', 'active', 'renew', 'release', 'override', 'inbox', 'thread', 'send',
        'deliver', 'read', 'ack', 'show', 'supersede', 'accept', 'reject', 'inventory', 'current', 'latest', 'diff', 'cleanup-plan',
        'html', 'json', 'markdown', 'tty', 'serve', 'stdio',
        '--json', '--project', '--workspace', '--store', '--plan', '--plan-file', '--approval-file',
        '--idempotency-key', '--session', '--task', '--format', '--runtime', '--integrity', '--output', '--payload'
    ) | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
        [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
    }
}
`,
}
