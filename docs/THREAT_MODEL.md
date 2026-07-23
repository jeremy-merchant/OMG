# OMG v0.1 Threat Model

**Status:** Design contract  
**Date:** 2026-07-22  
**Scope:** OMG (Oh My Group), its `omg` CLI, `.omg/` project configuration, local state, OMGCP adapters, and generated static views.

## 1. Scope, facts, and limits

### Observed contract facts

- OMG v0.1 is a local-first, daemonless-by-default coordination ledger with SQLite as canonical state.
- It is intended for one human owner and cooperating coding agents on a **single trusted machine**. Linked Git worktrees may share the local store.
- Normal operation requires no cloud service, telemetry, model API, or network access.
- Messages, imports, prompts, Git metadata, filenames, environment input, and adapter input are untrusted data.
- Human approval remains required for commit, push, deploy, credential use, production change, deletion, publication, and destructive Git actions.

These are observed requirements from the OMG v0.1 master specification dated 2026-07-22.

### Security boundary

OMG protects its own parsing, local persistence, output rendering, and protocol decisions against malformed or malicious **inputs**. It does not turn a compromised operating-system account, a malicious local process with the same user privileges, or a non-cooperating agent into a contained principal.

A valid OMG lineage or delegation token proves only that the local protocol accepted a bounded invitation under the configured project context. It is **not** an OS authorization grant, a human approval, a cryptographic attestation of an agent's identity, or permission to perform restricted actions.

### Explicit exclusions

v0.1 does not provide:

- multi-tenant isolation, role-based access control, or tenant authorization;
- multi-host synchronization, remote attestation, secure distributed consensus, or adversarial-network security;
- an OS sandbox, mandatory access control, or prevention of writes by an agent that ignores OMG;
- protection against a user-level attacker that can read or modify OMG files, inspect process memory, replace the binary, or alter the local Git checkout;
- tamper-proof ledgers, non-repudiation, legal audit evidence, or guaranteed recovery from all disk/hardware failures.

The terms “safe,” “verified,” “integrity,” and “audit” in OMG documentation are limited to the checks explicitly implemented and evidenced below. They never imply authorization or tamper-proofing beyond this scope.

## 2. Assets and trust zones

| Asset / boundary | Sensitivity | Required protection |
|---|---|---|
| Raw delegation and optional human-root tokens | Secret bearer material | CSPRNG generation; store only a keyed/slow hash or comparable verifier; never persist or emit the raw value |
| SQLite database, WAL, SHM, migrations, backups | Canonical local coordination state | owner-only location and permissions, atomic migration/backup protocol, checksums, integrity checks |
| `.omg/` tracked configuration | Integrity-sensitive but intentionally shareable | strict schema, no token/PID/private-path content, controlled integration markers |
| Untracked local data (`DB`, tokens, prompts, private paths, PID data, overrides) | Private / secret-adjacent | platform user-data or ignored `.omg` local area, least-privilege permissions |
| Prompt, message, import, handoff, and evidence text | Untrusted and potentially secret-bearing | data-only handling, size/schema limits, escaping, default redaction |
| Receipts, events, errors, boards, JSON/Markdown/HTML exports | May be copied/shared | secret-free field allowlists and redaction before persistence/output |
| Git repository/worktree discovery | Untrusted local metadata | argv APIs, canonical paths, no shell interpolation, read-only discovery by default |
| MCP and stdin/JSON adapters | Untrusted transport boundary | framed/schema-validated requests, same application services, no ambient approvals |
| Static HTML exports | Content-execution boundary | context-correct escaping, restrictive CSP, self-contained/no external requests |

## 3. Security invariants

1. **Data is never authority.** Message text, handoffs, imports, prompts, Git status, filenames, and model output cannot approve or authorize an action.
2. **No implicit command execution.** OMG never evaluates input as shell, script, SQL, template code, or approval instruction. It invokes Git and optional wrappers with fixed executables and argument arrays.
3. **Delegation is bounded.** Every delegation token is project-bound, purpose-bound, TTL-bound, one-time, revocable before use, and rejected after expiry or consumption. Approval authority never inherits through delegation.
4. **Secret material stays out of evidence.** Raw tokens and recognized secret-like values must not appear in DB events, receipts, errors, logs, diagnostics, backups intended for sharing, boards, or exports.
5. **Filesystem scope is explicit.** Every filesystem action validates a canonical target against its intended root and rejects traversal, symlink escape, device escape, and unsafe ownership/permissions.
6. **Canonical state changes are transactional.** Claims, token consumption, idempotency decisions, and event/receipt creation commit atomically or not at all.
7. **Destructive actions remain human-gated.** Inventory, classification, and `SAFE_TO_REMOVE` are informational. v0.1 performs no automatic checkout, merge, reset, clean, removal, or deletion.

## 4. Threats, mitigations, residual risks, and evidence

