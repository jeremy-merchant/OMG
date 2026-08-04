# OMG Todo Intake and Task Hierarchy Architecture

Status: implementation-ready design proposal

Date: 2026-07-31
Scope: Todo intake, existing Task hierarchy formalization, promotion, closure, CLI/MCP, compatibility, and rollout

Implementation status on 2026-07-31: the backward-compatible Task hierarchy policy and integrity slice, centralized required-child verification gate, SQLite migration v12, and atomic `reserve.batch-add` call-compression path are implemented in the local working tree. The Todo intake tables, Todo lifecycle commands, and Todo-to-Task promotion phases remain specified here but are not part of that first implementation slice.

## 1. Review provenance and limits

This design is based on direct inspection of the current OMG source tree and read-only inspection of representative local OMG ledgers.

The local Brain CLI was discovered at `/Users/kiunlee/.local/bin/brain`. Its help, status, doctor, authenticated profile, and repository review invocation contract were verified. The requested repository-attached Brain review did **not** execute: both `brain session start ... --repo /Users/kiunlee/OMG --prompt-mode code-review` attempts were denied by the connector's network approval gate before command execution. Therefore no recommendation in this document is attributed to Brain.

Evidence reviewed on 2026-07-31:

- current OMG task/run/session/dependency/reservation/handoff/candidate-close source paths;
- Pygmalion canonical board, status, stale classifications, selected task boards, and parent distribution;
- industry-os canonical board, status, and stale classifications;
- the Zoomzi base repository and a recent release worktree, both of which had empty OMG ledgers;
- workspace discovery for cmux, which was not locally available under the expected project name/path.

## 2. Executive decisions

1. **Do not introduce a separate Subtask entity.** OMG already models a subtask as a `Task` whose existing `parent_task_id` is set. The work is to make that relationship semantically complete and safe.
2. **Introduce Todo as a separate intake entity.** A Todo is uncommitted intent. It cannot own runs, reservations, handoffs, Git evidence, or verified completion.
3. **Keep containment and blocking separate.** `parent_task_id` means decomposition/containment. `task_dependencies` means execution order or unblock criteria. Neither implies the other.
4. **Keep Task state canonical and hierarchy readiness derived.** Child state changes never silently mutate parent state. Parent readiness is calculated from one read snapshot.
5. **Every executable Task keeps its own closure proof.** Child completion can be a prerequisite, but it is never sufficient by itself to mark a parent `VERIFIED_DONE`.
6. **Adopt hierarchy enforcement compatibly.** Existing tasks remain `INDEPENDENT`; existing parent edges remain `OPTIONAL`. New gated behavior is explicit and cannot retroactively deadlock current ledgers.
7. **Recursive Todo promotion is one atomic transaction.** It either creates the complete Task tree and links every Todo, or creates nothing.
8. **Todo completion follows verified Task completion.** `WORK_COMPLETE` is not enough. A promoted Todo becomes `DONE` only in the same transaction that moves its linked Task to `VERIFIED_DONE`.
9. **Do not weaken reservation conflicts merely because tasks share a parent/root.** Hierarchy relation is useful context, not authority to overlap paths.
10. **Do not add automatic LLM decomposition.** This design supports explicit human/agent-authored trees and remains consistent with the v0.1 non-goal.

## 3. Findings from current OMG

### 3.1 Existing strengths to preserve

OMG already has the correct extension points:

- `lineage.Task` has `ParentTaskID`.
- `Task` and `TaskRun` are separate canonical concepts.
- `WORK_COMPLETE` and `VERIFIED_DONE` are separate states.
- directed dependencies have hard/soft/informational kinds, cycle rejection, and explicit unblock criteria.
- task display numbers are project-scoped and atomic.
- terminal rendering already builds a task tree from `parent_task_id` and suppresses infinite rendering on a detected cycle.
- SQLite project scoping rejects a parent task belonging to another project.
- Store callbacks provide one transaction across repositories and receipts.
- `task.finish-lite` and `candidate.close` already demonstrate atomic multi-record lifecycle finalization.
- `candidate.close` re-inspects canonical state in the write transaction, does not mutate Git, and can release reservations/archive the source session atomically.
- CLI and MCP share the application dispatcher.
- the structured error/recovery contract can represent missing child evidence and safe recovery commands without creating transport-specific semantics.

These should be extended, not replaced.

### 3.2 Current hierarchy gaps

The existing parent link is structurally present but semantically incomplete:

