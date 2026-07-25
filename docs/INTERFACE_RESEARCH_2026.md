# OMG interface research and application notes — 2026

> Audited: 2026-07-24
> Scope: local CLI help and the deterministic self-contained operator board
> Constraint: no JavaScript, no external resources, no weakened redaction or machine-output contracts

## Why this document exists

The interface should not chase visual trends independently of the coordination model. The useful contemporary direction is consistent across the reviewed systems: make priority perceptible at a glance, make controls visibly actionable, keep dense facts legible, and adapt the presentation to the available surface without hiding canonical information.

This document records the official sources inspected, the source-level OMP patterns reviewed, and the decisions applied to OMG.

## Sources reviewed

### Material 3 Expressive

Official sources:

- https://design.google/library/expressive-material-design-google-research
- https://blog.google/products/android/material-3-expressive-android-wearos-launch/
- https://developer.android.com/design/ui/mobile/guides/layout-and-content/m3-expressive

Relevant findings:

- Expression is created through purposeful color, shape, scale, typography, motion, and containment rather than decorative novelty.
- Larger and more distinct action targets improve discoverability and scan speed.
- Live or frequently changing information should be glanceable before the user enters a detail view.
- Emphasis must remain selective. When every card is equally expressive, hierarchy disappears.

OMG application:

- The HTML board now begins with a state-driven masthead and six bounded signal cards instead of a generic slogan followed by equally weighted sections.
- Semantic tones are attached to operational meaning: working, blocked, warning, success, and informational.
- The signal deck uses scale and containment for the first scan; detailed canonical facts stay in quieter solid surfaces.
- Motion is limited to a short initial rise and is disabled by `prefers-reduced-motion`.

### Apple interface materials and hierarchy

Official sources:

- https://developer.apple.com/design/human-interface-guidelines/materials
- https://developer.apple.com/documentation/technologyoverviews/adopting-liquid-glass
- https://developer.apple.com/design/human-interface-guidelines/designing-for-ios

Relevant findings:

- Translucent material is most effective as a functional layer for navigation and controls.
- Content surfaces should preserve legibility and should not become a stack of competing glass panels.
- Hierarchy, harmony, and consistency are more durable than a one-off visual effect.

OMG application:

- Backdrop blur is restricted to the desktop rail and compact mobile control bar.
- Signal, task, handoff, Git, and metadata content remain on solid high-legibility surfaces.
- The mobile rail collapses into a sticky brand and horizontally scrollable section index; secondary rail health/context is removed because the same facts are available in the masthead, signal deck, and snapshot.
- `prefers-reduced-transparency` removes the remaining translucent treatment.

### WCAG 2.2 and platform accessibility modes

Official sources:

- https://www.w3.org/TR/WCAG22/
- https://www.w3.org/WAI/WCAG22/Understanding/target-size-minimum.html
- https://www.w3.org/WAI/WCAG22/Understanding/focus-not-obscured-minimum.html

Relevant findings:

- Keyboard focus must remain visible and must not be hidden by sticky content.
- Pointer targets need sufficient size and spacing.
- Reflow must preserve reading and operation without document-level horizontal scrolling.
- High-contrast and user preference modes are part of the interface contract, not optional polish.

OMG application:

- Section navigation, masthead actions, and signal cards have a measured minimum target height of 44 CSS pixels.
- Browser automation verifies that the focused masthead action is keyboard reachable, visible, and not covered by another element.
- `forced-colors` receives an explicit 3-pixel system `Highlight` outline because box-shadow focus rings can disappear in high-contrast rendering.
- `prefers-contrast: more`, `prefers-reduced-motion`, `prefers-reduced-transparency`, light/dark color schemes, print, and forced-colors are covered by CSS contracts and renderer/browser tests.
- The 430 CSS-pixel layout has zero document-level horizontal overflow. Only the section index intentionally scrolls horizontally.

### Current browser capabilities

Official source:

- https://developer.mozilla.org/en-US/docs/Web/API/View_Transition_API

Decision:

The View Transition API was reviewed but deliberately not added. The OMG export is a static, script-free snapshot, and anchor navigation does not need a document transition runtime. A short CSS-only entrance is enough and remains preference-aware.

Container queries were adopted because the signal deck and masthead should respond to the available content width rather than only the outer browser width. `content-visibility` was tested and removed: it could cause long-section fragment navigation to resolve against an estimated intrinsic position before final layout, which is unacceptable for an operator index.

