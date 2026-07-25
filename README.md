# OMG

**Local-first coordination and recovery for people and AI coding agents.**

OMG keeps agent lineage, task state, progress, dependencies, messages, handoffs, path reservations, and observed Git state in one local SQLite ledger. It is daemonless by default, runtime-neutral, and designed for mixed Codex, Claude, ChatGPT2Codex, GJC, and shell-agent workflows.

> Release state: **NOT PUBLISHED**. `OMG`, `omg`, `.omg/`, `OMG_`, and `OMGCP` are private working identifiers pending final name clearance.

## What it does

- Records who instructed each agent and how delegated or resumed sessions relate.
- Separates `WORK_COMPLETE` from independently accepted `VERIFIED_DONE`.
- Provides durable task, progress, dependency, mailbox, handoff, and reservation state without a daemon.
- Observes Git repositories and worktrees without committing, merging, rebasing, pushing, resetting, cleaning, or deleting them.
- Renders the same redacted canonical snapshot as TTY, JSON, Markdown, or self-contained HTML.
- Adds and removes bounded OMG instruction blocks in `AGENTS.md` and `CLAUDE.md` without taking over existing files.
- Exposes the CLI through a thin MCP stdio adapter and can explicitly wrap an existing agent executable.

OMG does **not** grant an agent commit, push, deploy, credential, production, deletion, publication, or destructive Git authority. Messages and model output are untrusted data, never approval.

## Build from this checkout

Requirements: Go 1.26 or the version declared in `go.mod`. Release binaries are built with `CGO_ENABLED=0` and require no Node, Python, shared SQLite library, daemon, or cloud account.

```bash
go build -trimpath -o ./bin/omg ./cmd/omg
./bin/omg version --json
./bin/omg release status --json
```

The expected release status is `NOT PUBLISHED`.

## Fresh-project quickstart

Schema changes are never implicit. Initialization creates the local project configuration and state location, then reports pending schema migrations. Review the exact plan and verified backup before creating the separate approval file.

```bash
PROJECT=/absolute/path/to/your/project
OMG=/absolute/path/to/this/checkout/bin/omg

"$OMG" init --project "$PROJECT" --json
"$OMG" migration plan --project "$PROJECT" --output "$PROJECT/.omg/migration-plan.json" --json
"$OMG" backup create --project "$PROJECT" --plan-file "$PROJECT/.omg/migration-plan.json" --json
```

The backup command returns `data.checksum`. Review `.omg/migration-plan.json`, confirm its project, schema versions, checksums, and `backup_location`, then create `.omg/migration-approval.json` with this exact shape:

```json
{
  "approval_id": "UNIQUE ONE-TIME APPROVAL ID",
  "approved_by": "YOUR NAME",
  "evidence_reference": "local quickstart approval",
  "plan_id": "COPY plan.id",
  "project": "COPY plan.project",
  "from_version": "COPY plan.from_version",
  "to_version": "COPY plan.to_version",
  "checksums": ["COPY every plan checksum in order"],
  "backup_location": "COPY plan.backup_location",
  "backup_checksum": "COPY backup data.checksum",
  "command": "omg migration apply",
  "timestamp": "CURRENT UTC RFC3339 TIMESTAMP ENDING IN Z",
  "expires_at": "UTC RFC3339 TIMESTAMP ENDING IN Z, AFTER timestamp AND NO MORE THAN 15 MINUTES LATER"
}
```

`approval_id` is consumed by the successful apply and cannot be replayed. Generate it locally, keep the approval evidence bounded and secret-free, and execute before `expires_at`; approvals older than 15 minutes are rejected.

Before applying, restrict the approval file to the current user. On POSIX systems run `chmod 600 "$PROJECT/.omg/migration-approval.json"`; on Windows use an owner-only ACL. The CLI rejects approval files that other users can read or replace.

Apply only after that explicit review:

```bash
"$OMG" migration apply \
  --project "$PROJECT" \
  --plan-file "$PROJECT/.omg/migration-plan.json" \
  --approval-file "$PROJECT/.omg/migration-approval.json" \
  --json

"$OMG" doctor --project "$PROJECT" --integrity --json
"$OMG" board all --project "$PROJECT" --format tty
"$OMG" export html --project "$PROJECT" --output "$PROJECT/omg-board.html"
```

The board is empty until sessions and work are registered. The HTML export is static, self-contained, network-free, and created only at a new destination.

## Optional instruction integration

Always inspect before mutation. Apply inserts only the OMG managed block; remove deletes only that block.

```bash
"$OMG" integration plan --project "$PROJECT" --json
"$OMG" integration apply --project "$PROJECT" --json
"$OMG" integration status --project "$PROJECT" --json
"$OMG" integration remove --project "$PROJECT" --status --json
```

## Agent and MCP adapters

Run an existing executable explicitly; OMG never shadows its binary name:

```bash
omg run --runtime codex -- codex
omg run --runtime claude -- claude
```

Serve the same command policy over newline-delimited MCP JSON-RPC 2.0 on stdio:

```bash
omg mcp serve --stdio
```

Running `omg` with no arguments prints a concise, non-dispatching discovery view. A bare namespace such as `omg task` or `omg board` opens that family's contextual help; explicit `omg --help` remains the full global contract.

Shell helpers are generated, not installed automatically. Initialization adds bounded `preflight`, `board`, and `checkpoint` helpers. Completion is description-rich, traverses command/subcommand paths, and uses native directory/file/format candidates for typed option values:

```bash
omg shell-init bash
omg completion zsh
omg completion powershell
```

The deterministic hostile-input preview can exercise every presentation without touching canonical state:

```bash
go run ./examples/board-preview --format html --output board-preview.html
go run ./examples/board-preview --format tty --output -
go run ./examples/board-preview --format markdown --output board-preview.md
go run ./examples/board-preview --format json --output board-preview.json
```

File output is owner-only and refuses to overwrite an existing path. `--output -` is the explicit stdout mode for terminal QA. Human TTY output is grapheme-aware and supports `OMG_COLOR_SCHEME=light|dark`; a conventional light `COLORFGBG` value is used only as a fallback, while `NO_COLOR` and `TERM=dumb` remain authoritative plain-output controls.

## Documentation

- [Operator guide](docs/OPERATIONS.md): storage, migrations, backup/recovery, watch, MCP, privacy, and troubleshooting.
- [Command reference](docs/COMMAND_REFERENCE.md): supported command syntax, payloads, JSON envelopes, and stable exit codes.
- [Adapter guide](docs/ADAPTERS.md): runtime wrappers, contextual shell helpers/completion, instruction surfaces, watch, MCP, and native-session metadata.
- [2026 interface research](docs/INTERFACE_RESEARCH_2026.md): official trend research, OMP source audit, accessibility decisions, and browser measurements.
- [Product specification](docs/PRODUCT_SPEC.md) and [acceptance matrix](docs/ACCEPTANCE_MATRIX.md).
- [Security policy](docs/SECURITY.md), [privacy contract](docs/PRIVACY.md), and [threat model](docs/THREAT_MODEL.md).

## Safety summary

- Canonical mutable state is outside Git. `.omg/` contains only tracked-safe configuration and operator-created plan/approval files.
- Private runtime homes and opaque native-session locators are excluded from default output and exports.
- OMG does not copy native conversation transcripts.
- Raw prompts, message bodies, final output, private paths, tokens, and secret-like values are hidden or redacted by default.
- `SAFE_TO_REMOVE` is a classification, not authorization. v0.1 has no destructive Git command.

See [SECURITY.md](docs/SECURITY.md) before integrating OMG into automated workflows.
