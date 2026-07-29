# ADR 0003: SQLite Coordination State with Generated TTY, JSON, Markdown, and HTML Views

- **Status:** Accepted; Git authority boundary amended by ADR 0004
- **Date:** 2026-07-22
- **Decision owners:** OMG maintainers

## Context

OMG must let an agent and a human answer the same operational questions: who owns the lineage, what task is active or blocked, what has changed, what remains, which messages/handoffs/reservations exist, and which Git assets need attention. The product must render those facts in terminal, machine-readable JSON, Markdown reports, and self-contained accessible HTML without allowing a renderer or integration to invent a conflicting state.

The product contract already selects SQLite as canonical state. It also requires append-only audit events, command receipts, sensitive-by-default redaction, and a distinction between self-reported `WORK_COMPLETE` and verified `VERIFIED_DONE`. Views therefore need a precise snapshot, projection, ordering, redaction, and escaping contract.

## Decision

### Canonical data and invariants

SQLite tables are the only canonical persistence for OMG coordination and policy facts. Canonical records include project/workspace identity, humans, agent sessions and lineage, tasks and task runs, append-only progress, dependencies, messages and acknowledgements, reservations, handoffs, audit events, command receipts, and migration metadata. Explicit Git inventory records are durable evidence that an observation occurred; Git remains authoritative for repository state under ADR 0004.

The following invariants are enforced in the domain/application layer and, where relationally expressible, by SQLite constraints/foreign keys:

1. Every record belongs to exactly one resolved project or explicit workspace scope; a query MUST NOT merge scopes merely because display names match.
2. An agent session has one lineage kind (`human_direct`, `agent_delegated`, `resumed`, `adopted`, or `imported`) and links to a valid human root or permitted predecessor according to that kind.
3. Task display numbering is allocated atomically per scope. A task claim has at most one active exclusive owner under the task state machine.
4. `WORK_COMPLETE` is an actor/self-reported run outcome; `VERIFIED_DONE` is a separate verified task/run transition with recorded verification evidence. Renderers MUST NOT collapse them into one status.
5. Progress is append-only `done`/`doing`/`next` history. A current-progress projection is derived from ordered entries; editing a view never mutates history.
6. Dependency edges are directed and may not introduce a cycle. Unblock notification state is durable and exactly-once at the domain outcome level, not inferred from whether a renderer ran.
7. Messages, handoffs, audit events, explicitly recorded Git evidence, and command receipts are immutable facts. A Git evidence record proves only what OMG observed at that time; it does not replace live Git state. Corrections use a superseding record with an explicit relation, never in-place rewrite.
8. Reservations are advisory coordination facts with mode, scope, expiration, renewal/release, and audited override; views may warn but cannot claim operating-system enforcement.
9. Git inventory is observational and cleanup-plan-only. A `SAFE_TO_REMOVE` classification is not an authorization and is rendered with that limitation.
10. Raw delegation tokens are never stored. Raw prompts, message/final-output bodies, private paths, and secret-like values are treated as sensitive data and are absent or redacted by default.

### Snapshot query boundary

All four renderers consume a transport-neutral, versioned `BoardView` or `ReportView` built by the query/application layer. The rendering boundary is:

```text
Canonical SQLite state
  -> QueryHandler.Query(scope, selector, visibility policy, snapshot options)
  -> Versioned ViewModel { metadata, facts, warnings, redactions }
  -> TTY | JSON | Markdown | HTML renderer
```

The query handler performs scope authorization, selection, visibility/redaction policy, and deterministic ordering. It obtains one consistent read snapshot for a view and returns a plain immutable view model. Renderers MUST NOT open SQLite, call Git, infer task state, mutate state, fetch network resources, or make authorization decisions. A renderer failure does not roll back a committed command; a command does not need a renderer to establish its domain outcome.

Every generated view includes a metadata block with:

- `schema_version` and `view_version`;
- generation timestamp in UTC and the canonical snapshot sequence/audit cursor used;
- scope identity and selected board/report selector;
- redaction policy/version and a boolean indicating whether content was omitted/redacted; and
- a warning that Git results are observations as of the snapshot, not destructive authorization.

