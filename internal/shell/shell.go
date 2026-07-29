// Package shell generates optional, static OMG shell adapter scripts.
package shell

import (
	"errors"
	"strings"
)

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

// Completion returns a deterministic contextual OMG completion script.
// Generating the script does not execute OMG or modify the invoking shell or
// filesystem.
func Completion(target Shell) (Result, error) {
	content, ok := generateCompletion(target)
	if !ok {
		return Result{}, ErrUnsupportedShell
	}
	return Result{Shell: target, Content: content}, nil
}

// CompletionWords returns a defensive flattened copy of all known completion
// words. It exists for contract tests and documentation tooling.
func CompletionWords() []string {
	return append([]string(nil), completionVocabulary...)
}

// CompletionItem is one context-aware shell completion candidate.
type CompletionItem struct {
	Value       string
	Description string
}

// CompletionCandidates returns the defensive candidate set shown after one
// top-level command. An empty command returns top-level discovery candidates.
func CompletionCandidates(command string) []string {
	return append([]string(nil), completionCandidates(command)...)
}

// CompletionItems returns defensive candidates with concise descriptions for
// shells that can surface completion help next to each value.
func CompletionItems(command string) []CompletionItem {
	values := completionCandidates(command)
	items := make([]CompletionItem, 0, len(values))
	for _, value := range values {
		items = append(items, CompletionItem{Value: value, Description: completionDescription(command, value)})
	}
	return items
}

// CompletionPathCandidates traverses one command/subcommand level. Once a
// valid leaf subcommand is selected, sibling subcommands are removed and only
// options remain. For `help`, the selected command becomes the active family.
func CompletionPathCandidates(command, subcommand string) []string {
	return append([]string(nil), completionPathCandidates(command, subcommand)...)
}

// CompletionPathItems is CompletionPathCandidates with static descriptions.
func CompletionPathItems(command, subcommand string) []CompletionItem {
	effective := completionEffectiveCommand(command, subcommand)
	values := completionPathCandidates(command, subcommand)
	items := make([]CompletionItem, 0, len(values))
	for _, value := range values {
		items = append(items, CompletionItem{Value: value, Description: completionDescription(effective, value)})
	}
	return items
}

// CompletionDescription returns the static description associated with one
// candidate. It never reflects untrusted input in the returned text.
func CompletionDescription(command, value string) string {
	return completionDescription(command, value)
}

var initScripts = map[Shell]string{
	Bash: `# OMG shell initialization.
omg_preflight() {
    command omg preflight --project "$PWD" "$@"
}
omg_board() {
    command omg board all --project "$PWD" "$@"
}
omg_checkpoint() {
    command omg checkpoint --project "$PWD" "$@"
}
`,
	Zsh: `# OMG shell initialization.
omg_preflight() {
    command omg preflight --project "$PWD" "$@"
}
omg_board() {
    command omg board all --project "$PWD" "$@"
}
omg_checkpoint() {
    command omg checkpoint --project "$PWD" "$@"
}
`,
	Fish: `# OMG shell initialization.
function omg_preflight
    command omg preflight --project "$PWD" $argv
end
function omg_board
    command omg board all --project "$PWD" $argv
end
function omg_checkpoint
    command omg checkpoint --project "$PWD" $argv
end
`,
	PowerShell: `# OMG shell initialization.
function OMG-Preflight {
    & omg preflight --project $PWD.Path @args
}
function OMG-Board {
    & omg board all --project $PWD.Path @args
}
function OMG-Checkpoint {
    & omg checkpoint --project $PWD.Path @args
}
`,
}

var completionCommands = []string{
	"help", "init", "preflight", "status", "doctor", "migration", "backup", "release", "version", "agent", "worker",
	"human", "session", "delegate", "checkpoint", "task", "progress", "dependency", "message", "handoff", "reserve",
	"board", "git", "orphan", "canary", "export", "import", "integration", "watch", "run", "example", "shell-init", "completion", "mcp", "receipt",
}

