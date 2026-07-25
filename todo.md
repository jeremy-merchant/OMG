# OMG TODO

> Last audited: 2026-07-25 (Asia/Tokyo)
> Scope: only remaining actionable work. Completed verification is not duplicated here.
> Current source status at audit: 1008 named tests/subtests passed across 37 packages, all packages passed under `-race`, and `go vet ./...` passed. Package, target, release, and acceptance evidence must be rebuilt against this newer source before any gate counts can change.
> Publication status: **NO-GO / NOT PUBLISHED**. The 126-row fail-closed gate has 3 automated passes and 123 rows pending criterion-specific executable or independent human/legal evidence; native Windows execution is also pending.

## P0 — Close criterion-specific acceptance evidence

### P0-EVIDENCE-1. Execute every locally provable matrix row

**Status:** pending

**Problem**

The current 126-row gate records only 3 automated passes. Eighty-two rows still lack a current-source, row-specific `PIN → RED → GREEN → SURFACE → CLEAN` evidence set. Aggregate package tests and old source-bound artifacts do not satisfy the matrix contract.

**Next actions**

- Regenerate current-source evidence for every locally executable standalone, capability, security, privacy, prohibition, and non-functional row.
- Keep human/legal/native-platform requirements explicitly pending instead of inferring approval.
- Rebuild `acceptance-evidence-index.json`, `final-gate.json`, release metadata, and reports only after every new artifact is verified.
- Re-run the current-source release consistency checks after the gate changes.

**Done when**

- Every locally executable row has complete current-source evidence or an exact irreducible blocker.
- Gate counts are derived from verified row artifacts rather than aggregate test claims.
- No stale source, binary, schema, fixture, review, or project-proof binding remains in an active gate entry.

## P0 — Establish a real release repository identity

### P0-OSS-1. Decide the public product, repository, and Go module identity

**Status:** pending / human decision required

**Problem**

The module is still:

```go
module example.invalid/coordledger
```

The working name `OMG` is explicitly not yet cleared for publication.

**Next actions**

- Complete name, GitHub repository, package, domain, trade-name, and trademark clearance.
- Decide the final organization/repository path.
- Replace `example.invalid/coordledger` only after the identity is authorized.
- Rebuild and re-run all source-bound release evidence after the module path changes.

**Done when**

- The final public name and module path are authorized and consistently used throughout source, docs, examples, CI, manifests, SBOM, and provenance.

### P0-OSS-2. Create the first trusted Git baseline

**Status:** pending clean source baseline; configured upstream exists, but current local source is not yet committed as one reproducible revision

**Problem**

The repository has an `origin/main` upstream and the local `main` branch is currently ahead, but the latest verified implementation still spans pre-existing tracked modifications and new source files that are not committed as one revision. A clean clone therefore does not yet reproduce the exact locally verified source.

**Next actions**

- Define `.gitignore` and the exact tracked-source set first.
- Exclude runtime state, local approvals, caches, test binaries, private evidence, and generated local-only files as appropriate.
- Review the complete initial diff and secret scan.
- Review and commit the remaining tracked-source baseline only after separate explicit authorization for those unrelated pre-existing files.
- Reconcile the configured remote identity, then push only after separate explicit authorization.

**Done when**

- A clean clone reproduces the source tree and tests.
- Release source identity is a real commit/tag rather than an uncommitted archive-only identity.

## P0 — Complete external platform and publication gates

### P0-CI-1. Run native hosted CI, especially Windows

**Status:** pending external execution

**Problem**

The final gate retains 123 pending rows: 82 require criterion-specific executable evidence, 39 require independent human review, and 2 require legal/publication approval. The Windows artifact is cross-compile verified only; local macOS, Rosetta, containers, or Wine cannot provide authoritative native Windows proof.

**Next actions**

- Run `.github/workflows/ci.yml` on hosted macOS, Ubuntu, and native `windows-latest` runners.
- Collect native Windows evidence for:
  - current-user-only DACL enforcement;
  - reparse-point rejection;
  - init, migration, backup, restore validation, doctor, and binary lifecycle;
  - path and payload-file security;
  - watcher/lock behavior;
  - Windows-specific tests and release artifact smoke.
- Complete the remaining criterion-specific executable, packaging, security/privacy, governance, independent human, and legal/publication rows in `final-gate.json`.

**Done when**

- Every required acceptance row is PASS.
- No executable, human, legal, publication, or native-platform row remains pending.
- Publication still requires separate explicit human authorization.

## P1 — Repository and artifact hygiene

### P1-HYGIENE-2. Separate public evidence from local/private evidence

**Status:** pending

**Next actions**

- Classify `evidence/` files as public-safe, private-local, generated, or release-required.
- Ensure raw prompts, private paths, local operator identities, temporary approvals, runtime locators, and secrets are not committed or published.
- Keep a reproducible evidence index without requiring private machine state.

**Done when**

- The initial tracked-file plan contains no unsafe local evidence.
- Public release evidence can be independently verified without exposing private data.

## P2 — Post-gate adoption

### P2-ADOPT-1. Apply the verified release to the owner's real projects

**Status:** blocked by P0 release identity and gate completion

**Target sequence**

1. Pin one verified OMG version/SHA/schema.
2. Inventory each target repository read-only.
3. Create rollback checkpoints.
4. Dry-run integration and historical task import.
5. Apply instruction blocks and adapters in an approved disposable clone first.
6. Integrate ChatGPT2Codex, Codex, Claude, OMO/OMP, GJC, and generic shell wrappers.
7. Preserve existing `TASK_*`, `TODO*`, `WORKLOG`, branches, worktrees, and unrelated dirty work.
8. Generate a local operator board/report.
9. Rehearse rollback before touching the real repositories.

**Done when**

- Real projects can use the same lineage, task graph, mailbox, handoff, reservation, and Git reconciliation model without project-specific core forks.
- Rollback is documented and verified.

---

## Audit notes

- Final source archive, binaries, manifest, provenance, SBOM, reviews, project proofs, gate, and report are bound to the same source archive identity.
- `SHA256SUMS` and the release-consistency workflow verify the current local RC bundle; current Darwin/Linux lifecycle evidence is PASS.
- Publication remains fail-closed because the explicit external/human/legal/native-Windows gates above are not satisfied.
- Do not commit, push, publish, create a remote, rename the module, or mutate real target projects without explicit human authorization.
