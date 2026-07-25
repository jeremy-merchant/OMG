package cli

import "strings"

type helpSubcommand struct {
	Name        string
	Summary     string
	Example     string
	Mutation    bool
	PayloadNote string
}

type helpCommand struct {
	Name        string
	Group       string
	Summary     string
	Usage       []string
	Subcommands []helpSubcommand
	Options     [][2]string
	Examples    []string
}

type helpWorkflow struct {
	Marker  string
	Name    string
	Command string
	Summary string
}

var helpWorkflows = []helpWorkflow{
	{Marker: "01", Name: "First run", Command: "omg init → omg preflight → omg board all", Summary: "Create state, verify readiness, then inspect the whole coordination graph."},
	{Marker: "02", Name: "Start work", Command: "omg session create → omg task claim → omg checkpoint", Summary: "Establish identity, claim exactly one task, and record liveness."},
	{Marker: "03", Name: "Share state", Command: "omg progress add → omg message send → omg handoff create", Summary: "Publish done / doing / next, coordinate, then transfer evidence and risk."},
	{Marker: "04", Name: "Recover safely", Command: "omg doctor → omg board git → omg backup create", Summary: "Inspect health and repository risk before creating a recovery artifact."},
}

var helpCommands = []helpCommand{
	{Name: "init", Group: "START + VERIFY", Summary: "Initialize the local coordination ledger without applying pending migrations.", Usage: []string{"omg init [selection] [--json]"}, Examples: []string{"omg init --project /absolute/project", "omg init --workspace /absolute/workspace --json"}},
	{Name: "preflight", Group: "START + VERIFY", Summary: "Resolve startup readiness, identity, active work, inbox, blockers, reservations, and Git state.", Usage: []string{"omg preflight [selection] [--session ID] [--json]"}, Options: [][2]string{{"--session <id>", "Select the current session identity"}}, Examples: []string{"omg preflight --project /absolute/project", "omg preflight --project /absolute/project --session SESSION_ID"}},
	{Name: "doctor", Group: "START + VERIFY", Summary: "Inspect configuration, store, platform, recovery, and optional SQLite integrity health.", Usage: []string{"omg doctor [selection] [--integrity] [--json]"}, Options: [][2]string{{"--integrity", "Run the optional SQLite integrity check"}}, Examples: []string{"omg doctor --project /absolute/project --integrity"}},
	{Name: "migration", Group: "START + VERIFY", Summary: "Plan or apply a checksummed schema migration through explicit approval files.", Usage: []string{"omg migration <plan|apply> [selection] [options]"}, Subcommands: []helpSubcommand{{Name: "plan", Summary: "Create a read-only migration plan; optionally save it privately.", Example: "omg migration plan --project /project --output plan.json --json"}, {Name: "apply", Summary: "Apply the exact plan after validating a matching approval and backup.", Example: "omg migration apply --project /project --plan-file plan.json --approval-file approval.json --json", Mutation: true}}, Options: [][2]string{{"--output <path>", "Save a complete private plan"}, {"--plan-file <path>", "Read the exact private plan"}, {"--approval-file <path>", "Read the matching private approval"}}},
	{Name: "backup", Group: "START + VERIFY", Summary: "Create or validate recovery artifacts without weakening migration safeguards.", Usage: []string{"omg backup <create|restore> [selection] [options]"}, Subcommands: []helpSubcommand{{Name: "create", Summary: "Create and verify a private backup, optionally bound to a plan.", Example: "omg backup create --project /project --plan-file plan.json --json"}, {Name: "restore", Summary: "Validate a guarded restore request; never overwrite state implicitly.", Example: "omg backup restore --project /project --payload-file restore.json --json", Mutation: true}}},
	{Name: "release", Group: "START + VERIFY", Summary: "Inspect publication status and source-bound release readiness.", Usage: []string{"omg release status [--json]"}, Subcommands: []helpSubcommand{{Name: "status", Summary: "Report whether this checkout is published."}}},
	{Name: "version", Group: "START + VERIFY", Summary: "Print the current OMG build version.", Usage: []string{"omg version [--json]"}},

	{Name: "human", Group: "COORDINATE WORK", Summary: "Create or inspect canonical human identities; content never grants authority.", Usage: []string{"omg human <create|get> [selection] [payload options]"}, Subcommands: coordinationSubs([]helpSubcommand{{Name: "create", Summary: "Create or supersede a canonical human identity.", Mutation: true}, {Name: "get", Summary: "Read one canonical human identity."}})},
	{Name: "session", Group: "COORDINATE WORK", Summary: "Create, resume, adopt, or import an agent session while preserving lineage and provenance.", Usage: []string{"omg session <create|resume|adopt|import> [selection] [payload options]"}, Subcommands: coordinationSubs([]helpSubcommand{{Name: "create", Summary: "Create a new human-direct or delegated session.", Mutation: true}, {Name: "resume", Summary: "Resume a known session with explicit continuation metadata.", Mutation: true}, {Name: "adopt", Summary: "Adopt interrupted session work without fabricating completion.", Mutation: true}, {Name: "import", Summary: "Import an externally normalized session record.", Mutation: true}})},
	{Name: "delegate", Group: "COORDINATE WORK", Summary: "Issue, redeem, or revoke bounded one-use delegation without placing raw tokens in normal output.", Usage: []string{"omg delegate <issue|register|revoke> [selection] [payload options]"}, Subcommands: coordinationSubs([]helpSubcommand{{Name: "issue", Summary: "Issue a short-lived project-scoped delegation token.", Mutation: true}, {Name: "register", Summary: "Redeem a token from stdin or a private payload file.", Mutation: true, PayloadNote: "Raw delegation tokens are rejected in inline --payload."}, {Name: "revoke", Summary: "Revoke an unconsumed delegation token.", Mutation: true}})},
	{Name: "checkpoint", Group: "COORDINATE WORK", Summary: "Record session liveness and continuation state.", Usage: []string{"omg checkpoint [record] [selection] [payload options]"}, Subcommands: coordinationSubs([]helpSubcommand{{Name: "record", Summary: "Append a canonical liveness checkpoint.", Mutation: true}})},
	{Name: "task", Group: "COORDINATE WORK", Summary: "Create, inspect, claim, and transition tasks and their execution runs atomically.", Usage: []string{"omg task <subcommand> [selection] [payload options]"}, Subcommands: coordinationSubs([]helpSubcommand{{Name: "create", Summary: "Create a task with an atomic display number.", Example: "omg task create --project /project --idempotency-key task-1 --payload-file task.json --json", Mutation: true}, {Name: "get", Summary: "Read one task and its canonical state.", Example: "omg task get --project /project --payload '{\"task_id\":\"TASK_ID\"}' --json"}, {Name: "claim", Summary: "Claim one ready task for exactly one session.", Example: "omg task claim --project /project --idempotency-key claim-1 --payload '{\"task_id\":\"TASK_ID\",\"session_id\":\"SESSION_ID\"}' --json", Mutation: true}, {Name: "transition", Summary: "Apply a validated task state transition and dependency reconciliation.", Example: "omg task transition --project /project --idempotency-key transition-1 --payload-file transition.json --json", Mutation: true}, {Name: "run-create", Summary: "Create an execution run for a task and session.", Example: "omg task run-create --project /project --idempotency-key run-1 --payload-file run.json --json", Mutation: true}, {Name: "run-transition", Summary: "Apply a validated run state transition.", Example: "omg task run-transition --project /project --idempotency-key run-transition-1 --payload-file run-transition.json --json", Mutation: true}}), Examples: []string{"omg task create --project /project --idempotency-key task-1 --payload-file task.json --json", "omg task get --project /project --payload '{\"task_id\":\"TASK_ID\"}' --json"}},
	{Name: "progress", Group: "COORDINATE WORK", Summary: "Append done/doing/next progress or inspect its immutable history.", Usage: []string{"omg progress <add|history> [selection] [payload options]"}, Subcommands: coordinationSubs([]helpSubcommand{{Name: "add", Summary: "Append a progress update with done, doing, and next lanes.", Mutation: true}, {Name: "history", Summary: "Read progress history for one task."}})},
	{Name: "dependency", Group: "COORDINATE WORK", Summary: "Connect prerequisite tasks to dependent work and reject cycles.", Usage: []string{"omg dependency <add|list> [selection] [payload options]"}, Subcommands: coordinationSubs([]helpSubcommand{{Name: "add", Summary: "Add a validated dependency edge and unblock criterion.", Mutation: true}, {Name: "list", Summary: "Read the dependency graph."}})},
	{Name: "message", Group: "COORDINATE WORK", Summary: "Exchange typed coordination messages and explicit delivery/read/ack receipts.", Usage: []string{"omg message <subcommand> [selection] [payload options]"}, Subcommands: coordinationSubs([]helpSubcommand{{Name: "send", Summary: "Send an inert typed message to explicit recipients.", Mutation: true}, {Name: "inbox", Summary: "Read one recipient inbox."}, {Name: "thread", Summary: "Read one message thread."}, {Name: "deliver", Summary: "Record message delivery.", Mutation: true}, {Name: "read", Summary: "Record message read state.", Mutation: true}, {Name: "ack", Summary: "Record explicit acknowledgement.", Mutation: true}})},
	{Name: "handoff", Group: "COORDINATE WORK", Summary: "Transfer work, evidence, risks, and ownership decisions without treating self-report as verification.", Usage: []string{"omg handoff <subcommand> [selection] [payload options]"}, Subcommands: coordinationSubs([]helpSubcommand{{Name: "create", Summary: "Create an immutable handoff with evidence and remaining risks.", Mutation: true}, {Name: "show", Summary: "Read one safe handoff projection."}, {Name: "history", Summary: "Read handoff history for a task."}, {Name: "supersede", Summary: "Replace a handoff through an explicit superseding record.", Mutation: true}, {Name: "accept", Summary: "Accept a submitted handoff as an explicit decision.", Mutation: true}, {Name: "reject", Summary: "Reject a submitted handoff as an explicit decision.", Mutation: true}, {Name: "adopt", Summary: "Adopt orphaned canonical work without destructive Git action.", Mutation: true}})},
	{Name: "reserve", Group: "COORDINATE WORK", Summary: "Declare path intent, inspect conflicts, renew leases, and record explicit releases or overrides.", Usage: []string{"omg reserve <subcommand> [selection] [payload options]"}, Subcommands: coordinationSubs([]helpSubcommand{{Name: "add", Summary: "Add an advisory path reservation.", Mutation: true}, {Name: "list", Summary: "List reservation records."}, {Name: "active", Summary: "List active reservation records."}, {Name: "history", Summary: "Read one reservation history."}, {Name: "renew", Summary: "Renew a live reservation from a checkpoint.", Mutation: true}, {Name: "release", Summary: "Release a reservation with a reason.", Mutation: true}, {Name: "override", Summary: "Record a human override without hiding the conflict.", Mutation: true}})},

	{Name: "board", Group: "INSPECT + INTEGRATE", Summary: "Render a canonical coordination snapshot for one session, task, tree, Git view, or the whole project.", Usage: []string{"omg board <me|tree|task|all|git> [selection] [selector] [--format FORMAT | --json]"}, Subcommands: []helpSubcommand{{Name: "me", Summary: "Show one selected session and adjacent work."}, {Name: "tree", Summary: "Show session and task lineage."}, {Name: "task", Summary: "Show one selected task and adjacent coordination facts."}, {Name: "all", Summary: "Show the complete safe operator snapshot."}, {Name: "git", Summary: "Focus the snapshot on Git observations and risks."}}, Options: [][2]string{{"--session <id>", "Required by board me"}, {"--task <id>", "Required by board task"}, {"--format <format>", "tty, markdown, html, or json"}}, Examples: []string{"omg board all --project /absolute/project", "omg board task --project /project --task TASK_ID --format tty"}},
	{Name: "git", Group: "INSPECT + INTEGRATE", Summary: "Record and inspect Git observations, diffs, advisory cleanup plans, and canonical ownership.", Usage: []string{"omg git <subcommand> [selection] [payload options]"}, Subcommands: coordinationSubs([]helpSubcommand{{Name: "inventory", Summary: "Record a safe Git inventory observation.", Mutation: true}, {Name: "current", Summary: "Read the current observation."}, {Name: "latest", Summary: "Read the latest observation."}, {Name: "history", Summary: "Read observation history."}, {Name: "diff", Summary: "Compare two observations."}, {Name: "cleanup-plan", Summary: "Produce a non-destructive advisory cleanup plan."}, {Name: "adopt", Summary: "Change canonical ownership metadata only.", Mutation: true}})},
	{Name: "export", Group: "INSPECT + INTEGRATE", Summary: "Write a new private static board export without overwriting an existing file.", Usage: []string{"omg export [selection] --json", "omg export <html|markdown|tty|json> [selection] --output NEW_FILE"}, Subcommands: []helpSubcommand{{Name: "html", Summary: "Write a self-contained HTML operator board."}, {Name: "markdown", Summary: "Write a Markdown snapshot."}, {Name: "tty", Summary: "Write terminal text without relying on a live TTY."}, {Name: "json", Summary: "Write the canonical JSON view model."}}, Options: [][2]string{{"--output <path>", "Create a new owner-only file"}}},
	{Name: "import", Group: "INSPECT + INTEGRATE", Summary: "Import one externally normalized generic record into canonical state.", Usage: []string{"omg import record [selection] [payload options]"}, Subcommands: coordinationSubs([]helpSubcommand{{Name: "record", Summary: "Import one normalized record; ambiguous authority remains unverified.", Mutation: true}})},
	{Name: "integration", Group: "INSPECT + INTEGRATE", Summary: "Plan, apply, inspect, or remove bounded project instruction integration.", Usage: []string{"omg integration <plan|apply|status|remove> --project PATH [--status] [--json]"}, Subcommands: []helpSubcommand{{Name: "plan", Summary: "Preview instruction integration changes."}, {Name: "apply", Summary: "Apply the reviewed integration plan.", Mutation: true}, {Name: "status", Summary: "Inspect current integration state."}, {Name: "remove", Summary: "Remove only the managed integration block.", Mutation: true}}},
	{Name: "watch", Group: "INSPECT + INTEGRATE", Summary: "Follow local coordination changes or inspect watcher status.", Usage: []string{"omg watch [selection] [--json]", "omg watch status [selection] [--json]"}, Subcommands: []helpSubcommand{{Name: "status", Summary: "Report whether a watcher is active."}}},
	{Name: "run", Group: "INSPECT + INTEGRATE", Summary: "Run a guarded child command through a selected runtime while preserving the child exit code.", Usage: []string{"omg run --runtime NAME -- <command> [args...]"}, Options: [][2]string{{"--runtime <name>", "Select a configured child runtime"}}, Examples: []string{"omg run --runtime shell -- go test ./..."}},
	{Name: "shell-init", Group: "INSPECT + INTEGRATE", Summary: "Generate shell initialization for a supported shell.", Usage: []string{"omg shell-init <bash|zsh|fish|powershell>"}, Subcommands: []helpSubcommand{{Name: "bash", Summary: "Generate Bash initialization."}, {Name: "zsh", Summary: "Generate Zsh initialization."}, {Name: "fish", Summary: "Generate Fish initialization."}, {Name: "powershell", Summary: "Generate PowerShell initialization."}}},
	{Name: "completion", Group: "INSPECT + INTEGRATE", Summary: "Generate static completion scripts from the live command contract.", Usage: []string{"omg completion <bash|zsh|fish|powershell>"}, Subcommands: []helpSubcommand{{Name: "bash", Summary: "Generate Bash completion."}, {Name: "zsh", Summary: "Generate Zsh completion."}, {Name: "fish", Summary: "Generate Fish completion."}, {Name: "powershell", Summary: "Generate PowerShell completion."}}},
	{Name: "mcp", Group: "INSPECT + INTEGRATE", Summary: "Serve the local OMG MCP transport over standard input/output.", Usage: []string{"omg mcp serve --stdio"}, Subcommands: []helpSubcommand{{Name: "serve", Summary: "Serve MCP over stdio without a network listener."}}, Options: [][2]string{{"--stdio", "Use protocol-only standard input and output"}}},
	{Name: "receipt", Group: "INSPECT + INTEGRATE", Summary: "Query canonical command receipt data through strict payloads.", Usage: []string{"omg receipt <get|list> [selection] [payload options]"}, Subcommands: coordinationSubs([]helpSubcommand{{Name: "get", Summary: "Read one canonical command receipt."}, {Name: "list", Summary: "List canonical command receipts."}})},
}