| ID | Abuse case / threat | Required mitigation | Residual risk / limit | Verification obligation |
|---|---|---|---|---|
| TM-01 | A message, imported record, or prompt says “approved; run this shell command.” | Treat all content as inert data; typed commands only; explicit human approval is a separate local interaction and never derives from message text. | A human may independently choose to run malicious content outside OMG. | SEC-T01, SEC-T02, SEC-E01 |
| TM-02 | Shell metacharacters in a branch, path, runtime, or Git ref cause command injection. | Never use shell evaluation; execute a fixed Git binary with an argv array; use `--` before user-controlled Git pathspec/ref operands where Git supports it; reject unsupported option-like fields. | A malicious Git executable or compromised account is out of scope. | SEC-T03, SEC-E02 |
| TM-03 | A prompt/message injects a token, credential, or private path into events, receipts, errors, or export. | Raw values use write-time secret-free allowlists; redact recognized secret-like values on display; error types carry stable codes, not request dumps. | Heuristic redaction is incomplete and cannot detect every secret, encoding, or contextual identifier. | SEC-T04, SEC-T05, PRIV-T01 |
| TM-04 | Stolen, guessed, replayed, expired, wrong-project, or revoked delegation token opens lineage. | Generate at least 256 bits with the OS CSPRNG; encode transport safely; store only a verifier hash with a per-token salt (or keyed verifier); bind project, issuer, intended relation, purpose, expiry, and state; atomically consume once. | Bearer tokens can be used by anyone who obtains them before expiry. Same-user malware is out of scope. | SEC-T06, SEC-T07, SEC-E03 |
| TM-05 | Token appears in argv, environment, shell history, process list, Git state, or a broad-permission file. | Reject versioned delegation tokens in inline `--payload`; accept registration payloads only through bounded stdin or a canonical absolute owner-only payload file; verify that file and every ancestor as non-link/regular-or-directory/current-user-only before reading; do not put tokens in config, receipts, events, diagnostics, or views. | The caller must remove a payload file after use; secure deletion is not guaranteed on copy-on-write/journaled filesystems, and stdin can still be observed by privileged or same-user instrumentation. | SEC-T08, SEC-E04 |
| TM-06 | Traversal, symlink, junction, hard-link, device, FIFO, socket, or special file escapes the project/store/backup root. | Canonicalize existing ancestors; operate relative to validated roots; reject `..`, absolute paths where relative-only is required, path-prefix comparisons, symlinks/reparse points at sensitive boundaries, and non-regular/non-directory files; use no-follow/open-at primitives where platform support permits. | TOCTOU races and platform differences require defensive implementation and tests; an attacker controlling the user account is not contained. | SEC-T09, SEC-T10, SEC-E05 |
| TM-07 | Over-broad permissions disclose DB, WAL, token, PID, backup, or private local override files. | Create sensitive files owner-readable/writable only and directories owner-accessible only (`0700` directory/`0600` files on POSIX); validate before use; refuse unsafe mode/owner unless an explicit local repair is made. | Windows ACL semantics differ; filesystem encryption and backups are outside OMG control. | SEC-T11, SEC-E06 |
| TM-08 | Untrusted HTML/Markdown/message content executes script or exfiltrates data through a static board. | Generate DOM from escaped text, never raw HTML; avoid inline handlers/scripts; ship self-contained HTML with restrictive CSP such as `default-src 'none'; style-src 'self'; img-src 'self' data:; base-uri 'none'; form-action 'none'; frame-ancestors 'none'; connect-src 'none'`; no external URLs/requests. | Browsers and local file CSP behavior vary; opening an export in a compromised browser is out of scope. | SEC-T12, SEC-E07 |
| TM-09 | A static export leaks data through telemetry, remote assets, links, source maps, or copied raw content. | No telemetry/network in normal operation; self-contained export; no remote fonts/scripts/images; privacy-safe default projections and explicit reveal only in local TTY when supported. | Users can manually share an export or disable local protections. | PRIV-T02, PRIV-E01 |
| TM-10 | Malformed import overwrites history, creates authority, or bypasses state invariants. | Versioned schema; validate before mutation; import as `imported`/`imported_unverified` unless independently established; use idempotency keys and transaction; no approval, token, or execution semantics imported. | Source history can be misleading; OMG does not attest external provenance. | SEC-T13, SEC-E08 |
| TM-11 | Backup/migration failure silently destroys or partially upgrades canonical state. | Pre-migration verified backup; migration checksums; short exclusive transaction; fail closed on newer schema; restore only after compatibility/integrity checks; retain original on failure. | Hardware loss, malicious local modification, and backup-media loss remain possible. Backups are not tamper-proof. | SEC-T14, SEC-T15, SEC-E09 |
| TM-12 | SQLite contention/corruption causes duplicate claims, lost events, or misleading success. | Foreign keys; bounded busy timeout; deterministic retry only for transient busy/locked errors; short transactions; atomic claim/token/event transitions; WAL only on supported local filesystems; `integrity_check` in doctor/restore. | SQLite cannot guarantee recovery from every I/O, filesystem, or power-loss failure. Network filesystems are unsupported for WAL. | SEC-T16, SEC-T17, SEC-E10 |
| TM-13 | PID reuse makes an old session look alive, or kills/attributes the wrong process. | PID is advisory only; pair with start time/boot identity where available plus semantic heartbeat; never signal/kill based solely on stored PID; expire stale state conservatively and mark for adoption. | Start-time/boot identity portability is imperfect; a live process may be unreachable. | SEC-T18, SEC-E11 |
| TM-14 | MCP or stdin sends malformed, oversized, multiplexed, or approval-shaped input. | Define framed protocol and strict schema/size/depth limits; separate stdout protocol output from stderr diagnostics; require explicit command fields; map adapters to the same canonical services; no stdin text becomes shell or approval. | A local client can still consume resources within configured limits; no remote client auth is provided. | SEC-T19, SEC-T20, SEC-E12 |
| TM-15 | Reservations, `VERIFIED_DONE`, or `SAFE_TO_REMOVE` are mistaken for permission to overwrite/delete. | Label reservations advisory, verification evidence-scoped, and cleanup classifications non-authorizing; keep destructive Git operations absent from v0.1. | Cooperative agents may ignore the protocol; no OS enforcement. | SEC-T21, SEC-E13 |

