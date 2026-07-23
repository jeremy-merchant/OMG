# OMG v0.1.0-rc.1 Local Rollback

**STATUS: NOT PUBLISHED**

Production installation and publication are outside this candidate. This procedure applies only after an operator manually copies one verified RC binary into an explicit local destination.

1. Record the installed destination, file size, and SHA-256 digest. Compare the digest with `SHA256SUMS` and `install-manifest.draft.json`.
2. Stop starting new OMG commands. If optional watch is active, request a graceful stop and confirm its owner stopped; do not kill an unverified PID.
3. Copy the installed executable to an operator-chosen backup location before replacement or removal.
4. Restore the prior executable only when its expected digest and target destination are known. Do not resolve an `omg` package from a registry.
5. Run the restored binary's `version --json` and `doctor --integrity --json` against a disposable fixture before using it on retained state.
6. Preserve `.omg` configuration, SQLite state, migration backups, approval records, and evidence. Binary rollback does not authorize database downgrade, restore apply, secure deletion, Git reset/clean, branch/worktree removal, or unrelated file deletion.
7. If the candidate was never copied, rollback is a no-op: retain or archive this local bundle and keep `NOT PUBLISHED`.

Any schema restore or migration change requires its own compatibility/integrity-validated plan, verified backup, and exact human approval. A failed or ambiguous check must leave canonical state untouched and return to manual review.
