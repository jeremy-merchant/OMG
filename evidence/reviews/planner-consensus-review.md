# Planner Consensus Review — Phase 0 Remediation

**Role and scope:** Planner-only re-review of the decision and execution artifacts. This review evaluates whether the plan is decision-complete, dependency-correct, acceptance-owned, and evidence-addressable. It does not review implementation quality, architecture correctness beyond planning dependencies, security/privacy effectiveness, legal clearance, human approvals, or publication. The working identity remains private and **NOT PUBLISHED**.

**Product authority:** [`docs/PRODUCT_SPEC.md`](../../docs/PRODUCT_SPEC.md).  
**Supporting evidence reviewed:** [`docs/ACCEPTANCE_MATRIX.md`](../../docs/ACCEPTANCE_MATRIX.md), [`docs/IMPLEMENTATION_PLAN.md`](../../docs/IMPLEMENTATION_PLAN.md), ADRs 0001–0003, [`docs/THREAT_MODEL.md`](../../docs/THREAT_MODEL.md), [`docs/SECURITY.md`](../../docs/SECURITY.md), [`docs/PRIVACY.md`](../../docs/PRIVACY.md), [`docs/research/UPSTREAM_REUSE_DECISION.md`](../../docs/research/UPSTREAM_REUSE_DECISION.md), [`docs/brand/NAME_CLEARANCE.md`](../../docs/brand/NAME_CLEARANCE.md), and the pre-fix review [`discovery-gap-review.md`](discovery-gap-review.md).

## Blockers

None identified in this Planner review.

## Planning trace and dependency review

### Product contract and phase ownership

| Product contract block | Plan phase / primary owner | Gate and evidence route | Planner finding |
|---|---|---|---|
| Discovery decisions, reuse, name, ADR, threat, and matrix requirements (`PRODUCT_SPEC.md` §Delivery phases and gates, phase 1) | D1 decisions | D1 gate in `IMPLEMENTATION_PLAN.md` §4 Phase 1; `OMG-RG-002`, decision/scope portions of `OMG-PRO-007` and `OMG-NG-001/004/007/008`; C4 records | The decision prerequisites are explicit and no D1 artifact claims implementation, clearance, publication, or authorization. |
| Foundation: Go CLI, project/store resolution, SQLite, migrations, events/receipts, init/doctor/version/backup (`PRODUCT_SPEC.md` phase 2) | D2, P2-A/B/C | D2 gate and C1 rollback in plan §4 Phase 2; `OMG-AC-001`–`003`, `012`, `016`; foundation NFR/prohibition/capability ownership; matrix PIN→RED→GREEN→SURFACE→CLEAN rows | Package, migration, command, gate, and recovery ownership are named before the parallel lanes begin. |
| Lineage and coordination (`PRODUCT_SPEC.md` phase 3) | D3, P3-A/B | D3 gate and C1/superseding-record rollback in plan §4 Phase 3; `OMG-AC-004`–`008`, `011`; `OMG-CAP-001`–`010` | The frozen vocabulary and P3 split preserve the required lineage, task/run, progress, dependency, mailbox, handoff, and adoption dependencies. |
| Reservations and Git reconciliation (`PRODUCT_SPEC.md` phase 4) | D4, P4-A/B | D4 gate and C1-only rollback in plan §4 Phase 4; `OMG-AC-009`–`010`; `OMG-CAP-011`–`013` | Advisory/plan-only boundaries and non-destructive Git constraints are gated before views and proofs. |
| Integrations and generated views (`PRODUCT_SPEC.md` phase 5) | D5, P5-A/B | D5 gate; C2 integration cleanup and adapter-disable rollback in plan §4 Phase 5; `OMG-AC-013`–`015`, P5 rendering of `007` and `012`, `OMG-CAP-014` | The snapshot/view layer precedes transports; CLI and MCP are required to use shared application services rather than duplicate domain logic. |
| OSS readiness, independent reviews, local RC (`PRODUCT_SPEC.md` phase 6) | D6, P6-A/B | `OMG-RG-001` D1–D6 gate; C4; `OMG-AC-017`–`021`, remaining NFR/release obligations, all required security/privacy ledger IDs | D6 owns the RC evidence and is an explicit prerequisite to either proof phase. It retains `NOT PUBLISHED`. |
| Pygmalion proof (`PRODUCT_SPEC.md` phase 7) | D7, external `proofs/pygmalion` harness | D6 RC gate plus explicit limited disposable-clone approval, `OMG-PYG-001`–`008`, C3 | D7 is separately gated and cannot start from a plan or a dry run. |
| Zoomzi proof (`PRODUCT_SPEC.md` phase 8) | D8, external `proofs/zoomzi` harness | Completed Pygmalion evidence plus explicit Zoomzi disposable-clone approval, `OMG-ZOO-001`–`008`, C3/C4 | D8 follows D7, uses the same RC pin, and owns parity evidence. |