var completionOptions = []string{
	"--help", "-h", "--json", "--verbose", "--integrity", "--status", "--stdio", "--payload-stdin",
	"--project", "--workspace", "--store", "--output", "--plan-file", "--approval-file", "--idempotency-key",
	"--format", "--session", "--task", "--runtime", "--controller-session", "--human", "--role", "--payload", "--payload-file",
}

var completionCommandDescriptions = map[string]string{
	"help":        "Explore command families and contextual help.",
	"init":        "Create local canonical state.",
	"preflight":   "Check readiness, identity, blockers, inbox, reservations, and Git.",
	"status":      "Show the compact operator summary and workflow bottleneck.",
	"doctor":      "Inspect store, platform, recovery, and integrity health.",
	"migration":   "Plan or apply an explicitly approved schema migration.",
	"backup":      "Create or validate private recovery artifacts.",
	"release":     "Inspect publication and source-bound readiness.",
	"version":     "Print the current build version.",
	"agent":       "Install, inspect, diagnose, or remove global coding-agent discovery surfaces.",
	"worker":      "Bootstrap one worker directly into its scoped task and inbox.",
	"human":       "Create or inspect canonical human identities.",
	"session":     "Create, resume, adopt, or import agent sessions.",
	"delegate":    "Issue, redeem, or revoke bounded delegation.",
	"checkpoint":  "Record session liveness and continuation.",
	"task":        "Create, inspect, claim, or transition tasks and runs.",
	"progress":    "Record or inspect done / doing / next.",
	"dependency":  "Connect prerequisite work and reject cycles.",
	"message":     "Send messages and record delivery / read / acknowledgement.",
	"handoff":     "Transfer work, evidence, risk, and ownership decisions.",
	"reserve":     "Declare and inspect path intent and conflicts.",
	"board":       "Render operator views for sessions, tasks, Git, or all work.",
	"git":         "Observe repository state and canonical ownership.",
	"orphan":      "Find read-only Git and handoff orphan risks in the selected repository.",
	"canary":      "Record exact-SHA canary start and finish receipts.",
	"export":      "Write a private static board snapshot.",
	"import":      "Import one normalized external record.",
	"integration": "Manage bounded project instruction integration.",
	"watch":       "Follow local coordination changes.",
	"run":         "Execute a guarded child command.",
	"example":     "List or show copyable examples generated from the live help contract.",
	"shell-init":  "Generate shell initialization.",
	"completion":  "Generate completion scripts from the command contract.",
	"mcp":         "Serve OMG over protocol-only MCP stdio.",
	"receipt":     "Inspect canonical command receipts.",
}

var completionOptionDescriptions = map[string]string{
	"--help": "Show contextual help.", "-h": "Show contextual help.",
	"--json":               "Emit the stable machine envelope.",
	"--verbose":            "Include detailed preflight records.",
	"--integrity":          "Run the optional SQLite integrity check.",
	"--status":             "Include final status where supported.",
	"--stdio":              "Use protocol-only standard input and output.",
	"--payload-stdin":      "Read one strict payload from standard input.",
	"--project":            "Select a project root.",
	"--workspace":          "Select a local multi-project workspace.",
	"--store":              "Select an exact SQLite store.",
	"--output":             "Create a new owner-only output file.",
	"--plan-file":          "Read the exact private plan file.",
	"--approval-file":      "Read the matching private approval file.",
	"--idempotency-key":    "Provide the required mutation replay key.",
	"--format":             "Choose tty, markdown, html, or json output.",
	"--session":            "Select one canonical session.",
	"--task":               "Select one canonical task.",
	"--runtime":            "Select a configured child runtime.",
	"--controller-session": "Select the controller session for worker bootstrap.",
	"--human":              "Select the canonical human owner for worker bootstrap.",
	"--role":               "Select the worker role.",
	"--payload":            "Supply one strict inline JSON payload.",
	"--payload-file":       "Read one strict owner-only payload file.",
}