func coordinationSubs(subcommands []helpSubcommand) []helpSubcommand {
	return subcommands
}

func helpCommandByName(name string) (helpCommand, bool) {
	for _, command := range helpCommands {
		if command.Name == name {
			return command, true
		}
	}
	return helpCommand{}, false
}

func helpSubcommandByName(command helpCommand, name string) (helpSubcommand, bool) {
	for _, subcommand := range command.Subcommands {
		if subcommand.Name == name {
			return subcommand, true
		}
	}
	return helpSubcommand{}, false
}

func knownCommand(name string) bool {
	_, ok := helpCommandByName(name)
	return ok
}

func commandNames() []string {
	result := make([]string, 0, len(helpCommands))
	for _, command := range helpCommands {
		result = append(result, command.Name)
	}
	return result
}

func closestCommand(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	best := ""
	bestDistance := 1 << 30
	for _, candidate := range commandNames() {
		distance := editDistance(value, candidate)
		if distance < bestDistance {
			best, bestDistance = candidate, distance
		}
	}
	limit := 2
	if len(value) >= 8 {
		limit = 3
	}
	if bestDistance <= limit {
		return best
	}
	return ""
}

func closestSubcommand(command helpCommand, value string) string {
	best := ""
	bestDistance := 1 << 30
	for _, subcommand := range command.Subcommands {
		distance := editDistance(strings.ToLower(strings.TrimSpace(value)), subcommand.Name)
		if distance < bestDistance {
			best, bestDistance = subcommand.Name, distance
		}
	}
	if bestDistance <= 2 {
		return best
	}
	return ""
}