### D1–D8 ordering and boundary checks

- The DAG in `IMPLEMENTATION_PLAN.md` §3 makes D1 → P2-A → P2-B a prerequisite chain; P3/P4 wait for frozen P2-A contracts, P5 waits for P3/P4 facts, P6 waits for the named retain/rename decision, D7 waits for the D6 RC gate and proof authorization, and D8 waits for completed Pygmalion evidence and its own authorization.
- Plan §2.1 fixes the core dependency direction and places the proof harnesses at `proofs/pygmalion` and `proofs/zoomzi`, outside core package/module boundaries. It expressly prohibits core imports, build tags, conditional branches, fixtures, and project-specific configuration for either proof project.
- Plan §2.3 assigns migration files and schema responsibility without overlap: P2-B owns immutable `0001_foundation.sql` and runner/checksum rules; P3 owns `0002_coordination.sql`; P4 owns `0003_reconciliation.sql`; proof harnesses write no migrations. Plan §3 gives each parallel lane exclusive file scope and routes shared contract changes back to P2-A/P5-A.
- Plan §2.3 and §4 Phase 2 preserve the explicit-schema-migration boundary from ADR 0001 §Schema, migrations, and recovery: opening/status is non-mutating; `migration plan` creates a stable plan ID; every `migration apply` requires a separate approval PIN bound to the exact project/fixture, from/to versions, checksums, verified backup checksum/destination, command, and UTC time.
- `PRODUCT_SPEC.md` §Delivery phases and gates, matrix §Gate interpretation, and plan §4 Phases 6–8 all require the standalone D6/RC gate before D7 or D8. D8 further requires completed Pygmalion evidence, so no proof may establish the RC it is meant to consume.
- The working-name-to-RC order is explicit in `PRODUCT_SPEC.md` §Delivery phases and gates, matrix `OMG-AC-019`/`OMG-RG-002` and §Gate interpretation, and plan §1 rule 2 and §4 Phase 6: a named human retain/rename decision precedes the final local RC pin. A later identity change invalidates the RC and all dependent Pygmalion, Zoomzi, and parity artifacts; replacement requires rebuild and both proofs/parity rerun. This is not legal clearance or publication approval.
- Cross-platform claims are correctly separated from local evidence. Matrix §Common execution contract defines portable `YYYYMMDDTHHMMSSZ` directory components while retaining full RFC3339 in metadata; §Gate interpretation names CI-only IDs/portions and requires a subtree from every required runner. Plan §1 rule 6 prohibits treating a local result as the five-target evidence.
- Rollback remains bounded: C1 restores only to a newly validated disposable destination and compares PIN inventory/checksums; C2 changes only integration markers; C3 is an approved disposable proof-clone rehearsal; C4 archives/reports without public action. Plan §§4 and 6 prohibit destructive Git cleanup and real-project mutation.

### Required domain-capability ledger

The matrix §F defines one D6-required, executable row with phase, platform, `E(<ID>)` evidence destination, and cleanup for every product capability. Plan §4 and §5 assign the corresponding primary owner and phase.

| Capability IDs | Product requirement | Plan owner / phase | Gate / evidence |
|---|---|---|---|
| `OMG-CAP-001`–`004` | Human provenance; complete session record; five lineage semantics; one-time TTL verifier-only delegation | P3-A / D3 | D3 gate; C1; rows `OMG-CAP-001`–`004`; D6 prerequisite |
| `OMG-CAP-005`–`006` | Atomic numbering/exclusive claim; task/run distinction | P3-A / D3 | D3 gate; C1; rows `OMG-CAP-005`–`006`; D6 prerequisite |
| `OMG-CAP-007`–`010` | Append-only progress; dependency/unblock; typed mailbox; immutable/superseding handoff | P3-B / D3 | D3 gate; C1; rows `OMG-CAP-007`–`010`; D6 prerequisite |
| `OMG-CAP-011`–`012` | Reservation lifecycle/audit; read-only Git reconciliation | P4-A/B / D4 | D4 gate; C1; rows `OMG-CAP-011`–`012`; D6 prerequisite |
| `OMG-CAP-013` | Non-destructive orphan adoption | P4-A/B / D4 (with D3 dependency) | D4 gate; C1; row `OMG-CAP-013`; D6 prerequisite |
| `OMG-CAP-014` | Canonical TTY/JSON/Markdown/self-contained HTML views | P5-A/B / D5 | D5 gate; C0; row `OMG-CAP-014`; D6 prerequisite |
| `OMG-CAP-015` | Events, receipts, idempotency, backup/integrity, fail-closed newer schema | P2-A/B/C / D2 | D2 gate; C1; row `OMG-CAP-015`; D6 prerequisite |

