-- Immutable canonical P3 coordination schema. All timestamps are UTC RFC3339 strings.
PRAGMA foreign_keys = ON;

CREATE TABLE humans (
    id TEXT PRIMARY KEY, display_name TEXT NOT NULL, provenance_confidence TEXT NOT NULL CHECK(provenance_confidence IN ('explicit','verified','asserted','unknown')),
    created_at TEXT NOT NULL, supersedes_id TEXT REFERENCES humans(id), CHECK(supersedes_id IS NULL OR supersedes_id <> id)
);
CREATE TABLE agent_sessions (
    id TEXT PRIMARY KEY, project_id TEXT NOT NULL REFERENCES projects(id), human_id TEXT REFERENCES humans(id),
    lineage_kind TEXT NOT NULL CHECK(lineage_kind IN ('human_direct','agent_delegated','resumed','adopted','imported')),
    runtime TEXT NOT NULL, role TEXT NOT NULL, instruction_source TEXT NOT NULL CHECK(instruction_source IN ('human','delegation_token','resume','adoption','import')),
    source_ref TEXT NOT NULL, parent_session_id TEXT REFERENCES agent_sessions(id), root_session_id TEXT REFERENCES agent_sessions(id),
    continuation_of_id TEXT REFERENCES agent_sessions(id), task_id TEXT, worktree_ref TEXT NOT NULL DEFAULT '',
    native_access_state TEXT NOT NULL CHECK(native_access_state IN ('available','missing','unreadable','unsupported')),
    runtime_home TEXT, native_session_id TEXT, native_session_ref TEXT, native_session_started_at TEXT,
    native_session_fingerprint TEXT, native_parent_session_id TEXT,
    started_at TEXT NOT NULL, ended_at TEXT, interrupted_at TEXT, heartbeat_at TEXT, supersedes_id TEXT REFERENCES agent_sessions(id),
    CHECK(parent_session_id IS NULL OR parent_session_id <> id), CHECK(supersedes_id IS NULL OR supersedes_id <> id),
    CHECK((native_access_state = 'unsupported' AND runtime_home IS NULL AND native_session_id IS NULL AND native_session_ref IS NULL AND native_session_started_at IS NULL AND native_session_fingerprint IS NULL AND native_parent_session_id IS NULL) OR (native_access_state IN ('available','missing','unreadable') AND native_session_id IS NOT NULL AND native_session_ref IS NOT NULL AND native_session_fingerprint IS NOT NULL AND length(native_session_fingerprint) = 64 AND native_session_fingerprint NOT GLOB '*[^0-9a-f]*'))
);
CREATE TABLE delegation_tokens (
    id TEXT PRIMARY KEY, project_id TEXT NOT NULL REFERENCES projects(id), task_id TEXT REFERENCES tasks(id), parent_session_id TEXT NOT NULL REFERENCES agent_sessions(id),
    algorithm TEXT NOT NULL CHECK(algorithm = 'PBKDF2-HMAC-SHA256'), iterations INTEGER NOT NULL CHECK(iterations >= 100000), salt BLOB NOT NULL CHECK(length(salt)>=16), verifier BLOB NOT NULL UNIQUE CHECK(length(verifier)=32), issued_at TEXT NOT NULL, expires_at TEXT NOT NULL,
    revoked_at TEXT, consumed_at TEXT, consumed_by_session_id TEXT REFERENCES agent_sessions(id),
    CHECK(expires_at > issued_at), CHECK((consumed_at IS NULL) = (consumed_by_session_id IS NULL))
);
CREATE TABLE project_task_sequences (project_id TEXT PRIMARY KEY REFERENCES projects(id), next_number INTEGER NOT NULL CHECK(next_number > 0));
CREATE TABLE tasks (
    id TEXT PRIMARY KEY, project_id TEXT NOT NULL REFERENCES projects(id), display_number INTEGER NOT NULL CHECK(display_number > 0),
    title TEXT NOT NULL, state TEXT NOT NULL CHECK(state IN ('READY','CLAIMED','IN_PROGRESS','WAITING','BLOCKED','REWORK','WORK_COMPLETE','VERIFIED_DONE','FAILED','ABANDONED','INTERRUPTED','STALE','CANCELLED')),
    created_by_session_id TEXT REFERENCES agent_sessions(id), claimed_by_session_id TEXT REFERENCES agent_sessions(id), claimed_at TEXT,
    parent_task_id TEXT REFERENCES tasks(id), created_at TEXT NOT NULL, updated_at TEXT NOT NULL, supersedes_id TEXT REFERENCES tasks(id),
    UNIQUE(project_id,display_number), CHECK(parent_task_id IS NULL OR parent_task_id <> id), CHECK(supersedes_id IS NULL OR supersedes_id <> id),
    CHECK((claimed_by_session_id IS NULL) = (claimed_at IS NULL))
);
CREATE TABLE task_runs (
    id TEXT PRIMARY KEY, task_id TEXT NOT NULL REFERENCES tasks(id), session_id TEXT NOT NULL REFERENCES agent_sessions(id),
    state TEXT NOT NULL CHECK(state IN ('RUNNING','WAITING','BLOCKED','REWORK','WORK_COMPLETE','VERIFIED_DONE','FAILED','ABANDONED','INTERRUPTED','STALE','CANCELLED')),
    evidence_json BLOB, started_at TEXT NOT NULL, ended_at TEXT, parent_lost_at TEXT, supersedes_id TEXT REFERENCES task_runs(id),
    CHECK(supersedes_id IS NULL OR supersedes_id <> id), CHECK(state <> 'VERIFIED_DONE' OR evidence_json IS NOT NULL)
);
CREATE TABLE session_heartbeats (id TEXT PRIMARY KEY, session_id TEXT NOT NULL REFERENCES agent_sessions(id), observed_at TEXT NOT NULL, liveness TEXT NOT NULL CHECK(liveness IN ('alive','stale','interrupted')), detail_json BLOB NOT NULL DEFAULT '{}');
CREATE TABLE lineage_events (id TEXT PRIMARY KEY, aggregate_type TEXT NOT NULL, aggregate_id TEXT NOT NULL, event_type TEXT NOT NULL, payload_json BLOB NOT NULL, occurred_at TEXT NOT NULL, supersedes_event_id TEXT REFERENCES lineage_events(id));