The generation timestamp is presentation metadata. Deterministic fixture tests inject a clock and snapshot sequence so equivalent canonical state yields byte-stable JSON/Markdown/HTML except where a documented timestamp field is deliberately varied.

### Selectors, ordering, and absence

The canonical query selectors are `me`, `tree`, `task`, `all`, and `git`; the CLI may expose them through `omg board`. Selectors are query definitions, not separate sources of state.

- `me` shows the calling actor's safe lineage, active/previous task, current `done`/`doing`/`next`, inbox summary, blockers, reservations, handoffs, and Git warnings.
- `tree` shows safe human-root and agent lineage plus task/dependency relationships.
- `task` shows one requested task and its runs, progress history, dependencies, messages/handoffs, reservations, and outcome/verification distinctions.
- `all` shows all visible scope facts, paginated or explicitly bounded when necessary.
- `git` shows the latest inventory observation, ownership link when known, classification, and the non-destructive limitation.

Collections are ordered deterministically: canonical display IDs ascending where present; otherwise immutable creation sequence ascending; time ties break by immutable record ID. The query response explicitly represents missing/unknown/not-applicable rather than converting them to an empty string, false completion, or guessed owner. Pagination tokens, if exposed, are opaque and bound to the selector, scope, ordering version, and snapshot cursor.

### Format contracts

#### TTY

TTY is a compact accessibility-conscious presentation of `BoardView`. It may use color only as a supplement; state labels and text remain sufficient without color. It indicates redaction, unknown ownership, warnings, and the `WORK_COMPLETE` versus `VERIFIED_DONE` distinction in text. It is not a stable parsing interface.

#### JSON

JSON is the stable machine interface. All success responses use the common OMG envelope:

```json
{
  "ok": true,
  "data": { "view_version": 1 },
  "meta": { "schema_version": 1, "snapshot_cursor": "..." },
  "warnings": []
}
```

Error responses use `ok: false` and a stable safe error object/code; they never emit partial undocumented data. JSON field names, enums, nullability, redaction markers, and exit-code mapping are versioned contracts. Additive fields are permitted within a view version; removing or changing semantics requires a new documented view version. JSON serializes only the redacted view model by default.

#### Markdown

Markdown is a generated, human-readable report, not a second editable source of truth. It uses fixed heading/order conventions, semantic tables/lists where appropriate, explicit redaction markers, and links/identifiers that resolve within the report where possible. It carries snapshot metadata and an `Observed facts` section; any inference (for example, a possible reservation overlap) is labelled **Inference** and names its evidence. Markdown import is not supported in v0.1.

#### HTML

HTML is a self-contained static report generated from the same redacted view model. It contains no external scripts, fonts, images, stylesheets, analytics, or network requests. All user-controlled text is contextually escaped before insertion; no user-controlled HTML, URL scheme, CSS, or JavaScript is trusted. The document emits a restrictive CSP that permits only local inline content needed by the generated document and denies external connection/resource loading (at minimum `default-src 'none'; base-uri 'none'; form-action 'none'`; any inline style/script exception requires a nonce or hash generated by the renderer). Standalone HTML cannot enforce its own frame ancestry because browsers ignore `frame-ancestors` in a meta-delivered CSP; any future HTTP delivery adapter MUST send `Content-Security-Policy: frame-ancestors 'none'` as a response header rather than claim document-level enforcement.

HTML uses semantic landmarks, a page title, heading hierarchy, table headers/captions, visible focus handling, keyboard-operable disclosure controls, and text alternatives for non-text content. It does not depend on color alone. Its static content must remain usable when JavaScript is disabled; JavaScript is unnecessary in v0.1.

### Export and adapter boundaries

`omg export` selects a known view type and format, generates it from a read snapshot, then writes a new output file or stdout. It MUST NOT mutate canonical coordination state merely because an export succeeded. Exports default to the safe/redacted visibility policy. A future privileged unredacted export requires a separate explicit policy/ADR, an auditable opt-in, and no raw-token export.

Pygmalion, Zoomzi, shell, watch, and MCP integrations consume application commands and versioned view models. They may add adapter-owned presentation around the result but MUST NOT write a competing task/session/message ledger, scrape terminal text as an API, or interpret generated Markdown/HTML as an import source. Integration-specific data remains outside the general-purpose core schema unless a future generic OMG concept justifies it.

