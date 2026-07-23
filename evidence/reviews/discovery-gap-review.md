# Discovery Gap Review — Pre-Fix Artifacts

**Review scope:** This review records the state of the discovery artifacts before remediation. It is not a post-fix approval. Each finding below remains open in this record, and remediation requires a separate independent re-review.

## Verdict

**DISCOVERY REVISE**

Implementation-plan approval remains blocked until a re-review verifies explicit per-apply migration approval, complete D6 domain/security/privacy coverage, identity-bound RC invalidation and both-proof reruns, portable evidence paths, two real Pygmalion applies, and proof harnesses outside core; it must also confirm clean-destination fixture restore and stable public authority links before public-document finalization.

**Overall correctness:** incorrect  
**Review confidence:** 0.99

## Findings

### 1. Gate every schema migration on explicit human approval

- **Priority:** P1 — BLOCKER
- **Confidence:** 0.99
- **Cited anchor:** `docs/adr/0001-language-and-store.md`, `### Schema, migrations, and recovery`, lines 82–86

The authoritative master spec, `## Constraints` line 148, requires separate explicit human approval for any migration, but pre-fix ADR 0001 `### Schema, migrations, and recovery` described automatically backing up and applying every unapplied migration and reserved an approved apply only for restore; pre-fix `OMG-AC-016` also executed migration without appearing in the matrix's human-gated list. Define non-mutating migration status/plan commands and bind each migration apply to its own explicit human approval record before implementation-plan approval.

### 2. Add mandatory security and privacy gates to the matrix

- **Priority:** P1 — BLOCKER
- **Confidence:** 0.99
- **Cited anchor:** `docs/ACCEPTANCE_MATRIX.md`, lines 138–149

`docs/THREAT_MODEL.md` `## 5. Required abuse-case tests` lines 83–108 makes `SEC-T01`–`SEC-T21` and `SEC-E01`–`SEC-E13` release-gate obligations, while `docs/PRIVACY.md` `## 9. Privacy abuse cases and evidence` lines 100–110 defines `PRIV-T01`–`PRIV-T05` and `PRIV-E01`–`PRIV-E03`; the pre-fix matrix source ledger contained none of those IDs. Add executable rows with phase, platform, immutable evidence destination, cleanup, and explicit D6 gating so the RC cannot pass on generic review alone.

### 3. Map all 15 required domain capabilities to acceptance evidence

- **Priority:** P1 — BLOCKER
- **Confidence:** 0.99
- **Cited anchor:** `docs/ACCEPTANCE_MATRIX.md`, lines 140–149

The master spec `### Required domain capabilities` lines 104–119 defines 15 required capabilities, but the pre-fix matrix source ledger jumped directly to runtime/surface clauses and did not map them. Incidental coverage does not prove omitted details such as every mailbox variant, immutable-or-superseding handoffs, append-only progress, or atomic display numbering; add a one-to-one `DOM-01`–`DOM-15` ledger or equivalent traceable procedures and make every row a D6 prerequisite.

### 4. Bind the final RC to the working-name decision

- **Priority:** P1 — BLOCKER
- **Confidence:** 0.99
- **Cited anchor:** `docs/ACCEPTANCE_MATRIX.md`, lines 113–115

`docs/brand/NAME_CLEARANCE.md` `## Provisional status` and `## Publication gate` require a retain-or-rename decision and identify binary, config, environment, protocol, module, and upgrade impacts, while the pre-fix matrix allowed `OMG-AC-019` to create the local RC and `OMG-RG-001` to start both proofs before `OMG-RG-002` completed naming work. Require the private retain/rename decision before the final RC pin and state that any later identity change invalidates that RC plus both Pygmalion/Zoomzi proof and parity artifacts, which must be rebuilt and rerun.

### 5. Use a Windows-safe evidence timestamp

- **Priority:** P1 — BLOCKER
- **Confidence:** 0.99
- **Cited anchor:** `docs/ACCEPTANCE_MATRIX.md`, lines 25–29

The pre-fix evidence directory used `<UTC-RFC3339>`, whose normal representation contains colons, but the master spec requires Windows amd64 and the matrix requires every target runner to publish an evidence subtree. Windows cannot create the mandated directory component; use a portable UTC component such as `YYYYMMDDTHHMMSSZ` and preserve the full RFC3339 value inside metadata.