var completionValueDescriptions = map[string]map[string]string{
	"migration":   {"plan": "Create a read-only checksummed migration plan.", "apply": "Apply the exact approved migration plan."},
	"backup":      {"create": "Create and verify a private backup.", "restore": "Validate a guarded restore request."},
	"release":     {"status": "Inspect local publication readiness."},
	"agent":       {"install": "Install always-on OMG skills and global instructions.", "status": "Inspect global agent discovery surfaces.", "doctor": "Diagnose discovery, permissions, and drift.", "uninstall": "Remove only OMG-managed global surfaces."},
	"worker":      {"bootstrap": "Ensure worker identity and task ownership, then return its scoped board and inbox."},
	"human":       {"create": "Create or supersede a human identity.", "get": "Read one canonical human identity."},
	"session":     {"create": "Create a new canonical session.", "resume": "Resume a known session with continuation metadata.", "adopt": "Adopt interrupted work without fabricating completion.", "import": "Import a normalized session record.", "archive": "Remove a finished session from active counts."},
	"delegate":    {"issue": "Issue a short-lived project-scoped delegation.", "register": "Redeem a delegation from stdin or a private file.", "revoke": "Revoke an unconsumed delegation."},
	"checkpoint":  {"record": "Append a canonical liveness checkpoint."},
	"task":        {"create": "Create a task with an atomic display number.", "get": "Read one task and its canonical state.", "claim": "Claim one ready task for exactly one session.", "transition": "Apply a validated task state transition.", "run-create": "Create an execution run for a task and session.", "run-transition": "Apply a validated run state transition."},
	"progress":    {"add": "Append a done, doing, and next update.", "history": "Read immutable progress history."},
	"dependency":  {"add": "Add a validated dependency edge.", "list": "Read the dependency graph."},
	"message":     {"send": "Send an inert typed coordination message.", "inbox": "Read one recipient inbox.", "thread": "Read one message thread.", "deliver": "Record message delivery.", "read": "Record message read state.", "ack": "Record explicit acknowledgement."},
	"handoff":     {"create": "Create an immutable handoff with evidence and risk.", "show": "Read one safe handoff projection.", "history": "Read handoff history for a task.", "lifecycle": "Read append-only integration lifecycle events.", "advance": "Append a validated integration lifecycle event.", "supersede": "Replace a handoff through a superseding record.", "accept": "Accept a submitted handoff explicitly.", "reject": "Reject a submitted handoff explicitly.", "adopt": "Adopt orphaned canonical work."},
	"reserve":     {"add": "Add an advisory path reservation.", "list": "List reservation records.", "active": "List active reservations.", "history": "Read reservation history.", "renew": "Renew a live reservation.", "release": "Release a reservation with a reason.", "override": "Record a human override without hiding conflict."},
	"board":       {"summary": "Count states and show workflow bottlenecks.", "me": "Show one session and adjacent work.", "tree": "Show session and task lineage.", "task": "Show one task and adjacent coordination facts.", "all": "Show the complete safe operator snapshot.", "git": "Focus on Git observations and risks.", "tty": "Render terminal output.", "markdown": "Render Markdown output.", "html": "Render the self-contained operator board.", "json": "Render the canonical JSON view model."},
	"git":         {"inventory": "Record a safe Git inventory observation.", "current": "Read the current observation.", "latest": "Read the latest observation.", "history": "Read observation history.", "diff": "Compare two observations.", "cleanup-plan": "Produce a non-destructive cleanup plan.", "reconcile": "Verify source SHA/tree inclusion in an integration ref.", "adopt": "Change canonical ownership metadata only."},
	"orphan":      {"scan": "Scan selected-repository orphan risks without mutation."},
	"canary":      {"start": "Pin an exact integration SHA/tree.", "finish": "Record a structured canary result."},
	"export":      {"html": "Write a self-contained HTML board.", "markdown": "Write a Markdown snapshot.", "tty": "Write terminal text.", "json": "Write the canonical JSON model."},
	"import":      {"record": "Import one normalized record."},
	"integration": {"queue": "Show handoffs awaiting integration lifecycle work.", "plan": "Preview instruction integration changes.", "apply": "Apply the reviewed integration plan.", "status": "Inspect current integration state.", "remove": "Remove only the managed integration block."},
	"watch":       {"status": "Report whether a watcher is active."},
	"example":     {"list": "List available example topics.", "show": "Render one current command example."},
	"shell-init":  {"bash": "Generate Bash initialization.", "zsh": "Generate Zsh initialization.", "fish": "Generate Fish initialization.", "powershell": "Generate PowerShell initialization."},
	"completion":  {"bash": "Generate Bash completion.", "zsh": "Generate Zsh completion.", "fish": "Generate Fish completion.", "powershell": "Generate PowerShell completion."},
	"mcp":         {"serve": "Serve MCP over stdio without a network listener."},
	"receipt":     {"get": "Read one canonical command receipt.", "list": "List canonical command receipts."},
}

