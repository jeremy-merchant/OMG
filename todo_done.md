# OMG completed work

## 2026-07-25 — P0-OSS: Publish canonical open-source source identity

**Status:** complete

- Authorized the canonical public repository and Go module identity as `github.com/jeremy-merchant/OMG`.
- Confirmed Apache License 2.0 and updated README, contribution, governance, support, security, conduct, naming, brand, command, and release-status contracts.
- Changed `omg release status --json` to report `SOURCE PUBLISHED`, `Apache-2.0`, the canonical repository, and `stable_release: false`.
- Removed generated acceptance evidence and obsolete local RC binaries from the public source tree; future evidence and artifacts are local or CI outputs and must be regenerated from a pinned public revision.
- Created a verified ignored local Git bundle before replacing the private history with a sanitized public baseline.
- Verified 1,008 named tests across 37 packages, full `-race`, `go vet`, five CGO-free target builds, workflow/SVG/document parsing, and the public release-status payload.
- `govulncheck` reported zero reachable vulnerabilities; Git history scanning reported no credential leaks before the public-history replacement.

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

- `34ac0f1` — operator surface redesign and verified regressions.

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

- `34ac0f1` — test stabilization included with the verified operator redesign.

**Remaining limitations**

- This change removes the instrumentation-sensitive 100 ms assumption; it does not introduce a startup performance guarantee.

## 2026-07-24 — P0-CLI-3: Add width-aware command discovery and actionable recovery

**Status:** complete

**Completed problem and fix**

The CLI still assumed a wide terminal even after the first visual redesign. Baseline global help exceeded a 40-column terminal on 27 lines, a 60-column terminal on 21 lines, and an 80-column terminal on 14 lines; `omg task --help` repeated the global palette; typo recovery recommended unrelated commands; success values fell back to Go-style dumps; and the board/preflight renderers retained fixed-width operational rows.

- Added one typed command-help catalog covering command groups, summaries, usages, subcommands, read/write semantics, applicable options, examples, PowerShell shell surfaces, and receipt queries.
- Added focused `omg <command> --help`, `omg <command> <subcommand> --help`, and `omg help <command> <subcommand>` navigation without changing stable JSON envelope schemas.
- Added closest-command and closest-subcommand recovery, required-subcommand guidance, and validation errors that point only to valid help targets.
- Replaced raw Go-value success output and one-line runtime summaries with deterministic labeled facts, stable map ordering, compact collection rendering, and width-aware CJK/long-token wrapping.
- Added actual Unix/Windows terminal-width detection plus `COLUMNS`, `NO_COLOR`, `TERM=dumb`, and non-TTY fallbacks.
- Applied the same width, glyph, color, connector, and wrapping system to board and preflight TTY output, including long canonical metadata, progress lanes, actions, and snapshot facts.
- Preserved security fail-closed behavior for unsafe store paths while surfacing safe, path-free causes and exact owner-only recovery guidance instead of the generic `foundation service is unavailable` message.
- Added a production-composition regression proving fresh initialization succeeds under an owner-only temporary project/store root; the host's real home path remains correctly rejected because another local account has ACL write/delete authority over an ancestor.

**Changed files**

- `docs/COMMAND_REFERENCE.md`
- `internal/app/foundation/foundation.go`
- `internal/app/foundation/foundation_test.go`
- `internal/bootstrap/production_foundation_test.go`
- `internal/transport/cli/cli.go`
- `internal/transport/cli/command_help.go`
- `internal/transport/cli/command_help_test.go`
- `internal/transport/cli/presentation.go`
- `internal/transport/cli/presentation_redesign_test.go`
- `internal/transport/cli/public_preflight.go`
- `internal/transport/cli/public_restore.go`
- `internal/transport/cli/terminal_layout.go`
- `internal/transport/cli/terminal_width_unix.go`
- `internal/transport/cli/terminal_width_windows.go`
- `internal/view/preflight.go`
- `internal/view/render.go`
- `internal/view/terminal.go`
- `internal/view/terminal_layout.go`
- `internal/view/terminal_width_test.go`
- `internal/view/terminal_width_unix.go`
- `internal/view/terminal_width_windows.go`
- `todo.md`
- `todo_done.md`

