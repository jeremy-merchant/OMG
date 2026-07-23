# OMG v0.1.0-rc.1 — Local Release Candidate

**STATUS: NOT PUBLISHED**

This is a private, local validation candidate. It creates no public release, tag, registry package, remote repository, domain transaction, signature, or distribution approval.

## Candidate scope

- One CGO-free Go binary for macOS arm64/amd64, Linux arm64/amd64, and Windows amd64.
- Local SQLite coordination ledger with human/agent lineage, tasks and runs, progress, dependencies, typed mailbox/ACK, handoffs, path reservations, Git inventory, recovery planning, boards, exports, instruction-surface integration, generic runtime wrappers, optional watch mode, and MCP stdio parity.
- Safe migration planning with checksums, verified backup binding, and separately approved apply.
- Default redaction of prompts, messages, final output, private paths, runtime homes, opaque native references, and secret-like values.
- Original local OMG geometric brand candidate and deterministic SVG/PNG package.

## Artifact verification

Run `shasum -a 256 -c SHA256SUMS` from this directory. Select only the binary whose OS and architecture match the machine. Then run:

```text
./omg-v0.1.0-rc.1-<os>-<arch> version --json
./omg-v0.1.0-rc.1-<os>-<arch> release status --json
```

The expected version is `v0.1.0-rc.1`; the expected release status is `NOT PUBLISHED`.

## Supply-chain records

- `SHA256SUMS`: digest for every RC bundle file except itself and the immutable release manifest that records it.
- `sbom.spdx.json`: SPDX 2.3 inventory of the application and the 12 modules embedded in the stripped binary.
- `THIRD_PARTY_NOTICES.txt`: exact module versions and verbatim root license files used by those modules.
- `provenance.intoto.json`: in-toto Statement v1 with SLSA provenance v1 predicate for the source archive and five binaries.
- `install-manifest.draft.json`: explicit local-only install draft; it performs no installation.
- `release-manifest.json`: local immutable candidate inventory and status.

## Working-name decision and limits

The accountable local operator `kiunlee` retained `OMG` only for this private RC. Exact public package/CLI namespace collisions remain documented in `docs/brand/NAME_CLEARANCE.md`. This is not legal or trademark clearance. Public repository creation, package publication, domain acquisition, and distribution remain blocked.

The source repository has no `HEAD` commit. Therefore the SHA-256 digest of `omg-v0.1.0-rc.1-source.tar.gz` is the canonical source identity for this candidate. A later identity or source change invalidates this RC and every proof or parity artifact derived from it.

## Known verification boundary

The exact extracted source passed 883 tests in 35 packages, and the same 883 tests under Go's race detector, with zero failures; `go vet ./...` is clean. The extracted-source build and `version --json` check passed. These results do not complete the release lifecycle: the current evidence gate records 33 executable, 32 human-review, two legal-publication, and six native-platform items pending, and an independent QA rerun remains required. Native runner evidence is absent for darwin/amd64, linux/amd64, linux/arm64, and windows/amd64; cross-compilation establishes buildability only, not native execution. No human approval for high-impact operations, migration application, legal/name clearance, or publication has been recorded. The current evidence gate, rather than this immutable candidate bundle, is the authoritative record of those mutable dispositions.

## Rollback

No installation or publication occurred while producing this bundle. Follow `ROLLBACK.md`. Preserve `.omg` state, databases, backups, and evidence unless a separate retention decision authorizes removal.