var completionByCommand = map[string][]string{
	"help":        completionCommands,
	"init":        {},
	"preflight":   {},
	"status":      {},
	"doctor":      {},
	"migration":   {"plan", "apply"},
	"backup":      {"create", "restore"},
	"release":     {"status"},
	"version":     {},
	"agent":       {"install", "status", "doctor", "uninstall"},
	"worker":      {"bootstrap"},
	"human":       {"create", "get"},
	"session":     {"create", "resume", "adopt", "import", "archive"},
	"delegate":    {"issue", "register", "revoke"},
	"checkpoint":  {"record"},
	"task":        {"create", "get", "claim", "transition", "run-create", "run-transition"},
	"progress":    {"add", "history"},
	"dependency":  {"add", "list"},
	"message":     {"send", "inbox", "thread", "deliver", "read", "ack"},
	"handoff":     {"create", "show", "history", "lifecycle", "advance", "supersede", "accept", "reject", "adopt"},
	"reserve":     {"add", "list", "active", "history", "renew", "release", "override"},
	"board":       {"summary", "me", "tree", "task", "all", "git", "tty", "markdown", "html", "json"},
	"git":         {"inventory", "current", "latest", "history", "diff", "cleanup-plan", "reconcile", "adopt"},
	"orphan":      {"scan"},
	"canary":      {"start", "finish"},
	"export":      {"html", "markdown", "tty", "json"},
	"import":      {"record"},
	"integration": {"queue", "plan", "apply", "status", "remove"},
	"watch":       {"status"},
	"run":         {},
	"example":     {"list", "show"},
	"shell-init":  {"bash", "zsh", "fish", "powershell"},
	"completion":  {"bash", "zsh", "fish", "powershell"},
	"mcp":         {"serve"},
	"receipt":     {"get", "list"},
}

var completionVocabulary = buildCompletionVocabulary()

func completionCandidates(command string) []string {
	if command == "" {
		return uniqueCompletionWords(completionCommands, []string{"--help", "-h"})
	}
	values, known := completionByCommand[command]
	if !known {
		return uniqueCompletionWords(completionCommands, []string{"--help", "-h"})
	}
	return uniqueCompletionWords(values, completionOptions)
}

func completionPathCandidates(command, subcommand string) []string {
	if command == "help" {
		if _, known := completionByCommand[subcommand]; known {
			return completionCandidates(subcommand)
		}
		return completionCandidates(command)
	}
	values, known := completionByCommand[command]
	if !known {
		return completionCandidates("")
	}
	if containsCompletionValue(values, subcommand) {
		return uniqueCompletionWords(completionOptions)
	}
	return completionCandidates(command)
}

func completionEffectiveCommand(command, subcommand string) string {
	if command == "help" {
		if _, known := completionByCommand[subcommand]; known {
			return subcommand
		}
	}
	return command
}

