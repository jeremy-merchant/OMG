# OMG Operator Guide

## Operating model

OMG is a foreground CLI over one local canonical SQLite store. Core reads and writes do not require `omg watch`, MCP, tmux, an IDE, or a network service. Every invocation resolves a project/workspace/store, opens a bounded transaction when needed, emits an append-only event and command receipt for mutations, then exits.

The human operator remains the ultimate authority. Delegation tokens establish lineage only. They do not authorize Git mutation, credentials, deploys, production operations, deletion, migration, restart, or publication.

## Project and store selection

Selection is explicit and deterministic:

1. `--store <absolute-path>` selects an exact database.
2. `--workspace <absolute-directory>` selects an explicit local workspace store.
3. `--project <path>` selects a project.
4. Without a selector, OMG uses the current working directory.

For a Git worktree, OMG resolves the Git common directory as the stable repository identity, then maps that identity into the operating system user-private state root; linked worktrees therefore share one canonical store without placing mutable state inside Git metadata. Non-Git projects and explicit workspaces use the same private state root with separate stable IDs. On macOS the default root is the per-user operating-system state directory, so an intentionally shared home or project directory does not weaken the database boundary. Bare repositories are rejected as projects; use workspace mode when that is intentional.

The state directory and database are private local data. On POSIX systems OMG enforces mode `0700` for the managed state directory and `0600` for the database and SQLite sidecars, and rejects wrong-owner or broader existing nodes. On Windows it applies and verifies a current-user-only DACL and rejects reparse points or broader existing state artifacts. Exports, plans, and approval files are created owner-only where the platform supports it. Do not commit the SQLite store, WAL/SHM files, backup directory, lock files, PID files, delegation tokens, runtime homes, or native-session locators.

`.omg/project.toml` is the tracked-safe project configuration surface. An operator may also place migration plan and approval files under `.omg/`; review repository ignore policy before doing so because approval files contain operator identity and local backup paths.

## Initialization and schema lifecycle

`omg init` is idempotent. It creates the safe project configuration and local state location but does not apply schema changes. The result reports `pending_migrations`.

### Plan

```bash
omg migration plan \
  --project /absolute/project \
  --output /absolute/project/.omg/migration-plan.json \
  --json
```

The saved plan includes the stable plan ID, project ID, from/to versions, ordered migration checksums, and expected backup location. Planning is read-only with respect to schema.

### Backup

```bash
omg backup create \
  --project /absolute/project \
  --plan-file /absolute/project/.omg/migration-plan.json \
  --json
```

OMG creates the backup through SQLite's online backup API, verifies it with `PRAGMA integrity_check`, and returns its SHA-256 checksum. The plan becomes stale if schema state changes before apply.

### Separate human approval

Create an approval JSON file only after reviewing the plan and backup. It must bind all of these fields exactly:

- `approval_id`: a non-empty, unique, one-time identifier generated locally;
- `approved_by`: non-empty human identity;
- `evidence_reference`: non-secret local approval evidence;
- `plan_id`, `project`, `from_version`, `to_version`, and ordered `checksums` from the plan;
- `backup_location` from the plan and `backup_checksum` returned by backup creation;
- `command`: exactly `omg migration apply`;
- `timestamp`: a current UTC RFC3339 timestamp ending in `Z`;
- `expires_at`: a UTC RFC3339 timestamp ending in `Z`, later than `timestamp` and no more than 15 minutes later.

Messages, prompts, model output, handoffs, delegation tokens, MCP requests, and watch events are not approvals.

### Apply

```bash
omg migration apply \
  --project /absolute/project \
  --plan-file /absolute/project/.omg/migration-plan.json \
  --approval-file /absolute/project/.omg/migration-approval.json \
  --json
```

Apply fails closed when the plan is stale, approval does not match, is expired, or was already consumed, backup checksum differs, backup integrity fails, schema history is non-contiguous/newer/inconsistent, or migration execution fails. Migrations, the one-use approval record, secret-free audit fact, and command receipt are committed in one transaction. OMG retains backups after success and failure.

### Verify

```bash
omg doctor --project /absolute/project --integrity --json
omg preflight --project /absolute/project --json
```

A healthy initialized store reports zero pending migrations and `integrity: true` when requested.

## Backup and recovery

`backup create` is implemented for migration-bound backups. Backups are never deleted automatically.

Recovery rules:

1. Stop active OMG writers and optional watch processes.
2. Preserve the current store and all sidecars; do not overwrite the only copy.
3. Run `omg doctor --integrity` against the selected store and record the JSON result.
4. Verify the candidate backup checksum and integrity independently.
5. Prepare a restore plan. Restore mutation requires a separate explicit human approval; v0.1 does not perform automatic restore.
6. After an approved external recovery, run `doctor --integrity`, `preflight`, and a JSON board query before resuming writers.