-- P3-B frozen storage contracts.
CREATE TABLE progress_updates (id TEXT PRIMARY KEY, task_id TEXT NOT NULL REFERENCES tasks(id), run_id TEXT REFERENCES task_runs(id), session_id TEXT REFERENCES agent_sessions(id), phase TEXT NOT NULL, done_json BLOB NOT NULL, doing_json BLOB NOT NULL, next_json BLOB NOT NULL, created_at TEXT NOT NULL, supersedes_id TEXT REFERENCES progress_updates(id), CHECK(supersedes_id IS NULL OR supersedes_id <> id));
CREATE TABLE task_dependencies (id TEXT PRIMARY KEY, blocker_task_id TEXT NOT NULL REFERENCES tasks(id), blocked_task_id TEXT NOT NULL REFERENCES tasks(id), kind TEXT NOT NULL CHECK(kind IN ('hard','soft','informational')), unblock_on TEXT NOT NULL CHECK(unblock_on IN ('work_complete','verified_done')), satisfied_at TEXT, satisfaction_evidence_json BLOB, unblock_message_id TEXT, created_at TEXT NOT NULL, UNIQUE(blocker_task_id,blocked_task_id), CHECK(blocker_task_id <> blocked_task_id));
CREATE TABLE messages (id TEXT PRIMARY KEY, thread_id TEXT NOT NULL, project_id TEXT NOT NULL REFERENCES projects(id), sender_session_id TEXT REFERENCES agent_sessions(id), related_task_id TEXT REFERENCES tasks(id), type TEXT NOT NULL CHECK(type IN ('NOTICE','QUESTION','DEPENDENCY','CONFLICT','HANDOFF','DONE','BLOCKED','CANCEL','ACK')), subject TEXT NOT NULL CHECK(length(subject)<=240), body TEXT NOT NULL, created_at TEXT NOT NULL, supersedes_id TEXT REFERENCES messages(id), CHECK(supersedes_id IS NULL OR supersedes_id <> id));
CREATE TABLE message_recipients (id TEXT PRIMARY KEY, message_id TEXT NOT NULL REFERENCES messages(id), recipient_session_id TEXT REFERENCES agent_sessions(id), recipient_human_id TEXT REFERENCES humans(id), recipient_task_id TEXT REFERENCES tasks(id), recipient_role TEXT, delivered_at TEXT, read_at TEXT, ack_at TEXT, CHECK((CASE WHEN recipient_session_id IS NOT NULL THEN 1 ELSE 0 END + CASE WHEN recipient_human_id IS NOT NULL THEN 1 ELSE 0 END + CASE WHEN recipient_task_id IS NOT NULL THEN 1 ELSE 0 END + CASE WHEN recipient_role IS NOT NULL THEN 1 ELSE 0 END) = 1));
CREATE TABLE handoffs (id TEXT PRIMARY KEY, task_id TEXT NOT NULL REFERENCES tasks(id), target_task_id TEXT REFERENCES tasks(id), run_id TEXT REFERENCES task_runs(id), source_session_id TEXT NOT NULL REFERENCES agent_sessions(id), target_session_id TEXT REFERENCES agent_sessions(id), supersedes_id TEXT REFERENCES handoffs(id), summary TEXT NOT NULL, final_output_text TEXT, final_output_hash TEXT, final_output_policy TEXT, changed_files_json BLOB NOT NULL, commits_json BLOB NOT NULL, verification_json BLOB NOT NULL, risks_json BLOB NOT NULL, actions_json BLOB NOT NULL, status TEXT NOT NULL CHECK(status IN ('draft','submitted','accepted','rejected','superseded')), created_at TEXT NOT NULL, CHECK(supersedes_id IS NULL OR supersedes_id <> id));
CREATE TABLE handoff_decisions (id TEXT PRIMARY KEY, handoff_id TEXT NOT NULL REFERENCES handoffs(id), decision TEXT NOT NULL CHECK(decision IN ('accepted','rejected')), decided_by_session_id TEXT NOT NULL REFERENCES agent_sessions(id), created_at TEXT NOT NULL);
CREATE TABLE adoptions (id TEXT PRIMARY KEY, project_id TEXT NOT NULL REFERENCES projects(id), adopter_session_id TEXT NOT NULL REFERENCES agent_sessions(id), orphan_session_id TEXT REFERENCES agent_sessions(id), orphan_task_id TEXT REFERENCES tasks(id), orphan_handoff_id TEXT REFERENCES handoffs(id), git_asset_ref TEXT, reason TEXT NOT NULL, created_at TEXT NOT NULL, CHECK((CASE WHEN orphan_session_id IS NOT NULL THEN 1 ELSE 0 END + CASE WHEN orphan_task_id IS NOT NULL THEN 1 ELSE 0 END + CASE WHEN orphan_handoff_id IS NOT NULL THEN 1 ELSE 0 END + CASE WHEN git_asset_ref IS NOT NULL THEN 1 ELSE 0 END) = 1));