func editDistance(first, second string) int {
	a := []rune(first)
	b := []rune(second)
	previous := make([]int, len(b)+1)
	for index := range previous {
		previous[index] = index
	}
	for i, left := range a {
		current := make([]int, len(b)+1)
		current[0] = i + 1
		for j, right := range b {
			cost := 0
			if left != right {
				cost = 1
			}
			current[j+1] = minInt(current[j]+1, previous[j+1]+1, previous[j]+cost)
		}
		previous = current
	}
	return previous[len(b)]
}

func minInt(values ...int) int {
	minimum := values[0]
	for _, value := range values[1:] {
		if value < minimum {
			minimum = value
		}
	}
	return minimum
}

func commandRequiresSubcommand(name string) bool {
	switch name {
	case "release", "migration", "backup", "board", "integration", "shell-init", "completion",
		"human", "session", "delegate", "task", "progress", "dependency", "message",
		"handoff", "reserve", "git", "import", "mcp", "receipt":
		return true
	default:
		return false
	}
}

func commandPathProblem(name, subcommand string) (string, terminalErrorContext, bool) {
	command, found := helpCommandByName(name)
	if !found {
		return "", terminalErrorContext{}, false
	}
	if subcommand == "" {
		if !commandRequiresSubcommand(name) {
			return "", terminalErrorContext{}, false
		}
		return name + " subcommand is required", terminalErrorContext{Next: "omg " + name + " --help"}, true
	}
	if len(command.Subcommands) == 0 {
		return "", terminalErrorContext{}, false
	}
	if _, ok := helpSubcommandByName(command, subcommand); ok {
		return "", terminalErrorContext{}, false
	}
	suggestion := closestSubcommand(command, subcommand)
	context := terminalErrorContext{Next: "omg " + name + " --help"}
	if suggestion != "" {
		context.Hint = "Did you mean \"" + suggestion + "\"?"
		context.Next = "omg " + name + " " + suggestion + " --help"
	}
	return "unknown " + name + " subcommand \"" + subcommand + "\"", context, true
}

