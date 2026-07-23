# Post-fix architecture review

- **Reviewer:** PostFixArchitecture-2
- **Reviewed at:** 2026-07-23T03:10:49Z
- **Source state:** `current-workspace-post-fix`
- **Verdict:** **BLOCKED**

## Scope and authority

Reviewed `docs/PRODUCT_SPEC.md`, `docs/ADAPTERS.md`, and the accepted architecture decisions in:

- `docs/adr/0001-language-and-store.md`
- `docs/adr/0002-daemonless-cli.md`
- `docs/adr/0003-canonical-state-and-generated-views.md`

The requested `docs/architecture/*.md` path is absent in the current workspace; `docs/adr/*.md` was treated as the current architecture authority.

The implementation review covered dependency direction, CLI/MCP dispatch parity, sensitive payload transport placement, shared Windows DACL policy, SQLite/migration/restore ownership, daemonless/local-only operation, Git observation scope, and the absence of Pygmalion/Zoomzi branches in generic core.

## Findings

### ARCH-PF-001 — P1 — Require an explicit project on every MCP application request

`internal/transport/mcp/server.go:261-280` (`validRequest`) accepts a request whose `project`, `workspace`, and `store` are all empty. `internal/app/dispatch.go:62` carries those empty values into `foundation.Selection`; `internal/app/foundation/foundation.go:222-227` sends them to the resolver; and `internal/platform/resolver.go:50-58` silently substitutes the MCP server process working directory.

This server can address multiple roots because each tool call accepts `request.project`. A syntactically valid mutating request that omits it can therefore create tasks, messages, handoffs, reservations, or Git observations in the server working directory's project instead of failing closed. That contradicts the explicit-selection requirement in ADR 0002's optional MCP adapter boundary.

**Required fix:** reject MCP application requests without an explicit canonical project selection before dispatch, make that requirement visible in `tools/list`, and add a negative test proving an omitted selector never reaches `Dispatcher`.

### ARCH-PF-002 — P2 — Bind Git inventory scans to the selected project root

`internal/app/dispatch_recovery.go:205-220` passes untrusted `payload.directory` unchanged to `git.Service.Scan` while assigning the resulting snapshot to `resolved.Project`. `internal/app/git/service.go:121-128` invokes the scanner without validating a relationship between that directory or the observed repository identity and the selected project. `internal/platform/git_scanner.go:51-111` consequently observes whichever repository the payload names.

Calling `git inventory` with project A selected and repository B as `directory` persists B's worktrees, branches, and classifications as project A's canonical Git snapshot. Board, cleanup-plan, and adoption queries for A then consume incorrectly scoped facts. This violates ADR 0003's one-resolved-scope invariant and the threat model's explicit filesystem-scope rule.

**Required fix:** derive the scan target from `resolved.ProjectRoot`, or validate the requested directory/returned Git common directory against `resolved.ProjectRoot` and `resolved.GitCommonDir` before persistence. Add cross-repository rejection and linked-worktree acceptance coverage.

## Boundary evidence checked

### Dependency direction and composition

- `cmd/omg/main.go`: `main`
- `internal/bootstrap/foundation.go`: `Foundation`, `Dispatcher`
- `internal/ports/ports.go`: `Store`, `FoundationStore`, `StoreResolver`, `Git`
- `internal/app/foundation/foundation.go`: `WithCurrentStore`
- `internal/store/sqlite/store.go`: `SQLiteStore`, `scopedStore`

Concrete resolver, Git scanner, and SQLite construction remain outside domain/application packages. `internal/domain` and `internal/ports` do not import app, transport, platform, bootstrap, SQLite, watch, or renderer packages.

### CLI/MCP dispatch parity

- `internal/transport/cli/cli.go`: `runApplicationCommand`, `loadBoard`, `runMCP`
- `internal/transport/mcp/server.go`: `allowedCommands`, `handleToolCall`, `validRequest`
- `internal/app/dispatch.go`: `ServiceDispatcher.Dispatch`
- `internal/app/dispatch_lineage.go`: `dispatchLineage`
- `internal/app/dispatch_coordination.go`: `dispatchCoordination`
- `internal/app/dispatch_recovery.go`: `dispatchRecovery`
- `internal/app/dispatch_import.go`: `dispatchImport`