## Consequences

- Every user surface can explain the same durable facts and distinguish observation, inference, and authority boundaries.
- Rendering is portable and testable without a terminal, browser, Git repository, or MCP server.
- JSON becomes a compatibility commitment that must evolve intentionally.
- View creation may lag a concurrent mutation by snapshot design; it is accurate to its recorded cursor rather than falsely claiming live global state.
- Default redaction improves safety but means a user may need an explicitly authorized local inspection path for full private operational content.

## Rejected alternatives

1. **Separate files/YAML per view as state:** rejected because synchronizing task, message, handoff, and Git facts across formats would produce drift and unsafe merge semantics.
2. **TTY output parsed by adapters:** rejected because terminal layout, width, color, and localization/presentation changes are not a stable API.
3. **MCP-specific or integration-specific boards:** rejected because equivalent domain outcomes must remain equivalent across adapters.
4. **Client-side HTML fetching live data:** rejected because reports must be self-contained, local-first, and usable without network access.
5. **Unescaped user-provided HTML/Markdown:** rejected because messages, prompts, paths, and handoffs are untrusted data.
6. **Treating `WORK_COMPLETE` as verified completion:** rejected because it erases the required verification boundary.
7. **Export/import round-tripping generated reports:** rejected because generated reports omit/redact data and cannot safely reconstruct canonical history.

## Risks and mitigations

- **Projection drift:** put selectors and view-model construction in the shared query handler; test every renderer against the same fixtures.
- **Sensitive-data exposure:** make redaction default, centralize policy before rendering, test text/JSON/HTML for forbidden raw values, and never store raw delegation tokens.
- **XSS or external data leakage:** contextually escape output, use restrictive CSP, prohibit external resources, and browser-test hostile fixture strings.
- **Ambiguous operational presentation:** use explicit unknown/not-applicable states, show snapshot metadata, and label inferences separately from observed facts.
- **Large boards:** require bounded/paginated selectors and streaming output only after a complete, consistent view model definition; never silently omit records.

## Rollback and revisit triggers

Revisit this ADR if the canonical SQLite schema cannot express a required generic OMG invariant; an adapter demonstrably requires a new transport-neutral query; a stable JSON field proves unsafe or semantically ambiguous; or an HTML accessibility/security review identifies a material defect.

Rollback a renderer by selecting the prior supported `view_version` or disabling that export format while preserving canonical records and other renderers. Do not roll back by editing generated reports or deleting audit/history rows. Any schema rollback follows ADR 0001 backup/migration recovery rules.

## Testable acceptance checks

1. A fixture containing a human root, nested agents, task runs, dependencies, progress, messages, handoffs, reservations, and Git observations renders equivalent fact IDs/statuses through TTY, JSON, Markdown, and HTML from one query snapshot.
2. A `WORK_COMPLETE` but unverified run is visibly distinct from a `VERIFIED_DONE` task in every format and in JSON enum values.
3. Equivalent state with an injected fixed clock/snapshot cursor produces deterministic JSON, Markdown, and HTML output; all collections follow the documented tie-break ordering.
4. A renderer is tested with hostile HTML, Markdown, URL-like, shell-like, CJK, spaces, and secret-like strings. HTML contains escaped text and restrictive CSP, makes no external request, and JSON/Markdown/TTY contain only the redacted policy result.
5. Default board/export fixtures demonstrate that raw tokens never appear and that private paths/prompts/messages/final outputs are omitted or marked redacted according to policy.
6. A generated HTML report passes automated keyboard/semantic checks for landmarks, heading order, table headers, focus visibility, and non-color-only state labels, with JavaScript disabled.
7. A task selector for an unknown task returns the stable not-found envelope/exit code; a visible record with unknown owner is represented as unknown rather than complete or absent.
8. Repeating a view query during a concurrent committed mutation yields either the documented pre-mutation or post-mutation snapshot cursor, never a mixed view; exporting it creates no canonical audit/task/message mutation.
9. CLI JSON and MCP query responses for the same selector share the documented view version, fields, redaction behavior, and stable outcome/error semantics.