- `Task.Validate` only validates stable IDs and ordinary fields.
- the database check prevents only `parent_task_id = id`.
- creation pre-checks cross-project references, but an absent parent falls through to the SQLite foreign-key failure instead of a stable, contextual domain error.
- no service-level ancestor traversal rejects indirect cycles.
- no maximum traversal budget or corruption report exists.
- no canonical reparent/detach operation exists.
- no rule prevents attaching new work below a closed or verification-pending parent.
- no rule prevents moving a task after runs, handoffs, or reservations exist.
- task transitions and `candidate.close` do not inspect required children.
- board JSON exposes `parent_task_id` but not aggregate child readiness/counts.
- the renderer suppresses a cycle visually, but doctor/preflight do not surface the underlying corrupt hierarchy as an actionable failure.
- hierarchy relation is not included in reservation conflict context.

### 3.3 Current verification weakness relevant to hierarchy

`CanTransitionTask` currently permits a claimed/in-progress/waiting/blocked/rework/interrupted/stale task to transition directly to `VERIFIED_DONE` when the evidence byte slice is non-empty. That is too weak to become the foundation of parent closure.

Before child-gated parent completion is relied upon, every route to `VERIFIED_DONE` must pass through one central application operation that applies:

- lifecycle transition validation;
- verification-evidence validation;
- hierarchy prerequisites;
- dependency reconciliation;
- linked Todo completion;
- consistent idempotency conflict detection.

Compatibility may retain `task.transition` as an adapter command, but `to=VERIFIED_DONE` must delegate to that central operation rather than update task state directly.

### 3.4 Real-ledger findings

#### Pygmalion

The inspected Pygmalion ledger contained:

- 484 tasks and 515 runs;
- 343 child tasks under only 27 distinct parents;
- one parent with 209 children and another with 90;
- 336 tasks in `WORK_COMPLETE` but only 23 in `VERIFIED_DONE`;
- 232 `finished_unclosed` sessions and 137 `runtime_unobservable` sessions;
- zero explicit task dependency records;
- a representative parent task still `CLAIMED` while carrying 43 associated runs, 41 handoffs, and 248 reservation records.

Implications:

- parent links are already used in production-like work and cannot be treated as a new empty feature;
- large parents are being used simultaneously as initiatives and executable work items;
- a naive migration to “all children must be verified” would deadlock existing work;
- percentage-complete based on child count would be misleading;
- dependency UX is underused even when execution ordering is obvious;
- current closure backlog must remain visible rather than being hidden behind a green parent roll-up.

#### industry-os

The inspected industry-os ledger contained four sequential tasks/runs, all `WORK_COMPLETE`, three submitted handoffs, one `finished_unclosed` session, and no dependency records. Continuation was expressed in task titles instead of canonical parent/dependency edges.

Implications:

- Task creation needs convenient explicit parent selection;
- hierarchy/dependency context should be visible in preflight and task views;
- continuation, containment, supersession, and retry must be distinct concepts.

#### Zoomzi and cmux

The inspected Zoomzi roots had empty OMG ledgers. cmux was not present in the workspace registry or expected local path.

These are evidence gaps, not evidence that the design is portable. OMG should report “initialized but empty” distinctly and make first-task/Todo intake obvious. It must not fabricate cross-project parity from unavailable state.

## 4. Canonical domain boundaries

### 4.1 Todo

A Todo is an intake record: an idea, request, obligation, finding, or proposed unit of work whose execution commitment is not yet canonical.

A Todo may have:

- a safe title and bounded summary;
- priority, labels, due time, and source lineage;
- a parent Todo for human organization;
- an explicit promotion link to one Task.

A Todo may not have:

- a TaskRun;
- a reservation;
- a handoff;
- Git integration/canary evidence;
- `WORK_COMPLETE` or `VERIFIED_DONE` states;
- an execution owner.

### 4.2 Task and Subtask

A Subtask is not a distinct domain type. It is:

```text
Task{ParentTaskID: <another task in the same project>}
```

A parent Task remains a normal executable Task. It may own its own run, handoff, evidence, and reservations. Child verification can gate its closure, but does not replace its own proof.

A future container-only `GROUP`/initiative kind is deliberately deferred until real usage proves it necessary. Introducing it now would conflict with existing parents that already own runs and handoffs.

### 4.3 TaskRun

A TaskRun is one execution attempt for one Task. Repeated runs are retries/continuations of execution, not children.

### 4.4 Dependency

A dependency is a directed execution prerequisite. Siblings are not ordered merely because they share a parent. A child is not blocked merely because its parent is incomplete.

### 4.5 Supersession and continuation

- `parent_task_id`: decomposition/containment;
- dependency edge: execution blocking/order;
- `supersedes_id`: replacement lineage;
- multiple runs: execution attempts;
- session continuation: runtime continuation.

Adapters and docs must not use these interchangeably.

## 5. Todo lifecycle

### 5.1 States

```text
OPEN ───────→ READY ───────→ PROMOTED ───────→ DONE
 │             │                 │
 ├──────────→ DONE               └── linked Task failure/cancellation
 │             │                     leaves Todo PROMOTED + needs_review
 └──────────→ CANCELLED          └── no silent reopen/cancel
               └──────────→ CANCELLED
```

