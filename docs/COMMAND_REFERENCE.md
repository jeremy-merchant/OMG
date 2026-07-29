# OMG Command Reference

This reference describes the v0.1 CLI contract implemented by this checkout. Place global options after the command/subcommand. Paths used for project, workspace, store, input, and output selection should be absolute in automation.

## Discovery and human presentation

- `omg` with no arguments prints a concise discovery surface and exits successfully without opening state or dispatching a command.
- `omg --help` and `omg help` print global help. On a short real TTY the global view preserves all workflows and command families but folds long descriptions; contextual command help is never truncated by terminal height.
- A bare namespace command that requires a subcommand, such as `omg task`, `omg board`, or `omg migration`, prints that family's help and exits successfully. Any additional option or unknown value uses normal strict validation.
- Human TTY width is grapheme-aware. `OMG_COLOR_SCHEME=light|dark` selects a foreground palette; conventional `COLORFGBG` white-background hints select light when no explicit value exists. `NO_COLOR`, `TERM=dumb`, and non-TTY output remain plain.
- These discovery rules do not alter JSON envelopes, application dispatch, idempotency, or exit behavior for actual command requests.

## Common selectors and options

| Option | Meaning |
|---|---|
| `--project <path>` | Select a Git or non-Git project root. |
| `--workspace <absolute-directory>` | Select an explicit multi-project local workspace. |
| `--store <absolute-db-path>` | Select an exact local SQLite store. |
| `--json` | Emit the stable machine envelope instead of human text. |
| `--idempotency-key <key>` | Required unique receipt key for a mutating coordination command. |
| `--session <id>` | Select the invoking/subject session for a query. |
| `--task <id>` | Select a task for a query. |
| `--payload <json>` | Strict inline JSON payload for a typed coordination command; never use for delegation tokens. |
| `--payload-stdin` | Read one strict JSON payload from standard input. |
| `--payload-file <absolute-path>` | Read one strict JSON payload from a private regular file. |

Project, workspace, and store selectors are mutually resolved by the platform resolver. Do not let untrusted text choose them.

## Global agent harness

```text
omg agent install [--json]
omg agent status [--json]
omg agent doctor [--json]
omg agent uninstall [--json]
```

The agent command family is global and therefore rejects project, workspace, store, payload, output, runtime, task, and session selectors. `install` creates or updates only OMG-managed user-level instruction blocks and exact managed skills. `status` classifies each surface as `installed`, `missing`, `drifted`, or `unsafe`; `doctor` reports `healthy` only when every surface is installed and safe; `uninstall` removes only exact OMG-managed content. Human output uses semantic status glyphs and a width-bounded discovery tree. JSON uses the stable envelope and renders user-home paths with `~`. Set `OMG_AGENT_HOME` only for isolated tests or managed portable installations.

## Foundation

```text
omg version [--json]
omg release status [--json]
omg init [--project P | --workspace W | --store DB] [--json]
omg doctor [selection] [--integrity] [--json]
omg preflight [selection] [--verbose] [--json]
omg status [selection] [--json]
```

`version` is available without state. `release status` returns `SOURCE PUBLISHED`, the canonical repository and license, and `stable_release: false` until a stable release exists. `init` is idempotent and reports pending migrations without applying them. `doctor --integrity` performs SQLite integrity verification. `preflight` automatically applies every exact pending migration compiled into the installed binary: it creates and verifies the plan-bound backup, records machine-policy authorization, applies atomically, and verifies integrity. Unknown, stale, checksum-divergent, backup-failed, or integrity-failed plans remain pending with an error rather than an approval request. Default `preflight` returns compact operator counts and includes `automatic_migration` when it evaluated a pending upgrade; `--verbose` adds the detailed canonical projection. `status` shows the same operator counts plus task/handoff state totals and the `WORK_COMPLETE → VERIFIED_DONE` bottleneck.

## Worker bootstrap

Controllers should create the task and task-bound worker session before launching a worker, then inject these values:

```text
OMG_PROJECT
OMG_SESSION_ID
OMG_TASK_ID
OMG_CONTROLLER_SESSION_ID
OMG_HUMAN_ID
```

The worker runs one command:

