# Critic Re-review — C1 Brand-Acceptance Remediation

**Role and scope:** Critic-only adversarial re-review of the current product authority, acceptance matrix, execution plan, ADR/review gate record, and name-gate contract. This evaluates whether the specified future execution contract is complete and internally consistent. It makes no implementation, proof, QA, security, privacy, legal-clearance, publication, or human-approval claim. Working identity remains private and **NOT PUBLISHED**.

## Review order and retained history

1. [`planner-consensus-review.md`](planner-consensus-review.md) records Planner **APPROVE**.
2. [`architecture-review.md`](architecture-review.md) retains the initial Architect **REVISE** with A1 and A2.
3. [`architecture-rereview.md`](architecture-rereview.md) records the subsequent Architect **APPROVE**, with A1/A2 corrected.
4. This Critic re-review follows that Planner → Architect **APPROVE** sequence. The immutable prior Critic artifact, [`critic-review.md`](critic-review.md), remains historical **REVISE** for C1 and is not overwritten by this artifact.

**Authority order checked:** `docs/PRODUCT_SPEC.md` is the stable product authority (`PRODUCT_SPEC.md:3-5`). The matrix and plan expressly subordinate themselves to it (`ACCEPTANCE_MATRIX.md:3-7`; `IMPLEMENTATION_PLAN.md:3-5`). The private master material is retained provenance only and must not be a public deliverable link (`ACCEPTANCE_MATRIX.md:34`, `241-243`; `IMPLEMENTATION_PLAN.md:229,235`).

## C1 remediation re-check

| Requirement under review | Current contract anchors | Critic disposition |
|---|---|---|
| Executable brand acceptance structure | Matrix `OMG-BR-001` and `OMG-BR-002` each specify PIN → RED → GREEN → SURFACE → CLEAN, `E(<ID>)`, C4 cleanup, Local/offline platform, and D6 ownership (`ACCEPTANCE_MATRIX.md:99-105`; `21-44`). | Addressed. |
| Concept, 24×24 form, shape distinction, prohibited motifs | `OMG-BR-001` names the monochrome-first enclosing O, M-like delegated branches, returning G-like handoff path, shape-not-color distinction, and all listed exclusions (`ACCEPTANCE_MATRIX.md:103`). P6 repeats the same future validation procedure (`IMPLEMENTATION_PLAN.md:181-182`). | Partially addressed; see C1-R1. |
| Required asset package and final-source constraints | `OMG-BR-002` requires primary/mono mark, wordmark, horizontal/stacked lockups, favicon, 16/32/128/512 PNGs, social preview, light/dark previews, clean vector paths, provenance, font-license record, and mood-only AI treatment (`ACCEPTANCE_MATRIX.md:104`). P6 enumerates the same package and assigns both IDs to P6-A/B (`IMPLEMENTATION_PLAN.md:181-187,225`). | Addressed as a future contract. |
| D6/RC/proof gating and ownership | Brand IDs are D6 prerequisites in `OMG-RG-001` (`ACCEPTANCE_MATRIX.md:121-123`), the source ledger (`224,233`), Gate interpretation (`239-242`), and P6 gate (`IMPLEMENTATION_PLAN.md:185,191`). D7/D8 remain downstream in the DAG (`IMPLEMENTATION_PLAN.md:83-104`). | Addressed. |
| Source-ledger, evidence portability, and safe cleanup | Matrix has immutable portable evidence destinations with UTC directory stamps/full RFC3339 metadata (`ACCEPTANCE_MATRIX.md:21-34`), CI runner isolation where applicable (`237-239`), and C4 is archive/report-only with `NOT PUBLISHED` (`36-42`). | Addressed. |
| No clearance or publication implication | Product authority retains `NOT PUBLISHED` and a separate publication gate (`PRODUCT_SPEC.md:5,165,170`). The name contract calls the status publication-blocked and expressly disclaims legal clearance (`NAME_CLEARANCE.md:7-13,25-27,66-76`). Brand rows and P6 record `NOT PUBLISHED`; they do not state clearance (`ACCEPTANCE_MATRIX.md:103-104`; `IMPLEMENTATION_PLAN.md:181-192`). | Addressed. |

