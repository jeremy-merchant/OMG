# OMG Product Specification — v0.1

> **Implementation authority mirror.** This document is the stable public-English mirror of the authoritative OMG product contract. It intentionally omits interview metadata, transcripts, private session locations, and hidden working paths; it does not weaken, reinterpret, or replace the contract.
>
> **Public identity and release status:** OMG (**Oh My Group**), CLI `omg`, tracked configuration directory `.omg/`, environment prefix `OMG_`, and **OMG Coordination Protocol (OMGCP)** are published as open-source project identifiers. The canonical Go module is `github.com/jeremy-merchant/OMG`. This is not a trademark-clearance claim. Source is public under Apache-2.0; no stable binary release exists yet.

## Purpose and scope

OMG is an independent, local-first, vendor-neutral open-source coordination ledger. It lets heterogeneous coding agents and human operators working in the same project structurally share identity, lineage, work, progress, dependencies, messages, handoffs, path conflicts, and residual Git work so that they can continue safely.

OMG is a general-purpose product, not a Pygmalion or Zoomzi feature. An independent release candidate (RC) must be verified first. Pygmalion and Zoomzi are then separate, approved proof applications that demonstrate portability and practical value.

### Working product identity

- **Product:** OMG
- **Expansion:** Oh My Group
- **Working CLI:** `omg`
- **Working configuration directory:** `.omg/`
- **Working environment prefix:** `OMG_`
- **Working protocol:** OMG Coordination Protocol (OMGCP)
- **English tagline:** *A local coordination ledger for every coding agent.*
- **Supporting tagline:** *Every agent knows who asked, what waits, and what remains.*
- **Korean tagline:** *어떤 에이전트든, 같은 장부에서 이어서 일하게.*

## Core user outcomes

Every agent or human operator must be able to answer:

1. Who am I, which OMG runtime/session am I in, and can OMG safely locate the original runtime session when I explicitly request it?
2. Was I instructed directly by a human, or delegated by which parent/root agent?
3. Who is the root human?
4. What are my current task, previous task, and original prompt summary?
5. What have I completed, what am I doing now, and what is next?
6. Which branch and worktree am I using?
7. Which task am I waiting for, and which tasks are waiting for me?
8. Which messages, acknowledgements (ACKs), and handoffs have arrived?
9. Which paths does whom intend to modify, and what conflicts may exist?
10. Is interrupted or unregistered Git work still present?
11. What is self-reported `WORK_COMPLETE`, and what is verified `VERIFIED_DONE`?
12. What is the safe next action?

## Required domain capabilities

1. Human principal and provenance confidence.
2. Agent sessions with runtime, role, instruction source, parent/root/continuation, current/previous task, branch/worktree, process heartbeat, and semantic check-in. Native-runtime linkage is separate: `native_session_id`, private `runtime_home`, opaque `native_session_ref`, native start time, SHA-256 fingerprint, `available|missing|unreadable|unsupported` access state, and optional `native_parent_session_id`.
3. `human_direct`, `agent_delegated`, `resumed`, `adopted`, and `imported` lineage semantics.
4. One-time, TTL-bound delegation and optional human-root tokens; raw tokens are never stored.
5. Atomic task numbering and exclusive claim with stable conflict behavior.
6. Task/run state machines separating `WORK_COMPLETE` from `VERIFIED_DONE`.
7. Append-only `done / doing / next` progress updates.
8. Directed task dependencies, cycle rejection, configurable unblock criteria, and exactly-once unblock notifications.
9. Typed asynchronous mailbox: `NOTICE`, `QUESTION`, `DEPENDENCY`, `CONFLICT`, `HANDOFF`, `DONE`, `BLOCKED`, `CANCEL`, `ACK`.
10. Immutable or superseding handoffs with final output, changed files, commits, verification evidence, remaining risks, and suggested actions.
11. Advisory exact/path/glob reservations with shared/exclusive modes, TTL, renewal, release, conservative possible-conflict, and audited override.
12. Git reconciliation independent of registered sessions: worktrees, branches, detached heads, dirty state, unpushed/diverged/merged assets, owners, orphan classification, and cleanup-plan-only behavior.
13. Adoption of orphan sessions, tasks, handoffs, and Git assets without automatic checkout, merge, deletion, reset, or clean.
14. TTY, JSON, Markdown, and self-contained accessible HTML views generated from canonical state.
15. Append-only audit events, idempotency keys, command receipts, migration backups, integrity checks, and fail-closed newer-schema handling.