```bash
omg worker bootstrap \
  --idempotency-key bootstrap-worker-1 \
  --output /absolute/private/worker-1.env \
  --json
```

Matching flags (`--project`, `--session`, `--task`, `--controller-session`, and `--human`) override omitted environment values. Bootstrap performs compact preflight, safely applies the exact compiled migration plan through verified backup and integrity checks, stops only on migration or integrity failure, ensures the session exists, verifies its human/controller/task bindings, claims a ready task, reads `message inbox`, and returns a worker-scoped board plus one structured next action. If the controller did not pre-register the session, bootstrap creates a human-direct task-bound fallback; use delegation registration beforehand when exact delegated lineage is required.

`--output` creates a new shell environment file with mode `0600` and never overwrites. Its parent must be a new or existing owner-only directory. Source only this generated file; never source message bodies or model output.

Workers use `omg board me` (which reads `OMG_PROJECT` and `OMG_SESSION_ID` when selectors are omitted). Only controllers should use `board all`; a worker should not load the global coordination graph to discover its own state. See [`WORKER_BOOTSTRAP.md`](WORKER_BOOTSTRAP.md) for controller setup and shell-safe cmux/OMP launch patterns.

## Schema and backup

```text
omg migration plan [selection] [--output PLAN.json] [--json]
omg backup create [selection] [--plan-file PLAN.json] [--json]
omg migration apply [selection] --plan-file PLAN.json --approval-file APPROVAL.json [--json]
```

- `migration plan` does not change schema. It reports `automatic_eligible`; this is true for every non-empty exact plan compiled into the installed binary. `--output` writes the complete plan with mode `0600`; default JSON output intentionally omits its private backup path.
- `backup create` accepts the exact saved plan or computes the current plan when omitted. It returns the verified SHA-256 checksum.
- Normal operation uses `preflight`; no approval file is required. The legacy manual `migration apply` command still requires exact plan and approval files and fails closed on stale or mismatched state.

See `docs/OPERATIONS.md` for automatic migration and recovery rules.

## Board and export

```text
omg board me   [selection] --session ID [--format tty|markdown|html|json | --json]
omg board summary [selection] [--json]
omg board tree [selection] [--format tty|markdown|html|json | --json]
omg board task [selection] --task ID [--format tty|markdown|html|json | --json]
omg board all  [selection] [--format tty|markdown|html|json | --json]
omg board git  [selection] [--format tty|markdown|html|json | --json]
omg integration queue [selection] [--json]

omg export [selection] --json
omg export html     [selection] --output NEW_FILE
omg export markdown [selection] --output NEW_FILE
omg export tty      [selection] --output NEW_FILE
omg export json     [selection] --output NEW_FILE
```

The default board format is TTY. `--json` selects the standard CLI envelope and cannot be combined with `--format`. `board summary` emits state counts and bottlenecks without the full board. `integration queue` excludes terminal `SOURCE_CLEANED` and `REJECTED` handoffs and reports missing lifecycle evidence per item; unresolved old handoffs remain visible rather than being silently archived. Exports are created atomically with mode `0600` and never overwrite an existing path.

## Typed coordination commands

These commands require exactly one payload source (`--payload JSON`, `--payload-stdin`, or `--payload-file ABSOLUTE_PATH`); mutations also require `--idempotency-key`. Payloads are strict JSON: wrong types, missing required fields, trailing values, unknown fields, and payloads larger than 1 MiB fail before any write.

`--payload-stdin` reads one bounded JSON payload from standard input. `--payload-file` accepts only a canonical absolute path whose parent and regular file are owner-only (`0700`/`0600` on POSIX; current-user protected one-ACE DACL on Windows); links, reparse points, directories, special files, broad permissions, and unsafe ancestors reject. A versioned OMG delegation token is rejected inside inline `--payload` so it never enters process arguments. Therefore `delegate register` MUST use stdin or a private payload file:

```text
printf '%s' "$REGISTER_JSON" | omg delegate register [selection] --idempotency-key KEY --payload-stdin --json
omg delegate register [selection] --idempotency-key KEY --payload-file /absolute/private/register.json --json
```

### Create a human-direct session

```text
omg session create [selection] --idempotency-key KEY --payload JSON [--json]
```