Canonical values:

- `OPEN`: captured but not execution-ready;
- `READY`: reviewed and eligible for promotion;
- `PROMOTED`: atomically linked to a Task;
- `DONE`: resolved directly or its linked Task reached `VERIFIED_DONE`;
- `CANCELLED`: explicitly removed from scope before promotion.

`DONE` and `CANCELLED` are terminal.

### 5.2 Transition rules

| From | Allowed targets | Conditions |
|---|---|---|
| `OPEN` | `READY`, `DONE`, `CANCELLED` | direct `DONE` requires a non-empty resolution summary |
| `READY` | `OPEN`, `DONE`, `CANCELLED`, `PROMOTED` | `PROMOTED` only through `todo.promote` |
| `PROMOTED` | `DONE` | only in the linked Task's verified-closure transaction |
| `DONE` | none | immutable |
| `CANCELLED` | none | immutable |

A failed, abandoned, or cancelled linked Task does not automatically cancel or reopen the Todo. The Todo remains `PROMOTED` and its derived `promotion_health` becomes `NEEDS_REVIEW`. An explicit future replacement flow may supersede the linked Task; v1 does not silently create one.

### 5.3 Todo hierarchy

Todo parentage is organization, not execution order.

Required invariants:

- parent exists;
- parent belongs to the same project;
- parent is not self;
- the new edge creates no ancestor cycle;
- terminal Todos cannot gain children;
- promoted Todos cannot be moved or edited;
- traversal depth is bounded by `MaxHierarchyDepth = 16` for new mutations;
- existing deeper or corrupt trees are reported by doctor and remain read-only until repaired explicitly.

## 6. Task hierarchy contract

### 6.1 Canonical storage

Keep `tasks.parent_task_id` as the sole canonical parent edge. Do not also persist `root_task_id`, `depth`, or a closure table in the first implementation.

Reasons:

- duplicate materialized ancestry can drift;
- current ledgers are large but shallow enough for indexed recursive CTEs;
- writes remain simple and auditable;
- read projections can calculate ancestry in one SQLite snapshot.

Add an index on `(project_id, parent_task_id, display_number, id)`.

If profiling later proves recursive reads too expensive, a closure table may be added as a rebuildable projection, not a second source of truth.

### 6.2 New task fields

Add:

```text
completion_policy  INDEPENDENT | ALL_REQUIRED_CHILDREN_VERIFIED
parent_requirement REQUIRED | OPTIONAL
```

Semantics:

- `completion_policy` belongs to the parent Task.
- `parent_requirement` belongs to the child edge represented by that Task's `parent_task_id`.
- `INDEPENDENT`: child state does not gate parent verified closure.
- `ALL_REQUIRED_CHILDREN_VERIFIED`: every direct child marked `REQUIRED` must be `VERIFIED_DONE` before the parent can be verified.
- `OPTIONAL` children remain visible but do not gate closure.

Compatibility defaults:

- migration default for every existing Task: `completion_policy=INDEPENDENT`;
- migration default for every existing child edge: `parent_requirement=OPTIONAL`;
- application default for a newly created child: `parent_requirement=REQUIRED` unless explicitly set;
- application default for a newly created Task: `completion_policy=INDEPENDENT` unless explicitly set.

No existing parent becomes blocked merely by installing the migration.

### 6.3 Why direct children, not all descendants

The closure prerequisite is defined over direct required children. Recursive integrity follows composition:

- a child with its own gated children cannot become `VERIFIED_DONE` until its own prerequisites pass;
- therefore a verified direct child is the stable proof boundary for its subtree;
- the parent avoids repeatedly enumerating every descendant during closure;
- independent legacy subtrees retain their declared semantics.

Views may show descendant totals, but the normative closure gate is direct-child based.

### 6.4 Hierarchy mutation rules

`task.create` may assign a parent only when:

- the parent exists in the same project;
- the parent is not `WORK_COMPLETE`, `VERIFIED_DONE`, `FAILED`, `ABANDONED`, or `CANCELLED`;
- the resulting depth is at most 16;
- the child requirement is valid.

A new `task.reparent` command is compare-and-set and requires:

- `task_id`;
- `expected_parent_task_id` (empty means root);
- `new_parent_task_id` (empty means detach);
- `parent_requirement` when attaching;
- actor session and idempotency key.

Reparent/detach is allowed only while the task:

- is `READY`;
- has never had a run;
- has no handoff;
- has no active reservation;
- is not claimed;
- has no children unless the entire subtree is moved intact;
- would remain in the same project and cycle-free.

The command moves the whole subtree because descendants reference the moved Task, not its former ancestors. It rejects a target inside that subtree.

Do not permit an unrestricted SQL-style parent update.

### 6.5 Derived hierarchy readiness

Do not mutate parent state when a child changes. Compute a read model in the same transaction/snapshot as the task view.

