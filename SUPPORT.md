# OMG Support

## Project status

OMG source is public under Apache-2.0. There is no stable binary release, package channel, compatibility promise, or support SLA yet. Source builds from `main` receive best-effort community support.

Use the official GitHub issue tracker for reproducible usage questions and non-sensitive bugs. Include `omg version --json`, operating system and architecture, commands with private paths replaced by placeholders, stable error and exit codes, a disposable-project reproduction, and expected versus observed behavior.

Do not attach a canonical database, backup, migration approval, raw prompt, message body, final output, token, runtime home, native-session reference, transcript, private path, or secret-bearing log. Security vulnerabilities and privacy incidents follow `SECURITY.md`, not a public issue.

## Best-effort scope

- installation and source-build questions;
- project, workspace, and store selection;
- migration planning, backup verification, and explicit apply failures;
- daemonless coordination, boards, exports, instruction integration, runtime wrapping, watch, and MCP stdio;
- privacy-safe diagnostics and recovery planning.

## Out of scope for v0.1

- cloud hosting or multi-host synchronization;
- multi-tenant isolation;
- automatic merge, rebase, push, deploy, migration, reset, clean, deletion, or release publication;
- OS-level sandbox enforcement for agents that ignore the protocol;
- recovery from manual database modification;
- private agent-runtime internals or replicated conversation transcripts;
- project-specific Pygmalion or Zoomzi behavior as a core compatibility guarantee.

A future tagged release must state supported platforms, schema compatibility, upgrade and rollback requirements, and its support window explicitly.