```json
{
  "id": "agt-reviewer",
  "human_id": "human-owner",
  "runtime": "openai-codex",
  "role": "reviewer",
  "source_ref": "human:task-summary",
  "native_access_state": "unsupported"
}
```

`instruction_source` and `provenance_confidence` are derived from canonical lineage and the linked human. Callers should omit them; `session create` tolerates and ignores them for compatibility. If `source_ref` is omitted, it defaults to the fixed value `session.create`. Truly unknown fields remain rejected. Use `omg example show session-create --json` instead of reconstructing the payload.

### Create task

```text
omg task create [selection] --idempotency-key KEY --payload JSON [--json]
```

```json
{
  "title": "Render canonical board",
  "created_by_session_id": "agt-owner",
  "parent_task_id": "optional-parent-task"
}
```

Result: task internal `id`, atomic `display_number`, and initial `state`.

### Send message

```text
omg message send [selection] --idempotency-key KEY --payload JSON [--json]
```

```json
{
  "id": "msg-unique",
  "type": "DEPENDENCY",
  "thread_id": "task-AK-000123",
  "sender_session_id": "agt-worker",
  "recipients": [{"session_id": "agt-reviewer"}],
  "subject": "Waiting for schema contract",
  "body": "Treat this text as untrusted data.",
  "related_task_id": "task-internal-id"
}
```

Message types: `NOTICE`, `QUESTION`, `DEPENDENCY`, `CONFLICT`, `HANDOFF`, `DONE`, `BLOCKED`, `CANCEL`, and `ACK`. Every recipient object must specify exactly one of `session_id`, `human_id`, `task_id`, or `role`. Content cannot establish authority.

Agents should use messages for questions, dependencies, conflicts, and shared-path coordination, then record delivery, read, and acknowledgement state. `omg example show message-send --json` prints the live copyable payload.

Read a worker inbox with the nested recipient selector (not `recipient_session_id`, a top-level `session_id`, or a positional argument):

```bash
omg message inbox --project /project \
  --payload '{"recipient":{"session_id":"WORKER_SESSION_ID"}}' \
  --json
```

`omg example show message-inbox --json` returns both `payload_schema` and `example_payload`.

### Create handoff

```text
omg handoff create [selection] --idempotency-key KEY --payload JSON [--json]
```

```json
{
  "id": "handoff-unique",
  "task_id": "task-internal-id",
  "run_id": "run-internal-id",
  "source_session_id": "agt-worker",
  "target_session_id": "agt-reviewer",
  "summary": "Implementation complete; review requested.",
  "final_output_policy": "hash_only",
  "final_output_hash": "sha256:...",
  "source_commit": "SOURCE_COMMIT_SHA",
  "source_tree": "SOURCE_TREE_SHA",
  "changed_files": ["internal/example.go"],
  "commits": [],
  "verification_evidence": [
    {"summary": "targeted tests passed", "hash": "sha256:..."}
  ],
  "remaining_risks": [],
  "suggested_actions": ["review", "accept"]
}
```

Specify at most one target (`target_session_id` or `target_task_id`). Final output policy is validated by the domain; raw final output is hidden from default views.

### Remaining lineage, task, and run commands

```text
omg human create|get
omg session create|resume|adopt|import|archive
omg delegate issue|register|revoke
omg checkpoint [record]
omg task get|claim|transition|run-create|run-transition
```

Every command takes exactly one payload source as described above; mutations also take `--idempotency-key KEY`. The read-only commands are `human get` and `task get`.

