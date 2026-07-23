## Problem and decision

<!-- What observable problem does this solve? Why this design? -->

## Contract impact

- Acceptance IDs:
- CLI/schema/protocol version impact:
- Compatibility or migration impact:
- Security/privacy impact:
- Working-identity impact: none / describe

## Verification

<!-- Exact commands/scenarios and observed results. Do not attach secrets, stores, backups, prompts, transcripts, private paths, or approval artifacts. -->

## Safety checklist

- [ ] Core remains daemonless and local-first.
- [ ] No LLM call/selection, hidden telemetry, or transcript replication was added.
- [ ] No message/model/MCP content is treated as approval or executable text.
- [ ] No destructive Git, migration, restore, publication, credential, deploy, or production authority was added.
- [ ] Private paths, tokens, runtime locators, prompts, messages, and final output remain hidden/redacted by default.
- [ ] New durable mutation is transactional, idempotent, and emits its event/receipt.
- [ ] Focused behavior was exercised; `go test -race ./...` passes when applicable.
- [ ] Documentation/release notes reflect any public contract change.
- [ ] Commits carry DCO `Signed-off-by` trailers.
