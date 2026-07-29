# Global agent installation

OMG uses an OMP-style global harness model: install the binary once, let supported coding agents discover the OMG lifecycle automatically, and keep project state local to each repository.

## User experience

After a tagged release is published, the intended installation is one command.

### macOS and Linux

```bash
curl -fsSL https://raw.githubusercontent.com/jeremy-merchant/OMG/main/install | sh
```

### Windows PowerShell

```powershell
& ([scriptblock]::Create((Invoke-RestMethod https://raw.githubusercontent.com/jeremy-merchant/OMG/main/install.ps1)))
```

The installer:

1. detects the operating system and architecture;
2. downloads the matching GitHub release archive;
3. verifies the archive against `checksums.txt`;
4. installs `omg` atomically in the user binary directory;
5. adds that directory to the user PATH without duplicating its managed block;
6. runs `omg agent install` automatically.

The user does not run project-specific OMG lifecycle commands for an agent. The discovered skill first chooses a proportional mode, then performs only the coordination that mode requires.

No stable release exists yet. Until release assets are published, build from source and run `omg agent install` once. The install scripts support an offline reviewed candidate through `OMG_INSTALL_SOURCE` plus the exact `OMG_INSTALL_SHA256`; the source path must be a regular non-symlink file.

## Global discovery surfaces

`omg agent install` applies bounded managed instructions and always-on skills under the current user home.

| Provider | Global surface |
|---|---|
| Generic agent ecosystem | `~/.agents/skills/omg/SKILL.md` |
| OMP | `~/.omp/agent/skills/omg/SKILL.md` |
| Claude | `~/.claude/CLAUDE.md`, `~/.claude/skills/omg/SKILL.md` |
| Codex | `~/.codex/AGENTS.md`, `~/.codex/skills/omg/SKILL.md` |
| Gemini | `~/.gemini/GEMINI.md`, `~/.gemini/skills/omg/SKILL.md` |
| Cursor | `~/.cursor/rules/omg.mdc` |
| Windsurf | `~/.windsurf/rules/omg.md` |
| Cline | `~/.cline/rules/omg.md` |
| OpenCode | `~/.config/opencode/AGENTS.md` |

Skills use a `SKILL.md` frontmatter contract with `alwaysApply: true`. Managed instruction files preserve all pre-existing content outside the OMG marker block. A foreign or drifted OMG skill fails closed instead of being overwritten.

## Commands

```bash
omg agent install
omg agent status
omg agent doctor
omg agent uninstall
```

- `install` is idempotent and uses atomic replacement with rollback.
- `status` reports every discovery surface as `installed`, `missing`, `drifted`, or `unsafe` and marks locally detected agent executables.
- `doctor` reports whether the global harness is healthy without exposing the absolute home path.
- `uninstall` removes only exact OMG-managed skills and instruction blocks. It does not remove agent-owned configuration.

Set `OMG_AGENT_HOME` only for isolated testing or managed portable environments. Normal users should not set it.

## Project behavior

The global harness does not create one shared cloud account or central database. When an agent enters a repository, it selects one lifecycle:

1. `OBSERVE`: read-only inspection with no file mutation or external side effect. It creates no session, task, run, reservation, progress, or handoff and does not run preflight solely to answer a question.
2. `WORK_LITE`: a single-owner repository change. It runs preflight, records session/task/run plus changed-path reservations, records progress only when long-running or blocked, then completes and archives without a handoff by default.
3. `FULL`: multi-agent, release/canary, production, auth/payment, or ownership-transfer work. It uses the complete handoff, independent review, exact-SHA integration/canary, and source-cleanup lifecycle.

`omg mode classify --payload <risk-signals> --json` exposes the deterministic contract. Safety-triggered FULL work cannot be downgraded by an override. For stateful modes, preflight applies exact compiled migrations through a plan-bound verified backup and integrity checks. Workers receive `board me`; controllers normally use `board actionable`, while `board history` retains the complete audit view.

Message bodies and model output remain inert data. Global installation never grants commit, push, deployment, credential, publication, destructive Git, production authority, or access to an unrelated repository.

## Release asset contract

`scripts/package-release.sh` produces these assets:

```text
omg_darwin_amd64.tar.gz
omg_darwin_arm64.tar.gz
omg_linux_amd64.tar.gz
omg_linux_arm64.tar.gz
omg_windows_amd64.zip
omg_windows_arm64.zip
checksums.txt
```

POSIX archives contain `omg` at the archive root. Windows archives contain `omg.exe`. `checksums.txt` contains a SHA-256 entry for every archive. The installer accepts no unverified network artifact.

Required packaging invocation:

```bash
OMG_RELEASE_VERSION=vX.Y.Z sh scripts/package-release.sh
```

The output directory must be absent or empty. Release publication remains a separate human-authorized operation.

## Terminal design adopted from OMP

OMG adopts the useful terminal interaction principles rather than copying OMP's visual shell:

- semantic state glyphs and colors before decoration;
- state on the first line, path and detection metadata underneath;
- tree connectors for related discovery surfaces;
- bounded output at narrow widths using grapheme-aware wrapping;
- home paths rendered as `~` rather than absolute workstation paths;
- control-character neutralization before rendering;
- JSON remains the stable automation interface.

OMG deliberately keeps a quieter operator-ledger identity. It does not frame every row as a card and does not use animation or decorative gradients for dense coordination output.