| Command | Strict payload fields |
|---|---|
| `human create` | `id?`, `display_name`, `confidence`, `supersedes_id?` |
| `human get` | `id` |
| `session create` | `id?`, `human_id`, `runtime`, `role`, `source_ref?`, inert compatibility fields `instruction_source?` and `provenance_confidence?`, `task_id?`, `worktree_ref?`, `native_access_state`, and optional private native-runtime fields |
| `session resume\|adopt\|import` | `id?`, `human_id?`, `runtime`, `role`, `source_ref`, `parent_session_id?`, `continuation_of_id?`, `task_id?`, `worktree_ref?`, `native_access_state`, and optional private native-runtime fields |
| `session archive` | `id`, `session_id`, `actor_session_id`, `reason` |
| `delegate issue` | `task_id?`, `parent_session_id`, `ttl_seconds` |
| `delegate register` | `raw_token`, `task_id?`, `parent_session_id`, `session` |
| `delegate revoke` | `token_id` |
| `checkpoint record` | `id`, `session_id`, `liveness`, `detail?` |
| `task get` | `task_id` |
| `task claim` | `task_id`, `session_id` |
| `task transition` | `task_id`, `state`, `evidence?`, `actor_session_id?` |
| `task run-create` | `id?`, `task_id`, `session_id` |
| `task run-transition` | `run_id`, `state`, `evidence?` |

`task transition` with `actor_session_id` atomically reconciles dependencies and emits the resulting notifications. Native runtime homes and opaque locator fields are accepted only for adapter linkage and never appear in default views.

`session archive` is append-only: it records an archived/interrupted liveness event and removes the session from active counts only after every owned run is terminal. It requires both the target session and controller actor to exist in the selected project; it never deletes session history.

### Progress, dependency, mailbox, and handoff lifecycle

```text
omg progress add|history
omg dependency add|list
omg message send|inbox|thread|deliver|read|ack
omg handoff create|show|history|lifecycle|advance|supersede|accept|reject|adopt
```

Mutation/query status and payloads:

| Command | Mutation | Strict payload fields |
|---|:---:|---|
| `progress add` | yes | `id`, `task_id`, `run_id?`, `session_id`, `phase` (`inspect`, `plan`, `implement`, `test`, `review`, or `wait`), `done[]`, `doing[]`, `next[]`, `supersedes_id?` |
| `progress history` | no | `task_id` |
| `dependency add` | yes | `id`, `prerequisite_task_id`, `dependent_task_id`, `kind`, `criterion` |
| `dependency list` | no | `{}` |
| `message send` | yes | payload shown above |
| `message inbox` | no | `recipient` |
| `message thread` | no | `thread_id` |
| `message deliver\|read\|ack` | yes | `message_id`, `recipient` |
| `handoff create` | yes | payload shown above |
| `handoff show` | no | `handoff_id` |
| `handoff history` | no | `task_id` |
| `handoff lifecycle` | no | `handoff_id` |
| `handoff advance` | yes | `id`, `handoff_id`, `actor_session_id`, `state`, and state-specific evidence |
| `handoff supersede` | yes | `handoff_id`, `new_id`, `summary` |
| `handoff accept\|reject` | yes | `handoff_id`, `decision_id?`, `actor_session_id` |
| `handoff adopt` | yes | `id`, `entity_kind`, `entity_id`, `new_owner_session_id`, `reason` |

The append-only success path is `SUBMITTED → REVIEWING → ACCEPTED → INTEGRATED → CANARY_RUNNING → CANARY_PASSED → SOURCE_CLEANED`. A canary may instead finish as `CANARY_MOCK_PASSED`, `CANARY_FAILED`, `CANARY_SKIPPED`, or `CANARY_INVALIDATED`; those states may start a new canary, but none permits source cleanup. `CANARY_PASSED` means `PASS_REAL` against the exact recorded SHA/tree with an unchanged ref-history fingerprint. Acceptance and rejection continue through `handoff accept|reject`.

Recipient objects contain exactly one of `session_id`, `human_id`, `task_id`, or `role`. Adoption entity kinds are `session`, `task`, `handoff`, and `git_asset`.

### Reservations and Git observations

```text
omg reserve add|list|active|history|renew|release|override
omg git inventory|current|latest|history|diff|cleanup-plan|reconcile|adopt
omg orphan scan
omg canary start|finish
```