func commandSupportsJSON(name string) bool {
	switch name {
	case "run", "mcp":
		return false
	default:
		return true
	}
}

func globalCommandSummary(name string) string {
	summaries := map[string]string{
		"init":        "Create local canonical state.",
		"preflight":   "Check readiness, identity, blockers, inbox, reservations, and Git.",
		"doctor":      "Inspect store, platform, recovery, and integrity health.",
		"migration":   "Plan or apply an explicitly approved schema migration.",
		"backup":      "Create or validate private recovery artifacts.",
		"release":     "Inspect publication and source-bound readiness.",
		"version":     "Print the current build version.",
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
		"export":      "Write a private static board snapshot.",
		"import":      "Import one normalized external record.",
		"integration": "Manage bounded project instruction integration.",
		"watch":       "Follow local coordination changes.",
		"run":         "Execute a guarded child command.",
		"shell-init":  "Generate shell initialization.",
		"completion":  "Generate completion scripts from the command contract.",
		"mcp":         "Serve OMG over protocol-only MCP stdio.",
		"receipt":     "Inspect canonical command receipts.",
	}
	if summary := summaries[name]; summary != "" {
		return summary
	}
	if command, ok := helpCommandByName(name); ok {
		return command.Summary
	}
	return ""
}

func relatedCommandNames(name string) []string {
	related := map[string][]string{
		"init":        {"preflight", "board"},
		"preflight":   {"board", "doctor"},
		"doctor":      {"preflight", "migration", "backup"},
		"migration":   {"backup", "doctor", "preflight"},
		"backup":      {"doctor", "migration"},
		"release":     {"version", "doctor"},
		"version":     {"release"},
		"human":       {"session", "delegate"},
		"session":     {"task", "checkpoint", "delegate"},
		"delegate":    {"session", "human"},
		"checkpoint":  {"session", "progress", "reserve"},
		"task":        {"progress", "dependency", "handoff", "board"},
		"progress":    {"task", "message", "handoff"},
		"dependency":  {"task", "board"},
		"message":     {"task", "handoff", "board"},
		"handoff":     {"task", "message", "reserve", "board"},
		"reserve":     {"task", "handoff", "board"},
		"board":       {"preflight", "export", "watch"},
		"git":         {"board", "reserve", "handoff"},
		"export":      {"board", "preflight"},
		"import":      {"session", "task", "board"},
		"integration": {"preflight", "doctor"},
		"watch":       {"board", "preflight"},
		"run":         {"preflight", "board"},
		"shell-init":  {"completion"},
		"completion":  {"shell-init"},
		"mcp":         {"preflight", "board"},
		"receipt":     {"task", "message", "handoff"},
	}
	return append([]string(nil), related[name]...)
}