## Runtime and architecture constraints

- Use current stable Go and one primary cross-platform binary.
- SQLite is canonical state. Use foreign keys, bounded busy timeout, short transactions, migrations with checksums and pre-migration backup, deterministic transient-busy retry, and WAL only on supported local filesystems.
- In Git repositories, resolve shared state with `git rev-parse --path-format=absolute --git-common-dir` so linked worktrees share one store.
- Safe tracked configuration lives under `.omg/`; the database, tokens, prompts, private paths, PID data, and local overrides remain untracked.
- Non-Git mode uses the platform user-data directory. Explicit workspace mode may coordinate multiple projects in one local database.
- Core operation is daemonless. `omg watch`, local notifications, shell integration, and MCP are optional adapters.
- CLI/domain application services are canonical. MCP and other adapters must not duplicate business logic.
- Normal operation requires no cloud, telemetry, model API, or network access.

## Required product surfaces

- `omg preflight`: identity, task, inbox, dependencies, reservations, and Git warnings before work.
- `omg board me|tree|task|all|git`: human and agent views.
- `omg checkpoint`: semantic heartbeat, inbox, dependency, and reservation refresh.
- Commands for human, session, task, progress, delegate, message, reserve, handoff, Git inventory/adoption/cleanup-plan, integration, shell-init, completion, watch, MCP, export/import, backup, doctor, and version.
- A stable JSON success/error envelope and documented stable exit codes.
- Idempotent plan/apply/status/remove integration for `AGENTS.md`, `CLAUDE.md`, and configurable runtime instruction surfaces; preserve existing content, encoding, EOL, symlinks, and nested rules.
- Generic `omg run --runtime ... -- <command>` wrapper; never silently shadow existing agent binaries.

## Safety and authority prohibitions

- The human remains the ultimate owner. Agent delegation never grants commit, push, deploy, credential, production, deletion, or publication authority.
- Message content is untrusted data and must never be shell-evaluated or treated as approval.
- No destructive Git automation exists in v0.1. `SAFE_TO_REMOVE` is a classification, not authorization.
- Raw prompts, messages, final output, tokens, private paths, and secret-like values are hidden or redacted by default in boards and exports.
- Core behavior works without a background daemon.
- Required targets are macOS arm64/amd64, Linux amd64/arm64, and Windows amd64.
- Paths with spaces and CJK, non-`main` default branches, detached HEAD, bare repository detection, linked worktrees, and non-Git directories are covered.
- No commit, push, remote repository creation, package publication, domain purchase, deploy, restart, branch/worktree deletion, reset, or clean occurs without separate explicit human approval. Schema migrations are local state maintenance: every exact compiled plan runs through the backup-bound automatic policy, while unknown, stale, checksum-divergent, backup-failed, or integrity-failed plans fail closed.
- Pygmalion and Zoomzi adapters remain outside the general-purpose core.

## Non-goals for v0.1

- Calling or selecting LLMs.
- Collecting private model reasoning or every tool-call transcript.
- Automatically decomposing tasks or choosing the best agent.
- Replacing terminal GUIs, tmux managers, IDEs, Git hosting, Jira, Linear, or GitHub Issues generally.
- Automatic merge, rebase, push, deploy, production migration, branch/worktree deletion, reset, or clean.
- Treating agent messages as permissions or approvals.
- Multi-host real-time mesh, cloud SaaS, or multi-tenant authorization.
- OS-level write sandboxing when an agent ignores the protocol.

## Acceptance criteria

All criteria below are mandatory. Checkbox state is intentionally left unclaimed; no item represents an invented approval or completed proof.

### Standalone product (`artifact:standalone-omg`)