CREATE INDEX idx_sessions_project ON agent_sessions(project_id,started_at); CREATE INDEX idx_sessions_parent ON agent_sessions(parent_session_id); CREATE INDEX idx_sessions_native_identity ON agent_sessions(runtime,native_session_id);
CREATE INDEX idx_tokens_binding ON delegation_tokens(project_id,task_id,parent_session_id,expires_at); CREATE INDEX idx_tasks_project_state ON tasks(project_id,state,display_number); CREATE INDEX idx_runs_task ON task_runs(task_id,started_at); CREATE INDEX idx_heartbeats_session ON session_heartbeats(session_id,observed_at); CREATE INDEX idx_events_aggregate ON lineage_events(aggregate_type,aggregate_id,occurred_at); CREATE INDEX idx_progress_task ON progress_updates(task_id,created_at); CREATE INDEX idx_dependencies_blocked ON task_dependencies(blocked_task_id); CREATE INDEX idx_messages_project ON messages(project_id,created_at); CREATE INDEX idx_handoffs_task ON handoffs(task_id,created_at); CREATE INDEX idx_adoptions_project ON adoptions(project_id,created_at);
CREATE UNIQUE INDEX uq_recipient_session ON message_recipients(message_id,recipient_session_id) WHERE recipient_session_id IS NOT NULL;
CREATE UNIQUE INDEX uq_recipient_human ON message_recipients(message_id,recipient_human_id) WHERE recipient_human_id IS NOT NULL;
CREATE UNIQUE INDEX uq_recipient_task ON message_recipients(message_id,recipient_task_id) WHERE recipient_task_id IS NOT NULL;
CREATE UNIQUE INDEX uq_recipient_role ON message_recipients(message_id,recipient_role) WHERE recipient_role IS NOT NULL;
CREATE UNIQUE INDEX uq_handoff_terminal_decision ON handoff_decisions(handoff_id);
CREATE INDEX idx_handoff_decisions_handoff ON handoff_decisions(handoff_id,created_at);

