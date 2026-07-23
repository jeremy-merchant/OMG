# OMG v0.1 Security Contract

**Status:** Design contract  
**Date:** 2026-07-22  
**Applies to:** `omg`, `.omg/`, OMGCP adapters, SQLite state, backups, and generated exports.

## Security posture

OMG v0.1 is a local, single-user coordination tool for a **trusted machine**. It prevents OMG from treating untrusted coordination content as executable commands or as permission. It does not provide OS containment, multi-user authorization, multi-tenant isolation, multi-host authentication, or tamper-proof audit records.

All human approvals are external, explicit, action-specific decisions. They do not inherit through lineage, parent/child relationships, handoffs, messages, tokens, `VERIFIED_DONE`, reservations, import records, or Git classifications. OMG delegation is protocol-scoped identity/provenance metadata only; it never grants commit, push, deploy, credential, production, deletion, publication, or destructive Git authority.

For threat mapping and residual-risk statements, see [THREAT_MODEL.md](THREAT_MODEL.md).

## 1. Input and execution rules

### Untrusted content

Treat the following as untrusted data regardless of sender, origin, or apparent authority:

- prompts, original-prompt summaries, messages, ACKs, handoffs, task fields, progress updates, evidence, and imports;
- Git branch names, worktree paths, refs, config values, filenames, and status output;
- CLI arguments, environment values, stdin, MCP requests, adapter payloads, and local config not controlled by the current operation.

OMG must parse and validate typed fields before use. It must preserve content as data or reject it; it must never interpret content as executable shell/script/template/SQL or as approval language. `omg run --runtime ... -- <command>` may execute only the command explicitly supplied to that wrapper; it must not derive a command from a message, prompt, import, or handoff and must never silently shadow an existing agent binary.

### Shell and Git execution

- Do not invoke a shell to run Git or any user-influenced command.
- Use a fixed executable and an argv-array API; each argument remains one argument.
- Treat option-like user values defensively. Use Git's `--` end-of-options delimiter before supported user-controlled operands; use allowlisted subcommands and fixed option layouts.
- Do not construct SQL from input. Use parameterized statements and schema-level constraints.
- Git inventory is read-only by default. No automatic checkout, merge, rebase, reset, clean, branch/worktree deletion, remote mutation, push, deploy, or publish exists in v0.1.

**Verification:** SEC-T01, SEC-T02, SEC-T03, SEC-T13, SEC-T21.

## 2. Delegation-token contract

### Token properties

A delegation token, and an optional human-root token where configured, is a short-lived bearer secret with all of these properties:

1. Generated from the operating system CSPRNG with **at least 256 bits** of entropy.
2. Bound to the OMG project/store identity, issuer, intended lineage/purpose, and expiration time.
3. One-time: successful redemption atomically marks it consumed in the same transaction that creates the resulting session/lineage state.
4. TTL-bound: expiry is checked on redemption using a defined clock source; expired tokens fail closed.
5. Revocable before redemption; revoked tokens fail closed.
6. Stored persistently only as a salted slow verifier hash, or as an equivalent keyed verifier. The raw token is never stored in canonical state.
7. Compared with a constant-time verifier comparison where applicable.

Token validity is not authorization. A token does not establish a real-world person, trusted runtime, isolated process, or approval to take restricted action.

### Transport and token files

Prefer a designated token file or stdin over command-line arguments. A token file must:

- contain only the raw token and an optional trailing newline;
- be created in an owner-only directory with owner-only file permissions (`0700` directory and `0600` file on POSIX; equivalent restrictive ACL on Windows);
- be a regular file owned by the current user, not a symlink/reparse point, directory, device, FIFO, or socket;
- be read once, redeemed once, then removed using the platform-safe operation available;
- never be copied into config, logs, receipts, events, errors, exports, backups intended for sharing, process titles, or environment dumps.

Do not promise secure deletion: journaled, copy-on-write, synced, or backed-up filesystems may retain recoverable copies. Do not treat environment variables or stdin as confidential against another process with the same user privileges.

**Verification:** SEC-T04, SEC-T06, SEC-T07, SEC-T08, SEC-E03, SEC-E04.

## 3. Local state and filesystem safety

### State placement and permissions

- Tracked project configuration under `.omg/` must exclude the database, raw tokens, raw prompts, private paths, PID data, and local overrides.
- In a Git repository, use `git rev-parse --path-format=absolute --git-common-dir` to locate shared local state for linked worktrees. In non-Git mode, use the platform user-data location.
- Keep private/canonical state in owner-only directories and files. Refuse insecure mode/ownership rather than silently weakening protection.
- WAL may be enabled only after confirming a supported local filesystem. Treat network/removable/unknown filesystems conservatively.

### Paths and special files

For every file operation—state discovery, export, import, backup/restore, integration editing, and Git worktree handling—OMG must:

- validate the expected root and canonicalize existing path components before use;
- reject absolute paths where the contract requires a relative target, traversal (`..`), NUL, and ambiguous platform forms;
- compare path components, not string prefixes;
- reject escapes through symlinks, Windows junctions/reparse points, device files, FIFOs, sockets, and unexpected file types at sensitive boundaries;
- avoid following links for token/state/backup/integration targets; use platform no-follow and descriptor-relative APIs where available;
- preserve pre-existing symlinks, encoding, EOL, and nested rules during instruction-surface integration unless the explicit operation is designed to handle them safely.