func containsCompletionValue(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func completionDescription(command, value string) string {
	if description := completionOptionDescriptions[value]; description != "" {
		return description
	}
	if command == "" || command == "help" {
		if description := completionCommandDescriptions[value]; description != "" {
			return description
		}
		return "OMG command or option."
	}
	if values := completionValueDescriptions[command]; values != nil {
		if description := values[value]; description != "" {
			return description
		}
	}
	return "OMG " + command + " value."
}

func buildCompletionVocabulary() []string {
	groups := [][]string{completionCommands, completionOptions}
	for _, command := range completionCommands {
		groups = append(groups, completionByCommand[command])
	}
	return uniqueCompletionWords(groups...)
}

func uniqueCompletionWords(groups ...[]string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0)
	for _, group := range groups {
		for _, word := range group {
			if word == "" {
				continue
			}
			if _, exists := seen[word]; exists {
				continue
			}
			seen[word] = struct{}{}
			result = append(result, word)
		}
	}
	return result
}

func generateCompletion(target Shell) (string, bool) {
	switch target {
	case Bash:
		return bashCompletion(), true
	case Zsh:
		return zshCompletion(), true
	case Fish:
		return fishCompletion(), true
	case PowerShell:
		return powerShellCompletion(), true
	default:
		return "", false
	}
}

func bashCompletion() string {
	var output strings.Builder
	output.WriteString(`# OMG bash completion.
_omg_completion() {
    local current="${COMP_WORDS[COMP_CWORD]}"
    local previous="${COMP_WORDS[COMP_CWORD-1]}"
    local command="${COMP_WORDS[1]}"
    local selected="${COMP_WORDS[2]}"
    local candidates
    case "$previous" in
        --project|--workspace)
            COMPREPLY=( $(compgen -d -- "$current") )
            return
            ;;
        --store|--output|--plan-file|--approval-file|--payload-file)
            COMPREPLY=( $(compgen -f -- "$current") )
            return
            ;;
        --format)
            COMPREPLY=( $(compgen -W 'tty markdown html json' -- "$current") )
            return
            ;;
    esac
    case "$command" in
`)
	writeShellCases(&output, "        ", ") candidates='", "' ;;\n")
	output.WriteString("        *) candidates='")
	output.WriteString(strings.Join(completionCandidates(""), " "))
	output.WriteString(`' ;;
    esac
    COMPREPLY=( $(compgen -W "$candidates" -- "$current") )
}
complete -F _omg_completion omg
`)
	return output.String()
}

func zshCompletion() string {
	var output strings.Builder
	output.WriteString(`#compdef omg
# OMG zsh completion.
_omg_completion() {
    local command="${words[2]}"
    local selected="${words[3]}"
    local previous="${words[CURRENT-1]}"
    local -a choices
    case "$previous" in
        --project|--workspace)
            _files -/
            return
            ;;
        --store|--output|--plan-file|--approval-file|--payload-file)
            _files
            return
            ;;
        --format)
            choices=(
                'tty:Render terminal output.'
                'markdown:Render Markdown output.'
                'html:Render the self-contained operator board.'
                'json:Render the canonical JSON view model.'
            )
            _describe 'OMG output format' choices
            return
            ;;
    esac
    case "$command" in
`)
	writeZshCases(&output)
	output.WriteString("        *) choices=(\n")
	writeQuotedItemRows(&output, CompletionItems(""), "            ", " ", 4, true)
	output.WriteString(`        ) ;;
    esac
    _describe 'OMG command, value, or flag' choices
}
compdef _omg_completion omg
`)
	return output.String()
}