| Command | Mutation | Strict payload fields |
|---|:---:|---|
| `reserve add` | yes | `id`, `pattern_kind`, `pattern`, `case_sensitivity`, `mode`, `human_id`, `session_id`, `task_id`, `run_id`, `intent`, `ttl_seconds` |
| `reserve list\|active` | no | `{}` |
| `reserve history` | no | `reservation_id` |
| `reserve renew` | yes | `reservation_id`, `checkpoint_id`, `ttl_seconds` |
| `reserve release` | yes | `reservation_id`, `reason` |
| `reserve override` | yes | `reservation_id`, `human_id`, `reason` |
| `git inventory` | yes | `directory`; `session_id`, `task_id`, and `run_id` must be supplied together or all omitted |
| `git current\|latest\|history` | no | payload omitted; optional `session_id` is accepted only as a compatibility hint |
| `git diff` | no | payload omitted for the latest pair; optional `before` and/or `after` observation IDs select bounds |
| `git cleanup-plan` | no | payload omitted for all assets; optional `fingerprint` selects one asset |
| `git reconcile` | no | direct `--integration-branch REF`; verifies actual source SHA/tree and merge, cherry-pick/squash patch equivalence, or exact-tree inclusion |
| `orphan scan` | no | optional direct `--integration-branch REF`; defaults to `HEAD` |
| `git adopt` | yes | `id`, `git_asset_id`, `new_owner_session_id`, `reason` |

Git observation and reconciliation are bounded to the selected project repository and its linked worktrees. They do not scan unrelated repositories elsewhere on the machine.

`canary start` takes `--handoff`, `--session`, `--integration-ref`, `--verification-command`, `--execution-kind real|mock`, `--environment-fingerprint`, and an idempotency key. It resolves the latest recorded integration commit and refuses to start if the selected ref is at another SHA. `canary finish` takes `--canary`, `--session`, `--exit-code`, `--passed`, `--failed`, `--skipped`, optional `--evidence-path`, and an idempotency key. OMG records the command and receipt; it does not execute the verification command. A changed SHA, tree, or ref-history fingerprint produces `CANARY_INVALIDATED` even when the supplied test counts otherwise pass.

Git reads are project-scoped. Prefer the payload-free forms below; do not invent a `session_id` filter. `git diff` reports the selected `before` and `after` IDs with its counts.

```text
omg git latest --project /project --json
omg git history --project /project --json
omg git diff --project /project --json
```

Git commands are observational. `cleanup-plan` is advisory and does not delete, reset, clean, merge, commit, push, or otherwise mutate Git. `git adopt` changes only canonical OMG ownership metadata.

Reservations require complete execution lineage. Create a task run before reserving a path, then supply the same human, session, task, and run IDs:

Reservations owned by the exact same `human_id` / `session_id` / `task_id` / `run_id` lineage may overlap without producing a conflict. They describe one execution unit's bounded work, not competing ownership. Overlap with a different lineage remains advisory or strict according to the selected policy.

```text
omg task run-create --project /project --idempotency-key run-1 \
  --payload '{"id":"run-1","task_id":"TASK_ID","session_id":"SESSION_ID"}' --json
omg reserve add --project /project --idempotency-key reserve-1 \
  --payload '{"id":"reservation-1","pattern_kind":"exact","pattern":"TODO.md","case_sensitivity":"sensitive","mode":"exclusive","human_id":"HUMAN_ID","session_id":"SESSION_ID","task_id":"TASK_ID","run_id":"run-1","intent":"edit TODO","ttl_seconds":3600}' --json
```

### Generic import

```text
omg import record [selection] --idempotency-key KEY (--payload JSON | --payload-stdin | --payload-file ABSOLUTE_PATH) [--json]
```

Payload fields: `source_record_id`, `source_state`, `title`, `runtime`, `role`, and optional `parent_task_id`. The generic core has no Pygmalion- or Zoomzi-specific branch; adapters normalize external records before invoking this command.

## Instruction integration

```text
omg integration plan   --project P [--json]
omg integration apply  --project P [--json]
omg integration status --project P [--json]
omg integration remove --project P [--status] [--json]
```

Default targets are root `AGENTS.md` and `CLAUDE.md`. Plan and status are read-only. Apply and remove are idempotent, bounded managed-block operations.

## Runtime and shell adapters

```text
omg run --runtime NAME -- EXECUTABLE [ARG ...]
omg shell-init bash|zsh|fish|powershell [--json]
omg completion bash|zsh|fish|powershell [--json]
omg watch [selection] [--json]
omg watch status [selection] [--json]
omg mcp serve --stdio
```