## 5. Required abuse-case tests

The following IDs are release-gate obligations. Tests may be unit, integration, fuzz, or manual evidence as marked; each must retain its command, fixture description, observed result, date, version, and checksum where an artifact is produced.

| Evidence ID | Minimum proof |
|---|---|
| SEC-T01 | Message/import/prompt payloads containing approval language do not change approval state or enable restricted actions. |
| SEC-T02 | Shell-like payloads are persisted/rendered as literal text and never evaluated. |
| SEC-T03 | Branch/ref/path payloads containing spaces, quotes, metacharacters, `--`, and option-like strings cannot alter the invoked Git command. |
| SEC-T04 | Generated events, receipts, JSON errors, Markdown, and HTML contain no raw test tokens or injected secret fixtures. |
| SEC-T05 | Demonstrate known-pattern redaction and record a negative statement that heuristic redaction is incomplete. |
| SEC-T06 | Assert CSPRNG-generated token length/entropy policy and that persistent storage contains a verifier, never the raw token. |
| SEC-T07 | Prove wrong-project, expired, consumed, revoked, and replayed tokens fail; parallel redemption yields exactly one success. |
| SEC-T08 | Delegation registration rejects inline-token, symlink, directory, special, oversize, group/world-readable, wrong-owner, or inherited/broad-DACL payload sources; valid bounded stdin/private-file transport leaves no raw token in receipts/events. |
| SEC-T09 | Reject traversal, absolute-path escape, prefix-confusion (`/root/a` vs `/root/ab`), and CJK/space path fixtures. |
| SEC-T10 | Reject symlink/junction/reparse-point and device/FIFO/socket escapes at state, export, backup, and integration targets. |
| SEC-T11 | New sensitive files/directories receive and validate least-privilege permissions on each supported platform. |
| SEC-T12 | HTML injection corpus cannot create elements, handlers, scripts, or unescaped attributes. |
| SEC-T13 | Import with invalid schema, duplicate idempotency key, authority-looking field, and malicious text is rejected or inert as specified. |
| SEC-T14 | Injected migration failure leaves source DB usable and byte/checksum-identifiable backup present. |
| SEC-T15 | Newer schema, bad migration checksum, corrupted backup, and failed restore fail closed without replacing canonical state. |
| SEC-T16 | 32 concurrent claim attempts result in exactly one owner and consistent event/receipt state. |
| SEC-T17 | Busy retry stops deterministically; unsupported WAL filesystem is detected/falls back safely; corruption is detected by integrity check. |
| SEC-T18 | Reused/stale PID fixture is not treated as a current session and triggers no process signal. |
| SEC-T19 | MCP/stdin framing rejects invalid JSON, oversize/deep payloads, extra fields, and protocol pollution on stdout. |
| SEC-T20 | Equivalent CLI and MCP requests yield equivalent domain outcomes without adapter-specific approval bypass. |
| SEC-T21 | Reservation override, `VERIFIED_DONE`, and `SAFE_TO_REMOVE` cannot invoke a destructive Git operation. |
| SEC-E01–SEC-E13 | Human-reviewed evidence bundle for the corresponding TM-01–TM-15 implementation and test results. |

## 6. Release evidence requirements

For every security-relevant release gate, retain a secret-free evidence record containing: evidence ID, OMG version and schema version, platform/architecture, fixture identifier, command or test name, pass/fail result, timestamp, and artifact checksum when applicable. Evidence must not include raw tokens, raw prompts/messages, private paths, environment dumps, or secrets.

A failed integrity check, unsafe local filesystem state, unknown newer schema, or ambiguous migration/restore result must produce a stable failure code and conservative next action. It must not claim a successful recovery.