CREATE TRIGGER humans_no_update BEFORE UPDATE ON humans BEGIN SELECT RAISE(ABORT,'humans are immutable'); END;
CREATE TRIGGER agent_sessions_no_update BEFORE UPDATE ON agent_sessions BEGIN SELECT RAISE(ABORT,'sessions are immutable'); END;
CREATE TRIGGER delegation_tokens_no_delete BEFORE DELETE ON delegation_tokens BEGIN SELECT RAISE(ABORT,'tokens are retained'); END;
CREATE TRIGGER lineage_events_no_update BEFORE UPDATE ON lineage_events BEGIN SELECT RAISE(ABORT,'lineage events are append-only'); END;
CREATE TRIGGER lineage_events_no_delete BEFORE DELETE ON lineage_events BEGIN SELECT RAISE(ABORT,'lineage events are append-only'); END;
CREATE TRIGGER progress_updates_no_update BEFORE UPDATE ON progress_updates BEGIN SELECT RAISE(ABORT,'progress is append-only'); END;
CREATE TRIGGER progress_updates_no_delete BEFORE DELETE ON progress_updates BEGIN SELECT RAISE(ABORT,'progress is append-only'); END;
CREATE TRIGGER messages_no_update BEFORE UPDATE ON messages BEGIN SELECT RAISE(ABORT,'messages are immutable'); END;
CREATE TRIGGER messages_no_delete BEFORE DELETE ON messages BEGIN SELECT RAISE(ABORT,'messages are immutable'); END;
CREATE TRIGGER handoffs_no_update BEFORE UPDATE ON handoffs BEGIN SELECT RAISE(ABORT,'handoffs are immutable'); END;
CREATE TRIGGER handoff_decisions_no_update BEFORE UPDATE ON handoff_decisions BEGIN SELECT RAISE(ABORT,'handoff decisions are append-only'); END;
CREATE TRIGGER handoff_decisions_no_delete BEFORE DELETE ON handoff_decisions BEGIN SELECT RAISE(ABORT,'handoff decisions are append-only'); END;
CREATE TRIGGER handoffs_no_delete BEFORE DELETE ON handoffs BEGIN SELECT RAISE(ABORT,'handoffs are immutable'); END;
CREATE TRIGGER adoptions_no_update BEFORE UPDATE ON adoptions BEGIN SELECT RAISE(ABORT,'adoptions are immutable'); END;
CREATE TRIGGER adoptions_no_delete BEFORE DELETE ON adoptions BEGIN SELECT RAISE(ABORT,'adoptions are immutable'); END;
