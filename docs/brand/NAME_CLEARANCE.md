# OMG Working-Name Collision Research

**Research date:** 2026-07-22  
**Evidence window (UTC):** 2026-07-22T09:50:18Z–2026-07-22T09:55:00Z  
**Working identity:** **OMG** — *Oh My Group*; CLI `omg`; project directory `.omg/`; environment prefix `OMG_`; **OMG Coordination Protocol (OMGCP)**.

## Provisional status

**Status: PUBLICATION BLOCKED — rename decision required before public distribution.**

This is product-name usability research, not legal clearance. Exact, current collisions exist for the `omg` executable/package name in npm, PyPI, and crates.io; the GitHub organization `omg` is occupied; and the short common domains examined are registered. These are material operational/discoverability conflicts even where the observed products serve different purposes. `OMG` is also an extremely crowded acronym, so the evidence does not support treating it as a distinctive public software name.

**Working-name treatment:** retain OMG unchanged for private discovery and implementation. Do **not** silently rename. No public repository, release, package publication, domain purchase, or external distribution may proceed until the publication gate below is satisfied.

## What this does and does not establish

### Observed facts

The registry and API results in this document are time-stamped, linked, and quoted or summarized narrowly below. A successful lookup means only what that source returned at that time; an HTTP 404 means that endpoint did not resolve that exact registry identifier at that time.

### Inference

The exact `omg` executable/package collisions, occupied GitHub organization, and registered domains make public discoverability and installation confusion likely enough to block publication. This is a product/usability conclusion, not a conclusion about trademark rights, infringement, registration validity, priority, or enforceability.

### Legal boundary

This document is **not legal advice** and is **not trademark clearance**. Trademark risk turns on jurisdiction, classes of goods/services, use, priority, similarity, channels, and other facts not determined here. A qualified trademark professional must perform and interpret a jurisdiction-and-class-specific clearance search before adopting, filing, or launching a public mark.

## Material conflicts

| Surface | Observed result | Why material | Status |
|---|---|---|---|
| CLI / npm | `omg` is an existing npm package whose latest metadata declares `bin: {"omg": "./lib/cli.js"}` and describes a microservices CLI. | An npm/global install can place the exact executable name `omg` on `PATH`. | **Material** |
| CLI / PyPI | `omg` is an existing PyPI project, version `1.3.9`, whose description says to run scripts with `omg`. | A Python install can place/use the exact CLI spelling in the same developer environment. | **Material** |
| CLI / crates.io | `cargo search omg` returned `omg = "0.2.1"`, described as a CLI for the omg.lol API. | Exact crate identifier and CLI-oriented use create package/install ambiguity. | **Material** |
| GitHub organization | `GET /orgs/omg` returned the verified organization `OMG`, with `https://omg.games` and eight public repositories. | The obvious organization URL `github.com/omg` cannot be used. | **Material** |
| GitHub repositories | GitHub repository-name search for `omg` returned `total_count: 8278`; examples include `kongzhecn/OMG` and `PengJiyuan/omg`. | Exact repository-name reuse is pervasive; a distinctive owner/repository identity is required. | **Material** |
| Domains | `omg.com`, `omg.org`, `omg.dev`, and `ohmygroup.com` all returned RDAP registration records. | The most intuitive public URLs are unavailable; the latter is explicitly delegated to for-sale nameservers. | **Material** |
| Product expansion | The exact GitHub repository search for `"oh-my-group"` returned `total_count: 0`; general-web exact search did not provide an independently verifiable exact-use result. | This is weak negative evidence only; it does not overcome the `OMG` short-name collisions or prove availability. | **Unresolved** |
| Protocol acronym | GitHub repository-name search `omgcp in:name` returned two partial matches (`OmgCPP`, `omgcptsd.github.io`); npm and PyPI exact `omgcp` endpoints returned 404; `cargo search omgcp` returned no result. | No exact package result was observed in the checked registries, but acronym and trademark searches are incomplete. | **Unresolved; not presently material by itself** |

## Evidence log

All queries below were run in the UTC evidence window stated above. URLs are primary registry/API sources except where marked as a general-web discovery query. Results are intentionally descriptive rather than legal conclusions.

