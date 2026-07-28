# OMG operator interface design system

This document records the source evidence and design decisions used by OMG's CLI and self-contained HTML board.

## Source basis

The redesign is grounded in the official `can1357/oh-my-pi` source at commit `639bac596d94b5993349f3f6696176cb2bf9b5d3`, specifically:

- `packages/coding-agent/src/modes/theme/defaults/titanium.json`
- `packages/coding-agent/src/modes/theme/theme.ts`
- `packages/coding-agent/src/tui/output-block.ts`
- `packages/coding-agent/src/task/render.ts`
- `docs/theme.md`

The local DevSpace project boundary did not expose an installed `omp` binary or package directory, so the pinned official source is the reproducible implementation reference.

## Extracted OMP principles

1. **Semantic color before decoration.** Accent identifies active work and navigation; success, warning, error, muted, and dim have stable roles.
2. **Status before metadata.** A glyph and short state label lead each row. IDs, timestamps, ownership, and counts follow as lower-emphasis metadata.
3. **Selective framing.** Rounded borders are reserved for meaningful output/state blocks. Dense lists use whitespace, dividers, and tree connectors instead of a box around every section.
4. **Relationships are spatial.** Parent/child work is shown with `├─`, `└─`, and `│`; a task or session identifier is treated as a breadcrumb rather than an undifferentiated field.
5. **Dense output is bounded.** Long values wrap or abbreviate in the primary view while full canonical values remain available in secondary detail.
6. **One-cell terminal rhythm.** Content commonly starts one cell inside a frame or connector, metadata is joined with ` · `, and empty results collapse to a single muted line.

## OMG tokens

### Terminal

Dark-background semantic roles preserve the established bright Titanium mapping: bright cyan accent/info, bright green success, bright yellow warning/pending, bright magenta blocked, bright red danger, and dim metadata. Light-background roles use normal ANSI cyan/green/yellow/magenta/red plus gray metadata.

`OMG_COLOR_SCHEME=light|dark` is the explicit preference. When absent, only conventional white-background `COLORFGBG` values are treated as light; ambiguous indexed colors preserve the dark palette. Color is enabled only for a real terminal when `NO_COLOR` is unset and `TERM` is not `dumb`. Plain output keeps the same glyphs, ordering, labels, and tree structure.

Cell measurement uses UAX #29 grapheme clusters through `github.com/rivo/uniseg`. Combining marks, ZWJ emoji, regional-indicator flags, and keycaps are never split. Hangul Compatibility Jamo follows current OMP terminal-identity behavior: Ghostty uses two cells even on macOS, other macOS terminals use one cell, and other platforms use the Unicode width result.

### HTML

Dark mode is the default and derives from OMP's titanium palette:

- canvas `#151820`
- surface `#0f1216`
- raised surface `#1b2028`
- primary text `#e8ecf4`
- secondary text `#bdc4cf`
- muted text `#858e9d`
- accent `#00b4ff`
- success `#00e982`
- warning `#f0ad45`
- danger `#ff5a67`
- info `#55c8ff`
- pending `#d4c090`
- blocked `#d989ff`
- strong border `#39424e`
- subtle border `#252c35`

A light palette is supplied through `prefers-color-scheme: light`; print has a separate high-contrast override.

## Status vocabulary

| Semantic state | Glyph | Examples |
| --- | --- | --- |
| verified/success | `✔` | `verified_done`, `accepted`, `satisfied`, `released` |
| working | `⟳` | `running`, `active`, `claimed`, `alive`, `in_progress` |
| pending | `○` | `pending`, `submitted`, `unread`, `no_signal` |
| warning | `⚠` | `stale`, dirty Git, advisory warnings |
| blocked | `⦸` | unsatisfied dependency, reservation conflict, `blocked` |
| error | `✘` | `failed`, `rejected`, `interrupted`, internal errors |
| inactive | `·` | unknown or informational states |

## Hierarchy and components

- The terminal starts with a product masthead, current project/scope, and one health line.
- The first board section is **Now**, answering who is active, what work is claimed, and what is blocked.
- Sessions and tasks are trees. Runs and progress are children of their owning task.
- Dependencies are directed blocker links, not generic rows.
- Inbox, handoffs, reservations, Git, and actions each use a component matched to their domain.
- Full canonical metadata remains present in HTML disclosure panels and in detailed terminal metadata lines, preserving the renderer contract without making raw fields the visual entry point.

## Accessibility and safety

- HTML remains self-contained with no JavaScript or external resources.
- The stylesheet is authorized by a SHA-256 CSP source.
- Canonical values are escaped before insertion; CSS classes are selected only from fixed semantic mappings.
- Skip navigation, landmarks, visible focus, keyboard-operable disclosure elements, reduced motion, responsive layout, and print support remain mandatory.

## Global-harness follow-up

OMP's current product surface confirms that its strongest installation and discovery ideas are architectural rather than decorative:

- one global install command instead of per-project bootstrap scripts;
- automatic discovery of existing agent rules, skills, and MCP configuration;
- one-level `skills/<name>/SKILL.md` capability packs with meaningful frontmatter;
- terminal status rendered through semantic cards or trees while absolute home paths are shortened;
- tool and edit output previewed or structurally bounded before mutation.

OMG adopts the first four ideas through `omg agent install` and the global discovery surfaces documented in `GLOBAL_AGENT_INSTALL.md`. The agent harness report follows the existing OMG visual grammar: a semantic masthead, short facts, then a status-first tree whose path and detection metadata wrap on following lines. A 36-cell regression test prevents horizontal overflow.

OMG does **not** copy OMP's complete interactive shell. OMP is an agent runtime and can own prompt cards, permission pickers, queued edits, animation, and streaming tool-call cards. OMG is a coordination ledger that must remain usable from TTY, JSON, MCP, scripts, and non-interactive agents. Therefore:

- no animation or spinner is required for canonical status;
- no card border is added around every discovery row;
- human TTY never replaces stable JSON contracts;
- install and uninstall remain bounded filesystem transactions rather than interactive approval UIs;
- project schema migration and external authority remain separately human-gated.