**Verification commands and results**

- Focused `go test ./internal/view ./internal/transport/cli -count=1` — passed after each help, presentation, and TTY-width batch.
- `go test ./internal/app/foundation ./internal/bootstrap ./internal/transport/cli -count=1` — passed with safe state-path error mapping and production fresh-init coverage.
- `go test ./... -count=1` — all 35 packages passed.
- `go test -json ./... -count=1` — 895 named tests/subtests passed.
- `go vet ./...` — passed with no diagnostics.
- `go build -trimpath -o .tmp/cli-polish/omg-final ./cmd/omg` — production binary built successfully.
- `GOOS=windows GOARCH=amd64 go test -c ./internal/transport/cli` and `./internal/view` — Windows test binaries cross-compiled successfully.
- `go test -race ./... -count=1` — all packages passed.
- Real PTY help smoke — ANSI styling detected at 40 columns (`66` sequences) and 60 columns (`58`); `NO_COLOR` produced zero ANSI sequences.
- Real production flow — owner-only OS temporary root completed `init` with eight explicit pending migrations, then rendered a 40-column ANSI preflight with migration warning, compact empty states, and no cell-width overflow.
- Unsafe host path smoke — retained exit 4 and fail-closed rejection, but now reports that an ancestor grants another account write access, explains the owner-only requirement, and points to `omg init --help`.

**Commit**

- `94d707d` — width-aware discovery, operational TTY consistency, structured results, and actionable secure-path recovery.

**Remaining limitations**

- Direct state paths beneath the current host home remain correctly rejected because its ACL grants another local account child creation/deletion rights. The later P0-STATE-1 fix preserves that boundary while moving the default database to the operating system's private per-user state root.
- Native Windows execution and hosted CI remain pending even though the new Windows width code and affected packages cross-compile successfully.
- Publication, remote push, and the repository-wide tracked-source baseline remain outside this task and stay in `todo.md`.

## 2026-07-24 — P0-UI-4: Rebuild CLI workflows and the self-contained operator board around glanceable action

**Status:** complete

**Completed problem and fix**

The previous redesign was clean and safe but still presented the HTML board as a generic masthead followed by similarly weighted sections, while global CLI help remained command-name-first. This pass grounded the interface in current official Material 3 Expressive, Apple interface-material, WCAG 2.2, browser-platform guidance, modern terminal patterns, and a source-level OMP audit, then rebuilt the surfaces around priority, context, and bounded next actions without copying OMP product styling.

- Replaced the generic HTML opening with state-driven masthead copy, six glanceable semantic signal cards, explicit attention/safe-command actions, an attention queue, counted section navigation, ordered section kickers, and state-accented task rows.
- Limited translucency to the desktop/mobile navigation layer; operational content remains on solid high-legibility surfaces, and `prefers-reduced-transparency` removes the effect entirely.
- Added a compact sticky 430-pixel mobile control rail, container-aware masthead/signal layouts, 44-pixel interaction targets, stable fragment navigation, reduced-motion, increased-contrast, forced-colors, dark/light, print, and explicit keyboard-focus behavior.
- Added semantic landmarks, resolvable `aria-labelledby`/`aria-describedby` relationships, skip-link focus above sticky controls, `dir=auto` for mixed RTL/CJK user content, unique ID/fragment validation, and no unnecessary live regions in the static snapshot.
- Added private browser chrome: safely escaped project/health titles, dark/light theme colors, no-referrer, noindex/nofollow/noarchive, and disabled telephone detection.
- Rebuilt global CLI help around four operator workflows, concise command-family summaries, group counts, bounded related paths, adaptive narrow command grids, and hanging-indent examples. The first workflow draft's 40-column output dropped from 137 to 74 lines after the adaptive density pass.
- Made long CLI/TTY tokens prefer semantic `=`, slash, underscore, and hyphen boundaries, and removed duplicated narrow-board facts while retaining full canonical metadata.
- Replaced duplicated shell completion scripts with one typed contextual vocabulary across Bash, Zsh, Fish, and PowerShell; fixed omitted `help`, `receipt`, payload/stdin/stdio/status words, removed the unsupported `--plan`, and added bounded preflight/board/checkpoint shell helpers.
- Expanded `examples/board-preview` into an owner-only, no-overwrite, hostile-input QA tool for HTML, TTY, Markdown, JSON, and explicit stdout mode; corrected its safe suggested command.
- Added [`docs/INTERFACE_RESEARCH_2026.md`](docs/INTERFACE_RESEARCH_2026.md) with official research, OMP source areas, adopted decisions, rejected trends, and measured browser evidence. Updated README, adapter, command, implementation, ADR, and design-system documentation to match the actual interface contract.