| Time (UTC) | Area and exact query / request | Source URL | Result | Limitation |
|---|---|---|---|---|
| 09:50:18 | General-web query: `"Oh My Group" software OR CLI OR open source` | [search](https://www.google.com/search?q=%22Oh+My+Group%22+software+OR+CLI+OR+open+source) | Returned adjacent “Oh My” coding-agent projects, not a verified exact OMG product collision. | Search-result rankings/indexing are not a registry or clearance search. |
| 09:50–09:55 | GitHub repository-name query: `omg in:name` | [GitHub REST search](https://api.github.com/search/repositories?q=omg+in%3Aname&per_page=10) | `total_count: 8278`, `incomplete_results: false`; examples include `kongzhecn/OMG` (diffusion-model research) and `PengJiyuan/omg` (2D drawing library). | Limited to public GitHub repositories and the first page of returned items; names are not rights determinations. |
| 09:50–09:55 | GitHub organization lookup: `omg` | [GitHub REST organization](https://api.github.com/orgs/omg) | Existing verified organization `OMG`, description “The bestest online multiplayer games!”, blog `https://omg.games`, eight public repos. | A GitHub handle is not a trademark search; it nevertheless proves that exact organization handle is unavailable. |
| 09:50–09:55 | GitHub repository query: `"oh-my-group"` | [GitHub REST search](https://api.github.com/search/repositories?q=%22oh-my-group%22&per_page=10) | `total_count: 0`, `incomplete_results: false`. | Does not search private repositories, organization/display names, package metadata, web content, or phonetic/variant names. |
| 09:50–09:55 | GitHub repository-name query: `omgcp in:name` | [GitHub REST search](https://api.github.com/search/repositories?q=omgcp+in%3Aname&per_page=10) | `total_count: 2`: `omagaDL/OmgCPP` and `omgcptsd/omgcptsd.github.io`; neither is an exact OMGCP protocol result from this evidence. | Repository-name search only; case/substring matches are not a clearance result. |
| 09:50–09:55 | npm exact package query: `omg` | [npm registry](https://registry.npmjs.org/omg/latest) | Existing `omg@1.0.0-alpha6`; description “A CLI to interact with microservices built with the OMG standard”; exact `omg` bin. | Registry occupancy says nothing about trademark rights or current maintenance. |
| 09:50–09:55 | npm exact package query: `omgcp` | [npm registry](https://registry.npmjs.org/omgcp) | HTTP 404. | Scoped packages and differently punctuated/phonetic variants were not exhaustively searched. |
| 09:50–09:55 | PyPI exact project query: `omg` | [PyPI JSON API](https://pypi.org/pypi/omg/json) | Existing project `omg`, version `1.3.9`, summary “A Python CLI replacement with live reload capabilities”; documentation says `pip install omg` and run `omg`. | PyPI name normalization and project existence do not establish rights. |
| 09:50–09:55 | PyPI exact project query: `omgcp` | [PyPI JSON API](https://pypi.org/pypi/omgcp/json) | HTTP 404. | No exhaustive variant or package-content search. |
| 09:50–09:55 | crates.io registry query: `cargo search omg --limit 10` | [crates.io search](https://crates.io/search?q=omg) | Exact crate `omg = "0.2.1"`, “CLI app for interacting with the omg.lol API”, plus many prefix/acronym results. | CLI search output is a current registry lookup, not proof of executable name ownership or legal rights. |
| 09:50–09:55 | crates.io registry query: `cargo search omgcp --limit 10` | [crates.io search](https://crates.io/search?q=omgcp) | No result printed. | Search result is not a reservation or trademark determination; variants remain unchecked. |
| 09:50–09:55 | Go module discovery query: `omg` | [pkg.go.dev search](https://pkg.go.dev/search?q=omg) | The static fetch exposed only a generic `search` page and no reliable result list. | No conclusion about Go-module availability. Go module paths depend on the eventual repository domain/owner; a final import path cannot be cleared before that decision. |
| 09:50–09:55 | Homebrew core query: `brew search omg`; direct formula/cask identifiers `omg` | [formula endpoint](https://formulae.brew.sh/api/formula/omg.json), [cask endpoint](https://formulae.brew.sh/api/cask/omg.json) | `brew search omg` returned `spdx-sbom-generator` and `omega`; both direct `omg` JSON endpoints returned HTTP 404. | Covers installed/current core search only, not third-party taps, historical formulas, aliases, or future availability. |
| 09:50–09:55 | RDAP exact domains: `omg.com`, `omg.org`, `omg.dev`, `ohmygroup.com` | [omg.com](https://rdap.org/domain/omg.com), [omg.org](https://rdap.org/domain/omg.org), [omg.dev](https://rdap.org/domain/omg.dev), [ohmygroup.com](https://rdap.org/domain/ohmygroup.com) | Each returned a registration record. `omg.com` expires 2027-04-21; `omg.org` 2028-02-08; `omg.dev` 2027-04-14; `ohmygroup.com` 2026-09-05 and uses `DOMAIN-FOR-SALE.HUGEDOMAINSDNS.COM` / `FORSALE.HUGEDOMAINSDNS.COM`. | Registration does not identify a safe alternate domain, prove use, or establish trademark rights; RDAP terms caution against relying on data as authoritative. |
| 09:50–09:55 | Ordinary trade-name discovery query: `"Oh My Group" company OR business` | [search](https://www.google.com/search?q=%22Oh+My+Group%22+company+OR+business) | Returned near matches such as OKMY GROUP LLC, Oh My Company, OH MY COM, and The Oh Group; no verified exact “Oh My Group” trade name was established from the result set. | General-web search is incomplete and jurisdiction-neutral. An OpenCorporates API request for this exact phrase returned HTTP 401, so no database result was available. |
| 09:50–09:55 | USPTO trademark-oriented query: `OMG` in the USPTO Trademark Search UI | [USPTO Trademark Search](https://tmsearch.uspto.gov/search/search-information) | The public UI loaded and accepted the entered query, but did not return a results page in the available session. No result count or mark-level conclusion is recorded. | Interactive application/session behavior prevented a reproducible result; this is explicitly **not** a USPTO clearance search. |
| 09:50–09:55 | Trademark-oriented public sources planned for counsel verification: `OMG`, `OH MY GROUP`, `OMGCP`, and phonetic/visual variants in relevant classes | [USPTO search](https://tmsearch.uspto.gov/), [WIPO Global Brand Database](https://branddb.wipo.int/), [EUIPO eSearch plus](https://euipo.europa.eu/eSearch/) | No mark-level result is asserted from these sources in this pass. | Jurisdiction, class, status, owner, goods/services, and similarity analysis remain required. |

## Publication gate

The working name cannot move from private working use to public distribution unless a named human owner records all of the following decisions/evidence:

1. **Rename decision:** explicitly retain OMG despite the documented conflicts, or explicitly approve a successor identity. A retention decision must state why exact `omg` package/CLI collisions are acceptable and how installers avoid ambiguity.
2. **Namespace control:** verify the chosen source-host organization/account, repository name, Go module path, package names, release-install command, and public domain(s) immediately before reservation/publication. No automated claim, registration, purchase, publication, or remote creation is authorized by this document.
3. **Trademark review:** obtain a qualified professional’s scoped search and advice for intended jurisdictions and software-related goods/services, including exact, phonetic, visual, acronym, and “Oh My …” variants. Record the date, scope, source records, and decision separately.
4. **Technical migration plan:** approve the migration-impact matrix below, compatibility/deprecation policy, and a tested upgrade/downgrade path before any public release.
5. **Human approval:** separately approve public repository creation, package publication, domain registration/purchase, and distribution. The master specification keeps all of those actions human-gated.

Until all five are complete: **publication status is NOT PUBLISHED**.

## Neutral internal namespace and rename decision criteria

To avoid claiming a public identity while the gate is open, use a neutral **internal-only** identifier such as `coordledger` for implementation-local package/module namespaces, test fixtures, and temporary artifact labels where a product name is not required. It is a containment label, not a candidate brand and not a rename. The visible working contract remains OMG until a human approves a decision.

A rename should be selected—not improvised—if any of these criteria is met:

- counsel identifies unacceptable trademark or unfair-competition risk in an intended jurisdiction/class;
- a unique source-host organization, domain, Go module path, and intended package/install identifiers cannot be secured without confusing overlap;
- an installer, package manager, shell `PATH`, or support workflow cannot unambiguously distinguish OMG from the existing exact `omg` tools;
- the planned public mark is too generic/crowded to meet the project’s discoverability and support requirements; or
- a human owner rejects the cost/risk of coexisting with the collisions.

A retention decision should require, at minimum, a unique publisher namespace and module path, an unambiguous installation command, package names that do not reuse bare `omg` where registries are occupied, and completed legal review. It does not remove the conflicts.

## Rename-impact matrix

This matrix describes required work **if and only if** a human approves a successor identity. It is a decision aid, not authorization to change the current working name.

| Surface | Current working value | Required migration if renamed | Compatibility / risk |
|---|---|---|---|
| Product and wordmark | `OMG` / `Oh My Group` | Replace product names, expansions, taglines, marks, release names, copyright/policy references, UI titles, and examples; preserve a dated rename record. | High user-facing ambiguity; never present the successor as the same unqualified public mark without a migration notice. |
| Binary / PATH | `omg` | Build and distribute the successor binary name; update installers, shell completion, scripts, CI, examples, release artifacts, checksums, and support instructions. | **High.** Do not ship a permanent unannounced `omg` alias; it collides with existing tools. A time-bounded, explicitly opt-in compatibility wrapper requires separate approval and collision testing. |
| Config directory | `.omg/` | Define successor directory, tracked config schema, discovery behavior, ignore rules, integration markers, backup/import/export behavior, and downgrade handling. | **High data-safety risk.** Never overwrite or auto-delete `.omg/`; require explicit `status`/`plan`/`apply`, migration backup, integrity check, and rollback. |
| Environment | `OMG_` | Rename documented environment variables, parsers, templates, shell integrations, CI secrets/configuration references, and diagnostics/redaction rules. | Medium. Exact old variables must not silently take precedence; if temporarily recognized, warn deterministically and redact values. |
| Protocol | `OMGCP` / “OMG Coordination Protocol” | Rename protocol title, wire/schema identifiers, content-type/version labels, MCP/integration documentation, examples, and exported envelopes. | **High interoperability risk.** Version the wire contract; do not reinterpret old messages under a new name. Support explicit import/translation only with a documented version boundary. |
| Go module and packages | Not yet assigned | Select and verify a domain/owner-backed module path; update `module`, imports, generated code, release docs, SBOM/provenance metadata, and install commands. | High downstream source breakage once public; choose before first public tag. |
| Git hosting | No public organization/repository assigned | Select a non-conflicting owner/repository path, update clone URLs, issue links, badges, provenance, release automation, and security contacts. | High link/provenance impact; do not assume `github.com/omg` is usable. |
| Package ecosystems | No package published | Reserve/verify scoped or successor identifiers across Go distribution, Homebrew, npm, PyPI, crates.io, and any future ecosystems; update install docs only after human approval. | High supply-chain/support confusion. Existing bare `omg` npm/PyPI/crates names must not be reused. |
| Documentation and instruction surfaces | `OMG`, `omg`, `.omg/`, `OMG_`, `OMGCP` | Update README, CLI reference, config docs, integrations for `AGENTS.md`/`CLAUDE.md`, examples, generated HTML/Markdown, changelog, migration guide, and public policy pages. | Medium–high: stale instructions can run the wrong binary or target the wrong config directory. |
| Persistent state and exports | Future SQLite schema, receipts, handoffs, audit events | Add a named migration record; retain original identifiers in provenance/audit history; document export/import mapping and test restore/downgrade. | **High integrity risk.** No destructive automatic rewrite; preserve backups and fail closed on incompatible newer schema. |

## Unresolved checks

- A manual/counsel-led USPTO, WIPO, EUIPO, and every intended national/regional trademark database search is required; this pass records no mark-level result.
- Final target jurisdictions, goods/services classes, ownership entity, source-host owner, public domain strategy, and package-distribution channels have not been selected.
- Go module availability cannot be meaningfully finalized before selecting the repository domain/owner; the public `pkg.go.dev` search did not yield a reliable result list in this environment.
- Third-party Homebrew taps, package scopes, alternate spellings (`om-g`, `oh-my-group`, `ohmygroup`, punctuation/case variants), phonetic equivalents, and non-English transliterations remain unchecked.
- Domain registration records do not establish active use, acquisition terms, or any legal relationship to the registrant. No domain acquisition was attempted.
- General-web and company-directory discovery are incomplete. In particular, the OpenCorporates API could not be queried without credentials (HTTP 401).

## Decision record template

Before public release, append or link a human-approved decision containing: decision date; chosen retain/rename outcome; accountable owner; target jurisdictions/classes; counsel/work-product reference; source-host and domain evidence; package-name verification; approved migration version; rollback plan; and separate approvals for repository creation, package publication, domain transaction, and distribution.

## Private local RC working-name decision

- **Decision date:** 2026-07-22
- **Accountable owner:** local operator `kiunlee`
- **Decision:** retain `OMG` / `omg` only for the private, local `v0.1.0-rc.1` candidate.
- **Why the known collisions are acceptable for this scope:** the candidate is an unpublished validation artifact, is not installed through npm, PyPI, crates.io, Homebrew, a public Go module, or another registry, and is not advertised as a globally discoverable command. The exact-name collisions remain unacceptable for public distribution without the unresolved clearance work above.
- **Installer ambiguity control:** local install-manifest drafts bind the exact version and SHA-256 digest and require an explicit source artifact and destination; they do not invoke a package manager, resolve `omg` from a registry, create a remote, or modify a public namespace.
- **Limits:** this is not trademark/legal clearance and authorizes no repository creation, package publication, domain transaction, distribution, or public release. Publication remains **NOT PUBLISHED**. A future identity change invalidates this RC pin and every proof/parity artifact derived from it.
- **Outstanding public decision:** every field and separate approval in the public decision template above remains unresolved.