### 6. Exercise real Pygmalion import idempotency

- **Priority:** P1 — BLOCKER
- **Confidence:** 0.99
- **Cited anchor:** `docs/ACCEPTANCE_MATRIX.md`, lines 79–80

The master Pygmalion proof requires repeated import to be idempotent, but pre-fix `OMG-PYG-004` repeated only `--dry-run` and `OMG-PYG-005` applied once. An importer that plans deterministically but duplicates canonical tasks, events, or receipts on a second real apply would pass; in the approved disposable clone, apply the identical pinned import twice and compare canonical exports, receipt/idempotency behavior, append-only history, and preserved historical-file hashes.

### 7. Move proof adapters outside the general-purpose core

- **Priority:** P1 — BLOCKER
- **Confidence:** 0.99
- **Cited anchor:** `docs/IMPLEMENTATION_PLAN.md`, lines 33–37

The master spec `## Constraints` line 149 requires Pygmalion and Zoomzi adapters to remain outside the general-purpose core, but pre-fix plan §2.1 placed both under `internal/adapter/{pygmalion,zoomzi}` inside the core package tree. A no-import rule does not cure physical/product coupling or prevent proof-specific fixtures and branches from shipping with core; place proof harnesses under an explicit external `proofs/pygmalion` and `proofs/zoomzi` boundary that consumes only released CLI/OMGCP interfaces, and prohibit core imports, branches, fixtures, build tags, or project-specific configuration.

### 8. Restore fixtures into a clean destination

- **Priority:** P2 — LATER CLEANUP
- **Confidence:** 0.99
- **Cited anchor:** `docs/ACCEPTANCE_MATRIX.md`, lines 37–40

Pre-fix cleanup C1 extracted the PIN archive over the mutated fixture. Archive overlay restores overwritten/deleted entries but does not remove files or other state created by the row, so later rows can inherit contamination. Restore into a newly validated disposable destination, compare inventory and checksums with PIN, preserve the mutated copy as evidence if needed, and never use destructive Git cleanup.

### 9. Replace the hidden-session master-spec link

- **Priority:** P3 — LATER CLEANUP
- **Confidence:** 0.99
- **Cited anchor:** `docs/research/UPSTREAM_REUSE_DECISION.md`, line 32

The upstream decision linked directly to `.gjc/_session-019f891e-47ba-7000-aa63-1c14a9629cdb/...`, a hidden session-specific path that will not exist in a normal public source package. Publish a stable authority document under `docs/` and update public-facing discovery documents to link there; retain the hidden master only as private provenance.

## Required Independent Re-Review Checklist

A separate independent re-review of the remediated artifacts must verify all of the following before the discovery review can be approved:

1. Every migration apply has an explicit, separate human approval record, with non-mutating status/plan commands available.
2. The acceptance matrix contains complete, executable D6-gated domain, security, and privacy coverage: `OMG-CAP-001`–`OMG-CAP-015` (or an equivalent one-to-one stable domain ledger), `SEC-T01`–`SEC-T21`, `SEC-E01`–`SEC-E13`, `PRIV-T01`–`PRIV-T05`, and `PRIV-E01`–`PRIV-E03`; each row has phase, platform, immutable evidence destination, and cleanup.
3. The retain-or-rename decision precedes the final RC pin, and any later identity change invalidates the RC plus Pygmalion/Zoomzi proof and parity artifacts, requiring rebuild and rerun.
4. Evidence directory timestamps are portable on Windows, with the full RFC3339 timestamp retained in metadata.
5. The Pygmalion proof performs two real applies of the identical pinned import in an approved disposable clone and verifies canonical exports, receipt/idempotency behavior, append-only history, and historical-file hashes.
6. Pygmalion and Zoomzi proof harnesses are outside the general-purpose core, consume only released CLI/OMGCP interfaces, and do not introduce core imports, branches, fixtures, build tags, or project-specific configuration.
7. Fixture restoration uses a newly validated disposable destination, compares inventory and checksums with PIN, preserves mutated evidence when needed, and does not use destructive Git cleanup.
8. Public-facing discovery documents link to a stable authority document under `docs/`; the hidden session master remains private provenance only.

No finding in this pre-fix review is represented as resolved by this document.