Suggested JSON shape:

```json
{
  "hierarchy": {
    "parent_task_id": "task_parent",
    "parent_requirement": "REQUIRED",
    "completion_policy": "ALL_REQUIRED_CHILDREN_VERIFIED",
    "depth": 2,
    "direct_children": {
      "total": 5,
      "required": 4,
      "optional": 1,
      "verified": 2,
      "work_complete": 1,
      "open": 1,
      "failed_or_cancelled": 0
    },
    "descendant_count": 12,
    "closure_readiness": "BLOCKED_BY_REQUIRED_CHILDREN",
    "blocking_child_ids": ["task_a", "task_b"],
    "blocking_child_count": 2,
    "blocking_child_ids_truncated": false
  }
}
```

`closure_readiness` values:

- `NOT_APPLICABLE`: `INDEPENDENT` or no required children;
- `BLOCKED_BY_REQUIRED_CHILDREN`;
- `REQUIRED_CHILDREN_VERIFIED`;
- `CORRUPT_HIERARCHY`.

Return a bounded child ID sample plus an exact count. Never emit thousands of IDs by default.

Do not report a weighted or percentage completion score in v1. A parent with 209 unequal children makes such a number deceptive.

## 7. Verified closure integration

### 7.1 Central verified-transition operation

Introduce one transaction-scoped operation used by every path that reaches `TaskVerifiedDone`:

```text
VerifyTask(repositories, project, task_id, actor_session_id, evidence, now)
```

It must:

1. load the canonical Task;
2. validate actor/project ownership rules;
3. validate the current state and evidence;
4. inspect direct required children when policy is gated;
5. reject closure if any required child is not `VERIFIED_DONE`;
6. transition the Task;
7. reconcile dependency notifications exactly once;
8. mark a linked promoted Todo `DONE` in the same transaction;
9. return canonical post-state and hierarchy readiness.

`task.transition(to=VERIFIED_DONE)`, `candidate.close`, and any future verifier command must call this operation.

### 7.2 Candidate close

Extend `inspectCandidateRepositories` and the write-time reinspection so a gated parent returns read-only readiness before closure.

When required children remain, the result must remain non-mutating:

```json
{
  "ready_to_close": false,
  "missing_evidence": [
    "required child task_a is not VERIFIED_DONE",
    "required child task_b is not VERIFIED_DONE"
  ],
  "next_argv": ["omg", "board", "task", "--task", "task_parent", "--include-children", "--json"]
}
```

Closing a child:

- never closes its parent;
- never changes its parent's canonical state;
- may make the parent's derived readiness `REQUIRED_CHILDREN_VERIFIED`;
- never releases parent/sibling reservations;
- may complete only the Todo linked to that child.

Closing a parent:

- never force-closes children;
- releases only reservations owned by the parent Task;
- archives only the source session proven by the parent candidate/run;
- still requires the parent's own accepted handoff/integration/exact-real-canary/source-cleanup evidence.

### 7.3 WORK_LITE

`task.finish-lite` moves Task/Run to `WORK_COMPLETE`; it must not mark a linked Todo `DONE` and must not satisfy an `ALL_REQUIRED_CHILDREN_VERIFIED` parent gate.

Its result should include:

```text
linked_todo_state=PROMOTED
awaiting_verification=true
parent_closure_readiness=<derived value>
```

## 8. Todo promotion

### 8.1 Single promotion

`todo.promote` converts one `READY` Todo into one `READY` Task and records the immutable link in one Store.Write transaction.

It does not create a run, claim the Task, reserve paths, or infer dependencies.

### 8.2 Recursive promotion

`todo.promote --recursive` maps:

```text
root Todo       -> root/attached Task
child Todo      -> child Task
nested Todo     -> nested Task
```

All source Todos must be `READY`. The transaction must:

1. read and validate the complete source subtree;
2. reject cycles, cross-project rows, terminal rows, existing promotion links, and depth overflow;
3. validate an optional external parent Task;
4. validate an expected tree fingerprint;
5. create Tasks in parent-before-child order;
6. create one link for every Todo/Task pair;
7. move every source Todo to `PROMOTED`;
8. persist one receipt containing the deterministic mapping;
9. commit all records together or roll back all records.

No partial promotion is permitted.

### 8.3 Promotion plan and fingerprint

Add a read-only `todo.promote-plan` command. It returns:

- ordered Todo IDs;
- projected Task parent mapping;
- requested completion policies/edge requirements;
- warnings;
- `tree_sha256` over canonical IDs, parent IDs, states, titles, and `updated_at` values.

Recursive `todo.promote` requires that hash. The write transaction recalculates it. A mismatch returns `state_conflict` with cause `promotion_plan_changed` and no receipt/mutation.

For a one-item promotion, the expected hash remains recommended but may be optional during the compatibility period.