func fishCompletion() string {
	var output strings.Builder
	output.WriteString("# OMG fish completion.\n")
	writeFishItems(&output, "__fish_use_subcommand", CompletionItems(""))
	for _, command := range completionCommands {
		base := "__fish_seen_subcommand_from " + command
		values := completionByCommand[command]
		if command == "help" {
			condition := base + "; and not __fish_seen_subcommand_from " + strings.Join(completionCommands, " ")
			writeFishItems(&output, condition, commandCompletionItems())
			for _, selected := range completionCommands {
				selectedValues := completionByCommand[selected]
				selectedCondition := base + "; and __fish_seen_subcommand_from " + selected
				if len(selectedValues) > 0 {
					selectedCondition += "; and not __fish_seen_subcommand_from " + strings.Join(selectedValues, " ")
				}
				writeFishItems(&output, selectedCondition, completionValueItems(selected))
				writeFishItems(&output, base+"; and __fish_seen_subcommand_from "+selected, completionOptionItems())
			}
			continue
		}
		if len(values) > 0 {
			writeFishItems(&output, base+"; and not __fish_seen_subcommand_from "+strings.Join(values, " "), completionValueItems(command))
		}
		writeFishItems(&output, base, completionOptionItems())
	}
	writeFishOptionValueCompletions(&output)
	return output.String()
}

func powerShellCompletion() string {
	var output strings.Builder
	output.WriteString(`# OMG PowerShell completion.
Register-ArgumentCompleter -CommandName omg -ScriptBlock {
    param($commandName, $parameterName, $wordToComplete, $commandAst, $fakeBoundParameters)
    $elements = $commandAst.CommandElements
    $command = if ($elements.Count -gt 1) { $elements[1].Extent.Text } else { '' }
    $selected = if ($elements.Count -gt 2) { $elements[2].Extent.Text } else { '' }
    $previous = if ($elements.Count -gt 1) { $elements[$elements.Count - 2].Extent.Text } else { '' }
    if ($previous -in @('--project', '--workspace')) {
        Get-ChildItem -LiteralPath . -Force -Directory -ErrorAction SilentlyContinue |
            Where-Object { $_.Name -like "$wordToComplete*" } |
            ForEach-Object { [System.Management.Automation.CompletionResult]::new($_.Name, $_.Name, 'ParameterValue', 'Directory') }
        return
    }
    if ($previous -in @('--store', '--output', '--plan-file', '--approval-file', '--payload-file')) {
        Get-ChildItem -LiteralPath . -Force -ErrorAction SilentlyContinue |
            Where-Object { $_.Name -like "$wordToComplete*" } |
            ForEach-Object { [System.Management.Automation.CompletionResult]::new($_.Name, $_.Name, 'ParameterValue', 'File or directory') }
        return
    }
    if ($previous -eq '--format') {
        @(
            'tty|Render terminal output.',
            'markdown|Render Markdown output.',
            'html|Render the self-contained operator board.',
            'json|Render the canonical JSON view model.'
        ) | ForEach-Object {
            $parts = $_ -split '\\|', 2
            if ($parts[0] -like "$wordToComplete*") {
                [System.Management.Automation.CompletionResult]::new($parts[0], $parts[0], 'ParameterValue', $parts[1])
            }
        }
        return
    }
    $candidates = switch ($command) {
`)
	for _, command := range completionCommands {
		output.WriteString("        '")
		output.WriteString(command)
		output.WriteString("' { ")
		values := completionByCommand[command]
		if command == "help" {
			output.WriteString("switch ($selected) {\n")
			for _, selected := range completionCommands {
				output.WriteString("            '")
				output.WriteString(selected)
				output.WriteString("' { @(\n")
				writePowerShellItemRows(&output, CompletionItems(selected), "                ", 3)
				output.WriteString("            ) }\n")
			}
			output.WriteString("            default { @(\n")
			writePowerShellItemRows(&output, commandCompletionItems(), "                ", 3)
			output.WriteString("            ) }\n        } }\n")
		} else if len(values) > 0 {
			output.WriteString("if ($selected -in @(")
			for index, value := range values {
				if index > 0 {
					output.WriteString(", ")
				}
				output.WriteString("'")
				output.WriteString(value)
				output.WriteString("'")
			}
			output.WriteString(")) { @(\n")
			writePowerShellItemRows(&output, completionOptionItems(), "            ", 3)
			output.WriteString("        ) } else { @(\n")
			writePowerShellItemRows(&output, CompletionItems(command), "            ", 3)
			output.WriteString("        ) } }\n")
		} else {
			output.WriteString("@(\n")
			writePowerShellItemRows(&output, CompletionItems(command), "            ", 3)
			output.WriteString("        ) }\n")
		}
	}
	output.WriteString("        default { @(\n")
	writePowerShellItemRows(&output, CompletionItems(""), "            ", 3)
	output.WriteString(`        ) }
    }
    $candidates | ForEach-Object {
        $parts = $_ -split '\|', 2
        if ($parts[0] -like "$wordToComplete*") {
            [System.Management.Automation.CompletionResult]::new($parts[0], $parts[0], 'ParameterValue', $parts[1])
        }
    }
}
`)
	return output.String()
}