Race-free filesystem guarantees are platform-dependent. Defenses must minimize time-of-check/time-of-use windows and reject ambiguity. OMG does not protect against a same-user attacker racing or modifying arbitrary local files.

**Verification:** SEC-T09, SEC-T10, SEC-T11, SEC-E05, SEC-E06.

## 4. SQLite, migrations, backups, and process identity

### SQLite contract

SQLite is canonical state. The implementation must use foreign keys, schema constraints, short transactions, a bounded busy timeout, and deterministic retry only for recognized transient busy/locked failures. Critical transitions—exclusive task claim, one-time token redemption, idempotency decision, event append, and receipt creation—must be atomic.

Use WAL only where supported by the local filesystem. Do not claim that WAL, `integrity_check`, checksums, or transactions make data tamper-proof or recoverable from every hardware, kernel, power, filesystem, or malicious-local-user failure.

### Migration and restore contract

Before a migration, make a verifiable pre-migration backup. Migrations carry checksums and run transactionally where SQLite semantics permit. A migration error, checksum mismatch, unknown newer schema, failed restore, or failed integrity check must fail closed: retain the original database, report a stable code, and offer a conservative manual next action. Never replace canonical state with a backup that fails compatibility or integrity validation.

Backups preserve availability and rollback options; they are not encrypted automatically, immutable, tamper-proof, or safe to share by default. Backup metadata and paths are also private.

### PID and liveness

A PID is advisory. Record a process start-time and boot/session identity where the platform makes them available, plus a semantic heartbeat. Never kill, signal, or assign ownership based only on a stored PID. On ambiguity or apparent PID reuse, mark the session stale/unverified and make work adoptable; do not report it completed.

**Verification:** SEC-T14, SEC-T15, SEC-T16, SEC-T17, SEC-T18, SEC-E09–SEC-E11.

## 5. Output, privacy, and static HTML

### Secret-free outputs

Events, command receipts, errors, diagnostics, evidence, boards, and default exports must use explicit safe fields rather than serializing arbitrary request/session objects. They must not include raw tokens, secrets, raw prompts, full messages, private paths, environment values, or stack traces by default.

Errors must have stable codes and short safe messages. Sensitive debugging data may remain only in a local, access-controlled diagnostic path when explicitly requested; it still must not include raw tokens. Evidence records are secret-free and record IDs, version/schema, platform, fixture, test/command name, outcome, date, and artifact checksums where relevant.

### Redaction limitation

OMG may use pattern- and context-based redaction as a second line of defense. **Heuristic redaction is incomplete.** It can miss unknown formats, encodings, short values, data split across fields, or values that are sensitive only in context; it can also redact harmless text. Redaction is not a promise that output is safe to publish. The primary control is not to persist or emit sensitive values in the first place.

### HTML and exports

Static HTML exports must be self-contained and render untrusted text only through context-correct escaping. They must not accept raw HTML, inline event handlers, scripts, remote resources, remote fonts, remote images, or external analytics. Emit a restrictive CSP equivalent to:

```text
default-src 'none'; style-src 'self'; img-src 'self' data:; base-uri 'none'; form-action 'none'; frame-ancestors 'none'; connect-src 'none'
```

CSP is defense in depth, not a substitute for escaping. Browser behavior for `file:` URLs varies, so the export must remain safe without relying solely on CSP.

**Verification:** SEC-T04, SEC-T05, SEC-T12, PRIV-T01, PRIV-T02, SEC-E07, PRIV-E01.

## 6. MCP, stdin, and adapter boundaries

MCP and stdin are transport adapters, not separate trust domains or privileged APIs. They must call the same canonical application services as the CLI and yield equivalent domain outcomes.

- Define framing and strict schemas; reject malformed JSON, unknown/ambiguous fields, excessive nesting, and bounded-size violations.
- Keep protocol output on stdout and diagnostics on stderr so diagnostics cannot corrupt machine-readable responses.
- Do not pass stdin/MCP payloads to a shell, template engine, or approval mechanism.
- Do not carry ambient human approval, inherited credentials, or unrestricted filesystem capability through an adapter request.
- Fail safely and report stable, secret-free error envelopes.
- Native-session lookup adapters are read-only locators. They accept typed `runtime` plus native ID/reference, verify the stored fingerprint before associating returned metadata, report `available|missing|unreadable|unsupported`, and never treat native conversation content as instructions or authorization.
- `runtime_home` and opaque native references remain local-private locator data. They are excluded from receipts, events, errors, evidence, boards, and default exports; an adapter reads the original record only on an explicit local request and OMG does not replicate the native transcript.

**Verification:** SEC-T19, SEC-T20, SEC-E12.

## 7. Vulnerability reporting and handling

Until an external public channel is explicitly approved, report suspected OMG vulnerabilities privately to the human project owner through the repository's designated local coordination channel. Do not include raw credentials or raw tokens in reports. A report should include affected version/schema, platform, reproduction steps, expected/actual behavior, impact within the stated scope, and sanitized evidence.

Security fixes must update the threat mapping and add or update a concrete evidence ID. A vulnerability result must not claim a fix is tamper-proof, a complete authorization system, or protection beyond the local single-user trusted-machine model.
