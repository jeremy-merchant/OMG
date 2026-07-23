# OMG completed work

## 2026-07-23 — P0-DESIGN-2: Rebuild CLI and HTML board from verified OMP implementation

**Status:** complete

**Completed problem and fix**

The previous TTY board rendered every section as the same framed key/value dump, while the HTML board rendered every domain as the same two-column table. The redesign now uses one OMP-derived operator design system across CLI help, success/error output, preflight, the TTY board, and the self-contained HTML board.

- Grounded semantic colors, glyphs, tree connectors, metadata hierarchy, frame rules, spacing, wrapping, and truncation in the official `can1357/oh-my-pi` source at commit `639bac596d94b5993349f3f6696176cb2bf9b5d3`.
- Added a product masthead, current context, board health, status-first rows, real session/task trees, dependency links, compact empty states, abbreviated primary IDs, dim canonical metadata, and a command-palette help surface.
- Replaced repeated HTML tables with a dark-first operator console containing a sticky section index, Now view, session tree, task timeline, dependency map, message stack, handoff flow, reservation list, Git console, command palette, and disclosure-based secondary metadata.
- Preserved stable JSON, full canonical facts, hostile-input escaping, redaction, hash-authorized CSP, no JavaScript or external resources, light/print themes, responsive behavior, visible focus, reduced motion, and terminal-control neutralization.
- Removed the superseded legacy renderer implementations and added behavior-level regressions for hierarchy, component diversity, ANSI/plain fallback, CJK, long IDs, CSP, escaping, contrast, and non-table HTML.
- Fixed a mobile document overflow found during the 430 px browser pass while preserving intentional horizontal scrolling only for the section index.
- Updated the deterministic hostile-input preview to include complete generated/scope/redaction metadata.

**Changed files**

- `docs/OMP_DESIGN_SYSTEM.md`
- `examples/board-preview/main.go`
- `internal/view/render.go`
- `internal/view/preflight.go`
- `internal/view/presentation.go`
- `internal/view/terminal.go`
- `internal/view/html_operator.go`
- `internal/view/render_test.go`
- `internal/view/operator_redesign_test.go`
- `internal/transport/cli/cli.go`
- `internal/transport/cli/presentation.go`
- `internal/transport/cli/cli_test.go`
- `internal/transport/cli/presentation_redesign_test.go`
- `todo.md`
- `todo_done.md`

**Verification commands and results**

- `go test ./internal/view ./internal/transport/cli -count=1` — passed after each focused renderer/CLI change.
- `go test ./... -count=1` — passed across all 35 packages.
- `go test -json ./... -count=1 | awk ...` — 826 named tests/subtests passed.
- `go vet ./...` — passed with no diagnostics.
- `go build -trimpath -o .tmp/omg-redesign/bin/omg ./cmd/omg` — production binary built successfully.
- `go test -race ./internal/transport/cli -count=3` — passed after the cancellation-test stabilization below.
- `go test -race ./... -count=1` — all packages passed.
- Built CLI smoke — invalid command exited 4 and rendered separate `code`, `cause`, `retryable`, `next`, and `exit` fields.
- Pseudo-TTY smoke — ANSI sequences present in a normal TTY (`35` matching lines), absent in piped output (`0`), `NO_COLOR` TTY (`0`), and `TERM=dumb` TTY (`0`).
- Deterministic static preview — 28,895-byte self-contained HTML; zero `<table>` elements, zero executable `<script>` elements, and eight keyboard-operable native disclosure elements.
- Desktop browser smoke — first viewport showed product identity, current scope, health, active ownership, blockers, warnings, and the start of session lineage.
- 430 px browser smoke — mobile single-column layout rendered the masthead, metadata, handoff, reservation, and Git components; hostile markup remained inert text; the initial document-level horizontal overflow was reproduced, fixed, and absent on the repeat capture.
- Source hygiene — no legacy renderer symbols, debug markers, unfinished TODO/FIXME/HACK markers, or task temporary files remained.

**Commit**

- `PENDING_FIRST_COMMIT` — operator surface redesign and verified regressions.

**Remaining limitations**

- The installed OMP package was not exposed by the DevSpace project boundary, so the pinned official upstream source is the reproducible design reference.
- Direct automated keyboard interaction in Chrome was not executed because JavaScript from Apple Events is disabled on the host. Native `<nav>`, anchors, `<details>/<summary>`, skip navigation, and `:focus-visible` behavior are covered structurally and by renderer tests; browser rendering was inspected manually.
- Native Windows behavior, hosted CI, publication/legal gates, and remote push remain outside this local redesign and are still listed in `todo.md`.

## 2026-07-23 — P1-TEST-1: Remove fixed-delay flakiness from watch cancellation coverage

**Status:** complete

**Completed problem and fix**

The first full race run exposed a nondeterministic integration-test failure: a fixed 100 ms cancellation timer could fire while the foundation service was still opening under race instrumentation, producing `unavailable` instead of the cancellation result the test intended to verify. The timer now allows a bounded two-second startup window and documents that the assertion covers cooperative cancellation rather than startup latency.

**Changed file**

- `internal/transport/cli/cli_test.go`

**Verification commands and results**

- Initial `go test -race ./... -count=1` — failed once in the watch cancellation integration path with `foundation service is unavailable`.
- `go test -race ./internal/transport/cli -count=2` before the fix — passed, confirming nondeterministic timing dependence.
- `go test -race ./internal/transport/cli -count=3` after the fix — passed.
- Final `go test -race ./... -count=1` — all packages passed.

**Commit**

- `PENDING_FIRST_COMMIT` — test stabilization included with the verified operator redesign.

**Remaining limitations**

- This change removes the instrumentation-sensitive 100 ms assumption; it does not introduce a startup performance guarantee.
