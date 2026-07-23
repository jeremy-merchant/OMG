# OMG Support

## Publication status

OMG is **NOT PUBLISHED**. This checkout has no public support SLA, package channel, compatibility promise, or asserted public contact. Local release candidates are for approved validation only.

Before publication, the release manifest must name the official repository, private security channel, supported release line, maintainers, and artifact verification keys. Do not trust an unofficial package, binary, domain, or account claiming to represent OMG.

## Getting help after publication

Use the official repository's issue tracker for reproducible usage questions and non-sensitive bugs. Include:

- `omg version --json` output;
- operating system and architecture;
- command shape with private paths replaced by placeholders;
- stable error code, exit code, and retryable flag;
- minimal reproduction against a disposable project;
- expected and observed behavior.

Do not attach a canonical database, backup, migration approval, raw prompt, message body, final output, token, runtime home, native-session reference, transcript, private path, or secret-bearing log.

Security vulnerabilities, privacy incidents, secret exposure, and unsafe migration behavior follow `SECURITY.md`, not a public issue.

## Support scope

A published v0.1 release supports only the OS/architecture artifacts listed in its signed release manifest and only the exact schema/command versions declared there. Source builds require the Go version declared in `go.mod`.

Best-effort support covers:

- installation and artifact verification;
- project/workspace/store selection;
- migration planning, backup verification, and explicit apply failures;
- daemonless coordination, boards, exports, instruction integration, runtime wrapping, watch, and MCP stdio;
- privacy-safe diagnostics and recovery planning.

Not supported in v0.1:

- cloud hosting or multi-host synchronization;
- multi-tenant isolation;
- automatic merge, rebase, push, deploy, migration, reset, clean, deletion, or publication;
- OS-level sandbox enforcement for agents that ignore the protocol;
- recovery from manual database modification;
- private agent-runtime internals or replicated conversation transcripts;
- project-specific Pygmalion/Zoomzi behavior as a core compatibility guarantee.

## Lifecycle

Until a public lifecycle policy is approved, only the newest published release would be eligible for fixes; no long-term-support line is promised. A security advisory may shorten support for a vulnerable release. Every release must state schema compatibility, upgrade/rollback requirements, and any support change explicitly.
