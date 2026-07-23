# ADR 0002: Daemonless CLI Core with Optional Watch and MCP Adapters

- **Status:** Accepted
- **Date:** 2026-07-22
- **Decision owners:** OMG maintainers

## Context

OMG coordinates humans and heterogeneous coding agents, but it must remain useful when no background process is running. A session must be able to discover lineage, task state, inbox, dependencies, reservations, handoffs, and Git warnings after interruption or restart. The product contract also requires optional shell integration, local notifications, `omg watch`, and MCP access without creating a second interpretation of the coordination rules.

Human authority is ultimate. Messages, model output, MCP requests, and watch events are untrusted input, never approval. In v0.1 no adapter may make commit, push, deploy, credential, production, deletion, publication, reset, clean, or branch/worktree-removal decisions.

## Decision

### One canonical command/application layer

`omg` is a daemonless CLI first. Every invocation independently resolves the store, opens it, runs one application command, writes any receipt/audit event atomically with the command outcome, renders the requested view, and closes resources. Core commands MUST work with no watch process, shell hook, notification service, or MCP server present.

The package boundary is:

```text
CLI parser / JSON transport / MCP transport / watch event source
                         -> Application command service
                         -> Domain policies and state transitions
                         -> Store, Git, clock, process, notification ports
```

Only the application command service may invoke domain mutations. Transports convert their input into a typed command, call the same service, and serialize the returned result. They MUST NOT issue their own SQL, apply competing validation/state transitions, calculate authorization from message content, or bypass receipts/idempotency.

The minimum implementation-facing contracts are:

```text
CommandHandler.Handle(ctx, ActorContext, Command) -> Result | DomainError
QueryHandler.Query(ctx, ActorContext, Query) -> ViewModel | DomainError
ReceiptStore.WriteOnce(ctx, idempotencyKey, operation) -> Result | PriorResult
EventPublisher.Publish(ctx, Event) -> error
Adapter.Start(ctx) -> error
Adapter.Stop(ctx) -> error
```

`ActorContext` contains authenticated local invocation provenance, resolved project/workspace identity, and explicit capability flags; it is not inferred from free-form message content. `Result` contains a stable outcome code, receipt ID, data/view model, and warnings. `DomainError` contains a stable error code, safe message, retryability, and no secret/raw-token payload. Application services return data structures, not terminal text or MCP protocol objects.

### CLI contract

1. `omg` is the primary local interface and has no process that must be started before ordinary commands.
2. Mutating commands require a command idempotency key unless the command is provably read-only. The CLI generates one when the caller does not supply one and returns it in the receipt; retrying the same key returns the original outcome rather than replaying the mutation.
3. Every command supports a stable JSON success/error envelope and documented stable exit codes. The human-oriented TTY renderer is a presentation adapter over the same `Result`/`ViewModel`.
4. `omg preflight`, `board`, `checkpoint`, human/session/task/progress/delegate/message/reserve/handoff, Git inventory/adoption/cleanup-plan, integration, backup, doctor, and version MUST remain operational with watch stopped.
5. Long-running or interactive work happens outside a database transaction. The core command records a semantic state transition first or returns a durable plan; it never relies on an in-memory watch loop to finish a required state change.

### Optional watch adapter

`omg watch` is an optional, single-host convenience adapter. It may watch filesystem/Git changes, refresh local notifications, and run periodic non-destructive checks. It MUST NOT become a source of truth, own an exclusive lock that prevents CLI commands, or be required for mailbox delivery, dependency unblocking, TTL evaluation, or reconciliation.

Watch behavior is defined as a loop of idempotent application commands/queries:

- it derives a change signal from a local source;
- it re-reads canonical state before acting;
- it submits an idempotency key derived from the event identity and operation;
- it emits a best-effort notification only after the canonical transition succeeds; and
- on crash/restart it may miss a notification but MUST NOT lose, invent, or duplicate a canonical domain outcome.

A stopped watch process has no correctness effect. On restart it performs a bounded reconciliation scan rather than replaying unbounded historical events. Watch owns only its local PID/lease metadata; stale metadata is advisory and never blocks a normal CLI command.

### Optional MCP adapter

MCP is an optional local adapter exposing OMG command and query capabilities to compatible agent runtimes. It MUST use the same `CommandHandler` and `QueryHandler` as the CLI and map MCP request IDs to OMG idempotency keys where the operation mutates state. It is not an authorization service, background daemon requirement, or alternate database API.

The adapter boundary requires:

- strict schema validation before constructing a typed command;
- explicit project/workspace selection rather than ambient working-directory guessing when an MCP host can address multiple roots;
- stable mapping of domain errors to MCP-safe errors without raw tokens, prompts, private paths, or unredacted message bodies;
- cancellation propagation to the application context; and
- no shell execution of model-provided text. If a future tool launches a runtime, it MUST use the generic `omg run --runtime ... -- <command>` wrapper and preserve argument boundaries.

An MCP request that expresses approval, asks for destructive Git activity, or contains a token-like string remains untrusted input. The canonical policy rejects or plans the operation exactly as the CLI would.

### Adapter lifecycle and failure isolation

Adapters are started explicitly (`omg watch`, `omg mcp`, or a future documented command) and stopped independently. Their startup failure MUST NOT corrupt the store or make core CLI commands unavailable. Adapters may subscribe to committed audit/domain events through `EventPublisher`, but delivery is at-least-once/best-effort and consumers MUST be idempotent. Events are facts after commit, not commands to be reinterpreted by adapters.

No v0.1 adapter performs remote synchronization, exposes a network listener by default, or requires telemetry/model API access.

## Consequences

- OMG remains operable for short-lived agents, headless CI, interrupted terminals, and users who do not install MCP or watch tooling.
- The CLI and MCP share semantics and receipts, reducing drift but requiring deliberate transport-neutral result types.
- Notifications may be delayed or absent when watch is stopped; the durable inbox and board remain authoritative.
- Optional adapters add lifecycle and idempotency testing, but failures are contained outside the core state transition path.

## Rejected alternatives

1. **Required always-on daemon:** rejected because it introduces lifecycle, crash-recovery, permissions, and installation dependencies that violate the daemonless core contract.
2. **MCP server as the product's primary implementation surface:** rejected because it excludes non-MCP users and risks duplicating CLI/domain logic.
3. **Adapter-specific SQL or business rules:** rejected because CLI/MCP outcomes would diverge and receipts/audit history would be incomplete.
4. **Polling/in-memory queues as mailbox or dependency truth:** rejected because a stopped process would lose correctness.
5. **Treating messages or model instructions as approval:** rejected because untrusted text must never grant authority.
6. **A default network service for cross-host coordination:** rejected because v0.1 is local-first and does not include remote authenticated synchronization.

## Risks and mitigations

- **CLI/MCP behavior drift:** enforce shared handler invocation and contract tests that issue equivalent commands through both transports.
- **Duplicate watch signals:** derive idempotency keys from stable event identity and re-read canonical state before transition.
- **Lost local notifications:** treat notifications as advisory; expose unread durable inbox items in `preflight` and `board`.
- **MCP prompt injection or malformed payloads:** validate schemas, keep capability checks in the application layer, redact errors, and never evaluate request text as shell or approval.
- **Long-running adapter blocking shutdown:** propagate cancellation and require adapters to stop without holding a store transaction.

## Rollback and revisit triggers

Disable or remove an optional adapter if it can mutate state outside the shared handlers, materially destabilizes local operation, or creates an unbounded resource/security risk. Disabling watch or MCP is safe because canonical state and CLI commands remain available.

Revisit the daemonless decision only if a future approved multi-host synchronization requirement cannot be met by explicit optional services while preserving local CLI correctness. Such a change requires a new ADR defining authentication, durability, authority, and offline behavior; it cannot be introduced as a watch/MCP implementation detail.

## Testable acceptance checks

1. With no `omg watch` process and no MCP process running, a fresh Git project can initialize and execute lineage, task, mailbox, dependency, reservation, handoff, Git inventory, board, backup, and doctor command paths.
2. Interrupting and restarting `omg watch` during repeated local signals does not lose canonical inbox/task data or create duplicate state transitions; a subsequent `preflight` exposes the same durable state.
3. The same valid mutating fixture sent through CLI JSON and MCP produces equivalent domain result codes, receipt semantics, persisted state, and redacted view model.
4. Repeating a mutating command with the same idempotency key returns the first receipt/outcome and creates no second domain event; different keys follow the documented conflict/state rules.
5. An MCP payload with invalid schema, shell-like text, token-like text, or an approval/destructive Git request is rejected or rendered as untrusted data without shell execution, secret disclosure, or destructive action.
6. A forced watch or MCP startup failure leaves a concurrent `omg board --json` command usable and leaves no open transaction or exclusive core lock.
7. Cancellation of an MCP request and termination of watch both complete without committing a partial domain outcome; committed outcomes have their matching receipt/audit event.