`run` deliberately does not support `--json`; stdout and stderr belong to the wrapped process, followed by one compact structured terminal result. When the child starts and exits, OMG preserves its exit code. An unavailable executable maps to exit 3.

`watch` remains in the foreground until cancellation. Shell commands print generated scripts and never edit startup files. `shell-init` emits explicit preflight/board/checkpoint helpers, and `completion` limits suggestions to the selected command family while sharing one vocabulary across Bash, Zsh, Fish, and PowerShell. MCP uses protocol-only stdout.

## Example discovery

```text
omg example list [--json]
omg example show TOPIC [--json]
```

Example topics are generated from the live help contract, so displayed commands and payloads stay synchronized with contextual help. In JSON mode, payload-bearing topics expose separate `payload_schema` and `example_payload` fields in addition to human `usage`. The required worker lifecycle topics include `message-inbox`, `progress-add`, `handoff-create`, `handoff-accept`, `checkpoint-record`, `reserve-add`, `session-create`, and `session-archive`. Topics use command-subcommand names such as `reserve-add` and `task-run-create`; `reservation-add` is accepted as a compatibility alias for `reserve-add`.

The complete task lifecycle is also copyable without reconstructing payloads from prose: `task-create`, `task-get`, `task-claim`, `task-transition`, `task-run-create`, and `task-run-transition` all expose `payload_schema` and `example_payload`.

JSON errors retain the compatibility `warnings` array and additionally expose machine-readable recovery under `error.recovery`:

```json
{
  "error": {
    "code": "not_found",
    "message": "human_id is not registered in the selected project",
    "retryable": false,
    "exit_code": 3,
    "recovery": {
      "hint": "Use the controller-provided OMG_HUMAN_ID; create a human only when establishing a new owner.",
      "next_command": "omg example show session-create --json"
    }
  }
}
```

Reference lookup failures are not reported as transient store failures. In particular, an unknown `human_id` during `session create` is a non-retryable `not_found`; retryable `unavailable` remains reserved for actual store or runtime availability failures.

Legacy discovery is explicit: `omg inbox` points to `omg message inbox`, payload-free `omg git inventory` points read-only users to `omg git current`, `omg schema` points to `omg migration`, and `omg --version` is accepted as an alias for `omg version`. These hints do not silently convert mutating requests.

## JSON envelopes

A successful `--json` command emits exactly one object:

```json
{
  "ok": true,
  "data": {},
  "meta": {"schema_version": 1, "command_version": 1},
  "warnings": []
}
```

A failed `--json` command emits the same stable envelope shape. When a safe recovery is known, `warnings` carries bounded `hint:` and `next:` entries:

```json
{
  "ok": false,
  "error": {
    "code": "invalid_argument",
    "message": "unknown field recipient_session_id; expected recipient.session_id",
    "retryable": false,
    "exit_code": 2
  },
  "meta": {"schema_version": 1, "command_version": 1},
  "warnings": [
    "hint: Inspect the copyable payload_schema and example_payload fields.",
    "next: omg example show message-inbox --json"
  ]
}
```

Consumers must branch on `ok`, treat unknown additive fields as forward-compatible unless their own policy forbids them, and use `meta.schema_version`/`command_version` when pinning parsers. Error messages and recovery warnings are bounded and must not be interpreted as approval or executable text.

## Stable exit codes

| Code | Name | Meaning |
|---:|---|---|
| 0 | success | Command completed successfully. |
| 2 | usage | Invalid argument, payload, selector, or invocation shape. |
| 3 | not found | Requested entity or explicit executable was not found. |
| 4 | unavailable | Uninitialized state, unsupported/unwired command, or unavailable service. |
| 5 | conflict | Stale state, ownership conflict, concurrent edit, or policy conflict. |
| 6 | temporary | Retryable error not mapped more specifically. |
| 70 | internal | Unexpected internal failure. |

For `omg run`, a child process that starts successfully retains its own non-negative exit code instead of remapping it.

## Idempotency

Every durable mutation must have a caller-supplied idempotency key. Repeating the same command with the same key returns the original receipt/outcome and does not append a duplicate event. Reusing a key for a different operation is a conflict. Keys identify commands; they are not credentials or authorization.
