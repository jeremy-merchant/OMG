# Security Policy

## Supported versions

OMG source is public under Apache-2.0, but no stable binary release exists yet.

| Version | Supported |
|---|---|
| `main` source builds | Best-effort security fixes |
| Stable tagged releases | None published yet |
| Historical local RC artifacts | Unsupported |

A future release will define its supported platforms, schema compatibility, checksums, SBOM, provenance, and support window in the release manifest.

## Reporting a vulnerability

Do not open a public issue for a vulnerability, suspected secret exposure, unsafe migration, path traversal, command execution, authorization bypass, privacy leak, or malicious MCP/runtime payload.

Use GitHub private vulnerability reporting from the repository **Security** tab and choose **Report a vulnerability**. For a confidential Code of Conduct report, use the same private channel and prefix the title with `Code of Conduct`.

A useful report includes:

- affected source revision or version, OS, and architecture;
- impact and preconditions;
- a minimal reproduction using synthetic data;
- expected versus observed behavior;
- whether canonical state, backups, tokens, private paths, or runtime metadata may be exposed;
- a suggested mitigation, if known.

Never send a real SQLite store, backup, approval file, delegation token, raw prompt, message body, final output, transcript, runtime home, opaque native reference, private path, credential, or production identifier. Replace them with synthetic values and hashes.

## Response process

Maintainers will make a best effort to acknowledge the report privately, reproduce and assess severity, coordinate a fix under embargo where needed, credit the reporter if requested and safe, and disclose residual risk after a fix is available. No response-time SLA is promised before the first stable release.

## Security model

The normative engineering security contract is in `docs/SECURITY.md`, with threats and privacy boundaries in `docs/THREAT_MODEL.md` and `docs/PRIVACY.md`.

- OMG is local-first and assumes a single user or trusted machine; it is not a multi-tenant isolation boundary.
- Agent lineage and tokens do not grant commit, push, deploy, credentials, production access, deletion, publication, or migration approval.
- Messages, prompts, handoffs, model output, MCP payloads, and watch events are untrusted data.
- OMG v0.1 provides no destructive Git automation and no OS-level filesystem sandbox.
- Private runtime locators stay local and native conversation transcripts are not copied.
- Compiled schema migrations use the backup-verified automatic policy; unknown or mismatched schema state fails closed. Restore mutations still require separate explicit human approval.