### Contemporary developer CLI and TUI sources

Official or first-party sources reviewed:

- shadcn/ui Command and Sidebar: https://ui.shadcn.com/docs/components/command and https://ui.shadcn.com/docs/components/sidebar
- GitHub Primer NavList: https://primer.style/product/components/nav-list/
- Charm Bubble Tea v2: https://github.com/charmbracelet/bubbletea
- Crush releases: https://github.com/charmbracelet/crush/releases
- OpenTUI core: https://github.com/anomalyco/opentui/tree/main/packages/core
- CLI Guidelines help and completion guidance: https://clig.dev/
- Fish completion model: https://fishshell.com/docs/current/completions.html
- Gemini CLI command, settings, and theme documentation: https://geminicli.com/docs/reference/commands/, https://geminicli.com/docs/cli/settings/, and https://geminicli.com/docs/cli/themes/
- Atuin basic usage and compact/inline modes: https://docs.atuin.sh/main/guide/basic-usage/
- Lazygit configuration and contextual keybindings: https://github.com/jesseduffield/lazygit/blob/master/docs/Config.md

Applied findings:

- Commands are grouped by operator intent and carry concise descriptions instead of presenting an undifferentiated word list.
- Shell completion traverses command → subcommand, attaches descriptions where the shell supports them, and switches to directory, file, or enumerated-format completion for typed option values.
- Empty invocation and short terminals show a concise discovery surface; explicit global help remains complete on a tall terminal, and contextual command help is never truncated.
- Bare namespace commands such as `omg task` and `omg board` open their own help without dispatching or guessing a mutation.
- Static safe commands remain inert. HTML exposes focusable, whole-command-selectable `<pre><code>` blocks; TTY output presents purpose before the command and explicitly tells the operator to review before execution.
- Low-height screens reduce visual padding, not target size or canonical content. Mobile and coarse-pointer layouts suppress hover-only motion.
- The terminal palette supports explicit light/dark foreground roles and a conservative `COLORFGBG` hint while keeping `NO_COLOR` authoritative.

Deliberate exclusions:

- Dynamic OSC window titles and background-color terminal queries were not added. OMG output must remain safe for pipes, logs, captured evidence, and protocol-separated stdout.
- Interactive search, copy buttons, and command execution were not simulated in the static HTML export. The board stays useful without JavaScript and never implies that a command ran.
- A full-screen terminal application framework was not introduced. OpenTUI and Bubble Tea reinforce correctness and bounded layout principles, but OMG's human-readable command output remains non-interactive and its machine output remains stable JSON.

## OMP source audit

Official repository:

- https://github.com/can1357/oh-my-pi

Primary source areas reviewed:

- `packages/tui/src/` — strict width contracts, differential rendering, component composition, themes, symbols, keyboard input, autocomplete, select lists, and synchronized output
- `packages/coding-agent/src/modes/` — interactive mode orchestration, status/todo containers, event-driven updates, selectors, and lifecycle handling
- `packages/coding-agent/src/tools/` — uniform result metadata, bounded output, truncation accounting, and tool-card rendering boundaries
- `packages/coding-agent/src/task/` — typed subagent/task state, bounded concurrency, progress, and output ownership
- `packages/coding-agent/DEVELOPMENT.md` — current runtime architecture and responsibility boundaries

Reusable interaction principles, not copied visuals:

1. **Metadata drives discovery.** OMP builds command and completion surfaces from explicit command metadata. OMG now uses one command catalog for global, command, subcommand, PowerShell, receipt, JSON, and recovery help.
2. **Width is a contract.** OMP TUI components render against an explicit available width and current releases distinguish grapheme width from terminal-specific Hangul Compatibility Jamo behavior. OMG now uses UAX #29 grapheme segmentation for CLI and board wrapping, keeps ZWJ emoji and flags intact, and selects the macOS/Ghostty Jamo profile from terminal identity.
3. **Status precedes payload.** Tool cards present state before detailed output. OMG board sections and CLI results lead with semantic state, then primary identity, then canonical metadata.
4. **Progressive disclosure beats omission.** OMP truncation preserves retrieval metadata. OMG abbreviates primary identifiers but retains full canonical values in secondary metadata and disclosure regions.
5. **Bounded next actions matter.** OMP preview/accept and typed option flows keep the next operation explicit. OMG exposes a safe-next command in the first HTML viewport and related command paths in contextual CLI help.

## Resulting OMG interface model