**Changed files**

- `README.md`
- `docs/ADAPTERS.md`
- `docs/COMMAND_REFERENCE.md`
- `docs/IMPLEMENTATION_PLAN.md`
- `.gitignore`
- `docs/INTERFACE_RESEARCH_2026.md`
- `docs/OMP_DESIGN_SYSTEM.md`
- `docs/adr/0001-language-and-store.md`
- `examples/board-preview/main.go`
- `examples/board-preview/main_test.go`
- `internal/shell/shell.go`
- `internal/shell/shell_test.go`
- `internal/transport/cli/cli_test.go`
- `internal/transport/cli/command_help.go`
- `internal/transport/cli/command_help_test.go`
- `internal/transport/cli/presentation_redesign_test.go`
- `internal/transport/cli/terminal_layout.go`
- `internal/view/html_operator.go`
- `internal/view/html_operator_styles_v2.go`
- `internal/view/operator_redesign_test.go`
- `internal/view/render_test.go`
- `internal/view/terminal.go`
- `internal/view/terminal_layout.go`
- `internal/view/terminal_width_test.go`
- `todo.md`
- `todo_done.md`

**Verification commands and results**

- Focused renderer, CLI, shell, and preview tests passed throughout each inspect/edit/verify batch.
- `go test ./... -count=1` — all 35 packages passed.
- `go test -json ./... -count=1` — 924 named tests/subtests passed.
- `go test -race ./... -count=1` — all packages passed.
- `go vet ./...` — passed with no diagnostics.
- Production `omg` and `board-preview` binaries built successfully.
- Windows amd64 CLI, view, shell, and preview test binaries cross-compiled successfully.
- Real Bash completion execution — top-level, task, migration, and shell-init prefixes returned only contextual matches.
- Real pseudo-TTY hostile preview — 40-column max width 40 with 169 lines and 170 ANSI sequences; 80-column max width 80 with 86 lines and 148 ANSI sequences; `NO_COLOR` emitted zero ANSI; CJK, hostile text, canonical facts, and the safe action remained present.
- System Chrome desktop light/dark, 430×900 mobile, reduced-motion, and forced-colors audits — document overflow 0 pixels, minimum measured target 44 pixels, six signal cards, stable `#work` anchors, visible unobscured keyboard focus, working skip link, zero scripts, zero external links/requests, zero console errors, and no hostile markup execution.
- Palette regressions — muted/secondary and success/warning/danger/info/blocked combinations meet at least 4.5:1 in dark and light themes; status badges retain glyph and text labels instead of relying on color alone.
- `git diff --check` — passed; stale user-facing `--plan`/restore examples were corrected.

**Commit**

- No commit was created in this task because the user requested implementation and verification but did not explicitly request a Git commit.

**Remaining limitations**

