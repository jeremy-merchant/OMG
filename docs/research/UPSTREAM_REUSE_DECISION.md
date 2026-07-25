# OMG Upstream Reuse Decision

**Decision date:** 2026-07-22  
**Status:** Accepted for Phase 0 planning  
**Scope:** source and design due diligence only; this document neither copies nor authorizes copying upstream source code.

## Decision

**Verdict: build OMG greenfield; reuse concepts and externally documented interoperability patterns only. Do not fork any reviewed upstream.**

This is not a preference for novelty. It follows from the product contract: OMG is a Go, local-first, daemonless coordination ledger with SQLite as canonical state, one cross-platform binary, linked-worktree state discovery, explicit human-root lineage, a typed mailbox, immutable/superseding handoffs, reservations, and non-destructive Git reconciliation. No identified upstream supplies that complete contract without material product, runtime, storage, safety, or licensing uncertainty. A fork would inherit a mismatched domain model and create a permanent upstream-merge obligation while still requiring replacement of the core.

### Evidence boundary

* **Observed** means a statement taken from the linked upstream repository, its release/API metadata, or its license file on 2026-07-22.
* **Inference** means a compatibility conclusion from those observed facts and the OMG master specification.
* **Not legal advice:** license findings are engineering due diligence, not a legal opinion. Before redistribution, counsel or a designated license owner must review every direct dependency and any proposed copied material.
* “Latest” is limited to the latest **identifiable** release or default-branch revision observed on 2026-07-22. GitHub API rate limiting prevented independent metadata retrieval for some repositories; those entries deliberately say so instead of guessing.

## OMG evaluation baseline

OMG is not an agent runner, terminal multiplexer, cloud control plane, general issue tracker, or long-term memory vault. Its required lineage is:

```text
Human root -> agent session tree -> claimed task/run DAG
                                -> typed mailbox / ACKs
                                -> handoff and adoption evidence
                                -> advisory path reservations
                                -> independent Git inventory and safe recovery plan
```