### 8.4 Promotion links

Use a canonical one-to-one link table instead of duplicating IDs in both entities:

```sql
CREATE TABLE todo_task_links (
    todo_id TEXT PRIMARY KEY REFERENCES todos(id),
    task_id TEXT NOT NULL UNIQUE REFERENCES tasks(id),
    linked_at TEXT NOT NULL
);
```

A later supersession feature may add immutable link history. v1 permits one promoted Task per Todo.

## 9. Persistence design

### 9.1 Task hierarchy migration

Add to `tasks`:

```sql
completion_policy TEXT NOT NULL DEFAULT 'INDEPENDENT'
  CHECK(completion_policy IN ('INDEPENDENT','ALL_REQUIRED_CHILDREN_VERIFIED')),
parent_requirement TEXT NOT NULL DEFAULT 'OPTIONAL'
  CHECK(parent_requirement IN ('REQUIRED','OPTIONAL'))
```

Add:

```sql
CREATE INDEX idx_tasks_project_parent_display
ON tasks(project_id, parent_task_id, display_number, id);
```

SQLite cannot express the full cross-row cycle invariant as a simple CHECK. Enforce it in the application transaction with a recursive CTE and test the repository independently. `doctor` must also audit the complete graph.

### 9.2 Todo tables

Suggested first schema:

```sql
CREATE TABLE project_todo_sequences (
    project_id TEXT PRIMARY KEY REFERENCES projects(id),
    next_number INTEGER NOT NULL CHECK(next_number >= 1)
);

CREATE TABLE todos (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id),
    display_number INTEGER NOT NULL CHECK(display_number >= 1),
    title TEXT NOT NULL CHECK(length(trim(title)) > 0),
    summary TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL CHECK(state IN ('OPEN','READY','PROMOTED','DONE','CANCELLED')),
    priority TEXT NOT NULL DEFAULT 'NORMAL'
      CHECK(priority IN ('LOW','NORMAL','HIGH','URGENT')),
    parent_todo_id TEXT REFERENCES todos(id),
    created_by_session_id TEXT REFERENCES agent_sessions(id),
    due_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    promoted_at TEXT,
    closed_at TEXT,
    CHECK(parent_todo_id IS NULL OR parent_todo_id <> id),
    UNIQUE(project_id, display_number)
);

CREATE INDEX idx_todos_project_state_priority
ON todos(project_id, state, priority, display_number);

CREATE INDEX idx_todos_project_parent_display
ON todos(project_id, parent_todo_id, display_number, id);

CREATE TABLE todo_labels (
    todo_id TEXT NOT NULL REFERENCES todos(id) ON DELETE CASCADE,
    label TEXT NOT NULL,
    PRIMARY KEY(todo_id, label)
);

CREATE TABLE todo_task_links (
    todo_id TEXT PRIMARY KEY REFERENCES todos(id),
    task_id TEXT NOT NULL UNIQUE REFERENCES tasks(id),
    linked_at TEXT NOT NULL
);
```

The summary is bounded prompt-safe text, not a raw prompt/transcript container. Default board/export output must continue to pass through safe-text/redaction policy.

### 9.3 Repository boundaries

Add a separate `TodoRepository` and `Repositories.Todos()` rather than placing Todo CRUD in `CoordinationRepository`.

Rationale:

- Todo is intake, not execution coordination;
- repository capabilities remain narrow;
- promotion can still call `repositories.Todos()` and `repositories.Coordination()` inside one Store.Write transaction;
- future adapters cannot accidentally give Todos run/reservation authority.

Task hierarchy queries remain part of `CoordinationRepository` because they operate on canonical Tasks.

Suggested repository methods:

```go
type TodoRepository interface {
    Create(context.Context, todo.Todo) (todo.Todo, error)
    Get(context.Context, todo.ID) (todo.Todo, bool, error)
    List(context.Context, domain.ProjectID, todo.Filter) ([]todo.Todo, error)
    ListChildren(context.Context, domain.ProjectID, todo.ID) ([]todo.Todo, error)
    Update(context.Context, todo.Todo, time.Time) (todo.Todo, error)
    Transition(context.Context, todo.ID, todo.State, time.Time) (todo.Todo, error)
    LinkTask(context.Context, todo.ID, lineage.ID, time.Time) error
    GetTaskLink(context.Context, todo.ID) (lineage.ID, bool, error)
    GetTodoForTask(context.Context, lineage.ID) (todo.Todo, bool, error)
}
```

Hierarchy-specific coordination methods should include bounded ancestor/descendant reads and a compare-and-set reparent operation. They must not expose an unvalidated generic parent update.

## 10. CLI and application commands

### 10.1 Todo commands

Canonical application/MCP commands:

```text
todo.create
todo.get
todo.list
todo.update
todo.transition
todo.children
todo.promote-plan
todo.promote
```

CLI:

```text
omg todo create
omg todo get
omg todo list
omg todo update
omg todo transition
omg todo children
omg todo promote-plan
omg todo promote
```

Do not initially create separate `close` and `cancel` business operations; they are strict payload wrappers over `todo.transition`. Human-friendly aliases may be added later without changing the dispatcher contract.

Useful list filters:

```text
state, priority, label, parent_todo_id, root_only, due_before,
source_session_id, promoted, needs_review
```

### 10.2 Task hierarchy commands

Extend existing `task.create` payload with:

```json
{
  "parent_task_id": "optional",
  "parent_requirement": "REQUIRED",
  "completion_policy": "INDEPENDENT"
}
```

Add:

```text
task.children      read-only direct child view
task.hierarchy     read-only ancestry/readiness summary
task.reparent      guarded compare-and-set mutation
task.policy-set    guarded completion-policy mutation
```

Use existing `board task`/`board tree` for rich rendering rather than creating a second incompatible tree renderer. Extend them with bounded `--depth` and `--include-children` controls.

`task.policy-set` is permitted only before execution starts: no run, no handoff, no active reservation, task still `READY`, and no child already beyond `READY`. This prevents changing closure obligations after work has begun.

### 10.3 Strict payload contracts

All new commands must:

- reject unknown fields;
- require idempotency keys for mutations;
- reject secret-like stable metadata;
- use explicit enums rather than booleans for policies;
- return arrays as `[]`, not `null`;
- preserve the same result/error shape across CLI and MCP.

### 10.4 MCP

Add the same canonical command names to `allowedCommands`. MCP must not implement Todo/hierarchy rules itself. It only validates the outer frame and forwards a typed application request.

## 11. Idempotency requirements

The generic Store receipt contract should gain a canonical typed request fingerprint before recursive promotion ships.

Required semantics for every mutation:

- same operation + same key + same canonical typed payload: replay canonical result;
- same operation + same key + different canonical typed payload: non-retryable `idempotency_conflict`;
- same key reused for another operation: conflict;
- read-only not-ready checks: no receipt;
- failed transaction: no success receipt and no partial rows.

`candidate.close` currently implements a local payload SHA-256 wrapper. Extract that behavior into a reusable application/store mechanism rather than duplicating custom fingerprint code in Todo promotion and hierarchy mutation.

Canonical hashing must occur after strict typed decoding and normalization so JSON field order/whitespace do not change identity.

## 12. Structured error and recovery contract

Use the existing additive error detail schema. Do not create transport-specific hierarchy errors.

Recommended classifications:

| Condition | reason_code | cause/current state detail |
|---|---|---|
| parent/todo missing | `entity_not_found` | relevant parent ID |
| same-project violation/cycle/depth | `state_conflict` | `cross_project_parent`, `hierarchy_cycle`, or `hierarchy_depth_exceeded` |
| parent closed or task no longer movable | `invalid_transition` | canonical task state |
| required children not verified | `missing_evidence` | bounded child IDs and counts |
| promotion plan changed | `state_conflict` | `promotion_plan_changed` |
| stale expected parent | `state_conflict` | expected/current parent IDs |
| same key/different payload | `idempotency_conflict` | canonical key guidance |
| malformed enum/payload | `payload_validation` | prerequisites and contextual help |

Recovery actions must be real, copyable argv arrays. Examples:

```text
omg board task --project <root> --task <parent> --include-children --json
omg task hierarchy --project <root> --payload '{"task_id":"..."}' --json
omg todo promote-plan --project <root> --payload-file promote.json --json
```

No recovery path may run a canary, mutate Git, close children, or reparent tasks automatically.

## 13. Reservation semantics

Hierarchy does not grant overlapping write authority.

Rules:

- parent-child, ancestor-descendant, and sibling overlaps use the normal reservation conflict policy;
- do not automatically allow overlaps within the same root Task;
- conflict detail may add `hierarchy_relation=ancestor|descendant|sibling|unrelated` to improve operator judgment;
- closing a Task releases only reservations owned by that Task;
- recursive Todo promotion creates no reservations;
- parent reservations are not inherited by children;
- future explicit shared/coordinating reservation modes must remain opt-in and audited.

This is intentionally stricter than treating a hierarchy as one trust boundary. Sibling agents may still overwrite the same file.

## 14. Views, preflight, and health reporting

### 14.1 Board/task view

Add:

- completion policy and parent requirement;
- parent chain;
- direct child state counts;
- bounded blocker sample/count;
- descendant count;
- linked Todo ID/state where safe;
- `closure_readiness` separate from Task state.

Do not collapse `WORK_COMPLETE` children into verified progress.

### 14.2 Preflight

For the current Task, show:

- parent chain;
- whether this child is required or optional;
- required children blocking this Task's closure;
- parent Tasks that would become ready if this Task verifies;
- linked Todo and its derived promotion health.