- The HTML board remains a deterministic script-free snapshot by design; it does not become an SPA, poll canonical state, or use the View Transition API.
- Native Windows execution and hosted CI remain pending; the affected Windows packages cross-compile successfully but were not executed on Windows in this session.
- Publication, push, deployment, legal review, and the criterion-specific acceptance matrix remain outside this interface task and stay in `todo.md`.

## 2026-07-24 — P0-UI-5: Apply current developer-tool interaction patterns to discovery, actions, Unicode, and terminal themes

**Status:** complete

**Completed problem and fix**

A second research pass reviewed current, frequently discussed official/open-source developer-tool sources including shadcn/ui, GitHub Primer, OpenTUI, Bubble Tea v2, Crush, CLI Guidelines, Fish completion, Gemini CLI, Atuin, Lazygit, and current OMP releases. Only patterns compatible with OMG's deterministic, script-free, security-bounded product contract were applied.

- Added description-rich shell completion: Zsh, Fish, and PowerShell surface concise candidate descriptions, while Bash preserves simple value completion. Top-level descriptions are contract-tested against CLI help.
- Added nested command-tree completion. A selected leaf subcommand removes sibling values and exposes options only; `help task` traverses into the task family. Real Bash behavior verifies sibling, leaf, and nested-help paths.
- Added typed option completion across Bash, Zsh, Fish, and PowerShell: directories for `--project`/`--workspace`, files for private input/output options, and only `tty|markdown|html|json` for `--format`.
- Made empty invocation a successful, non-dispatching 25-line discovery surface. Explicit global help remains complete on tall terminals, short TTYs receive height-aware global progressive disclosure, and contextual command help is never truncated.
- Made bare namespace commands such as `omg task`, `omg board`, and `omg migration` open contextual help without dispatch; additional intent still uses strict validation.
- Replaced duplicated rune-sum width logic with shared UAX #29 grapheme segmentation through `github.com/rivo/uniseg` v0.4.7. Combining marks, ZWJ emoji, flags, and keycaps remain intact. Hangul Compatibility Jamo uses terminal identity: Ghostty wide, other macOS terminals narrow, otherwise Unicode.
- Added shared semantic ANSI palettes. `OMG_COLOR_SCHEME=light|dark` is explicit, conventional `COLORFGBG` white-background values are a conservative fallback, and `NO_COLOR`/`TERM=dumb` remain authoritative. CLI, board, and preflight share the same roles.
- Grouped safe actions by Inspect/Recover/Coordinate/Other, added bounded descriptions, and made HTML commands inert, focusable, whole-command-selectable `<pre><code>` targets with review guidance. TTY presents purpose before each command and tells the operator to review before execution.
- Added target-aware rail navigation through progressive `:has()`, low-height density without shrinking 44-pixel targets, mobile single-column command groups, coarse-pointer hover suppression, and forced-colors coverage.
- Deliberately rejected dynamic OSC/window titles, terminal background queries, scripted copy buttons, interactive command execution, and full-screen TUI frameworks because they would weaken pipe/log/protocol safety or the static export contract.

**Changed files**

- `go.mod`
- `go.sum`
- `README.md`
- `docs/ADAPTERS.md`
- `docs/COMMAND_REFERENCE.md`
- `docs/INTERFACE_RESEARCH_2026.md`
- `docs/OMP_DESIGN_SYSTEM.md`
- `examples/board-preview/main.go`
- `examples/board-preview/main_test.go`
- `internal/shell/shell.go`
- `internal/shell/shell_test.go`
- `internal/shell/nested_completion_test.go`
- `internal/shell/typed_completion_test.go`
- `internal/terminalstyle/palette.go`
- `internal/terminalstyle/palette_test.go`
- `internal/terminaltext/width.go`
- `internal/terminaltext/width_test.go`
- `internal/transport/cli/cli.go`
- `internal/transport/cli/cli_test.go`
- `internal/transport/cli/command_help_test.go`
- `internal/transport/cli/height_help_test.go`
- `internal/transport/cli/presentation.go`
- `internal/transport/cli/terminal_layout.go`
- `internal/transport/cli/terminal_width_unix.go`
- `internal/transport/cli/terminal_width_windows.go`
- `internal/transport/cli/theme_palette_test.go`
- `internal/view/html_operator.go`
- `internal/view/html_operator_styles_v2.go`
- `internal/view/html_operator_styles_v3.go`
- `internal/view/operator_redesign_test.go`
- `internal/view/preflight.go`
- `internal/view/terminal.go`
- `internal/view/terminal_layout.go`
- `internal/view/terminal_palette_test.go`
- `todo.md`
- `todo_done.md`

