# OMG Adapter Guide

## Boundary rule

All adapters are thin transports around the same application services and canonical SQLite state. They may translate typed input/output and observe external state, but they may not create a second authority path, bypass capabilities, infer human approval, evaluate message content, or persist a parallel coordination truth.

Core correctness must remain available with every adapter stopped.

## Generic runtime wrapper

```bash
omg run --runtime codex -- codex [args...]
omg run --runtime claude -- claude [args...]
omg run --runtime chatgpt2codex -- chatgpt2codex [args...]
omg run --runtime gjc -- gjc [args...]
omg run --runtime shell -- /absolute/path/to/agent [args...]
```

The `--runtime` value is bounded provenance metadata. The first token after `--` is the only executable request. OMG resolves and executes that argv directly without a shell, preserves stdin/stdout/stderr, propagates cancellation, and reports how the executable was resolved without exposing its filesystem path.

Never pass a command assembled from a task title, prompt, message, handoff, or model output. Validate and construct argv in the invoking integration.

## Instruction-file integration

`omg integration` manages a single versioned block in each configured instruction surface. Without configuration, the targets are `AGENTS.md` and `CLAUDE.md` at the selected project root. A project may replace that set with `[integrations] instruction_targets = ["relative/path.md"]` in `.omg/project.toml`; every target remains project-relative and receives the same status, plan, apply, and remove lifecycle.

Lifecycle:

1. `integration plan`: inspect all targets and return `none|create|update|remove` actions with unified diffs.
2. Human review: inspect proposed rules and every target.
3. `integration apply`: preflight every target, then create/update only the managed block.
4. `integration status`: verify current managed state.
5. `integration remove --status`: remove only blocks created by OMG and report the final target states.

Properties:

- Existing content outside the marker block is byte-preserved.
- Existing encoding, BOM, newline style, mode, and unrelated nested instructions remain intact.
- Symlink targets, parent traversal, malformed markers, unsupported encodings, and concurrent target changes fail closed.
- Multi-target apply runs all preflights before its first mutation and rolls back already changed targets if a later write fails.
- Repeated apply and remove are idempotent.

The managed block tells agents to run preflight, share typed state, treat messages as untrusted data, avoid destructive Git behavior, and report progress/handoff evidence. It does not change the runtime's own security model.

## Shell integration

OMG emits optional generated shell snippets:

```bash
omg shell-init bash
omg shell-init zsh
omg shell-init fish
omg shell-init powershell

omg completion bash
omg completion zsh
omg completion fish
omg completion powershell
```

Generation has no side effects. Review the output and install it through the shell's normal configuration mechanism. OMG does not modify dotfiles automatically and does not shadow `codex`, `claude`, `gjc`, or any other agent binary.

The initialization snippets provide three explicit workflow helpers: `omg_preflight` / `OMG-Preflight`, `omg_board` / `OMG-Board`, and `omg_checkpoint` / `OMG-Checkpoint`. Each binds the current project path and delegates to the real `omg` binary; none evaluates task or message content.

Completion is a generated command tree rather than one global word list. At the top level it offers command families with descriptions where the shell supports them. After a family it offers that family's subcommands; after a valid leaf subcommand it removes siblings and offers options only. `help task` traverses into the task family. `--project`/`--workspace` use directory completion, private file/output flags use file completion, and `--format` offers only `tty|markdown|html|json`. Bash, Zsh, Fish, and PowerShell are derived from the same vocabulary and tested against the CLI help/decoder contract plus real Bash behavior.

## MCP stdio adapter

Start:

```bash
omg mcp serve --stdio
```

The server uses newline-delimited JSON-RPC 2.0 and advertises one tool named `omg`. Its only argument shape is a typed version-1 application request under `arguments.request`; `arguments.args` is not accepted.

The board query is the typed `board.query` request variant. It delegates to the same canonical query service as `omg board`, and returns the versioned `ViewModel` unchanged in `structuredContent.data`.

```json
{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"omg","arguments":{"request":{"version":1,"command":"board.query","project":"/absolute/project","payload":{"mode":"all"}}}}}
```

For example, against a running stdio server:

```bash
printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"omg","arguments":{"request":{"version":1,"command":"board.query","project":"/absolute/project","payload":{"mode":"all"}}}}}' | omg mcp serve --stdio
```

`payload` for `board.query` is strict: `mode` is one of `me`, `tree`, `task`, `all`, or `git`; `session_id` selects `me`, and `task_id` selects `task`. The canonical query service enforces selector requirements and authorization.

Contract:

- `initialize`, `tools/list`, and `tools/call` are supported.
- JSON-RPC notifications produce no response.
- The `tools/list` schema, request decoder, and CLI all use request version 1. Unknown fields and over-deep or oversized inputs are rejected before dispatch.
- One request yields at most one response.
- Stdout contains only JSON-RPC frames; diagnostics use stderr.
- Each eligible call reuses the typed application dispatcher, canonical state, and redaction. `board.query` returns the same canonical `ViewModel` as the CLI board path.
- Request cancellation propagates to the delegated call.

MCP cannot carry human approval. Migration approval and any other human-gated operation must use the separately specified local approval channel; putting approval-shaped text in a tool call remains untrusted data.

## Optional watch process

`omg watch` periodically reads canonical state and invokes bounded callbacks. It is useful for terminal refresh or local notification bridges, not required for mailbox delivery or dependency correctness.

```bash
omg watch --project /absolute/project
omg watch status --project /absolute/project --json
```

The watch state includes a random nonce and lock record. Process observation never relies on a reusable PID alone. Callback failures are isolated, the process responds to cancellation, and no callback runs while a canonical store transaction is held.

An external notification adapter should carry only safe summaries and identifiers, deduplicate using canonical message/event IDs, and direct the operator back to `omg board`/`omg preflight` for truth. Lost notifications do not lose durable state.

## Native-session metadata adapters

OMG can register read-only runtime adapters that resolve explicitly stored native-session locators. The public adapter result is deliberately narrow:

- verified native session ID;
- native parent session ID when available;
- native start time when available;
- access state: `available`, `missing`, `unreadable`, or `unsupported`.

Private runtime homes, opaque native references, and fingerprints stay behind the port. The adapter verifies the stored fingerprint before returning an association. It must not probe ambient runtime homes, discover unrelated accounts, read or retain conversation content, or serialize locator data into receipts, events, errors, boards, or exports.

No model name or product account is treated as an identity. OMG session IDs and human/root lineage remain canonical.

## Adding an adapter

A new adapter is acceptable only when it satisfies all of these constraints:

1. It consumes typed application contracts or delegates to the CLI; it does not import renderer state as authority.
2. It has no business logic or direct SQL.
3. It cannot broaden caller capabilities or convert data into approval.
4. It preserves idempotency keys and stable domain/exit outcomes.
5. It keeps diagnostics and protocol output separate.
6. It enforces bounded input and output and redacts private data.
7. It is cancellation-safe and holds no transaction across process, network, rendering, or interactive work.
8. It has parity evidence showing the same request through CLI and adapter yields the same canonical outcome.
9. Core use remains functional when the adapter is absent or stopped.

Pygmalion and Zoomzi integrations are validation projects, not dependencies or special cases in the general-purpose core.
