# Security Policy

## Supported versions

OMG is currently **NOT PUBLISHED**. No public version is supported and no public vulnerability-reporting endpoint is asserted. Local `v0.1.0-rc.*` artifacts are validation candidates, not supported releases.

Before publication, the release gate must update this table and record a verified private reporting channel:

| Version | Supported |
|---|---|
| Unpublished working tree / local RC | No public support |

## Reporting a vulnerability

Do not open a public issue for a vulnerability, suspected secret exposure, unsafe migration, path traversal, command execution, authorization bypass, privacy leak, or malicious MCP/runtime payload.

While the project is private, report through the existing private channel to the project owner or designated security maintainer. After publication, use only the private security-advisory/contact mechanism named in the official repository and signed release manifest. If no verifiable private channel exists, do not transmit exploit details or sensitive artifacts; report only that a private channel is needed.

A useful report includes:

- affected version, source revision, OS, and architecture;
- impact and preconditions;
- minimal reproduction using synthetic data;
- expected versus observed behavior;
- whether canonical state, backups, tokens, private paths, or runtime metadata may be exposed;
- suggested mitigation, if known.

Never send a real SQLite store, backup, approval file, delegation token, raw prompt, message body, final output, transcript, runtime home, opaque native reference, private path, credential, or production identifier. Replace them with synthetic values and hashes.

## Response process

Once a verified channel and security team exist, maintainers will:

1. acknowledge receipt privately;
2. reproduce and assess severity without expanding access to sensitive data;
3. coordinate a fix, regression proof, advisory, and release under embargo where needed;
4. credit the reporter if requested and safe;
5. disclose residual risk and affected versions after a fix is available.

No SLA is promised before publication. A future public SLA must be added here explicitly.

## Security model

The normative engineering security contract is in `docs/SECURITY.md`, with threats and privacy boundaries in `docs/THREAT_MODEL.md` and `docs/PRIVACY.md`.

Key limits:

- OMG is local-first and assumes a single user/trusted machine; it is not a multi-tenant isolation boundary.
- Agent lineage and tokens do not grant commit, push, deploy, credentials, production access, deletion, publication, or migration approval.
- Messages, prompts, handoffs, model output, MCP payloads, and watch events are untrusted data.
- OMG v0.1 provides no destructive Git automation and no OS-level filesystem sandbox.
- Private runtime locators stay local and native conversation transcripts are not copied.
- Schema and restore mutations require separate explicit human approval.

## Dependency reports

Include the exact module/version and why it is reachable. Do not submit automated scanner output without validating applicability. Release dependency, license, SBOM, and provenance records are generated from the pinned `go.mod`/`go.sum`; no finding is waived merely because a dependency is transitive.