**Verification commands and results**

- Focused shell, CLI, view, preview, terminal-style, and terminal-text tests passed after each batch.
- `git diff --check` — passed.
- `go test ./... -count=1` — all 37 packages passed.
- Structured `go test -json ./... -count=1` parse — 1007 named tests/subtests passed.
- `go test -race ./... -count=1` — all 37 packages passed with no race report.
- `go vet ./...` — passed with no diagnostics.
- Production `omg` and `board-preview` binaries built successfully.
- Windows amd64 CLI, view, shell, terminal-style, terminal-text, and preview test binaries cross-compiled successfully.
- Real Chrome desktop default/action fragment, low-height desktop, 430×900 coarse-pointer mobile, dark/light, reduced-motion, and forced-colors audit — horizontal overflow 0, scripts/resources/external requests/console errors/hostile execution 0, every link/disclosure/copy target at least 44 pixels, correct Now/Actions active navigation, three inert copyable commands in two non-empty groups, mobile one-column layout, and 3-pixel forced-colors focus outline.
- Real pseudo-TTY grapheme audit — Apple Terminal and Ghostty at 40 columns max width 40 with 189 lines and 200 ANSI sequences; Ghostty at 80 columns max width 80 with 99 lines and 166 ANSI sequences; ZWJ emoji, flag, compatibility Jamo, hostile text, canonical content, and all safe actions preserved; `NO_COLOR` emitted zero ANSI.
- Real pseudo-TTY help/entry audit — 24×100 concise help 25 lines, 60×100 explicit help 62 lines, 24×40 max width 40; empty invocation exit 0 without error, explicit help full, contextual task help complete.
- Real pseudo-TTY palette audit — explicit light used normal ANSI roles plus gray metadata, explicit dark used bright roles, `COLORFGBG=0;15` selected light, and `NO_COLOR` emitted zero ANSI.

**Commit**

- No commit was created because the user requested continued implementation and verification, not a Git commit. Nothing was staged or pushed.

**Remaining limitations**

- Native Windows execution and hosted CI remain pending; changed packages cross-compile but were not executed on Windows.
- The HTML board intentionally remains a deterministic no-script snapshot. It does not poll state, execute or copy commands programmatically, or become an SPA/full-screen TUI.
- The macOS screenshot helper captured an unrelated existing codesign keychain prompt instead of the page, so no screenshot was accepted as evidence; real system-Chrome DOM, layout, computed-style, focus, resource, and hostile-input measurements passed.
- Publication, push, deployment, legal review, and criterion-specific acceptance evidence remain outside this interface task and stay in `todo.md`.

## 2026-07-24 — P0-STATE-1: Restore secure macOS operation and canonical self-dogfood

**Status:** complete

**Completed problem and fix**

The strict ancestor-ACL guard correctly rejected mutable state below the owner's intentionally shared macOS home directory, but Git mode placed the default database below the Git common directory and therefore made normal `init`, `preflight`, `doctor`, and `board` usage impossible on the real host.

