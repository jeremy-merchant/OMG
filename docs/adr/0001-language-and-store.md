# ADR 0001: Current Stable Go and a CGO-Free SQLite Store

- **Status:** Accepted
- **Date:** 2026-07-22
- **Decision owners:** OMG maintainers

## Context

OMG v0.1 is a local-first, cross-platform coordination ledger. It must run as one primary binary on macOS arm64/amd64, Linux amd64/arm64, and Windows amd64; it must also let linked Git worktrees observe one store while retaining a safe non-Git mode. Core operation must not require a daemon, network service, cloud account, or a C toolchain.

SQLite is the required canonical store. It must tolerate short concurrent CLI operations, preserve referential integrity, support safe schema evolution, and remain recoverable after a failed migration. The store contains potentially sensitive operational material, so it must not be tracked merely because project configuration is tracked.

### Evidence consulted

Observed on 2026-07-22:

- Go publishes its supported release policy and release history at <https://go.dev/doc/devel/release> and <https://go.dev/wiki/Go-Release-Cycle>.
- SQLite documents that WAL depends on shared-memory and does not work over network filesystems: <https://sqlite.org/wal.html>.
- SQLite foreign-key enforcement is per connection: <https://sqlite.org/foreignkeys.html>.
- SQLite documents bounded busy handling at <https://sqlite.org/c3ref/busy_timeout.html> and its online backup API at <https://sqlite.org/backup.html>.

No benchmark or performance threshold is asserted by this ADR.

## Decision

### Language, packaging, and driver

1. Implement OMG in the **current stable Go release line** at the time a release branch is cut. `go.mod`, CI, SBOM, and release provenance MUST record the exact Go toolchain patch version used. Maintainers MUST upgrade to a supported Go line before it exits the Go support window; this ADR does not hard-code a version number.
2. Ship a single statically usable Go CLI per required target. The release build MUST set `CGO_ENABLED=0` and MUST NOT require users to install a compiler, SQLite shared library, or runtime package.
3. Use a pure-Go SQLite driver backed by SQLite semantics (initial choice: `modernc.org/sqlite`). Pin its module version and transitive dependency checksums in `go.sum`. The driver is an implementation choice behind the `Store` port, not a domain dependency. Replacing it requires passing all store conformance checks in this ADR.
4. Build and smoke-test release artifacts for:

   | GOOS | GOARCH |
   | --- | --- |
   | darwin | arm64, amd64 |
   | linux | amd64, arm64 |
   | windows | amd64 |

### Store-location resolution

Store resolution MUST produce an absolute path before opening SQLite. `OMG_STORE_PATH`, when set, is an explicit local operator override; it MUST be absolute after path cleaning and MUST NOT silently be inferred from a relative working directory. Its containing directory is created with owner-only permissions where the platform permits it.

1. **Git project mode (default when inside a non-bare Git worktree):** run
   `git rev-parse --path-format=absolute --git-common-dir` with the project worktree as the Git context. The store path is `<absolute-git-common-dir>/omg/state.db`.
   - The common directory, rather than `.git` as seen from a linked worktree, is the authority. Every linked worktree for one repository therefore resolves the same database.
   - A bare repository is not a worktree project and MUST be rejected for project initialization with a diagnostic that offers explicit workspace mode.
   - The store directory and SQLite sidecars (`state.db-wal`, `state.db-shm`, rollback journals, backups, lock files, PID files) remain untracked. `.omg/` contains only safe, intentionally tracked configuration.
2. **Non-Git project mode:** store state below the platform user-data/config directory returned by Go's `os.UserConfigDir`, under `omg/projects/<stable-project-id>/state.db`. `stable-project-id` MUST be derived from a normalized absolute root and persisted in the database; it is not a security secret. This avoids placing a database in source directories by default.
3. **Explicit workspace mode:** `omg init --workspace <absolute-directory>` creates or joins one named local workspace record. Its database is below the same platform user-data/config root at `omg/workspaces/<workspace-id>/state.db`; enrolled project roots are rows in that database. A project belongs to at most one resolved workspace for a command invocation. Workspace mode is selected explicitly by flag or safe project configuration, never by discovering a nearby directory.
4. Store resolution MUST report its mode and resolved path to diagnostic commands, but normal human views MUST redact private path segments by default. The mode, project root identity, workspace identity, and Git common directory used for resolution are recorded in an append-only audit event.