- [ ] Fresh Git and non-Git directories can initialize and run OMG without Pygmalion or Zoomzi coupling.
- [ ] No required daemon: lineage, task, mailbox, dependency, reservation, handoff, and Git scan work with watch stopped.
- [ ] Two linked worktrees observe the same canonical state.
- [ ] Human root → root agent → child → grandchild lineage is exact; expired, reused, revoked, or wrong-project tokens fail.
- [ ] A 32-process claim race produces exactly one winner.
- [ ] Dependency cycles are rejected; completion unblocks dependents and emits exactly one notification.
- [ ] Boards show prompt-safe summary, previous task, done/doing/next, branch/worktree, blocked-by/blocks, messages, handoffs, conflicts, and Git warnings.
- [ ] Handoff evidence distinguishes `WORK_COMPLETE` from `VERIFIED_DONE`.
- [ ] Reservation exact/glob conflicts, TTL, release, renewal, and override audit pass.
- [ ] Git fixtures detect unregistered worktree, branch-only, dirty-unowned, unpushed, diverged, detached, merged-clean, and unknown assets without deleting anything.
- [ ] Interrupted sessions are not reported done; parent loss still leaves adoptable child handoffs.
- [ ] Default board/export output redacts sensitive content and never exposes `runtime_home` or private native-session references; raw tokens never appear at rest, in logs, errors, events, or evidence.
- [ ] HTML escapes user content, has a restrictive CSP, works without external requests, and is keyboard/screen-reader friendly.
- [ ] Instruction-surface apply is idempotent and remove deletes only the OMG marker.
- [ ] CLI and MCP produce equivalent domain outcomes.
- [ ] Migration failure preserves the original DB; backup/restore and integrity checks pass.
- [ ] Cross-platform CI and release artifact smoke tests pass.
- [ ] README quickstart is replayed from a fresh temp directory.
- [ ] Local `v0.1.0-rc.1` source archive, binaries, checksums, SBOM, provenance metadata, changelog, install-manifest drafts, release notes, and rollback evidence exist.
- [ ] Independent reviews report code/goal `APPROVE`, architecture `CLEAR`, QA `PASSED`, and security/privacy `CLEAR`.
- [x] Source publication is authorized and reports `SOURCE PUBLISHED`; stable binary release publication remains separately gated.

### Pygmalion proof (`artifact:pygmalion-proof`)

- [ ] Pin exact OMG version, checksum, schema version, and release evidence.
- [ ] Begin with read-only repository/rules/Git/task inventory and a durable rollback checkpoint.
- [ ] Preserve unrelated dirty work, historical `TASK_DONE.md`/`WORKLOG.md`, branches, worktrees, and private paths.
- [ ] Dry-run import current active/blocked/planned work; ambiguous records become `imported_unverified`; repeated import is idempotent.
- [ ] Migrate current in-progress work to OMG while keeping historical records accessible and unchanged.
- [ ] Prove a human-direct root, delegated child, second heterogeneous runtime, generic fallback, dependency unblock, conflict handling, handoff, orphan Git detection, and adoption.
- [ ] Generate project-compatible status views and a self-contained operator report without moving Pygmalion-specific behavior into OMG core.
- [ ] Rehearse uninstall/rollback and audit that no production mutation, push, deploy, delete, or unrelated diff occurred.

### Zoomzi proof (`artifact:zoomzi-proof`)

- [ ] Pin the same verified OMG RC used for Pygmalion.
- [ ] Use only standard minimal OMG project configuration plus clearly isolated adapter configuration.
- [ ] A different kind of coding agent discovers the existing owner and work, receives a handoff, and continues without duplicate work.
- [ ] Agents encounter and safely coordinate at least one dependency wait or overlapping-path conflict.
- [ ] After an interrupted session, a new agent discovers and safely adopts remaining Git work with no automatic cleanup.
- [ ] The same core OMG commands, domain meanings, and evidence schema work without Zoomzi-specific core branches.
- [ ] Produce a parity report comparing Pygmalion and Zoomzi outcomes and listing only adapter-level differences.
- [ ] Preserve existing Zoomzi work and rehearse rollback.

## Delivery phases and gates