Every advertised MCP command has an explicit consuming dispatch branch, and CLI coordination commands use the same dispatcher. Relevant inspected tests:

- `internal/transport/cli/cli_test.go`: `TestCLIAndMCPFreshRequestsHaveEquivalentCanonicalState`
- `internal/transport/cli/cli_test.go`: `TestBoardAndStaticExportUseCurrentCanonicalStore`
- `internal/transport/mcp/server_test.go`: typed calls, framing, cancellation, and schema cases
- `internal/app/dispatch_query_test.go`: `TestDispatchBoardQueryReturnsCanonicalViewModel`

The shared dispatch is structurally sound, but ARCH-PF-001 blocks the adapter boundary because omitted MCP scope is routed rather than rejected.

### Payload transport placement

- `internal/transport/cli/payload_transport.go`: `loadApplicationPayload`, `readBoundedPayload`
- `internal/transport/cli/payload_transport_unix.go`: `readPrivatePayloadFile`
- `internal/transport/cli/payload_transport_windows.go`: `readPrivatePayloadFile`
- `internal/transport/cli/plan_output_windows.go`: private-parent/DACL validation

Inspected `payload_transport_test.go`, `payload_transport_unix_test.go`, and `payload_transport_windows_test.go`. Inline/stdin/private-file acquisition remains in the CLI adapter; application services receive bounded typed JSON, not transport file handles or file-selection behavior.

### Shared Windows ACL policy

`internal/windowsacl/private_windows.go:IsPrivate` is reused by:

- `internal/store/sqlite/path_security_windows.go:validatePrivateDACL`
- `internal/transport/cli/plan_output_windows.go:validatePrivatePlanDACL`
- `internal/watch/privacy_windows.go:validatePrivateDACL`

Inspected `internal/windowsacl/private_windows_test.go`, SQLite Windows path-security tests, CLI Windows payload tests, and watch Windows tests. The three consumers share one ACE-semantic authorization predicate.

### SQLite and recovery boundaries

- `internal/app/foundation/foundation.go`: `Plan`, `Backup`, `Apply`, `WithCurrentStore`
- `internal/app/foundation/restore.go`: `PlanRestore`
- `internal/store/sqlite/backup_inspection.go`: `InspectBackup`
- `internal/store/sqlite/store.go`: `PlanMigrations`, `CreateMigrationBackup`, `ApplyMigrations`

Inspected:

- `internal/app/foundation/restore_test.go`: both restore-plan safety tests
- `internal/store/sqlite/migration_approval_acceptance_test.go`
- `internal/store/sqlite/store_test.go`: pending-schema and failed-migration/backup cases
- `internal/store/sqlite/configuration_test.go`: FK, busy timeout, and journal policy

Recovery orchestration depends on ports; concrete SQLite inspection/application remains in the store adapter; pending discovery does not auto-apply; restore remains plan-only.

### Daemonless/local-only scope

- `cmd/omg/main.go`: foreground CLI entry point
- `internal/transport/cli/cli.go:runWatch`
- `internal/watch/watch.go:Engine.Run`, `NewSystem`
- `internal/runtime/run.go:Run`

Inspected watch stopped/foreground/process-control tests and argv-preservation tests. Watch is an optional foreground callback loop over canonical queries. Production Go sources contain no network listener/client dependency.

### Generic-core neutrality

- `internal/app/dispatch_import.go`: `dispatchImport`, `ImportRecordPayload`
- `internal/app/importrecord/service.go`: `Service.Apply`
- production sources under `internal/domain` and `internal/app`

No Pygmalion- or Zoomzi-named production branch, type, import, fixture selector, or conditional was found. `import.record` is normalized and project-neutral. Relevant inspected tests are `internal/app/dispatch_import_test.go` and `internal/app/importrecord/service_test.go`.

## Remaining risks

- Windows-only ACL, payload, SQLite path, and watch tests were inspected but not executed on this Darwin host; Windows CI evidence remains required.
- The source tree has no Git commit baseline. `git diff` and `git diff --cached` were empty, and `git log` reported no commits, so this review covers the current workspace rather than a commit-relative patch.