### SQLite opening and transaction invariants

The storage adapter MUST expose a narrow unit-of-work boundary to application services:

```text
Open(resolvedStore) -> Store
Store.Read(ctx, fn) -> value
Store.Write(ctx, idempotencyKey, fn) -> receipt
Store.Backup(ctx, destination) -> backupMetadata
Store.CheckIntegrity(ctx) -> report
```

`fn` receives a transaction-scoped repository interface; repositories MUST NOT retain a transaction, database handle, or rows after `fn` returns. Domain/application services, not SQL callers in CLI or MCP code, define the operation and receipt. All mutations that form one domain outcome (including its audit event and idempotency receipt) occur in one write transaction.

For every opened connection, before domain SQL:

- enable and verify `PRAGMA foreign_keys = ON`; opening fails if it cannot be verified;
- set a documented, bounded `PRAGMA busy_timeout` (initial value: 5,000 ms); and
- set connection-local settings through the driver rather than assuming process-global state.

Transactions MUST be short: validate and parse input before entering a transaction; do no network, subprocess, long rendering, or interactive work inside one; commit or roll back before returning from the service. Write paths use a deterministic retry policy only for transient `SQLITE_BUSY`/`SQLITE_LOCKED`: a bounded number of attempts, bounded exponential backoff with deterministic jitter derived from the command receipt key, and context cancellation support. Exhaustion returns a stable retryable error; it MUST NOT replay a mutation without its idempotency key.

### Journal mode and filesystem safety

WAL is preferred only for a local filesystem that passes the store adapter's filesystem eligibility check. The check MUST reject known network/removable/virtual filesystem classifications on each supported OS and treat an unknown classification as ineligible. On an eligible filesystem the adapter MUST request `PRAGMA journal_mode=WAL`, read the returned mode, and use WAL only if SQLite reports `wal`. It MUST also verify that the database directory permits creation and locking of the WAL sidecars.

If eligibility, mode negotiation, or sidecar verification fails, the adapter MUST reopen using `PRAGMA journal_mode=DELETE` (rollback journal), record a `journal_mode_fallback` audit event containing the non-secret reason, and continue only if the rollback-journal open succeeds. It MUST NOT force WAL over a network share, nor claim WAL merely because the pragma was requested. A later `doctor --repair-journal-mode` may retry eligibility; it does not change the mode silently during an unrelated command.

### Schema, migrations, and recovery

- The binary embeds ordered, immutable migration SQL files. `schema_migrations(version, checksum, applied_at)` records each successful migration.
- Opening/discovery MUST inspect schema metadata and may report unapplied migrations as `pending`; it MUST NOT apply schema changes implicitly. Read-only `omg version`, `omg doctor`, and status/query commands remain available to report version, integrity, and pending state, subject to the existing fail-closed checks for a newer, checksum-mismatched, or partially applied schema.
- `omg migration plan` is the required non-mutating discovery step before any schema change. It reports the ordered pending versions/checksums, expected backup location, and stable plan identifier. `omg migration apply` is a distinct mutating command and MUST reject unless the operator supplies an explicit human approval PIN bound to that exact plan identifier, project/fixture, from/to schema versions, migration checksums, verified backup checksum/destination, command, and UTC timestamp; a message, handoff, `SAFE_TO_REMOVE`, dry run, or prior approval is never sufficient.
- After `migration plan` and before the approval PIN is accepted, create a timestamped backup using SQLite's online backup API into the store backup directory and verify it with `PRAGMA integrity_check`; the verified checksum/destination become part of that PIN's binding. The apply fails closed if backup creation, backup verification, the approval/plan binding, or migration preconditions fail; it performs no schema change in those cases.
- Apply each migration and its migration record in one transaction where SQLite permits. On failure, roll back the migration transaction, retain the original database and verified backup, and return a non-zero stable migration error. Never delete a backup as part of recovery. Local migration fixtures record the exact plan identifier and the secret-free approval-evidence reference/binding fields; they never store approval secrets.
- `omg backup`, `omg restore --plan`, and `omg doctor` use the same adapter. A restore that mutates state requires a separate explicit human-approved apply operation; this ADR authorizes no automatic restore.

