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
	{Marker: "02", Name: "Start work", Command: "omg worker bootstrap → omg board me → work", Summary: "Load controller-provided identity, claim one task, and inspect only the worker scope."},
	{Marker: "03", Name: "Share state", Command: "omg progress add → omg message send → omg handoff create", Summary: "Publish done / doing / next, coordinate, then transfer evidence and risk."},
	{Marker: "04", Name: "Recover safely", Command: "omg doctor → omg board git → omg backup create", Summary: "Inspect health and repository risk before creating a recovery artifact."},
}

var helpCommands = []helpCommand{
	{Name: "init", Group: "START + VERIFY", Summary: "Initialize the local coordination ledger without applying pending migrations.", Usage: []string{"omg init [selection] [--json]"}, Examples: []string{"omg init --project /absolute/project", "omg init --workspace /absolute/workspace --json"}},
	{Name: "preflight", Group: "START + VERIFY", Summary: "Check startup health with a compact default and opt-in detail.", Usage: []string{"omg preflight [selection] [--session ID] [--verbose] [--json]"}, Options: [][2]string{{"--session <id>", "Select the current session identity"}, {"--verbose", "Include sessions, tasks, inbox, reservations, Git, and actions"}}, Examples: []string{"omg preflight --project /absolute/project", "omg preflight --project /absolute/project --verbose --json"}},
	{Name: "status", Group: "START + VERIFY", Summary: "Show the compact operator summary and the largest workflow bottleneck.", Usage: []string{"omg status [selection] [--json]"}, Examples: []string{"omg status --project /absolute/project --json"}},
	{Name: "doctor", Group: "START + VERIFY", Summary: "Inspect configuration, store, platform, recovery, and optional SQLite integrity health.", Usage: []string{"omg doctor [selection] [--integrity] [--json]"}, Options: [][2]string{{"--integrity", "Run the optional SQLite integrity check"}}, Examples: []string{"omg doctor --project /absolute/project --integrity"}},
	{Name: "migration", Group: "START + VERIFY", Summary: "Plan or apply a checksummed schema migration through explicit approval files.", Usage: []string{"omg migration <plan|apply> [selection] [options]"}, Subcommands: []helpSubcommand{{Name: "plan", Summary: "Create a read-only migration plan; optionally save it privately.", Example: "omg migration plan --project /project --output plan.json --json"}, {Name: "apply", Summary: "Apply the exact plan after validating a matching approval and backup.", Example: "omg migration apply --project /project --plan-file plan.json --approval-file approval.json --json", Mutation: true}}, Options: [][2]string{{"--output <path>", "Save a complete private plan"}, {"--plan-file <path>", "Read the exact private plan"}, {"--approval-file <path>", "Read the matching private approval"}}},
	{Name: "backup", Group: "START + VERIFY", Summary: "Create or validate recovery artifacts without weakening migration safeguards.", Usage: []string{"omg backup <create|restore> [selection] [options]"}, Subcommands: []helpSubcommand{{Name: "create", Summary: "Create and verify a private backup, optionally bound to a plan.", Example: "omg backup create --project /project --plan-file plan.json --json"}, {Name: "restore", Summary: "Validate a guarded restore request; never overwrite state implicitly.", Example: "omg backup restore --project /project --payload-file restore.json --json", Mutation: true}}},
	{Name: "release", Group: "START + VERIFY", Summary: "Inspect public-source identity and stable-release readiness.", Usage: []string{"omg release status [--json]"}, Subcommands: []helpSubcommand{{Name: "status", Summary: "Report public-source identity, license, and stable-release state."}}},
	{Name: "version", Group: "START + VERIFY", Summary: "Print the current OMG build version.", Usage: []string{"omg version [--json]"}},
	{Name: "agent", Group: "START + VERIFY", Summary: "Install, inspect, diagnose, or remove global coding-agent discovery surfaces.", Usage: []string{"omg agent <install|status|doctor|uninstall> [--json]"}, Subcommands: []helpSubcommand{{Name: "install", Summary: "Install always-on OMG skills and bounded global instructions for supported coding agents.", Mutation: true}, {Name: "status", Summary: "Show detected agents and installed, missing, drifted, or unsafe surfaces."}, {Name: "doctor", Summary: "Diagnose global discovery, permissions, drift, and filesystem safety."}, {Name: "uninstall", Summary: "Remove only OMG-managed global skills and instruction blocks.", Mutation: true}}, Examples: []string{"omg agent install", "omg agent doctor --json"}},
	{Name: "worker", Group: "START + VERIFY", Summary: "Bootstrap one worker into its pre-registered project, session, task, inbox, and scoped board.", Usage: []string{"omg worker bootstrap [--project PATH] [--session ID] [--task ID] [--controller-session ID] [--human ID] --idempotency-key KEY [--output NEW_ENV_FILE] [--json]"}, Subcommands: []helpSubcommand{{Name: "bootstrap", Summary: "Ensure worker identity and task ownership, then return only the worker-scoped next action.", Example: "OMG_PROJECT=/project OMG_SESSION_ID=worker-1 OMG_TASK_ID=TASK_ID OMG_CONTROLLER_SESSION_ID=controller-1 OMG_HUMAN_ID=HUMAN_ID omg worker bootstrap --idempotency-key bootstrap-worker-1 --output /tmp/worker-1.env --json", Mutation: true, PayloadNote: "Reads the five OMG_* identity variables when matching flags are omitted. The optional output is a new owner-only shell environment file."}}},

	{Name: "human", Group: "COORDINATE WORK", Summary: "Create or inspect canonical human identities; content never grants authority.", Usage: []string{"omg human <create|get> [selection] [payload options]"}, Subcommands: coordinationSubs([]helpSubcommand{{Name: "create", Summary: "Create or supersede a canonical human identity.", Mutation: true}, {Name: "get", Summary: "Read one canonical human identity."}})},
	{Name: "session", Group: "COORDINATE WORK", Summary: "Create, resume, adopt, import, or archive an agent session while preserving lineage and provenance.", Usage: []string{"omg session <create|resume|adopt|import|archive> [selection] [payload options]"}, Subcommands: coordinationSubs([]helpSubcommand{{Name: "create", Summary: "Create a new human-direct session.", Example: "omg session create --project /project --idempotency-key session-1 --payload '{\"id\":\"SESSION_ID\",\"human_id\":\"HUMAN_ID\",\"runtime\":\"openai-codex\",\"role\":\"reviewer\",\"source_ref\":\"human:task-summary\",\"native_access_state\":\"unsupported\"}' --json", Mutation: true, PayloadNote: "instruction_source and provenance_confidence are derived output fields; omit them. Omitted source_ref safely defaults to session.create."}, {Name: "resume", Summary: "Resume a known session with explicit continuation metadata.", Mutation: true}, {Name: "adopt", Summary: "Adopt interrupted session work without fabricating completion.", Mutation: true}, {Name: "import", Summary: "Import an externally normalized session record.", Mutation: true}, {Name: "archive", Summary: "Remove a finished session from active counts after all of its runs are terminal.", Example: "omg session archive --project /project --idempotency-key archive-1 --payload '{\"id\":\"ARCHIVE_EVENT_ID\",\"session_id\":\"SESSION_ID\",\"actor_session_id\":\"CONTROLLER_SESSION_ID\",\"reason\":\"work completed\"}' --json", Mutation: true}})},
	{Name: "delegate", Group: "COORDINATE WORK", Summary: "Issue, redeem, or revoke bounded one-use delegation without placing raw tokens in normal output.", Usage: []string{"omg delegate <issue|register|revoke> [selection] [payload options]"}, Subcommands: coordinationSubs([]helpSubcommand{{Name: "issue", Summary: "Issue a short-lived project-scoped delegation token.", Mutation: true}, {Name: "register", Summary: "Redeem a token from stdin or a private payload file.", Mutation: true, PayloadNote: "Raw delegation tokens are rejected in inline --payload."}, {Name: "revoke", Summary: "Revoke an unconsumed delegation token.", Mutation: true}})},
	{Name: "checkpoint", Group: "COORDINATE WORK", Summary: "Record session liveness and continuation state.", Usage: []string{"omg checkpoint [record] [selection] [payload options]"}, Subcommands: coordinationSubs([]helpSubcommand{{Name: "record", Summary: "Append a canonical liveness checkpoint.", Example: "omg checkpoint record --project /project --idempotency-key checkpoint-1 --payload '{\"id\":\"CHECKPOINT_ID\",\"session_id\":\"SESSION_ID\",\"liveness\":\"alive\",\"detail\":\"working\"}' --json", Mutation: true}})},
	{Name: "task", Group: "COORDINATE WORK", Summary: "Create, inspect, claim, and transition tasks and their execution runs atomically.", Usage: []string{"omg task <subcommand> [selection] [payload options]"}, Subcommands: coordinationSubs([]helpSubcommand{{Name: "create", Summary: "Create a task with an atomic display number.", Example: "omg task create --project /project --idempotency-key task-1 --payload-file task.json --json", Mutation: true}, {Name: "get", Summary: "Read one task and its canonical state.", Example: "omg task get --project /project --payload '{\"task_id\":\"TASK_ID\"}' --json"}, {Name: "claim", Summary: "Claim one ready task for exactly one session.", Example: "omg task claim --project /project --idempotency-key claim-1 --payload '{\"task_id\":\"TASK_ID\",\"session_id\":\"SESSION_ID\"}' --json", Mutation: true}, {Name: "transition", Summary: "Apply a validated task state transition and dependency reconciliation.", Example: "omg task transition --project /project --idempotency-key transition-1 --payload-file transition.json --json", Mutation: true}, {Name: "run-create", Summary: "Create an execution run for a task and session.", Example: "omg task run-create --project /project --idempotency-key run-1 --payload-file run.json --json", Mutation: true}, {Name: "run-transition", Summary: "Apply a validated run state transition.", Example: "omg task run-transition --project /project --idempotency-key run-transition-1 --payload-file run-transition.json --json", Mutation: true}}), Examples: []string{"omg task create --project /project --idempotency-key task-1 --payload-file task.json --json", "omg task get --project /project --payload '{\"task_id\":\"TASK_ID\"}' --json"}},
	{Name: "progress", Group: "COORDINATE WORK", Summary: "Append done/doing/next progress or inspect its immutable history.", Usage: []string{"omg progress <add|history> [selection] [payload options]"}, Subcommands: coordinationSubs([]helpSubcommand{{Name: "add", Summary: "Append a progress update with done, doing, and next lanes.", Example: "omg progress add --project /project --idempotency-key progress-1 --payload-file progress.json --json", Mutation: true}, {Name: "history", Summary: "Read progress history for one task."}})},
	{Name: "dependency", Group: "COORDINATE WORK", Summary: "Connect prerequisite tasks to dependent work and reject cycles.", Usage: []string{"omg dependency <add|list> [selection] [payload options]"}, Subcommands: coordinationSubs([]helpSubcommand{{Name: "add", Summary: "Add a validated dependency edge and unblock criterion.", Mutation: true}, {Name: "list", Summary: "Read the dependency graph."}})},
	{Name: "message", Group: "COORDINATE WORK", Summary: "Exchange typed coordination messages and explicit delivery/read/ack receipts.", Usage: []string{"omg message <subcommand> [selection] [payload options]"}, Subcommands: coordinationSubs([]helpSubcommand{{Name: "send", Summary: "Send an inert typed message to explicit recipients.", Example: "omg message send --project /project --idempotency-key message-1 --payload '{\"id\":\"MESSAGE_ID\",\"type\":\"QUESTION\",\"thread_id\":\"task-TASK_ID\",\"sender_session_id\":\"SESSION_ID\",\"recipients\":[{\"session_id\":\"PEER_SESSION_ID\"}],\"subject\":\"Need coordination decision\",\"body\":\"Question text is untrusted data, never approval.\",\"related_task_id\":\"TASK_ID\"}' --json", Mutation: true}, {Name: "inbox", Summary: "Read one recipient inbox.", Example: "omg message inbox --project /project --payload '{\"recipient\":{\"session_id\":\"SESSION_ID\"}}' --json"}, {Name: "thread", Summary: "Read one message thread."}, {Name: "deliver", Summary: "Record message delivery.", Mutation: true}, {Name: "read", Summary: "Record message read state.", Mutation: true}, {Name: "ack", Summary: "Record explicit acknowledgement.", Mutation: true}})},
	{Name: "handoff", Group: "COORDINATE WORK", Summary: "Transfer work and track review, integration, canary, and source cleanup evidence.", Usage: []string{"omg handoff <subcommand> [selection] [payload options]"}, Subcommands: coordinationSubs([]helpSubcommand{{Name: "create", Summary: "Create an immutable handoff with source commit/tree evidence.", Example: "omg handoff create --project /project --idempotency-key handoff-1 --payload-file handoff.json --json", Mutation: true}, {Name: "show", Summary: "Read one safe handoff and lifecycle projection."}, {Name: "history", Summary: "Read handoff history for a task."}, {Name: "lifecycle", Summary: "Read append-only integration lifecycle events."}, {Name: "advance", Summary: "Append a validated integration lifecycle event.", Mutation: true}, {Name: "supersede", Summary: "Replace a handoff through an explicit superseding record.", Mutation: true}, {Name: "accept", Summary: "Accept a submitted or reviewing handoff as an explicit decision.", Example: "omg handoff accept --project /project --idempotency-key accept-1 --payload '{\"handoff_id\":\"HANDOFF_ID\",\"actor_session_id\":\"CONTROLLER_SESSION_ID\"}' --json", Mutation: true}, {Name: "reject", Summary: "Reject a submitted or reviewing handoff as an explicit decision.", Mutation: true}, {Name: "adopt", Summary: "Adopt orphaned canonical work without destructive Git action.", Mutation: true}})},
	{Name: "reserve", Group: "COORDINATE WORK", Summary: "Declare path intent, inspect conflicts, renew leases, and record explicit releases or overrides.", Usage: []string{"omg reserve <subcommand> [selection] [payload options]"}, Subcommands: coordinationSubs([]helpSubcommand{{Name: "add", Summary: "Add an advisory path reservation.", Example: "omg reserve add --project /project --idempotency-key reserve-1 --payload '{\"id\":\"reservation-1\",\"pattern_kind\":\"exact\",\"pattern\":\"TODO.md\",\"case_sensitivity\":\"sensitive\",\"mode\":\"exclusive\",\"human_id\":\"HUMAN_ID\",\"session_id\":\"SESSION_ID\",\"task_id\":\"TASK_ID\",\"run_id\":\"RUN_ID\",\"intent\":\"edit TODO\",\"ttl_seconds\":3600}' --json", Mutation: true, PayloadNote: "Requires human_id, session_id, task_id, and run_id. Create the run first with `omg task run-create --help`."}, {Name: "list", Summary: "List reservation records."}, {Name: "active", Summary: "List active reservation records."}, {Name: "history", Summary: "Read one reservation history."}, {Name: "renew", Summary: "Renew a live reservation from a checkpoint.", Mutation: true}, {Name: "release", Summary: "Release a reservation with a reason.", Mutation: true}, {Name: "override", Summary: "Record a human override without hiding the conflict.", Mutation: true}})},

	{Name: "board", Group: "INSPECT + INTEGRATE", Summary: "Render a compact summary or canonical coordination snapshot.", Usage: []string{"omg board <summary|me|tree|task|all|git> [selection] [selector] [--format FORMAT | --json]"}, Subcommands: []helpSubcommand{{Name: "summary", Summary: "Count states and expose the largest workflow bottleneck."}, {Name: "me", Summary: "Show one selected session and adjacent work."}, {Name: "tree", Summary: "Show session and task lineage."}, {Name: "task", Summary: "Show one selected task and adjacent coordination facts."}, {Name: "all", Summary: "Show the complete safe operator snapshot."}, {Name: "git", Summary: "Focus the snapshot on Git observations and risks."}}, Options: [][2]string{{"--session <id>", "Required by board me"}, {"--task <id>", "Required by board task"}, {"--format <format>", "tty, markdown, html, or json"}}, Examples: []string{"omg board summary --project /absolute/project --json", "omg board task --project /project --task TASK_ID --format tty"}},
	{Name: "git", Group: "INSPECT + INTEGRATE", Summary: "Record and inspect Git facts for only the selected project repository and its linked worktrees.", Usage: []string{"omg git <subcommand> [selection] [payload options]"}, Subcommands: coordinationSubs([]helpSubcommand{{Name: "inventory", Summary: "Record a safe Git inventory observation.", Mutation: true}, {Name: "current", Summary: "Read the current project observation.", Example: "omg git current --project /project --json", PayloadNote: "No payload is required. An optional session_id compatibility hint is accepted but does not change project scope."}, {Name: "latest", Summary: "Read the latest project observation.", Example: "omg git latest --project /project --json", PayloadNote: "No payload is required. An optional session_id compatibility hint is accepted but does not change project scope."}, {Name: "history", Summary: "Read project observation history.", Example: "omg git history --project /project --json", PayloadNote: "No payload is required. An optional session_id compatibility hint is accepted but does not change project scope."}, {Name: "diff", Summary: "Compare two observations; omission selects the latest pair.", Example: "omg git diff --project /project --json", PayloadNote: "No payload compares the two latest observations. Supply before and after observation IDs only to select another pair."}, {Name: "cleanup-plan", Summary: "Produce a non-destructive advisory cleanup plan.", Example: "omg git cleanup-plan --project /project --json", PayloadNote: "No payload plans all current assets; fingerprint optionally selects one asset."}, {Name: "reconcile", Summary: "Prove recorded source SHA/tree inclusion in the selected integration ref.", Example: "omg git reconcile --project /project --integration-branch main --json"}, {Name: "adopt", Summary: "Change canonical ownership metadata only.", Mutation: true}})},
	{Name: "orphan", Group: "INSPECT + INTEGRATE", Summary: "Find unowned dirty worktrees and unreconciled branches in only the selected project repository.", Usage: []string{"omg orphan scan [selection] [--integration-branch REF] [--json]"}, Subcommands: []helpSubcommand{{Name: "scan", Summary: "Run a fresh read-only orphan-risk scan.", Example: "omg orphan scan --project /project --integration-branch main --json"}}},
	{Name: "canary", Group: "INSPECT + INTEGRATE", Summary: "Pin and finish exact-SHA canary receipts; OMG records evidence but does not execute the command.", Usage: []string{"omg canary <start|finish> [selection] [options]"}, Subcommands: []helpSubcommand{{Name: "start", Summary: "Pin the recorded integration SHA/tree and current ref fingerprint before verification.", Example: "omg canary start --project /project --handoff HANDOFF --session SESSION --integration-ref main --verification-command 'go test ./...' --execution-kind real --environment-fingerprint ENV_HASH --idempotency-key KEY --json", Mutation: true}, {Name: "finish", Summary: "Record counts and invalidate automatically if the integration ref moved.", Example: "omg canary finish --project /project --canary CANARY_ID --session SESSION --exit-code 0 --passed 1 --failed 0 --skipped 0 --evidence-path /path/to/log --idempotency-key KEY --json", Mutation: true}}},
	{Name: "export", Group: "INSPECT + INTEGRATE", Summary: "Write a new private static board export without overwriting an existing file.", Usage: []string{"omg export [selection] --json", "omg export <html|markdown|tty|json> [selection] --output NEW_FILE"}, Subcommands: []helpSubcommand{{Name: "html", Summary: "Write a self-contained HTML operator board."}, {Name: "markdown", Summary: "Write a Markdown snapshot."}, {Name: "tty", Summary: "Write terminal text without relying on a live TTY."}, {Name: "json", Summary: "Write the canonical JSON view model."}}, Options: [][2]string{{"--output <path>", "Create a new owner-only file"}}},
	{Name: "import", Group: "INSPECT + INTEGRATE", Summary: "Import one externally normalized generic record into canonical state.", Usage: []string{"omg import record [selection] [payload options]"}, Subcommands: coordinationSubs([]helpSubcommand{{Name: "record", Summary: "Import one normalized record; ambiguous authority remains unverified.", Mutation: true}})},
	{Name: "integration", Group: "INSPECT + INTEGRATE", Summary: "Inspect the handoff queue or manage bounded project instruction integration.", Usage: []string{"omg integration queue [selection] [--json]", "omg integration <plan|apply|status|remove> --project PATH [--status] [--json]"}, Subcommands: []helpSubcommand{{Name: "queue", Summary: "Show handoffs awaiting review, integration, canary, or cleanup."}, {Name: "plan", Summary: "Preview instruction integration changes."}, {Name: "apply", Summary: "Apply the reviewed integration plan.", Mutation: true}, {Name: "status", Summary: "Inspect current instruction integration state."}, {Name: "remove", Summary: "Remove only the managed integration block.", Mutation: true}}},
	{Name: "watch", Group: "INSPECT + INTEGRATE", Summary: "Follow local coordination changes or inspect watcher status.", Usage: []string{"omg watch [selection] [--json]", "omg watch status [selection] [--json]"}, Subcommands: []helpSubcommand{{Name: "status", Summary: "Report whether a watcher is active."}}},
	{Name: "run", Group: "INSPECT + INTEGRATE", Summary: "Run a guarded child command through a selected runtime while preserving the child exit code.", Usage: []string{"omg run --runtime NAME -- <command> [args...]"}, Options: [][2]string{{"--runtime <name>", "Select a configured child runtime"}}, Examples: []string{"omg run --runtime shell -- go test ./..."}},
	{Name: "example", Group: "INSPECT + INTEGRATE", Summary: "List or show copyable examples generated from the live help contract.", Usage: []string{"omg example <list|show> [topic] [--json]"}, Subcommands: []helpSubcommand{{Name: "list", Summary: "List available example topics."}, {Name: "show", Summary: "Render one example with its current command and payload contract.", PayloadNote: "Topics use command-subcommand names, such as reserve-add or task-run-create."}}},
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

func exampleTopics() []string {
	topics := make([]string, 0)
	for _, command := range helpCommands {
		if len(command.Examples) != 0 {
			topics = append(topics, command.Name)
		}
		for _, subcommand := range command.Subcommands {
			if subcommand.Example != "" {
				topics = append(topics, command.Name+"-"+subcommand.Name)
			}
		}
	}
	return topics
}

func resolveExampleTopic(value string) (string, helpTarget, bool) {
	topic := strings.ToLower(strings.TrimSpace(value))
	if topic == "reservation-add" {
		topic = "reserve-add"
	}
	for _, command := range helpCommands {
		if topic == command.Name && len(command.Examples) != 0 {
			return topic, helpTarget{Command: command.Name}, true
		}
		for _, subcommand := range command.Subcommands {
			if topic == command.Name+"-"+subcommand.Name && subcommand.Example != "" {
				return topic, helpTarget{Command: command.Name, Subcommand: subcommand.Name}, true
			}
		}
	}
	return "", helpTarget{}, false
}

func closestExampleTopic(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	best := ""
	bestDistance := 1 << 30
	for _, topic := range exampleTopics() {
		distance := editDistance(value, topic)
		if distance < bestDistance {
			best, bestDistance = topic, distance
		}
	}
	if bestDistance <= 3 {
		return best
	}
	return ""
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
	case "release", "agent", "worker", "migration", "backup", "board", "integration", "shell-init", "completion",
		"human", "session", "delegate", "task", "progress", "dependency", "message",
		"handoff", "reserve", "git", "orphan", "canary", "import", "mcp", "receipt":
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
		"status":      "Show the compact operator summary and workflow bottleneck.",
		"doctor":      "Inspect store, platform, recovery, and integrity health.",
		"migration":   "Plan or apply an explicitly approved schema migration.",
		"backup":      "Create or validate private recovery artifacts.",
		"release":     "Inspect publication and source-bound readiness.",
		"version":     "Print the current build version.",
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
		"status":      {"board", "preflight", "integration"},
		"doctor":      {"preflight", "migration", "backup"},
		"migration":   {"backup", "doctor", "preflight"},
		"backup":      {"doctor", "migration"},
		"release":     {"version", "doctor"},
		"version":     {"release"},
		"worker":      {"preflight", "session", "task", "message", "board"},
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
		"orphan":      {"git", "handoff", "board"},
		"canary":      {"git", "handoff", "integration"},
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