## Blocking finding

| # | Exact anchors | Adversarial finding | Minimal correction |
|---|---|---|---|
| C1-R1 | Product authority: `PRODUCT_SPEC.md:174`; brand rows: `ACCEPTANCE_MATRIX.md:103-104`; P6 procedure: `IMPLEMENTATION_PLAN.md:181-182`; source ledger: `ACCEPTANCE_MATRIX.md:224` | The Product Spec independently requires replacing the **former K-shaped concept**. The remediated rows and P6 procedure require originality and the O/M/G concept, but neither PIN, RED, GREEN, SURFACE, expected result, nor source-ledger coverage explicitly rejects or verifies replacement of the former K-shaped concept. An original candidate can still be K-shaped; originality is therefore not a lossless substitute for that separate requirement. C1 is not fully resolved. | Add an explicit former-K-shaped-concept rejection to `OMG-BR-001` RED, an affirmative replacement check to GREEN/SURFACE/expected result, and state that coverage in the brand source-ledger entry and P6 procedure. Preserve the existing evidence route, C4, Local/offline/D6 labels, and `NOT PUBLISHED` boundary. |

## Full-contract adversarial re-checks without an additional blocker

- **Acceptance ranges and primary ownership:** the matrix lists all mandatory acceptance, proof, NFR, release-gate, prohibition, non-goal, capability, security, and privacy ranges (`ACCEPTANCE_MATRIX.md:216-233`); the plan assigns each range once with cross-phase references non-transferring (`IMPLEMENTATION_PLAN.md:214-229`).
- **DAG and identity invalidation:** D1 → P2 → P3/P4 → P5 → retain/rename decision → P6/D6 → P7 → P8 is expressed in the plan DAG and phase prerequisites (`IMPLEMENTATION_PLAN.md:81-115,189-211`). The retain/rename decision precedes the local RC pin; a later identity change invalidates dependent RC, proof, and parity artifacts (`PRODUCT_SPEC.md:5,165`; `ACCEPTANCE_MATRIX.md:240-242`; `IMPLEMENTATION_PLAN.md:9-14,189-191`). This re-review does not treat that decision as clearance, approval, or publication.
- **Migration/restore approval boundary:** each migration apply has an exact approval PIN bound to fixture/project, versions, checksums, verified backup, command, and UTC time; plan/status remain non-mutating without it (`ACCEPTANCE_MATRIX.md:66,239-240`; `IMPLEMENTATION_PLAN.md:12,71,131,137-140`). Restore is constrained to a fresh validated disposable destination with inventory/checksum comparison, never an overlay or destructive Git cleanup (`ACCEPTANCE_MATRIX.md:38-42`).
- **Proof isolation and double application:** proof harnesses are outside core and limited to released CLI/OMGCP interfaces; project-specific core coupling is prohibited (`IMPLEMENTATION_PLAN.md:20-38,79,196-212`). The Pygmalion contract requires two real applications of the same approved pinned import, with normalized canonical state, receipts/idempotency sets, append-only history, and historical hashes compared (`ACCEPTANCE_MATRIX.md:80`; `IMPLEMENTATION_PLAN.md:196-202`).
- **Security/privacy D6 gate:** all `OMG-CAP-*`, `SEC-T*`, `SEC-E*`, `PRIV-T*`, and `PRIV-E*` ranges are D6 prerequisites, with required dispositions not substituted by plans, dry runs, or evidence paths (`ACCEPTANCE_MATRIX.md:147-214,239-241`; `IMPLEMENTATION_PLAN.md:185,225`).
- **A1/A2 resolution:** the current plan gives immutable snapshot selection/authorization/redaction/order to `internal/app/query`, leaves `internal/view` a pure consumer, and forbids `app → view` (`IMPLEMENTATION_PLAN.md:20-38,65,166-177`). It also gives P4-A and P4-B exclusive `0003_reservations.sql` and `0004_git_assets.sql` ownership, while P2-B alone runs them in fixed order after the separate approval path (`IMPLEMENTATION_PLAN.md:69-72,106-115,154-164`). The Architecture re-review records these as resolved (`architecture-rereview.md:9-12,22-26`).

REVISE
