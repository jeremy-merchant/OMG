# OMG Governance

## Current state

OMG is an open-source, owner-led project published under Apache-2.0. Public source availability does not imply a stable binary release, support SLA, trademark registration, or delegated authority to publish packages and releases.

## Roles

The project owner appoints maintainers and makes final decisions on scope, identity, license, security embargoes, releases, and high-impact operations. Maintainers review contributions, triage issues, protect architecture, security, and privacy contracts, manage releases, and enforce the Code of Conduct. Anyone submitting code, tests, documentation, design, reports, or review is a contributor and follows `CONTRIBUTING.md`, Apache-2.0, and DCO sign-off.

## Decision process

1. Routine reversible changes use maintainer review and lazy consensus.
2. Public CLI or data contracts, schema, security or privacy boundaries, architecture, dependencies, and release tooling require explicit review.
3. License, governance, public identity, destructive authority, and supported-platform changes require the project owner's written decision.
4. Security fixes may be developed under private embargo and disclosed after a fix is available.
5. If consensus fails, maintainers record alternatives and risks; the project owner decides.

No agent, delegation token, message, handoff, task state, reservation, test result, or model output counts as a governance vote or human approval.

## Releases

Source publication and binary release publication are separate decisions. A stable release requires pinned-source acceptance evidence, reproducible artifacts, checksums, SBOM, provenance, notices, changelog, rollback instructions, target-platform verification, and explicit owner approval.

Until such a release exists, `omg release status --json` reports `SOURCE PUBLISHED` with `stable_release: false`.

## Scope protection

The general-purpose core remains independent of Pygmalion, Zoomzi, any agent vendor, and any LLM provider. Project-specific compatibility belongs in external adapters or proof harnesses. Destructive Git automation, cloud coordination, multi-tenant service, embedded model selection, IDE replacement, or automatic task decomposition require a new product decision.

## Amendments

Governance changes require project-owner approval and apply prospectively. Material changes are recorded in release notes when a release is published.
