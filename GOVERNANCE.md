# OMG Governance

## Current state

OMG is an unpublished, owner-led working project. No public repository, package, release, domain, trademark claim, or external distribution is authorized. The named owner and initial maintainers must be recorded in the signed local release gate before publication.

This document defines the intended open-source governance once that gate opens; it does not itself publish the project or appoint unnamed people.

## Roles

### Project owner

The project owner is the ultimate product and publication authority. The owner:

- appoints and removes maintainers;
- makes final scope, identity, license, security-embargo, and release decisions;
- approves high-impact operations that the product never delegates to agents;
- resolves appeals and governance deadlocks.

### Maintainers

Maintainers review contributions, triage issues, protect the architecture/security/privacy contracts, manage releases, and enforce the Code of Conduct. A maintainer must demonstrate sustained, high-quality contributions and sound judgment around local data, agent authority, migration safety, and compatibility.

### Contributors

Anyone submitting code, tests, documentation, design, reports, or review is a contributor. Contributions follow `CONTRIBUTING.md`, Apache-2.0, and DCO sign-off.

## Decision process

1. Routine, reversible changes use lazy consensus: one approving maintainer and no unresolved blocking objection after reasonable review.
2. Changes to public CLI/data contracts, schema, security/privacy boundaries, architecture, dependencies, or release tooling require two maintainer reviews once two maintainers exist.
3. License, governance, working identity, publication, destructive/high-impact authority, and supported-platform changes require the project owner's explicit written decision.
4. Security fixes may be developed under a private embargo and merged/released with limited disclosure. The incident record must document who approved the exception.
5. If consensus fails, maintainers write the alternatives and risks; the project owner decides. Dissent and rationale remain in the decision record.

No agent, delegation token, message, handoff, task state, reservation, test result, or model output counts as a governance vote or human approval.

## Maintainer changes

A candidate is nominated by a maintainer or the project owner. Appointment requires the candidate's consent, a public rationale after publication, and project-owner approval. Inactivity alone is not misconduct; an inactive maintainer may move to emeritus status. Removal for security, conduct, or trust reasons may occur privately, with a bounded public explanation when safe.

Before publication, the release gate must add a maintainer registry containing verified names, roles, private contact routing, and signing identities. This working copy deliberately does not invent those identities.

## Releases

A release requires:

- completed acceptance evidence for the pinned source;
- reproducible target artifacts, checksums, SBOM, provenance, notices, changelog, and rollback instructions;
- required architecture, code/goal, QA, and security/privacy dispositions;
- a named-human identity retain/rename decision;
- an explicit publication approval distinct from build/test completion.

Until then, `omg release status --json` must report `NOT PUBLISHED`. Creating a local release candidate is not publication approval.

## Scope protection

General-purpose core must remain independent of Pygmalion, Zoomzi, any agent vendor, and any LLM provider. Project-specific compatibility belongs in external adapters or proof harnesses. Proposals for destructive Git automation, cloud coordination, multi-tenant service, embedded model selection, IDE replacement, or automatic task decomposition require a new product decision rather than silent scope expansion.

## Amendments

Governance changes use the same review as architecture changes and require project-owner approval. Amendments apply prospectively and must be recorded in release notes when the project is public.