- Added a distinct user-state resolver dependency while preserving injected user-config behavior for existing tests and adapters.
- On macOS, resolved the operating system's private per-user state root through `getconf DARWIN_USER_DIR`; other platforms retain the current user-config root until a stronger native state root is specified.
- Mapped Git common-directory identity to `omg/git/<stable-id>/state.db` below that private root, preserving one database across linked worktrees without storing mutable state in Git metadata.
- Kept descriptor-bound owner, mode, symlink, sidecar, and extended-ACL validation unchanged; unsafe explicit paths below the shared home still fail closed.
- Initialized the actual project checkout, created and verified a backup, applied all eight migrations with a bounded one-time approval, and passed integrity and board checks.
- Applied OMG-managed instruction blocks to root `AGENTS.md` and `CLAUDE.md`.
- Ran canonical self-dogfood through human/session/task/run/progress/checkpoint/Git/message/handoff acceptance and separate `VERIFIED_DONE` transitions, then passed board and preflight.
- Replaced blank suggested actions with non-ambient command templates that require an explicit quoted `'<PROJECT_PATH>'` replacement and retain exact task, handoff, reservation, or Git selectors without leaking private paths.

**Changed files**

- `internal/platform/resolver.go`
- `internal/platform/resolver_test.go`
- `internal/platform/user_state_dir_darwin.go`
- `internal/platform/user_state_dir_other.go`
- `internal/app/query/service.go`
- `internal/app/query/service_test.go`
- `internal/view/html_operator.go`
- `internal/view/operator_redesign_test.go`
- `internal/view/terminal.go`
- `docs/OPERATIONS.md`
- `docs/IMPLEMENTATION_PLAN.md`
- `.gitignore`
- `docs/adr/0001-language-and-store.md`
- `AGENTS.md`
- `CLAUDE.md`
- `todo.md`
- `todo_done.md`

**Verification commands and results**

- Focused platform, SQLite, bootstrap, and foundation tests passed.
- Real-project `init` and `preflight` passed where the prior build exited 4.
- Approved migration `0 → 8`, verified backup, `doctor --integrity`, and `board all` passed.
- Self-integration status reports both instruction files managed and current.
- Canonical self-dogfood completed through independently accepted handoff and `VERIFIED_DONE`.
- Actual board JSON emitted two non-empty, explicitly scoped command templates with exact task and handoff selectors; HTML and TTY rendered the templates without private-path leakage.
- HTML export completed to a new owner-only (`0600`) 51,663-byte static file with no script element.
- Full current-source verification passed: 1008 named tests/subtests, all 37 packages under `-race`, `go vet ./...`, production build, and Windows platform-package cross-compilation.

**Remaining limitations**

- Native Windows execution, hosted CI, independent human/legal acceptance rows, and public name/module authorization remain separate release gates in `todo.md`.
- At this checkpoint the heterogeneous-process trial still required actual independent agent processes; it was subsequently closed by P1-DOGFOOD-2 below.
- No commit or push was created because the user did not explicitly request either action.

## 2026-07-25 — P1-DOGFOOD-2: Complete real heterogeneous multi-process coordination

**Status:** complete

**Completed scenario**

- Ran actual independent Codex CLI, Hermes Agent, and Command Code processes in isolated local Git sandboxes. Each process invoked only the copied `omg` binary and private trial scripts; the project source tree was not exposed as a writable agent workspace.
- Used the existing human-direct root coordinator and registered three one-use-token delegated sessions with distinct runtime provenance: `codex-cli`, `hermes-agent`, and `command-code`.
- Created three canonical tasks joined by two hard `work_complete` dependencies. Separate Hermes and Command Code processes attempted to claim their blocked tasks early and were rejected before recording `BLOCKED` messages.
- Completed the prerequisite chain with exactly-once dependency-satisfied notifications. Every direct message followed `delivered → read → acknowledged`, and every trial `BLOCKED` and `DONE` message appeared exactly once.
- Created overlapping owner-attributed exclusive reservations for the same project-relative file, captured the advisory conflict at reservation creation, then left every trial reservation `released` or `expired` so no ghost lock remained.
- Submitted and independently accepted immutable Codex → Hermes → Command Code → root handoffs with verification evidence. All three task runs and all three tasks reached `VERIFIED_DONE`.
- Started a real Codex continuation process, recorded it alive, terminated it with `SIGTERM`, recorded the session as interrupted/stale, and adopted the interrupted work through a new canonical session without fabricating completion.
- Recorded real Git inventory from the Command Code stage and retained OMG's non-authorizing Git warning semantics.
- Passed 23 of 23 deterministic core checks, including integrity, initialized schema state, task/run/dependency/message/handoff/reservation state, raw-token non-disclosure, and external-process marker evidence.
- Ran Brain as an independent observer over the finalized evidence JSON; it returned exactly `BRAIN_PASS`.