1. **Discovery and decisions:** upstream source/license comparison, fork-versus-greenfield decision, name clearance, architecture ADRs, threat model, and acceptance matrix.
2. **Foundation:** Go CLI, project detection, configuration, SQLite/migrations, events/receipts, init/doctor/version, JSON envelope, and backup.
3. **Lineage and coordination:** humans, sessions, delegation, tasks, runs, progress, dependency, mailbox, handoff, heartbeat, and adoption.
4. **Reservations and Git reconciliation:** safe path matching, TTL/override, Git parsing, classifications, orphan adoption, and plan-only cleanup.
5. **Integrations and views:** instruction surfaces, wrappers, shell-init, static HTML, watch, MCP, and examples.
6. **OSS readiness and brand:** documentation, policy files, CI, release artifacts, brand assets, independent reviews, and a local RC.
7. **Pygmalion proof:** approved read-only inventory, dry-run migration, active-work import, live adapters, report, and rollback.
8. **Zoomzi proof:** approved minimal setup, portable collaboration/recovery scenario, parity report, and rollback.

Each phase has its own evidence and approval gate. **The standalone RC gate must pass before either Phase 7 or Phase 8 may start.** Phase 7 and Phase 8 do not authorize commit, push, deploy, publication, or destructive cleanup. Fresh and incremental compiled schema plans use the same backup-bound automatic policy; unknown, stale, checksum-divergent, backup-failed, or integrity-failed plans remain blocked.

## Post-v0.1 deferrals

- Remote authenticated multi-host synchronization, cloud backend, IDE extensions, merge queues, and human approval objects are post-v0.1 candidates.
- Public release, package publication, remote repository creation, domains, and external distribution remain human-gated.

## Brand direction

- Replace the former K-shaped concept; the mark must be original to OMG.
- Explore a monochrome-first 24×24 symbol combining an enclosing **O** (shared group), an **M**-like pair of delegated branches, and a returning **G**-like handoff path.
- Distinguish human root and agent nodes by shape, not color alone.
- Avoid robot heads, brains, sparkles, generic hexagons, chat-bubble-only marks, gradients, and Git-logo imitation.
- Required assets: construction-grid SVG, primary/mono mark, wordmark, horizontal/stacked lockups, favicon, 16/32/128/512 PNGs, social preview, light/dark previews, provenance, and font-license documentation.
- The final primary source must use clean vector paths; AI image generation may support mood exploration only.

## Core ontology

| Entity | Role | Essential scope |
|---|---|---|
| OMG | Core product | Name, version, general-purpose scope; coordinates agent sessions across projects. |
| Project | Deployment context | Name, root, configuration, and store; hosts sessions, tasks, messages, and Git assets. |
| Human | Ultimate owner | Identity, display name, and provenance; owns root lineage and approvals. |
| Agent Session | Core domain | Runtime, role, instruction source, OMG parent/root/continuation, heartbeat, current/previous task, and separately modeled native-session identity/access metadata; performs task runs and exchanges messages. Native transcripts remain in their source runtime. |
| Task | Core domain | Display ID, prompt policy, state, dependency, and acceptance; owned and executed by agent sessions. |
| Task Run | Core domain | Attempt, progress, branch/worktree, and outcome; produces a handoff. |
| Message | Supporting domain | Type, sender, recipients, ACK, and related task; coordinates sessions. |
| Handoff | Supporting domain | Summary, final output, files, commits, verification, and risks; transfers task-run results. |
| Reservation | Supporting domain | Path/glob, owner, mode, TTL, and override; warns of overlapping work. |
| Git Asset | Supporting domain | Branch, worktree, dirty/ahead/behind state, owner, and classification; reconciled with project and tasks. |
| Pygmalion | Validation project | Active-work migration and historical records; validates adoption safety. |
| Zoomzi | Validation project | Minimal configuration and portable scenario; validates OMG generality. |

## Product boundary

OMG retains the applicable domain requirements of its advisory technical source material while using the OMG identity and namespace. Detailed technical design is delegated to implementation work, subject to this contract. Before implementation, planning must reconcile the full command and data model with the OMG namespace, current official upstream projects and licenses, current Go/SQLite/MCP versions, and platform constraints.