func writeShellCases(output *strings.Builder, indent, middle, suffix string) {
	for _, command := range completionCommands {
		output.WriteString(indent)
		output.WriteString(command)
		output.WriteString(")\n")
		values := completionByCommand[command]
		if command == "help" {
			output.WriteString(indent + "    case \"$selected\" in\n")
			for _, selected := range completionCommands {
				output.WriteString(indent + "        " + selected + ") candidates='")
				output.WriteString(strings.Join(completionCandidates(selected), " "))
				output.WriteString("' ;;\n")
			}
			output.WriteString(indent + "        *) candidates='")
			output.WriteString(strings.Join(completionCandidates(command), " "))
			output.WriteString("' ;;\n" + indent + "    esac\n")
		} else if len(values) > 0 {
			output.WriteString(indent + "    case \"$selected\" in\n")
			output.WriteString(indent + "        ")
			output.WriteString(strings.Join(values, "|"))
			output.WriteString(") candidates='")
			output.WriteString(strings.Join(completionOptions, " "))
			output.WriteString("' ;;\n")
			output.WriteString(indent + "        *) candidates='")
			output.WriteString(strings.Join(completionCandidates(command), " "))
			output.WriteString("' ;;\n" + indent + "    esac\n")
		} else {
			output.WriteString(indent + "    candidates='")
			output.WriteString(strings.Join(completionCandidates(command), " "))
			output.WriteString("'\n")
		}
		output.WriteString(indent + "    ;;\n")
	}
}

func writeZshCases(output *strings.Builder) {
	for _, command := range completionCommands {
		output.WriteString("        ")
		output.WriteString(command)
		output.WriteString(")\n")
		values := completionByCommand[command]
		if command == "help" {
			output.WriteString("            case \"$selected\" in\n")
			for _, selected := range completionCommands {
				output.WriteString("                " + selected + ") choices=(\n")
				writeQuotedItemRows(output, CompletionItems(selected), "                    ", " ", 4, true)
				output.WriteString("                ) ;;\n")
			}
			output.WriteString("                *) choices=(\n")
			writeQuotedItemRows(output, CompletionItems(command), "                    ", " ", 4, true)
			output.WriteString("                ) ;;\n            esac\n")
		} else if len(values) > 0 {
			output.WriteString("            case \"$selected\" in\n                ")
			output.WriteString(strings.Join(values, "|"))
			output.WriteString(") choices=(\n")
			writeQuotedItemRows(output, completionOptionItems(), "                    ", " ", 4, true)
			output.WriteString("                ) ;;\n                *) choices=(\n")
			writeQuotedItemRows(output, CompletionItems(command), "                    ", " ", 4, true)
			output.WriteString("                ) ;;\n            esac\n")
		} else {
			output.WriteString("            choices=(\n")
			writeQuotedItemRows(output, CompletionItems(command), "                ", " ", 4, true)
			output.WriteString("            )\n")
		}
		output.WriteString("            ;;\n")
	}
}

func completionOptionItems() []CompletionItem {
	items := make([]CompletionItem, 0, len(completionOptions))
	for _, option := range completionOptions {
		items = append(items, CompletionItem{Value: option, Description: completionDescription("", option)})
	}
	return items
}