### 14.3 Status/doctor

Add project-level counters:

```text
open_todos
ready_todos
promoted_todos
promoted_todos_awaiting_verification
todos_needing_review
root_tasks
child_tasks
parent_tasks
orphan_parent_refs
hierarchy_cycles
hierarchy_depth_violations
parents_blocked_by_required_children
parents_ready_for_verification
```

`doctor` must fail or warn explicitly on hierarchy corruption. Rendering a cycle-suppressed tree is not sufficient integrity evidence.

An initialized project with zero records should report `ledger_state=EMPTY`, not appear equivalent to a populated healthy adoption.

## 15. Markdown interoperability

The database is canonical. `TODO.md`, `todo.md`, issue text, and agent scratch lists are adapters/import sources.

Initial behavior:

```text
omg todo export --format markdown
omg todo import-markdown --dry-run
omg todo import-markdown --apply --idempotency-key ...
```

Stable markers prevent duplicates:

```markdown
- [ ] Release readiness audit <!-- omg:todo:todo_123 -->
```

Rules:

- no file watcher;
- no silent two-way synchronization;
- import is explicit, dry-run-first, and idempotent;
- unknown/ambiguous nesting becomes a warning, not an inferred dependency;
- historical project files remain untouched unless the human explicitly chooses an export destination;
- export never claims Task verification from a Markdown checkbox.

## 16. Implementation map

### 16.1 Existing hierarchy hardening

Likely source areas:

- `internal/domain/lineage/lineage.go`: policy/requirement enums and validation;
- `internal/domain/lineage/hierarchy.go`: pure hierarchy/readiness rules;
- `internal/app/lineage/service.go`: guarded create/reparent/policy operations;
- `internal/app/dependency/service.go`: central verified transition and dependency reconciliation;
- `internal/app/dispatch_candidate.go`: read/write-time child prerequisites;
- `internal/store/sqlite/coordination.go`: recursive queries, CAS reparent, indexes/columns;
- `internal/ports/ports.go`: bounded hierarchy methods;
- `internal/app/query`: hierarchy read model and status counters;
- `internal/view`: readiness/count rendering;
- CLI command help, payload contracts, examples, shell completion, and MCP allowlist.

### 16.2 Todo

New/extended areas:

- `internal/domain/todo/todo.go`;
- `internal/app/todo/service.go`;
- `internal/app/dispatch_todo.go`;
- `internal/store/sqlite/todo.go` and a new ordered migration;
- `TodoRepository` in `internal/ports/ports.go`;
- CLI/MCP/payload/help/completion surfaces;
- query/view projections and import/export adapters.

### 16.3 Cross-domain finalization

The central verified operation should be the only place that combines:

- Task transition;
- dependency satisfaction;
- linked Todo completion;
- hierarchy prerequisite checks.

`candidate.close` retains handoff/integration/canary/source-cleanup/session/reservation responsibilities and delegates the Task verification portion to that operation.

## 17. Rollout plan

### P0 — prerequisite integrity and observability

1. Add read-only hierarchy audit to doctor/preflight.
2. Add project parent index and recursive query helpers.
3. Add compatible `completion_policy`/`parent_requirement` columns.
4. Enforce parent existence, same project, terminal-parent rejection, depth, and cycle rules on new task creation.
5. Add hierarchy summary to Task/board/status.
6. Centralize every Task `VERIFIED_DONE` transition.
7. Add child-gate checking to central verification and `candidate.close`.
8. Generalize typed-payload fingerprint idempotency.
9. Add tests against legacy `INDEPENDENT/OPTIONAL` rows so existing Pygmalion data remains writable/closable.

### P1 — Todo canonical intake

1. Add Todo domain/repository/schema/migration.
2. Add create/get/list/update/transition/children commands.
3. Add board/preflight/status counters.
4. Add structured errors and CLI/MCP parity.
5. Add single Todo promotion and linked Todo completion on Task verification.

### P2 — recursive promotion and hierarchy mutation

1. Add promote-plan/tree fingerprint.
2. Add atomic recursive promotion.
3. Add guarded task reparent/detach and policy-set.
4. Add bounded subtree/ancestry rendering.
5. Add hierarchy relation to reservation conflict details.

### P3 — interoperability and adoption

1. Add explicit Markdown import/export.
2. Improve dependency creation UX during/after promotion without inferring order.
3. Add migration/adoption reports for existing giant parent trees.
4. Evaluate a future container-only initiative/group Task using real data, not assumption.

## 18. Required tests

### 18.1 Domain tests

- valid Todo transitions and terminal immutability;
- task/todo self-parent rejection;
- indirect cycle rejection;
- maximum-depth boundary;
- direct required-child readiness;
- optional child exclusion from closure gate;
- no automatic parent state mutation;
- Todo remains promoted when linked Task fails/cancels.

