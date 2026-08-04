# Global agent installation

OMG uses an OMP-style global harness model: install the binary once, let supported coding agents discover the OMG lifecycle automatically, and keep project state local to each repository.

## User experience

After a tagged release is published, the intended installation is one command.

### macOS and Linux

```bash
curl -fsSL https://raw.githubusercontent.com/jeremy-merchant/oh-my-group/main/install | sh
```

### Windows PowerShell

```powershell
& ([scriptblock]::Create((Invoke-RestMethod https://raw.githubusercontent.com/jeremy-merchant/oh-my-group/main/install.ps1)))
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

1. `OBSERVE`: read-only diagnosis, log inspection, review, or status lookup. It creates no OMG record and does not run project preflight solely to inspect.
2. `WORK_LITE`: one branch, one worker, and one bounded mutation. The controller supplies one complete start command and one complete finish command; normal diagnosis, RED/GREEN work, and Git verification happen between them without intermediate OMG writes by default.
3. `FULL`: multiple workers or candidates, shared rolling ownership, cross-session handoff, exact-SHA integration/Canary, deploy/release, database, authentication, authorization, or payment. It uses the complete independent-review and source-cleanup lifecycle.

`omg mode classify --payload <risk-signals> --json` exposes the deterministic boundary contract, including whether preflight/start/finish/intermediate writes are required, whether the controller must provide commands, and the Git source-of-truth fields. Safety-triggered FULL work cannot be downgraded. In stateful modes, preflight fails closed when execution or schema health fails or an ownership conflict exists. Git risks and historical `stale_sessions`, `runtime_unobservable_sessions`, `finished_unclosed_sessions`, and `integration_queue` counts are warnings/housekeeping rather than automatic blockers. Historical closure and reconciliation use separate controller operations.

OMG applies only to work that mutates or coordinates the selected repository. Installing a host-level CLI, logging into a provider, maintaining a package manager, or configuring a tool outside a repository does not create OMG project records and must not be routed through project preflight. Those operations still require the user's ordinary authority and normal host-security checks.

Agent-harness health is not a universal shell gate. `omg version` and `omg agent status`, `doctor`, `install`, and `uninstall` are bootstrap or self-repair commands and remain runnable when a managed surface is missing or drifted. A harness problem may stop coordinated repository mutation that depends on OMG, but it must never block diagnosis, its own repair path, or unrelated host-level work.

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
