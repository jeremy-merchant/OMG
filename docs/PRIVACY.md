# OMG v0.1 Privacy Contract

**Status:** Design contract  
**Date:** 2026-07-22  
**Scope:** Local OMG coordination data, CLI/MCP/stdin adapters, diagnostics, backups, and TTY/JSON/Markdown/HTML views.

## 1. Plain-language commitment

OMG v0.1 is designed to operate locally. Normal operation has **no required network access, cloud service, telemetry, analytics, model API, account, remote synchronization, or automatic upload**. OMG does not intentionally collect model reasoning or every tool-call transcript.

This is a product-behavior contract, not a guarantee that the operating system, terminal, IDE, Git client, browser, shell history, backup program, endpoint security software, or a separately configured adapter does not collect data. Those systems are outside OMG's control.

OMG is for a local single-user/trusted-machine environment. It does not offer multi-tenant privacy isolation, multi-host confidentiality, end-to-end encrypted synchronization, or protection from other processes running with the same user privileges.

## 2. Data categories and handling

| Category | Examples | Default handling | Privacy rule |
|---|---|---|---|
| Coordination metadata | task IDs/state, OMG lineage links, native-session ID/fingerprint/access state, dependency edges, reservation metadata, timestamps, semantic progress summaries | Canonical local SQLite state | Retain only fields required for coordination; expose privacy-safe summaries by default |
| Potentially sensitive content | prompts, messages, handoffs, final output, evidence text, import records | Local only when the feature requires it | Treat as untrusted and potentially secret-bearing; hide/redact in boards and exports by default |
| Secret bearer material | delegation tokens, optional human-root tokens | Transport-only and hashed verifier at rest | Never persist raw token; never emit in logs, events, receipts, errors, evidence, boards, or exports |
| Private local metadata | absolute private paths, native `runtime_home` and opaque session references, PID/start identity, local overrides, diagnostic context | Local DB or other untracked local state only where required | Do not put in tracked `.omg/` configuration, receipts/events/evidence, or default shared output |
| Git inventory metadata | worktree/branch/ref/dirtiness/ahead-behind classifications | Local inventory | Read-only by default; redact private paths in default views; classification is not authorization |
| Technical records | migrations, checksums, integrity results, safe error codes, version/schema | Local DB/backup/evidence | Use secret-free allowlists; retain enough for troubleshooting and release evidence |

Tracked `.omg/` configuration must not contain the SQLite DB, raw tokens, raw prompts, private paths, PID data, or local overrides. In Git repositories, shared state for linked worktrees is found through Git's common directory; this makes the store available to local linked worktrees and should be treated as local shared-machine data.

## 3. Data minimization and default views

OMG must prefer structured, minimal coordination state over full transcripts. `done / doing / next`, task state, dependency state, native-session locator metadata, and bounded summaries are preferred to copying raw native conversations or prompt/message history.

Boards, static exports, receipts, events, errors, and evidence must be built from explicit safe field sets. They must not serialize arbitrary request or session objects. Default output must hide or redact raw prompts, message bodies, final output, tokens, `runtime_home`, opaque native-session references, other private paths, and secret-like values.

An operator who elects to reveal or copy local content remains responsible for where it goes. A local “show raw” affordance, if implemented, must be clearly labeled, deliberate, limited to the local interactive surface, and must not change the safe defaults for saved exports or evidence.

## 4. Redaction: important limitation

OMG may pattern-match likely tokens, credentials, private paths, and secret-like strings before display. **Heuristic redaction is incomplete.** It can fail for new formats, encoded values, fragmented values, short values, values embedded in prose, or data that is sensitive only because of context. It can also over-redact benign content.

Therefore:

- Redaction is a fallback, not the primary privacy control.
- The primary control is to avoid accepting, persisting, and emitting sensitive data where it is not required.
- Redacted output must not be described as safe for public sharing, sanitized with certainty, comprehensive, or tamper-proof.
- Users must review any export before sharing it outside the trusted machine.

**Verification:** PRIV-T01 and SEC-T04–SEC-T05.

## 5. No telemetry and network behavior

Normal OMG operations must make no network request and must not require one to complete. This includes initialization, local state operations, SQLite migrations, task/message/handoff workflows, Git inventory, backup/restore, doctor, CLI JSON, MCP, and static exports.

OMG must not embed analytics, tracking pixels, remote images, remote fonts, remote JavaScript, automatic update checks, crash uploads, or remote error reporting in v0.1. Optional integrations must be explicit, locally configured, and documented with their data boundary; they do not change the default no-telemetry/no-network behavior.

Static HTML exports must be self-contained and have no external requests. Their CSP must prohibit connections and default external loads; see `SECURITY.md` for the required baseline.

**Verification:** PRIV-T02, PRIV-E01.

## 6. Exports, receipts, events, errors, and evidence

### Export contract

TTY, JSON, Markdown, and self-contained HTML views are projections of canonical state, not a wholesale database dump. Default projections must redact/hide sensitive categories and escape untrusted content appropriately for each format.

HTML must use context-correct escaping and a restrictive CSP. It must never render message/prompt/import content as active HTML. Markdown/text must avoid unsafe terminal control sequences where rendering can be influenced by content. JSON must use a stable schema and must not include arbitrary debug fields.

### Receipts, events, and errors

