# Changelog

## v0.1.0-rc.1 — 2026-07-22

**STATUS: NOT PUBLISHED**

### Added

- Standalone local-first coordination ledger and CGO-free cross-platform CLI.
- Human/direct, delegated, resumed, adopted, and imported lineage with bounded native-session metadata.
- Atomic tasks/runs, append-only progress, dependencies, typed mailbox/ACK, handoffs, and path reservations.
- Read-only Git classification, orphan/adoption and cleanup planning, and linked-worktree shared-state resolution.
- TTY, JSON, Markdown, and CSP-constrained HTML boards; shell integration; generic runtime wrapper; optional watch; MCP stdio parity.
- Transactional SQLite migrations, exact checksum verification, backup binding, doctor/integrity checks, and stable error envelopes.
- Security/privacy contracts, redacted defaults, CI/security workflows, OSS policy files, operator documentation, integration examples, and local brand candidate.
- Five target binaries, deterministic source archive, SHA-256 manifest, SPDX SBOM, third-party notices, SLSA provenance, install-manifest draft, release notes, and rollback instructions.
- Public CLI and MCP command families now share one typed application dispatcher for human/session/delegation, task/run/progress/dependency, message/handoff/checkpoint, reservation, Git recovery, import, board, migration, and release flows.
- Migration histories reject missing versions; plan, verified backup, expiring single-use approval, schema transition, audit event, and command receipt are applied atomically.
- Managed SQLite state, sidecars, backups, and approval artifacts reject symlink/reparse/special-file paths and enforce owner-only POSIX modes or a current-user-only Windows DACL.
- Delegation registration now rejects raw tokens in inline argv payloads and accepts bounded stdin or canonical owner-only payload files, with no-follow POSIX reads and semantic current-user-only Windows DACL validation.
- Token redemption validates bindings and consumes the verifier in one SQLite transaction; restore planning now has explicit no-mutation failure coverage; SQLite WAL, foreign-key, busy-timeout, bounded-retry, and newer-schema policies have direct tests.
- Command receipts bind idempotency keys to each public operation; failed transactions do not leave replayable success receipts.
- Scoped project ownership is enforced for humans, sessions, tasks, runs, mailbox state, reservations, and Git reconciliation.
- Interrupted sessions remain terminal, parent-loss heartbeats are bound to the run owner, and non-live sessions cannot report completion.
- Sensitive values split across structured write fields are rejected as one stream; role recipients and canonical board outputs are redacted at presentation boundaries.
- Existing-only SQLite opens use non-creating read/write mode and validate pre-existing POSIX modes or Windows DACLs without rewriting them.
- Backup restore accepts exactly one bounded payload transport and validates checksums and fresh destinations without mutating canonical state.

### Safety boundaries

- No commit, push, deploy, registry upload, public release, remote creation, domain purchase, destructive Git cleanup, automatic restore, or unapproved migration apply.
- Publication remains `NOT PUBLISHED`.
- Working name `OMG` is retained only for this private local candidate; public name and trademark gates remain unresolved.

### Compatibility

- Schema version: 1 at startup; pending migrations are reported without implicit application.
- Command schema version: 1.
- Platforms built: darwin/arm64, darwin/amd64, linux/arm64, linux/amd64, windows/amd64.
