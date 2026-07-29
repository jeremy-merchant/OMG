# ADR 0004: Git-Native Coordination Boundary

- **Status:** Accepted
- **Date:** 2026-07-29
- **Decision owners:** OMG maintainers
- **Amends:** ADR 0003

## Context

Git already owns code and document contents, commits, refs, branches, worktrees, tags, reflogs, diffs, and merge relationships. Persisting those facts as a second repository model makes OMG larger, slower, and easier to mistake for an authority over Git.

OMG still needs facts Git does not contain: human and agent lineage, task assignment, dependencies, typed coordination messages, approval decisions, external verification receipts, policy gates, and the relationship between a source SHA and an independently accepted integration/canary result.

## Decision

Git is the single source of truth for repository state. OMG is a thin coordination and policy control plane above Git.

1. `omg git current` scans the selected repository and linked worktrees live. It overlays coordination ownership in memory and writes no Git observation, audit event, or command receipt.
2. `omg git cleanup-plan` also derives its advisory result from a fresh, non-persisted Git scan.
3. `omg git reconcile`, exact-SHA canary checks, and orphan detection verify recorded coordination claims against live Git objects and refs.
4. `omg git inventory` is an explicit point-in-time evidence capture. `latest`, `history`, and OMG's observation `diff` operate only on that recorded evidence; they are not substitutes for `git log`, `git diff`, reflog, refs, branches, or worktrees.
5. JSON Git summaries identify `authoritative_source: "git"` and distinguish `source: "git_live", durable: false` from `source: "recorded_evidence", durable: true`.
6. OMG does not implement checkout, merge, rebase, commit, push, reset, clean, branch deletion, or worktree deletion. Any future Git mutation must be separately authorized and delegated to Git rather than mirrored in OMG state.
7. SQLite remains canonical only for OMG coordination and policy facts. A durable Git observation is canonical evidence that an observation occurred, not canonical repository state.

Project or production migrations are privileged external actions and require an action-specific approval mechanism at the execution boundary. OMG's own exact compiled SQLite schema upgrades are a narrower local maintenance exception: the automatic safe policy requires a plan-bound verified backup, exact compiled checksums, atomic apply, and integrity verification. The manual `omg migration apply` path continues to require and validate a matching approval file in code.

## Consequences

- Routine inspection does not grow the OMG database.
- A fresh repository can use `git current` without first recording an inventory.
- Operators use native Git for code history and diffs, and OMG for ownership, policy, external verification, and cross-session decisions.
- Existing recorded observations remain readable for compatibility and audit; no historical rows are deleted by this decision.
- Boards may display the latest recorded evidence, but must label it as observed evidence and never as live Git authority.

## Testable acceptance checks

1. Repeating `omg git current` changes no observation-history count and invokes the live scanner each time.
2. `omg git cleanup-plan` invokes the live scanner and creates no durable observation.
3. `git current` reports `git_live`, `durable:false`, and `authoritative_source:git`; `inventory/latest/history` report recorded evidence and `durable:true`.
4. Reconciliation and orphan scans use current Git objects/refs and do not inspect unrelated repositories.
5. Manual migration apply without an exact plan and matching approval file remains rejected by executable code.