### Web operator board

First scan order:

1. Current health and state-driven headline
2. Attention and safe command actions
3. Six signal cards: active sessions, open work, attention, inbox, handoffs, reservations
4. Priority signal and attention queue
5. Ownership, execution, constraints, communication, transfer, path intent, repository state, commands, provenance

Navigation behavior:

- Desktop: sticky counted rail with full context and owner-visible section counts
- Mobile: compact sticky control rail with horizontal section navigation
- Fragment targets receive a bounded visual highlight
- Long sections use normal layout so fragment positions remain stable

### CLI

Global scan order:

1. Usage
2. Four goal-oriented workflows
3. Adaptive command families
4. Global options

Adaptive density and discovery:

- Empty invocation is a successful, non-dispatching discovery path. At 100 columns it renders 25 lines instead of the 62-line explicit global contract.
- Explicit global help uses terminal height: a 24-row TTY receives concise workflows and every command family, while a 60-row TTY receives descriptions and full global options.
- Contextual command help is never height-truncated. `omg task --help` keeps subcommands, options, examples, and related paths even in a 24-row terminal.
- Bare namespace commands such as `omg task`, `omg board`, and `omg migration` open contextual help; any additional argument returns to strict validation.
- 68 columns and wider use command names plus concise descriptions; below 68 columns use wrapped command-name grids.
- Examples use hanging indentation and command help lists bounded related paths.
- Shell completion uses one command tree across Bash, Zsh, Fish, and PowerShell. It carries descriptions, traverses selected subcommands, removes siblings at leaf paths, and switches to directory/file/format candidates after typed options.
- Shell initialization adds explicit preflight, all-board, and checkpoint workflow helpers without evaluating untrusted content.
- Grapheme-aware wrapping preserves combining marks, ZWJ emoji, regional-indicator flags, keycaps, CJK, and terminal-specific Hangul Compatibility Jamo width.
- `OMG_COLOR_SCHEME=light|dark` explicitly selects the foreground palette; `COLORFGBG` is a conservative fallback and `NO_COLOR`/`TERM=dumb` still disable ANSI.

## Automated browser evidence

The deterministic hostile-input board was measured through system Chrome with Playwright at 1440-pixel desktop widths, a 700-pixel-low desktop, 430×900 mobile, dark/light schemes, coarse pointer, reduced motion, and forced-colors.

Verified values:

- Document horizontal overflow: 0 pixels in every measured mode
- Minimum measured height across every `a`, `summary`, skip link, and copyable command target: 44 pixels
- Signal cards: exactly 6; the low-height layout reduces card height without reducing targets
- Default navigation highlights **Now**; `#actions` highlights **Actions** through progressive `:has()` support
- Suggested actions are grouped into the non-empty Inspect/Recover/Coordinate/Other domains without dropping unknown future codes
- Script elements: 0
- External resource elements, external-origin links, and external network requests: 0
- Console errors: 0
- Unsafe hostile markup execution: false
- Mobile command groups: one column; coarse-pointer hover transform: none
- Skip link: first keyboard target, visible above the sticky rail, and moves focus to the main landmark
- Forced-colors focus indicator: solid 3-pixel system outline

## Automated terminal evidence

The deterministic preview and production binary were also exercised through real pseudo-terminals:

- Apple Terminal and Ghostty width profiles at 40 columns: maximum measured width 40 cells
- Ghostty at 80 columns: maximum measured width 80 cells
- ZWJ emoji `👩‍💻`, flag `🇰🇷`, compatibility Jamo `ㄱ`, hostile text, canonical facts, and all safe actions remained intact
- 24×100 global help: 25 lines; 60×100 explicit help: 62 lines; 24×40 concise help: maximum width 40
- Empty invocation: exit 0 and no dispatch; explicit `--help`: full contract; contextual `task --help`: complete even at 24 rows
- Explicit light palette: normal ANSI semantic colors plus gray metadata
- Explicit dark palette: established bright Titanium roles
- `COLORFGBG=0;15`: light fallback; `NO_COLOR`: zero ANSI sequences

## Non-goals

- Do not convert the static board into an SPA.
- Do not add decorative glass to operational content.
- Do not use motion to communicate facts that are unavailable without motion.
- Do not replace canonical identifiers with presentation-only aliases.
- Do not turn command output into a full-screen interactive TUI; human output and stable machine output remain separate.
- Do not copy OMP colors, names, or layouts as a product identity.