The target is a current-stable-Go executable named `omg`, daemonless for all core operations, with canonical SQLite state resolved through Git’s common directory so linked worktrees share one store. Required release targets are macOS arm64/amd64, Linux amd64/arm64, and Windows amd64. These requirements are from the stable [OMG product specification](../PRODUCT_SPEC.md#runtime-and-architecture-constraints); the private deep-interview source remains retained provenance and is not a public dependency.

## Primary-source inventory

| Upstream | Primary source and observed purpose | Identifiable revision / release and date | Runtime / architecture | License status | Decision |
|---|---|---|---|---|---|
| Braid | [alextes/braid](https://github.com/alextes/braid) — agent-oriented issue CLI with worktree-oriented agent setup and issue claims. | `main` `7d9c8ed63f21b6303683e89cb1c683480dcd417d`, committed 2026-07-05; no GitHub “latest release” returned by the API query. | Rust CLI; issues on a dedicated shared branch or an external repository. | MIT, observed via GitHub API metadata. | Reject fork: task claiming is useful precedent, but branch/external-repo state is not OMG’s SQLite/common-dir store. |
| Beads | [steveyegge/beads](https://github.com/steveyegge/beads) and [LICENSE](https://raw.githubusercontent.com/steveyegge/beads/main/LICENSE) — distributed graph issue tracker for AI agents. | **Not independently verifiable:** the unauthenticated GitHub API rate limit was exhausted while requesting default-branch SHA, commit date, and latest release; attempted source: [repository](https://github.com/steveyegge/beads) and [releases](https://github.com/steveyegge/beads/releases), both accessed 2026-07-22. | Git-backed/Dolt-backed issue graph; dependency-aware work selection. | MIT, observed in upstream LICENSE (copyright 2025 Beads Contributors). | Reject fork: graph/task concepts are relevant, but Dolt/Git-backed issue storage is a different persistence and reconciliation model. |
| agentize / youragent | [StanHus/youragent](https://github.com/StanHus/youragent) — package renamed from `youragent` to `agentize`; scaffolds `.agent/`, task graph, audits, and handoff/status artifacts. | `main` `6a9d606e4ae63f0ec60a4cec84d4510544a308ce`, 2026-07-07; no release returned. | Shell/npm-style repository scaffold rather than coordination service. | **No asserted SPDX license** (`NOASSERTION`) in GitHub API metadata. | Reject code reuse and fork. Conceptually retain idempotent bootstrap/audit and portable handoff output only. |
| Agent Message Queue (AMQ) | [avivsinai/agent-message-queue](https://github.com/avivsinai/agent-message-queue) — file-based local agent queue. | Release `v0.45.0`, published 2026-07-21; `main` `cec03366d1d2e6b1971c04a4665dd2c31bece7ea`, 2026-07-21. | Go; Maildir-style atomic file delivery; no server, daemon, or database. | MIT, observed in GitHub API metadata. | Reject fork: Maildir atomic-delivery and receipts are good design references, but OMG needs relational canonical state and typed domain transitions. |
| MCP Agent Mail | [Dicklesworthstone/mcp_agent_mail](https://github.com/Dicklesworthstone/mcp_agent_mail) — asynchronous identities, inboxes, threads, and advisory file leases; [original project location referenced by search](https://github.com/steveyegge/mcp_agent_mail). | Fork/default branch `5e481834ff1c373acda804d28c21d0349a116419`, 2026-07-20; no release returned. Original repository is **not verifiable by API** (404 on 2026-07-22), so no current original revision is asserted. | Python/FastMCP with Git and SQLite archive/indexing; MCP-first surface. | **No asserted SPDX license** for the reviewed fork (`NOASSERTION`). Original current license is not verifiable from the failed API lookup. | Reject fork: mailbox, threads, advisory leases, and auditability are useful concepts; OMG must keep MCP an adapter over a Go CLI/domain service and cannot inherit unasserted licensing. |
| AgentsMesh (the likely intended `agents-mesh`) | [AgentsMesh/AgentsMesh](https://github.com/AgentsMesh/AgentsMesh). The requested name is ambiguous; this reviewed candidate describes an agent workforce platform. | `v0.44.4`, published 2026-06-27; `main` `0cb56a28d2dae62b37765f515db3db19c4189279`, 2026-07-20. | Go control plane; gRPC/mTLS orchestration and relay/data-plane terminal streaming. | **No asserted SPDX license** (`NOASSERTION`). | Reject fork: it is a multi-machine control plane, expressly outside OMG v0.1’s local/single-host scope. |
| Agenity | [agenity.com](https://agenity.com/) is the only primary source found in the exact-name search; it describes a local-business AI integration service, not a public coding-agent coordinator. | **Not verifiable:** no authoritative public source repository, release, commit, or OSS license located. Attempted sources: exact-name [GitHub search result path](https://github.com/search?q=Agenity&type=repositories) and [vendor site](https://agenity.com/), 2026-07-22. | Service product; no verifiable relevant local CLI architecture. | Not verifiable. | Reject as unrelated and non-verifiable; no code or design dependency. |
| Entire CLI | [entireio/cli](https://github.com/entireio/cli) — Git-integrated AI-session observability/checkpoint CLI. | `main` `c4cf0886f44ff389eb7e059d256c7940ef6cddc8`, 2026-07-22; no release returned. | Go; hooks/checkpoints and a dedicated Git branch for session context. | MIT, observed in GitHub API metadata. | Reject fork: Git evidence capture and safe checkpointing are relevant ideas, but OMG must not retain raw prompts/reasoning by default and must not make a hidden Git branch canonical state. |
| dmux | [standardagents/dmux](https://github.com/standardagents/dmux) and [LICENSE](https://raw.githubusercontent.com/standardagents/dmux/main/LICENSE) — tmux/worktree agent multiplexer. | **Not independently verifiable:** API metadata query was rate-limited; attempted [repository](https://github.com/standardagents/dmux) and [releases](https://github.com/standardagents/dmux/releases), 2026-07-22. | Tmux TUI that launches supported agent CLIs and creates worktrees; includes lifecycle/merge automation. | MIT, observed in upstream LICENSE (copyright 2025 Justin Schroeder). | Reject fork: UI/process management is deliberately a non-goal; automatic commit/merge/cleanup conflicts with OMG’s non-destructive contract. Adapter interoperability is possible later. |
| Agent Deck | [asheshgoplani/agent-deck](https://github.com/asheshgoplani/agent-deck) and [LICENSE](https://raw.githubusercontent.com/asheshgoplani/agent-deck/main/LICENSE) — terminal mission control for agent sessions. | **Not independently verifiable:** API metadata query was rate-limited; attempted [repository](https://github.com/asheshgoplani/agent-deck) and [releases](https://github.com/asheshgoplani/agent-deck/releases), 2026-07-22. | TUI/session manager, conductor supervision, native agent-session forks, and worktree scripts. | MIT, observed in upstream LICENSE (copyright 2025 Ashesh Goplani). | Reject fork: a session cockpit is not a portable durable ledger. Retain only the operator-view concept. |
| Gas Town | [gastownhall/gastown](https://github.com/gastownhall/gastown), [releases](https://github.com/gastownhall/gastown/releases), and [LICENSE](https://raw.githubusercontent.com/gastownhall/gastown/main/LICENSE) — multi-agent workspace manager. | Search result identifies `1.0` as a production-stability milestone, but exact tag/commit is **not independently verifiable** because API metadata was rate-limited; attempted repository/releases 2026-07-22. | Agent workspace manager with terminal feed and federated coordination through DoltHub. | MIT, observed in upstream LICENSE (copyright 2025 Steve Yegge). | Reject fork: a broad orchestration product with federation/DoltHub exceeds local-only v0.1 and carries a different workflow model. |
| Gas City | [gastownhall/gascity](https://github.com/gastownhall/gascity) and [LICENSE](https://raw.githubusercontent.com/gastownhall/gascity/main/LICENSE) — orchestration-builder SDK extracted from Gas Town. | Search result identifies `v1.0.0`, but exact tag/commit is **not independently verifiable** because API metadata was rate-limited; attempted repository/releases 2026-07-22. | Configurable SDK with runtime providers, work routing, formulas, orders, and health patrol. | MIT, observed in upstream LICENSE (copyright 2025 Steve Yegge). | Reject fork: an orchestration SDK would make OMG an agent runner/control framework instead of a vendor-neutral ledger. |
| AgentCairn | [ccf/agentcairn](https://github.com/ccf/agentcairn) and [LICENSE](https://raw.githubusercontent.com/ccf/agentcairn/main/LICENSE) — durable cross-project memory in an operator-owned Obsidian vault. | Package index search identified `0.24.0`; exact repository default-branch commit/release is **not independently verifiable** after API rate limiting. Attempted [repository](https://github.com/ccf/agentcairn), [releases](https://github.com/ccf/agentcairn/releases), and [PyPI 0.24.0](https://pypi.org/project/agentcairn/0.24.0/), 2026-07-22. | Python CLI/MCP; plain Markdown source of truth plus rebuildable DuckDB index; daemonless. | Apache-2.0, observed in upstream LICENSE. | Reject fork: memory vault/index is distinct from per-project coordination state. Retain the operator-owned/rebuildable-index principle only. |

## Capability matrix against OMG

Legend: **Y** = directly present in the described upstream; **P** = partial or adjacent; **N** = not evidenced / contrary; **U** = not verifiable from primary source review. A `Y` is not a statement that the behavior meets OMG’s security, lineage, or safety requirements.

| Upstream | Task graph | Typed mailbox | Handoff | Reservations | Git reconciliation | Daemonless CLI | SQLite shared-worktree canonical state | Required five-target binary posture | Material mismatch with OMG |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---|
| Braid | Y | N | N | P | P | Y | N | U | issue state stored via branch/external repo, not canonical shared SQLite |
| Beads | Y | N | P | N | P | Y | N (Dolt) | U | durable task graph, but different storage and no verified OMG lineage/mailbox |
| agentize/youragent | Y | N | Y | N | N | Y | N | N | project scaffold, not a concurrent coordination ledger |
| AMQ | N | Y | Y | N | N | Y | N | P | message transport only; no task/lineage/reconciliation model |
| MCP Agent Mail | P | Y | P | Y | P | P | P | U | MCP/Python-first and unasserted reviewed-fork license |
| AgentsMesh | P | P | P | P | P | N | N | P | distributed control plane/relay rather than local daemonless ledger |
| Agenity | U | U | U | U | U | U | U | U | unrelated service; no public source identified |
| Entire CLI | N | N | P | N | P | P | N | P | captures agent sessions; OMG must redact rather than preserve reasoning |
| dmux | P | N | P | N | P | N | N | U | tmux/worktree runner and automatic merge/cleanup behavior |
| Agent Deck | P | P | P | P | P | N | N | U | interactive session manager, not canonical coordination state |
| Gas Town | Y | P | P | P | P | N | N (Dolt/federation) | P | federated orchestration product beyond v0.1 scope |
| Gas City | Y | P | P | P | P | N | N | U | runtime-provider orchestration SDK rather than a local ledger |
| AgentCairn | N | N | P | N | N | Y | N (Markdown/DuckDB) | P | cross-project memory, not coordination or Git recovery |
| **OMG required** | **Y** | **Y** | **Y** | **Y** | **Y, inventory/adoption/plan only** | **Y** | **Y** | **Y** | human-rooted, local-first, safety-constrained contract |

### Architecture comparison

1. **State ownership.** Braid and Entire use Git branches for selected state; Beads and Gas Town use Dolt-oriented models; AMQ uses files; AgentCairn uses Markdown plus DuckDB. OMG needs transactional SQLite with foreign keys, migrations/checksums, short transactions, and a common-directory resolution rule. **Inference:** grafting this into any candidate replaces its core state and migration architecture.
2. **Runtime boundary.** AMQ and AgentCairn validate daemonless operation; however, their scopes are transport and memory. AgentsMesh, Gas Town, Gas City, dmux, and Agent Deck center a running control/UI/process layer. **Inference:** only the former pair’s daemonless principle is reusable, not their code.
3. **Safety boundary.** dmux advertises automatic commit/merge/cleanup; Entire captures reasoning/session traces; OMG forbids destructive Git automation and redacts prompts/messages/output by default. **Inference:** OMG cannot adopt their default workflow semantics unchanged.
4. **Adapter boundary.** MCP Agent Mail and AgentCairn demonstrate MCP-based access. OMG’s contract makes the CLI/domain application service canonical and MCP optional. **Inference:** OMGCP should define adapter-neutral domain outcomes; an MCP implementation must call those services rather than become a second domain model.

## Reusable concepts, not source code

The following concepts are acceptable input to an original OMG design, subject to independent implementation and test design:

| Source | Concept to independently re-express | OMG application |
|---|---|---|
| Braid / Beads | atomic claim, stable task identifier, dependency-aware ready work | SQLite uniqueness/transactional claim and directed DAG cycle rejection |
| AMQ | atomic delivery boundary, receipts, waitable handoff | append-only typed messages, idempotency keys, ACK state, receipt events |
| MCP Agent Mail | inbox/outbox, searchable thread context, advisory file lease | typed mailbox plus reservations with TTL, renewal, override audit |
| Entire CLI | Git-linked evidence and checkpoint mindset | immutable command receipts, handoff verification evidence, backup/restore—not prompt/reasoning capture |
| dmux / Agent Deck | operator visibility across active worktrees and sessions | `omg board` views and optional shell/watch adapters only |
| Gas Town / Gas City | work routing and durable coordination state | explicit task dependencies and adoption workflow, while excluding federation/agent scheduling |
| AgentCairn | operator-owned data, readable exports, rebuildable derived indexes | canonical local SQLite plus self-contained HTML/Markdown/JSON views and backups |
| agentize/youragent | idempotent project onboarding and auditable status | `omg integration plan/apply/status/remove` with marker-only removal |

No upstream function, file layout, API schema, CLI spelling, test fixture, prompt, or implementation logic is authorized for copying. Similar high-level behavior must be designed from the OMG contract and implemented independently.

## Rejected fork options and maintenance cost

| Candidate / option | Why rejected | Maintenance cost if forked |
|---|---|---|
| Fork Braid or Beads as task core | Would retain incompatible branch/Dolt state and still need mailbox, lineage, handoff, reservations, Git inventory, shared SQLite, and cross-platform release work. | Continuous schema/storage divergence plus upstream merge conflicts in the core domain. |
| Fork AMQ as mailbox core | File queue solves delivery but not relational state transitions, task graph, lineage, redaction, or Git assets. | Two sources of truth or a destructive rewrite of AMQ’s defining architecture. |
| Fork MCP Agent Mail | Close feature vocabulary, but Python/FastMCP/Git-centric architecture conflicts with Go CLI canonicality; reviewed fork has no asserted SPDX license. | Rebuild most services while tracking a fast-moving MCP/runtime dependency graph and unresolved licensing. |
| Fork AgentsMesh, Gas Town, or Gas City | Distributed orchestration/control-plane design is outside v0.1; makes local-only, human-controlled non-goals harder to enforce. | Large operational surface (networking, relays, federation, runtime providers) that OMG explicitly does not need. |
| Fork dmux or Agent Deck | UI/process-manager orientation and automatic workflow actions are outside OMG’s safety model. | Terminal/session platform maintenance with no solution for canonical coordination state. |
| Fork Entire CLI | Different privacy posture: preserving reasoning/traces conflicts with OMG default redaction. | Hook/checkpoint compatibility and sensitive-data governance, while still missing task/mailbox core. |
| Fork AgentCairn | Long-term knowledge is a different bounded context. | Python/MCP/DuckDB/Obsidian integration with little reuse for OMG’s transactional coordination core. |
| Use Agenity | No relevant public OSS source or license verified. | Unbounded legal and technical uncertainty. |

## Attribution and license obligations

### If OMG remains greenfield (this decision)

* Mere study of public behavior and documentation does not create a code-attribution requirement, but maintain a research trail and do not reproduce expressive text, examples, prompts, or source structure.
* Do not use upstream names, logos, or implied endorsements in OMG branding. Mention them only as factual comparative references.
* Before adding third-party dependencies, generate an SBOM and preserve each dependency’s license/NOTICE obligations separately; that work is not completed by this document.

### If any future direct reuse is proposed

* **MIT** (Braid, Beads, AMQ, dmux, Agent Deck, Gas Town, Gas City): include the applicable copyright and permission notice in all substantial copied portions/distributions; preserve attribution. Confirm each exact file’s header and the repository’s current license at the pinned commit.
* **Apache-2.0** (AgentCairn): include the license; retain relevant notices; carry a `NOTICE` file’s required attributions if one exists; mark modified files; do not assume trademark rights.
* **Unasserted / not verifiable** (agentize/youragent, reviewed MCP Agent Mail fork, AgentsMesh, Agenity): **do not copy, vendor, or distribute code** until an authoritative license at a pinned commit is verified and approved.
* License terms do not grant trademark rights. All public OMG name clearance remains a separate required gate.

## Final rationale and follow-up constraints

**Observed:** no candidate was verified as simultaneously providing OMG’s whole contract. Several candidates have permissive licenses, but licensing alone does not cure architectural mismatch. Several closer conceptual candidates have unasserted or non-verifiable licensing.

**Inference:** a greenfield Go/SQLite core is lower-risk than a fork because the necessary divergence begins at the persistence model and trust boundary, not at an optional adapter. The implementation should borrow only the listed concepts, establish original OMGCP schemas and test fixtures, and treat all reviewed systems as optional interoperability/reference targets.

**Decision constraints:**

1. Pin any future external dependency by version/checksum and review its exact license before use.
2. Keep core daemonless and local; never introduce a relay, remote synchronization, or agent-runner requirement through upstream adoption.
3. Keep Git reconciliation read-only/plan-only in v0.1; no upstream automatic merge, cleanup, reset, or session-trace capture behavior may be imported.
4. Treat MCP, terminal multiplexers, and project-specific adapters as integrations outside OMG’s canonical domain services.
5. Re-run this assessment when selecting a dependency, proposing any code reuse, or preparing public distribution; source heads and licenses can change.