func completionValueItems(command string) []CompletionItem {
	values := completionByCommand[command]
	items := make([]CompletionItem, 0, len(values))
	for _, value := range values {
		items = append(items, CompletionItem{Value: value, Description: completionDescription(command, value)})
	}
	return items
}

func writeFishOptionValueCompletions(output *strings.Builder) {
	for _, option := range []struct {
		Name        string
		Mode        string
		Arguments   string
		Description string
	}{
		{Name: "project", Mode: "-r -f", Arguments: "(__fish_complete_directories)", Description: completionOptionDescriptions["--project"]},
		{Name: "workspace", Mode: "-r -f", Arguments: "(__fish_complete_directories)", Description: completionOptionDescriptions["--workspace"]},
		{Name: "store", Mode: "-r -F", Description: completionOptionDescriptions["--store"]},
		{Name: "output", Mode: "-r -F", Description: completionOptionDescriptions["--output"]},
		{Name: "plan-file", Mode: "-r -F", Description: completionOptionDescriptions["--plan-file"]},
		{Name: "approval-file", Mode: "-r -F", Description: completionOptionDescriptions["--approval-file"]},
		{Name: "payload-file", Mode: "-r -F", Description: completionOptionDescriptions["--payload-file"]},
		{Name: "format", Mode: "-x", Arguments: "tty markdown html json", Description: completionOptionDescriptions["--format"]},
	} {
		output.WriteString("complete -c omg -l '")
		output.WriteString(option.Name)
		output.WriteString("' ")
		output.WriteString(option.Mode)
		if option.Arguments != "" {
			output.WriteString(" -a '")
			output.WriteString(option.Arguments)
			output.WriteString("'")
		}
		output.WriteString(" -d '")
		output.WriteString(option.Description)
		output.WriteString("'\n")
	}
}

func commandCompletionItems() []CompletionItem {
	items := make([]CompletionItem, 0, len(completionCommands))
	for _, command := range completionCommands {
		items = append(items, CompletionItem{Value: command, Description: completionDescription("", command)})
	}
	return items
}

func writeFishItems(output *strings.Builder, condition string, items []CompletionItem) {
	for _, item := range items {
		output.WriteString("complete -c omg -f -n '")
		output.WriteString(condition)
		output.WriteString("' -a '")
		output.WriteString(item.Value)
		output.WriteString("' -d '")
		output.WriteString(item.Description)
		output.WriteString("'\n")
	}
}

func writeQuotedItemRows(output *strings.Builder, items []CompletionItem, indent, separator string, perRow int, zsh bool) {
	for index, item := range items {
		if index%perRow == 0 {
			output.WriteString(indent)
		}
		output.WriteString("'")
		output.WriteString(item.Value)
		if zsh {
			output.WriteString(":")
			output.WriteString(item.Description)
		}
		output.WriteString("'")
		if index != len(items)-1 {
			output.WriteString(separator)
		}
		if index%perRow == perRow-1 || index == len(items)-1 {
			output.WriteByte('\n')
		}
	}
}

func writePowerShellItemRows(output *strings.Builder, items []CompletionItem, indent string, perRow int) {
	for index, item := range items {
		if index%perRow == 0 {
			output.WriteString(indent)
		}
		output.WriteString("'")
		output.WriteString(item.Value)
		output.WriteString("|")
		output.WriteString(item.Description)
		output.WriteString("'")
		if index != len(items)-1 {
			output.WriteString(", ")
		}
		if index%perRow == perRow-1 || index == len(items)-1 {
			output.WriteByte('\n')
		}
	}
}

func writeQuotedWordRows(output *strings.Builder, words []string, indent, separator string, perRow int) {
	for index, word := range words {
		if index%perRow == 0 {
			output.WriteString(indent)
		}
		output.WriteString("'")
		output.WriteString(word)
		output.WriteString("'")
		if index != len(words)-1 {
			output.WriteString(separator)
		}
		if index%perRow == perRow-1 || index == len(words)-1 {
			output.WriteByte('\n')
		}
	}
}