### 18.2 SQLite/repository tests

- same-project parent enforcement;
- missing parent rejection;
- recursive CTE cycle/depth detection;
- concurrent create/reparent race with one canonical winner;
- CAS expected-parent mismatch;
- atomic Todo display numbering;
- recursive promotion all-or-nothing;
- unique Todo/Task links;
- migration defaults preserve old rows;
- doctor detects seeded corrupt hierarchy fixtures;
- large flat parent fixture with at least 250 children remains bounded and deterministic.

### 18.3 Application tests

- `task.create` new parent validation;
- `task.reparent` rejects task with run/handoff/reservation/claim;
- `task.policy-set` lifecycle guard;
- raw `task.transition(to=VERIFIED_DONE)` delegates to central verification;
- gated parent closure is read-only and receipt-free when children are incomplete;
- closing final required child only changes derived parent readiness;
- parent still requires its own verification evidence;
- linked Todo completes in the same successful transaction;
- transaction failure leaves Task and Todo unchanged;
- `finish-lite` leaves Todo promoted;
- same idempotency key/same payload replays;
- same key/different typed payload conflicts;
- recursive promotion plan hash mismatch is read-only.

### 18.4 CLI/MCP/shell tests

- strict JSON payload validation;
- human and JSON hierarchy outputs contain the same canonical facts;
- MCP allowlist and structured error detail parity;
- recovery argv is copyable and side-effect-labelled correctly;
- shell completion includes new commands and never evaluates payload data;
- arrays serialize as `[]` rather than `null`.

### 18.5 Regression fixtures

- import a Pygmalion-like legacy graph with 209 optional children and verify the parent is not newly blocked;
- import an industry-os-like sequence with no edges and verify no hierarchy is invented;
- empty Zoomzi-like ledger reports `ledger_state=EMPTY`;
- a missing cmux-like project produces a not-found/unavailable evidence report rather than fabricated metrics.

## 19. Existing OMG feedback and priorities

### P0 feedback

1. **Formalize existing parent links before adding Todo.** The data already uses them heavily.
2. **Centralize verified closure.** Non-empty evidence bytes alone are not a sufficient hierarchy proof boundary.
3. **Add generic payload-fingerprint idempotency.** Candidate-specific hashing should not be copied into every new atomic operation.
4. **Expose closure backlog and hierarchy readiness together.** Pygmalion's `WORK_COMPLETE` and `finished_unclosed` backlog is a product-level operational issue.
5. **Audit graph corruption explicitly.** A renderer's cycle suppression is a UI guard, not canonical integrity.

### P1 feedback

1. **Make hierarchy/dependency creation discoverable.** Real ledgers use parent links but almost no explicit dependencies.
2. **Clarify continuation semantics.** industry-os demonstrates title-based continuation that should use parent/dependency/supersession intentionally.
3. **Differentiate empty adoption from healthy populated state.** Zoomzi's empty ledger should prompt a safe onboarding next action.
4. **Add bounded aggregate queries.** Giant parents must not explode CLI/MCP response size.

### P2 feedback

1. Evaluate group/initiative Tasks only after hierarchy semantics stabilize.
2. Add Markdown intake only as an explicit adapter, not source of truth.
3. Consider dependency suggestions in promotion plans, but never create inferred blocking edges without explicit apply input.
4. Add weighted progress only if OMG gains explicit effort weights; child count is not effort.

## 20. Explicit non-goals

This design does not authorize or implement:

- automatic task decomposition by a model;
- automatic parent completion;
- automatic child closure/cancellation/reparenting;
- hierarchy-based reservation conflict bypass;
- automatic Git, canary, commit, push, deploy, cleanup, or worktree mutation;
- implicit two-way synchronization with TODO files;
- migration of existing parent policies to gated behavior without explicit operator action;
- treating `WORK_COMPLETE` as verified completion;
- treating unavailable project histories as portability proof.

## 21. Acceptance criteria for implementation

The feature is complete only when:

1. existing ledgers migrate without changing effective closure behavior;
2. new hierarchy mutations cannot create missing-parent, cross-project, cyclic, terminal-parent, or over-depth structures;
3. Task state and hierarchy readiness remain distinct;
4. every `VERIFIED_DONE` path uses one central prerequisite/finalization operation;
5. a gated parent cannot close while a required direct child is not verified;
6. child closure never auto-closes or mutates the parent state;
7. Todo promotion is atomic and strongly idempotent;
8. linked Todo completion is atomic with Task verification;
9. Todo never owns execution/Git/reservation/handoff authority;
10. CLI, MCP, shell, query, view, docs, migrations, and tests expose one consistent contract;
11. real Pygmalion-like large trees remain bounded and observable;
12. no commit, push, deploy, Git mutation, canary execution, or unrelated dirty-tree cleanup occurs as a side effect of these lifecycle operations.