### Security, privacy, acceptance, and release evidence ownership

- Matrix §G provides executable rows with phase/platform/evidence/cleanup for `SEC-T01`–`SEC-T21`, `SEC-E01`–`SEC-E13`, `PRIV-T01`–`PRIV-T05`, and `PRIV-E01`–`PRIV-E03`. Matrix §Gate interpretation states that D6 cannot pass without their GREEN evidence and required human dispositions. Plan §4 Phase 6 and §5 assign the entire set to P6-A/B.
- Matrix §Source coverage ledger accounts for every mandatory acceptance-ID range: `OMG-AC-001`–`021`, `OMG-PYG-001`–`008`, `OMG-ZOO-001`–`008`, `OMG-NFR-001`–`011`, `OMG-RG-001`–`003`, `OMG-PRO-001`–`008`, `OMG-NG-001`–`008`, `OMG-CAP-001`–`015`, all security IDs, and all privacy IDs. Plan §5 maps each range to one primary owner; cross-phase rendering/release references do not change that owner.
- The matrix’s common execution contract makes evidence immutable and uses one of C0–C4 for every row. Human-gated matrix rows remain expressly unperformed without their named approval. No plan, inventory, dry run, evidence path, reviewer assignment, or Planner verdict substitutes for a human action.

## Pre-fix re-review checklist

| Checklist item from `discovery-gap-review.md` §Required Independent Re-Review Checklist | Re-review evidence | Planner finding |
|---|---|---|
| 1. Separate explicit approval for every migration apply; non-mutating status/plan | ADR 0001 §Schema, migrations, and recovery; matrix `OMG-AC-016`, `SEC-T14`–`015`, `PRIV-T05`, and §Gate interpretation; plan §1 rule 4 and §4 Phase 2 | Addressed in the plan. Approval binding is per apply, and discovery/status/plan are non-mutating. |
| 2. Complete D6 domain/security/privacy coverage with phase, platform, immutable destination, cleanup | Matrix §§F–G, §Source coverage ledger, and §Gate interpretation; plan §4 Phase 6 and §5 | Addressed in the plan. All required ID ranges are present and expressly D6-gated. |
| 3. Retain/rename before RC; identity change invalidates RC and both proof/parity artifacts | Matrix `OMG-AC-019`, `OMG-RG-002`, §Gate interpretation; plan §1 rule 2 and §4 Phase 6 | Addressed in the plan, including rebuild and both-proof/parity rerun requirement. |
| 4. Windows-portable evidence timestamp with full RFC3339 metadata | Matrix §Evidence paths and cleanup codes; plan §1 rule 5 | Addressed in the plan. The directory component is portable UTC and metadata retains the full instant. |
| 5. Two real identical pinned Pygmalion applies with canonical state, receipts/idempotency, history, and historical-file hashes | Matrix `OMG-PYG-004`; plan §4 Phase 7 | Addressed in the plan. A reviewed non-mutating dry run precedes explicit import approval; the identical pinned import is then applied twice in a disposable clone with normalized exports, receipt/idempotency-set and history comparison, and preserved historical hashes. |
| 6. Proof harnesses outside core with only released interfaces and no core coupling | Plan §2.1 and §4 Phases 7–8; matrix `OMG-PYG-007`, `OMG-ZOO-002`, `OMG-ZOO-006`, `OMG-PRO-008` | Addressed in the plan. The physical/package boundary and prohibited coupling forms are explicit. |
| 7. Clean-destination fixture restore with inventory/checksum comparison and no destructive cleanup | Matrix C1 in §Evidence paths and cleanup codes; plan §§1 and 4 | Addressed in the plan. C1 requires a new validated disposable destination and recorded comparison. |
| 8. Public discovery links to stable `docs/` authority; hidden master only private provenance | `UPSTREAM_REUSE_DECISION.md` §OMG evaluation baseline links `docs/PRODUCT_SPEC.md`; matrix §Gate interpretation; plan §5/§6 | Addressed in the plan. Stable public authority is identified and private provenance is excluded from public links. |

## Non-blocking notes

1. This review records planning adequacy only. Future architecture, critic/code-goal, QA, and security/privacy reviews retain their independent dispositions and must not be inferred from this verdict.
2. No current artifact is treated as evidence that a migration, proof action, review disposition, publication, legal clearance, or other human-gated action occurred.

APPROVE
