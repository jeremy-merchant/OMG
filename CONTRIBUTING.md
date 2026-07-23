# Contributing to OMG

OMG is currently an unpublished working project. Do not create a public mirror, package, release, domain, or announcement from this checkout. Once the publication gate opens, contributions are welcome under the process below.

## Before starting

1. Read `README.md`, `docs/PRODUCT_SPEC.md`, `docs/SECURITY.md`, `docs/PRIVACY.md`, and the applicable ADRs.
2. Search existing issues and decisions before proposing a second architecture.
3. For a security or privacy issue, use the private process in `SECURITY.md`; do not open a public issue with exploit or secret details.
4. Keep Pygmalion- and Zoomzi-specific adapters outside the general-purpose core.

## Development requirements

- Go version: the version declared in `go.mod`.
- Build release-compatible code with `CGO_ENABLED=0`.
- Run `gofmt` on changed Go files.
- Preserve layer direction: transport → application → domain; infrastructure implements ports; renderers consume application-owned view models.
- Keep canonical state in SQLite and mutations behind transactional application services.
- Keep core behavior daemonless and network-independent.
- Do not add LLM selection/calls, hidden telemetry, raw reasoning capture, destructive Git automation, or implicit schema migration.
- Do not interpret prompts, messages, handoffs, MCP payloads, or delegation tokens as approval.
- Do not expose raw prompts, message bodies, final output, tokens, private paths, runtime homes, opaque native references, or secret-like values by default.

## Change workflow

1. Describe the user-visible contract and affected acceptance IDs.
2. Reproduce a bug or establish the pre-change baseline.
3. Make the smallest source fix; remove obsolete paths instead of leaving compatibility shims unless a published contract requires one.
4. Add or update tests only for observable behavior introduced or changed.
5. Run focused tests, then `go test -race ./...` before submitting.
6. For CLI changes, exercise the real binary and verify human and `--json` output plus exit status.
7. For UI/export changes, open the generated HTML offline and check keyboard navigation, hostile content escaping, CSP, and external network requests.
8. For schema changes, provide migration plan/backup/failure evidence. Never apply a migration without separate explicit human approval.
9. Update operator documentation and release notes when a public contract changes.

## Commits and pull requests

- Keep commits reviewable and single-purpose.
- Explain problem, decision, security/privacy impact, compatibility impact, and exact verification.
- Link acceptance evidence without including private paths, tokens, prompts, transcripts, or secret-bearing logs.
- Mark generated files and identify their deterministic source.
- State whether the change affects the working identity. Any identity change invalidates pinned release/parity artifacts.
- Never claim a cross-platform result without evidence from the named target runner.

## Developer Certificate of Origin

OMG uses the [Developer Certificate of Origin 1.1](https://developercertificate.org/). Sign off each commit with:

```text
Signed-off-by: Your Name <your-email@example.com>
```

By signing off, you certify that you have the right to submit the contribution under the project license. Use `git commit -s` to add the trailer. Do not submit code, prompts, art, documentation, or data copied from a source whose license or provenance is unknown.

## License

Unless explicitly stated otherwise, contributions intentionally submitted for inclusion are licensed under Apache License 2.0, consistent with `LICENSE`. Trademarks and working product identifiers are not licensed beyond the terms in Section 6 of that license.

## Conduct

Participation is governed by `CODE_OF_CONDUCT.md`. Technical disagreement is expected; harassment, threats, doxxing, discriminatory conduct, and disclosure of private project data are not acceptable.