**Runtime eligibility notes**

- Hermes completed through its authenticated `openai-codex` provider using `gpt-5.4` after the default CommandCode credential pool became temporarily unavailable.
- Claude Code was installed but excluded because the actual non-interactive invocation reported that it was not logged in.
- OMP and GJC were excluded because no executable under their expected command names was available in the active `PATH`.
- OpenCode was deliberately excluded from the final trial because it is not one of the owner's active agent runtimes.

**Changed tracked files**

- `todo.md`
- `todo_done.md`

**Local evidence**

- `.tmp/heterogeneous-final-v2/summary.json`
- `.tmp/heterogeneous-final-v2/board.json`
- `.tmp/heterogeneous-final-v2/brain-observer.json`
- `.tmp/heterogeneous-final-v2/brain-review.txt`

**Final verification**

- Final local evidence summary — 24 of 24 checks passed after adding the independent Brain verdict.
- Canonical snapshot audit — preflight initialized with zero pending migrations, integrity healthy, both instruction integrations managed, three trial tasks and runs `VERIFIED_DONE`, both dependencies satisfied, all three handoffs accepted, and all three reservations released or expired.
- External process audit — no Codex, Hermes, Command Code, trial orchestrator, or worker process remained after completion.
- `git diff --check` — passed.
- `go test -json ./... -count=1` — 1008 named tests/subtests passed; all 37 package results completed successfully, including four packages with no test files.
- `go vet ./...` — passed with no diagnostics.
- `CGO_ENABLED=0 go build -trimpath` — rebuilt `bin/omg` successfully as `v0.1.0-dev+a863f81.dirty`.
- `GOOS=windows GOARCH=amd64 go test -c ./internal/platform` — Windows platform tests cross-compiled successfully.
- Final high-confidence secret scan — passed across 10,737 repository-visible files after correcting a scanner false positive that had matched the `sk-` substring inside synthetic `task-...` test identifiers.

**Remaining limitations**

- This closes local personal-use heterogeneous coordination readiness. It does not satisfy native Windows, hosted CI, criterion-specific release evidence, public naming/module identity, legal approval, publication, or push gates.
- The verified implementation was subsequently committed as the local source-of-truth baseline in `794ac50`; no remote push was performed.


## 2026-07-25 — P0-OSS-2: Establish the first trusted local Git source of truth

**Status:** complete

**Completed work**

- Reviewed the complete tracked and untracked source set and confirmed that runtime databases, one-time approvals, local test captures, agent harness state, generated binaries, and implementation scratch files are excluded by `.gitignore`.
- Re-ran `git diff --check` and the high-confidence secret scan before staging; both passed.
- Committed the complete verified product state, including new source files, tests, documentation, and OMG-managed root instructions, as commit `794ac50` (`feat: establish OMG local source of truth`).
- Confirmed the working tree was clean immediately after the baseline commit.

**SOT definition**

- Local branch: `main`
- Baseline commit: `794ac50`
- Upstream: `origin/main`
- Remote push: not performed

**Remaining limitations**

- Public release identity and the final Go module path remain a separate human decision.
- A remote source of truth still requires explicit reconciliation and push authorization.
