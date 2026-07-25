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

## Foundation

```text
omg version [--json]
omg release status [--json]
omg init [--project P | --workspace W | --store DB] [--json]
omg doctor [selection] [--integrity] [--json]
omg preflight [selection] [--json]
```

`version` is available without state. `release status` returns `SOURCE PUBLISHED`, the canonical repository and license, and `stable_release: false` until a stable release exists. `init` is idempotent and reports pending migrations without applying them. `doctor --integrity` performs SQLite integrity verification. `preflight` reports initialization and pending-schema status before coordinated work.

## Schema and backup

```text
omg migration plan [selection] [--output PLAN.json] [--json]
omg backup create [selection] [--plan-file PLAN.json] [--json]
omg migration apply [selection] --plan-file PLAN.json --approval-file APPROVAL.json [--json]
```

- `migration plan` does not change schema. `--output` writes the complete plan with mode `0600`; default JSON output intentionally omits its private backup path.
- `backup create` accepts the exact saved plan or computes the current plan when omitted. It returns the verified SHA-256 checksum.
- `migration apply` requires exact plan and human-approval files. It fails closed on stale or mismatched state.

See `docs/OPERATIONS.md` for the approval schema and recovery rules.

## Board and export

```text
omg board me   [selection] --session ID [--format tty|markdown|html|json | --json]
omg board tree [selection] [--format tty|markdown|html|json | --json]
omg board task [selection] --task ID [--format tty|markdown|html|json | --json]
omg board all  [selection] [--format tty|markdown|html|json | --json]
omg board git  [selection] [--format tty|markdown|html|json | --json]

omg export [selection] --json
omg export html     [selection] --output NEW_FILE
omg export markdown [selection] --output NEW_FILE
omg export tty      [selection] --output NEW_FILE
omg export json     [selection] --output NEW_FILE
```

The default board format is TTY. `--json` selects the standard CLI envelope and cannot be combined with `--format`. Exports are created atomically with mode `0600` and never overwrite an existing path.

## Typed coordination commands

These commands require exactly one payload source (`--payload JSON`, `--payload-stdin`, or `--payload-file ABSOLUTE_PATH`); mutations also require `--idempotency-key`. Payloads are strict JSON: wrong types, missing required fields, trailing values, unknown fields, and payloads larger than 1 MiB fail before any write.

`--payload-stdin` reads one bounded JSON payload from standard input. `--payload-file` accepts only a canonical absolute path whose parent and regular file are owner-only (`0700`/`0600` on POSIX; current-user protected one-ACE DACL on Windows); links, reparse points, directories, special files, broad permissions, and unsafe ancestors reject. A versioned OMG delegation token is rejected inside inline `--payload` so it never enters process arguments. Therefore `delegate register` MUST use stdin or a private payload file:

```text
printf '%s' "$REGISTER_JSON" | omg delegate register [selection] --idempotency-key KEY --payload-stdin --json
omg delegate register [selection] --idempotency-key KEY --payload-file /absolute/private/register.json --json
```

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
omg session create|resume|adopt|import
omg delegate issue|register|revoke
omg checkpoint [record]
omg task get|claim|transition|run-create|run-transition
```

Every command takes exactly one payload source as described above; mutations also take `--idempotency-key KEY`. The read-only commands are `human get` and `task get`.

| Command | Strict payload fields |
|---|---|
| `human create` | `id?`, `display_name`, `confidence`, `supersedes_id?` |
| `human get` | `id` |
| `session create\|resume\|adopt\|import` | `id?`, `human_id?`, `runtime`, `role`, `source_ref`, `parent_session_id?`, `continuation_of_id?`, `task_id?`, `worktree_ref?`, `native_access_state`, and optional private native-runtime fields |
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

### Progress, dependency, mailbox, and handoff lifecycle

```text
omg progress add|history
omg dependency add|list
omg message send|inbox|thread|deliver|read|ack
omg handoff create|show|history|supersede|accept|reject|adopt
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
| `handoff supersede` | yes | `handoff_id`, `new_id`, `summary` |
| `handoff accept\|reject` | yes | `handoff_id`, `decision_id?`, `actor_session_id` |
| `handoff adopt` | yes | `id`, `entity_kind`, `entity_id`, `new_owner_session_id`, `reason` |

Recipient objects contain exactly one of `session_id`, `human_id`, `task_id`, or `role`. Adoption entity kinds are `session`, `task`, `handoff`, and `git_asset`.

### Reservations and Git observations

```text
omg reserve add|list|active|history|renew|release|override
omg git inventory|current|latest|history|diff|cleanup-plan|adopt
```

| Command | Mutation | Strict payload fields |
|---|:---:|---|
| `reserve add` | yes | `id`, `pattern_kind`, `pattern`, `case_sensitivity`, `mode`, owner IDs, `intent`, `ttl_seconds` |
| `reserve list\|active` | no | `{}` |
| `reserve history` | no | `reservation_id` |
| `reserve renew` | yes | `reservation_id`, `checkpoint_id`, `ttl_seconds` |
| `reserve release` | yes | `reservation_id`, `reason` |
| `reserve override` | yes | `reservation_id`, `human_id`, `reason` |
| `git inventory` | yes | `directory`; `session_id`, `task_id`, and `run_id` must be supplied together or all omitted |
| `git current\|latest\|history` | no | `{}` |
| `git diff` | no | `before`, `after` observation IDs |
| `git cleanup-plan` | no | `fingerprint` |
| `git adopt` | yes | `id`, `git_asset_id`, `new_owner_session_id`, `reason` |

Git commands are observational. `cleanup-plan` is advisory and does not delete, reset, clean, merge, commit, push, or otherwise mutate Git. `git adopt` changes only canonical OMG ownership metadata.

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

A failed `--json` command emits:

```json
{
  "ok": false,
  "error": {
    "code": "invalid_argument",
    "message": "safe bounded message",
    "retryable": false,
    "exit_code": 2
  }
}
```

Consumers must branch on `ok`, treat unknown additive fields as forward-compatible unless their own policy forbids them, and use `meta.schema_version`/`command_version` when pinning parsers. Error messages are bounded and must not be interpreted as approval or executable text.

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