- Append-only events and command receipts support local observability; they are not immutable, independently witnessed, non-repudiable, or tamper-proof.
- Receipts/events/errors/evidence must be secret-free by construction through explicit allowlists and stable identifiers.
- Error responses must not include raw tokens, full prompt/message bodies, environment dumps, SQL, filesystem-private paths, stack traces, or request-object serialization.
- A secret-free evidence bundle records evidence ID, date, OMG/version/schema, platform, fixture/test/command identifier, outcome, and non-secret artifact checksums where relevant.

**Verification:** PRIV-T01, SEC-T04, SEC-T12, SEC-T19, PRIV-E01.

## 7. Backups, migrations, and retention

Migration and backup data can contain coordination state and must be treated as private local data. Before schema migration, OMG creates a verifiable backup; migrations use checksums, and restores require compatibility and integrity checks. A failure must retain the original canonical DB and fail closed.

A backup helps recovery but is not encrypted automatically, immutable, tamper-proof, protected from same-user access, or safe to share. The user controls backup destination, OS backup inclusion, retention, and deletion. Secure deletion cannot be guaranteed on modern filesystems or external backup systems.

OMG should retain only operationally required canonical state and backups according to documented local policy. The v0.1 contract does not promise automatic retention expiry, cryptographic erasure, legal hold management, or compliance certification.

**Verification:** SEC-T14, SEC-T15, SEC-T17, SEC-E09.

## 8. Local process and transport boundaries

PIDs and heartbeats can reveal activity metadata and are private local data. A PID is advisory and can be reused; OMG must pair it with available start/boot identity and semantic heartbeat, must not kill/signal a process based on PID alone, and must conservatively mark ambiguity for adoption.

MCP and stdin inputs may contain sensitive data and are untrusted transport. OMG must validate framing/schema/size, keep protocol stdout distinct from diagnostics, and use the same canonical services as CLI. No adapter inherits human approval, and no request is allowed to turn input text into shell execution or privileged authorization.

Token transport rejects versioned delegation tokens in inline CLI arguments. Registration accepts only bounded stdin or a canonical absolute owner-only payload file validated before reading. Raw tokens are never stored at rest and never appear in process-facing outputs; callers remain responsible for removing payload files after use. Owner-only permissions reduce accidental disclosure but do not protect against privileged or same-user attackers.

Native conversation history stays in its originating runtime. OMG stores only the typed locator and integrity metadata needed to find that session and resolves it through a runtime-specific read adapter only when the operator explicitly requests access. OMG continuation lineage (`continuation_of_session_id`) and native application lineage (`native_session_id` plus optional `native_parent_session_id`) are independent facts. Lookup failure updates or reports the explicit access state; it must not silently bind a different session, copy the transcript into canonical OMG state, or expose its private location.

**Verification:** SEC-T06–SEC-T08, SEC-T18–SEC-T20.

## 9. Privacy abuse cases and evidence

| ID | Abuse case | Required outcome |
|---|---|---|
| PRIV-T01 | Put raw test tokens, credentials, private paths, prompt/message fixtures, and error-triggering input through state, receipt, error, JSON, Markdown, HTML, backup evidence, and doctor flows. | Default output/evidence contains no raw token or prohibited sensitive fixture; expected safe identifiers/redaction are observed; the evidence explicitly records redaction limits. |
| PRIV-T02 | Capture normal CLI, MCP, export, migration, backup, and doctor execution in a network-disabled/observed test environment; inspect generated HTML. | No OMG-originated network request, analytics artifact, remote resource, or telemetry record occurs; HTML is self-contained and CSP blocks connections. |
| PRIV-T03 | Export text containing HTML, Markdown, terminal-control, URL, and Unicode/CJK payloads. | HTML/text/JSON render as inert escaped data; no executable markup, external fetch, or unsafe private-path disclosure occurs by default. |
| PRIV-T04 | Attempt raw token delivery through inline argv/environment, unsafe/oversize payload files, and bounded private-file/stdin channels; inspect DB/WAL/SHM/events/receipts/errors/exports. | Inline-token and unsafe-file delivery reject before dispatch; raw token appears only in the bounded accepted transport channel; persistent state holds a verifier only; emitted artifacts remain token-free. |
| PRIV-T05 | Restore from a backup containing sensitive coordination records and attempt a failed migration/restore. | Original state remains available on failure; backup/private metadata is not emitted in public-facing errors/evidence. |
| PRIV-T06 | Register available, missing, unreadable, and unsupported native sessions with private runtime homes/references; attempt a fingerprint mismatch and inspect all default views, receipts, events, errors, and exports. | Valid linkage round-trips locally; mismatched linkage fails closed; OMG and native parent/continuation fields remain distinct; private locators are absent from default outputs; no native transcript is copied. |

| Evidence ID | Manual/release evidence requirement |
|---|---|
| PRIV-E01 | Secret-free capture of network-observation method, OMG/version/schema, platform, test command/fixture, result, export checksum, and CSP/resource inspection. |
| PRIV-E02 | Secret-free privacy output review showing all default views and evidence fields used by the release gate. |
| PRIV-E03 | Backup/migration privacy review showing only sanitized identifiers/checksums and a passed failure-preservation scenario. |

## 10. Out-of-scope privacy guarantees

OMG v0.1 does not promise confidentiality against another user/process with equivalent or greater local privileges; browser/terminal/IDE plugins; OS telemetry; malware; unencrypted disks; filesystem snapshots; external backups; Git hooks/remotes configured by the user; or manual sharing by an operator.

It also does not provide multi-tenant privacy, cross-machine encrypted synchronization, anonymous use, legal compliance guarantees, data-residency guarantees, a data-processing agreement, or a right-to-erasure workflow. These exclusions are deliberate scope boundaries, not statements that the risks are absent.