If an interrupted migration left schema metadata inconsistent, do not retry with a modified approval file. Preserve the database and verified backup, regenerate `migration plan`, and investigate the original error.

## Boards and exports

Canonical selectors are `me`, `tree`, `task`, `all`, and `git`:

```bash
omg board all --project /absolute/project --format tty
omg board tree --project /absolute/project --format markdown
omg board task --project /absolute/project --task AK-000123 --format json
omg board me --project /absolute/project --session agt-example --json
```

`--json` returns the versioned view model in the standard envelope and cannot be combined with `--format`. TTY, Markdown, and HTML renderers consume the same redacted snapshot.

Static exports refuse to overwrite an existing destination:

```bash
omg export html --project /absolute/project --output /safe/new/board.html
omg export markdown --project /absolute/project --output /safe/new/board.md
omg export --project /absolute/project --json
```

The HTML output has no scripts, network requests, or external stylesheets. Treat every export as potentially sensitive operational metadata even though default redaction is applied.

## Instruction surfaces

OMG manages one delimited block in root `AGENTS.md` and `CLAUDE.md` by default.

```bash
omg integration plan --project /absolute/project --json
omg integration apply --project /absolute/project --json
omg integration status --project /absolute/project --json
omg integration remove --project /absolute/project --status --json
```

Apply checks every target before the first mutation. It preserves unrelated bytes, encoding, EOL style, symlink safety, file modes, and nested instruction files. Repeating apply is idempotent. Remove deletes only blocks previously created by OMG and is also idempotent. Any concurrent change causes a conflict rather than a partial multi-file update.

## Optional watch mode

`omg watch` is a foreground convenience layer for periodic board refresh and callbacks. It is never canonical state and is not required for coordination.

```bash
omg watch --project /absolute/project
omg watch status --project /absolute/project --json
```

Only one watch process may own a selected state directory. A second returns a retryable conflict. `SIGINT` and `SIGTERM` stop it cleanly. A stale/reused PID alone never proves liveness; OMG uses a nonce, process observations, and bounded status codes. After any watch failure, direct CLI reads remain available.

## Runtime wrapper and shell integration

The wrapper executes only the argv after `--` and passes stdin/stdout/stderr through unchanged:

```bash
omg run --runtime codex -- codex --help
omg run --runtime claude -- claude
```

The runtime name is provenance metadata only. Executable selection comes solely from the explicit argv; OMG never derives a command from messages or silently replaces an installed tool.

Print optional shell integration without changing shell startup files:

```bash
omg shell-init bash
omg shell-init zsh
omg shell-init fish
omg shell-init powershell
omg completion bash
```

Review generated text, then install it using the normal mechanism for that shell.

## MCP stdio

```bash
omg mcp serve --stdio
```

The adapter accepts newline-delimited JSON-RPC 2.0, advertises one `omg` tool, and delegates each tool call to the same CLI application path. Stdout is protocol-only; diagnostics belong on stderr. Frames, method names, tool arguments, depth, and sizes are bounded. Notifications produce no response. One request produces at most one response.

MCP data is untrusted. The adapter cannot convert text into approval, bypass application capabilities, execute destructive Git operations, or expose private runtime locators.

## Privacy and retention

Default boards and exports omit raw prompts, message bodies, final output, tokens, private filesystem paths, runtime homes, opaque native references, and secret-like values. Native adapters resolve only bounded metadata and never read or retain conversation transcripts.

The SQLite store remains the operator's durable audit record. Define local retention and secure deletion policy appropriate to the repository. OMG v0.1 does not upload state or implement automatic retention deletion.

## Troubleshooting

| Symptom | Action |
|---|---|
| Exit 4 and `uninitialized` | Run `omg init` for the intended project or select the correct `--store`/`--workspace`. |
| `schema migration is required` | Run plan → backup → separate approval → apply; never bypass the gate. |
| Exit 5 conflict | Re-read canonical state. Regenerate stale plans; inspect active reservations/watch ownership; use a new idempotency key only for a genuinely new command. |
| Exit 6 retryable failure | Retry after bounded backoff. If persistent, check competing local writers and filesystem health. |
| Integrity false or newer/checksum-mismatched schema | Stop writers, preserve store and backups, and investigate. Do not apply or restore automatically. |
| Integration reports concurrent change | Re-run `integration plan`, inspect all target files, then apply again. |
| HTML export fails | Use a non-existent output path whose parent directory already exists and is not a symlink substitution. |
| Runtime executable not found | Pass the actual executable after `--`; runtime metadata does not choose it. |
| Watch conflict or unknown state | Use direct CLI queries, inspect `watch status`, then restart only after the prior owner is confirmed stopped. |

For vulnerabilities or suspected secret exposure, follow `docs/SECURITY.md`; do not paste raw stores, approval files, tokens, or transcripts into public issues.