## Consequences

- Releases remain simple to install and cross-compile, at the cost of accepting the pure-Go driver's footprint and keeping it pinned and audited.
- Linked worktrees share state automatically by Git's canonical common-directory identity; separate clones do not accidentally share state.
- Non-Git and workspace modes make state placement explicit and avoid coupling a general-purpose ledger to Git internals.
- SQLite supplies transactional coordination without a server, but write-heavy operations can still receive a bounded busy error and must be designed to retry safely.
- WAL is an optimization subject to a safety gate; rollback journal remains a supported, tested mode.

## Rejected alternatives

1. **CGO-backed SQLite (`mattn/go-sqlite3`) as the required driver:** rejected because it requires a C compiler/toolchain and complicates the promised release targets.
2. **A client/server database or required local daemon:** rejected because OMG must operate offline and daemonlessly.
3. **A database in each linked worktree's `.git` file/directory:** rejected because linked worktrees would split canonical state.
4. **A project-root tracked database under `.omg/`:** rejected because operational state, sidecars, tokens, and private paths must remain untracked.
5. **Always enabling WAL:** rejected because SQLite documents WAL as unsuitable for network filesystems.
6. **Foreign-key declarations without per-connection enforcement:** rejected because SQLite does not enforce them by default on every connection.

## Risks and mitigations

- **Pure-Go driver compatibility or maintenance risk:** isolate the driver behind `Store`; retain SQL conformance and migration fixtures.
- **Filesystem misclassification:** default unknown filesystems to rollback journal; expose the reason through `doctor` and audit it.
- **Lock contention:** keep transactions short, use a bounded timeout/retry policy, and return a stable retryable error instead of hanging.
- **Migration defect or storage corruption:** create verified backups before migration, fail closed on version/checksum anomalies, and expose integrity checks.
- **Path identity surprises (symlinks, case rules, CJK/spaces):** normalize absolute roots without shell interpolation and cover them in cross-platform tests.

## Rollback and revisit triggers

Revisit this decision if any required target cannot build CGO-free; the chosen driver fails SQLite conformance or a security maintenance requirement; Go changes its supported-release policy; or a supported local filesystem cannot safely satisfy either journal mode.

A driver rollback is a source change behind `Store`, followed by migration and store conformance testing. A journal-mode rollback is operational: stop new writers, make and verify a backup, switch through the explicit doctor/repair flow, and retain the pre-change backup. No rollback deletes user data.

## Testable acceptance checks

1. CI cross-compiles the five required GOOS/GOARCH pairs with `CGO_ENABLED=0`, and each artifact runs `omg version` in its target smoke environment.
2. In a repository with two linked worktrees, store resolution from each returns byte-identical absolute `git-common-dir` storage paths; a task created in one is visible in the other.
3. In a non-Git directory, resolution uses the platform user-data/config root, not the source directory; explicit workspace enrollment lets two nominated roots observe one workspace DB.
4. A fixture representing an ineligible or unknown filesystem selects `DELETE`, emits one fallback audit event, and still completes a write/read round trip; an eligible fixture uses WAL only after SQLite reports `wal` and sidecars can be created.
5. Deleting a parent row or inserting an invalid child through every supported connection fails with a foreign-key error.
6. A held write lock produces either a successful bounded retry or the documented retryable busy error before the caller context deadline; no duplicate audit event or mutation is created.
7. A deliberately failing approved migration leaves the original database queryable at its prior schema, retains a verified backup, and reports failure. A newer-schema and a checksum-mismatch fixture both fail closed.
8. A pending-migration fixture lets `version`, `doctor`, and status/query commands report pending state without applying it; only `migration plan` followed by separately approved `migration apply` changes schema, and fixtures retain the plan plus secret-free approval binding (project/fixture, schema/checksums, backup, command, UTC timestamp) as evidence.
