# Post-fix QA review

- **Reviewer:** PostFixQA-2
- **Reviewed:** 2026-07-23T03:07:51Z
- **Source state:** `current-workspace-post-fix`
- **Verdict:** **PASSED**
- **Publication:** **NOT PUBLISHED**

## Scope

Reviewed current post-fix regression-test source and persisted verification evidence for restore planning, migration approvals, delegation-token handling, SQLite policy, contention/replay behavior, Windows DACL/reparse boundaries, acceptance traceability, CI configuration, and human gates. This reviewer ran no full suite, formatter, linter, build, or source-modifying command; existing current-workspace evidence was sufficient.

## Automated local proof

- `evidence/release/sec-priv-source-test-map.json` identifies `current-workspace-post-fix`, records 26 automated security/privacy entries as PASS, a race run of 465 tests across 34 packages, clean `go vet`, and successful cross-builds for Darwin arm64/amd64, Linux arm64/amd64, and Windows amd64. Its modification time follows the current Go source/test files inspected.
- Restore coverage in `internal/app/foundation/restore_test.go` and SQLite backup inspection validates checksum, integrity, compatibility, safe absolute/distinct paths, fresh destinations, corrupt/incompatible inputs, and no destination mutation. The map traces this to SEC-T15.
- Token coverage in `internal/app/lineage/security_test.go`, `internal/safety/safety_test.go`, configuration, foundation, and dependency tests covers verifier-only persistence; wrong, expired, revoked, reused, and wrong-project tokens; 32-worker single-winner redemption; suffixed-token detection/redaction; and rejection before mutation. SEC-T06 and SEC-T07 are explicitly mapped.
- SQLite coverage in `configuration_test.go`, `migration_approval_acceptance_test.go`, `store_security_test.go`, and `scoped_receipts_audit_migration_test.go` exercises foreign keys, busy timeout, WAL/DELETE policy, bounded deterministic cancellation-aware retries, divergent/newer schema rejection, approval timing/binding/atomic consumption, replay, failed-apply rollback with retained backup, URI-significant paths, and the v4-to-v5 receipt/audit migration. SEC-T14, SEC-T15, and SEC-T17 are explicitly mapped.
- Claim/dependency source includes single-winner contention, cycle rejection, criterion-driven unblock, replay, and exactly-once notification assertions corresponding to SEC-T16, OMG-AC-005/006, and OMG-CAP-005/008.

## Windows-native proof

1. `20260723T024125Z/results.json` retained its overall FAIL, but native Windows/amd64 focused tests for `windowsacl.IsPrivate`, SQLite secure-state DACL, and watch ACL/reparse/lock/lease passed. Its migration-plan failure was not concealed.
2. `20260723T024823Z/results.json` and transcript show the follow-up native build passing focused CLI migration-output, private-payload, and non-argv token-transport tests; rejecting a broad output parent; accepting a private DACL parent; creating the plan and backup; and correctly refusing apply without approval.
3. `20260723T025251Z/results.json` and transcript show fresh init, private-parent plan, backup, bounded approved apply, consumed-approval replay rejection, doctor integrity, and cleanup all passing. Coordination read/write smoke was explicitly not run and is not claimed here.

## CI-only limitations

- `.github/workflows/ci.yml` configures Ubuntu, macOS, and Windows vet/race/native lifecycle jobs, five required native OS/architecture smoke runners, and a separate five-target cross-build matrix.
- No hosted run exists for the exact current source. The acceptance matrix therefore continues to classify CI-All/platform-wide assertions separately.
- `evidence/release/final-gate.json` retains 49 pending external criteria and `NOT PUBLISHED`. It and `FINAL_REPORT.md` predate the post-fix native evidence and still describe native Windows as wholly pending; neither proves the hosted CI-All gate passed.

## Human-gated items

- OMG-AC-016 and OMG-CAP-015 keep migration or external restore mutation approval-bound.
- OMG-AC-020 remains reviewer-gated. OMG-AC-021 and OMG-RG-003 keep publication `NOT PUBLISHED` without explicit authorization; OMG-RG-001/002 retain release, identity, legal, and human gates.
- The latest native apply used a ten-minute approval bound to the exact plan and backup checksum, and replay was rejected. That disposable-fixture approval authorizes no publication, deployment, or other external mutation.

## Acceptance traceability

`docs/ACCEPTANCE_MATRIX.md` defines stable PIN/RED/GREEN/SURFACE/CLEAN procedures, platform labels, and human gates. The current source-test map links the reviewed token, migration, recovery, claim-race, and SQLite-policy tests to SEC-T06, SEC-T07, SEC-T14, SEC-T15, SEC-T16, and SEC-T17; the OMG-CAP ledger independently names the corresponding observable behaviors.

## Findings

No QA blocker found within the reviewed local and native evidence scopes.

## Remaining risks

- Hosted exact-source CI-All and other platform-wide criteria remain publication blockers.
- Native coordination read/write smoke remains for hosted Windows coverage.
- The latest native artifacts are not cryptographically bound to a final current-workspace archive. Comparison with the `20260723T024823Z` snapshot found only a behavior-preserving retry-delay extraction in production plus test additions, but exact-source hosted CI and regenerated release evidence are still required.
- `final-gate.json` and `FINAL_REPORT.md` have older test totals and pre-fix native status and must be regenerated before final release use.
- Sixteen independent-review entries in the current security/privacy map were still pending when inspected; this artifact satisfies only QA review.

**Disposition:** QA is **PASSED** for the current post-fix workspace. This does not convert any CI-only, human/legal, other-reviewer, or publication gate to PASS.
