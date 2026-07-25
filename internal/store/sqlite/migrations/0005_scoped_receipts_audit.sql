DROP TRIGGER IF EXISTS git_observations_actor_lineage;
DROP TRIGGER IF EXISTS git_assets_owner_lineage;
DROP TRIGGER IF EXISTS git_assets_registered_owner_required;
DROP TRIGGER IF EXISTS git_observations_timestamp_valid;
DROP TRIGGER IF EXISTS git_assets_timestamps_valid;
DROP TRIGGER IF EXISTS git_observations_no_update;
DROP TRIGGER IF EXISTS git_observations_no_delete;
DROP TRIGGER IF EXISTS git_assets_no_update;
DROP TRIGGER IF EXISTS git_assets_no_delete;
ALTER TABLE git_observation_assets RENAME TO git_observation_assets_legacy;
ALTER TABLE git_observations RENAME TO git_observations_legacy;
CREATE TABLE git_observations_v5 (
 id TEXT PRIMARY KEY, project_id TEXT NOT NULL REFERENCES projects(id), idempotency_key TEXT NOT NULL,
 actor_session_id TEXT REFERENCES agent_sessions(id), task_id TEXT REFERENCES tasks(id), run_id TEXT REFERENCES task_runs(id), trigger_kind TEXT NOT NULL CHECK(trigger_kind='scan'),
 observed_at TEXT NOT NULL CHECK(length(observed_at)=30 AND observed_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]Z'),
 revision TEXT NOT NULL CHECK(revision='git-observation/v1'), observation_hash TEXT NOT NULL CHECK(length(observation_hash)=64 AND observation_hash NOT GLOB '*[^0-9a-f]*'), repository_state TEXT NOT NULL CHECK(repository_state IN ('unknown','non_git','bare','worktree')), confidence TEXT NOT NULL CHECK(confidence IN ('observed','incomplete','unknown')), common_dir TEXT NOT NULL CHECK(length(common_dir)<=4096), top_level TEXT NOT NULL CHECK(length(top_level)<=4096), default_branch TEXT NOT NULL CHECK(length(default_branch)<=1024), sequence_no INTEGER NOT NULL CHECK(sequence_no>0), UNIQUE(project_id,sequence_no),
 CHECK((actor_session_id IS NULL AND task_id IS NULL AND run_id IS NULL) OR (actor_session_id IS NOT NULL AND task_id IS NOT NULL AND run_id IS NOT NULL)),
 CHECK(actor_session_id IS NULL OR (length(actor_session_id)>0 AND length(task_id)>0 AND length(run_id)>0)),
 UNIQUE(project_id,idempotency_key)
);
INSERT INTO git_observations_v5 SELECT * FROM git_observations_legacy;
CREATE TABLE git_observation_assets_v5 (
 id TEXT PRIMARY KEY, observation_id TEXT NOT NULL REFERENCES git_observations_v5(id), fingerprint TEXT NOT NULL CHECK(length(fingerprint)=64 AND fingerprint NOT GLOB '*[^0-9a-f]*'), asset_type TEXT NOT NULL CHECK(asset_type IN ('main_worktree','linked_worktree','local_branch','detached_head')),
 first_seen_at TEXT NOT NULL CHECK(length(first_seen_at)=30 AND first_seen_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]Z'), last_seen_at TEXT NOT NULL CHECK(length(last_seen_at)=30 AND last_seen_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]Z' AND first_seen_at<=last_seen_at),
 facts_confidence TEXT NOT NULL CHECK(facts_confidence IN ('observed','incomplete','unknown')), worktree_path TEXT NOT NULL CHECK(length(worktree_path)<=4096), branch TEXT NOT NULL CHECK(length(branch)<=1024), detached INTEGER NOT NULL CHECK(detached IN (0,1)), branch_only INTEGER NOT NULL CHECK(branch_only IN (0,1)), default_branch INTEGER NOT NULL CHECK(default_branch IN (0,1)), worktree_prunable INTEGER NOT NULL CHECK(worktree_prunable IN (0,1)),
 status_branch TEXT NOT NULL CHECK(length(status_branch)<=1024), status_head TEXT NOT NULL CHECK(length(status_head)<=1024), status_detached INTEGER NOT NULL CHECK(status_detached IN (0,1)), status_upstream TEXT NOT NULL CHECK(length(status_upstream)<=1024), status_ahead INTEGER NOT NULL CHECK(status_ahead>=0 AND status_ahead<=1000000000), status_behind INTEGER NOT NULL CHECK(status_behind>=0 AND status_behind<=1000000000), status_tracked_dirty INTEGER NOT NULL CHECK(status_tracked_dirty>=0 AND status_tracked_dirty<=1000000000), status_untracked INTEGER NOT NULL CHECK(status_untracked>=0 AND status_untracked<=1000000000), status_ignored INTEGER NOT NULL CHECK(status_ignored>=0 AND status_ignored<=1000000000), status_confidence TEXT NOT NULL CHECK(status_confidence IN ('observed','incomplete','unknown')),
 merge_base_known INTEGER NOT NULL CHECK(merge_base_known IN (0,1)), merge_base_equals_head INTEGER NOT NULL CHECK(merge_base_equals_head IN (0,1)), default_counts_known INTEGER NOT NULL CHECK(default_counts_known IN (0,1)), default_ahead INTEGER NOT NULL CHECK(default_ahead>=0 AND default_ahead<=1000000000), default_behind INTEGER NOT NULL CHECK(default_behind>=0 AND default_behind<=1000000000), owner_registered INTEGER NOT NULL CHECK(owner_registered IN (0,1)), owner_state TEXT NOT NULL CHECK(owner_state IN ('unknown','active','waiting','ready','stale')),
 wt_path TEXT NOT NULL CHECK(length(wt_path)<=4096), wt_head TEXT NOT NULL CHECK(length(wt_head)<=1024), wt_branch TEXT NOT NULL CHECK(length(wt_branch)<=1024), wt_detached INTEGER NOT NULL CHECK(wt_detached IN (0,1)), wt_bare INTEGER NOT NULL CHECK(wt_bare IN (0,1)), wt_locked INTEGER NOT NULL CHECK(wt_locked IN (0,1)), wt_prunable INTEGER NOT NULL CHECK(wt_prunable IN (0,1)), wt_prune_reason TEXT NOT NULL CHECK(length(wt_prune_reason)<=4096), classification_json BLOB NOT NULL CHECK(json_valid(classification_json) AND json_type(classification_json)='array'), classification_confidence TEXT NOT NULL CHECK(classification_confidence IN ('observed','incomplete','unknown')),
 owner_session_id TEXT REFERENCES agent_sessions(id), owner_task_id TEXT REFERENCES tasks(id), owner_run_id TEXT REFERENCES task_runs(id), UNIQUE(observation_id,fingerprint)
);
INSERT INTO git_observation_assets_v5 SELECT * FROM git_observation_assets_legacy;
DROP TABLE git_observation_assets_legacy;
DROP TABLE git_observations_legacy;
ALTER TABLE git_observations_v5 RENAME TO git_observations;
ALTER TABLE git_observation_assets_v5 RENAME TO git_observation_assets;
DROP TRIGGER IF EXISTS audit_events_no_update;
DROP TRIGGER IF EXISTS audit_events_no_delete;

CREATE TABLE command_receipts_v5 (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL DEFAULT 'legacy',
    idempotency_key TEXT NOT NULL,
    outcome TEXT NOT NULL,
    result_json BLOB NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE(project_id, idempotency_key)
);
CREATE TABLE audit_events_v5 (
    sequence_no INTEGER PRIMARY KEY AUTOINCREMENT,
    id TEXT NOT NULL UNIQUE,
    project_id TEXT NOT NULL DEFAULT 'legacy',
    receipt_id TEXT REFERENCES command_receipts_v5(id),
    event_type TEXT NOT NULL,
    payload_json BLOB NOT NULL,
    occurred_at TEXT NOT NULL
);
INSERT INTO command_receipts_v5(id,project_id,idempotency_key,outcome,result_json,created_at)
SELECT id,'legacy',idempotency_key,outcome,result_json,created_at FROM command_receipts;
INSERT INTO audit_events_v5(id,project_id,receipt_id,event_type,payload_json,occurred_at)
SELECT id,'legacy',receipt_id,event_type,payload_json,occurred_at FROM audit_events ORDER BY rowid;
DROP TABLE audit_events;
DROP TABLE command_receipts;
ALTER TABLE command_receipts_v5 RENAME TO command_receipts;
ALTER TABLE audit_events_v5 RENAME TO audit_events;
CREATE TABLE migration_approvals (
    approval_id TEXT PRIMARY KEY,
    plan_id TEXT NOT NULL,
    project_id TEXT NOT NULL,
    approved_by TEXT NOT NULL,
    evidence_reference TEXT NOT NULL,
    from_version INTEGER NOT NULL,
    to_version INTEGER NOT NULL,
    checksums_json BLOB NOT NULL,
    backup_location TEXT NOT NULL,
    backup_checksum TEXT NOT NULL,
    command TEXT NOT NULL,
    approved_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    consumed_at TEXT NOT NULL
);
CREATE INDEX idx_audit_events_project_sequence ON audit_events(project_id,sequence_no DESC);
CREATE TRIGGER audit_events_no_update BEFORE UPDATE ON audit_events BEGIN SELECT RAISE(ABORT, 'audit events are append-only'); END;
CREATE TRIGGER audit_events_no_delete BEFORE DELETE ON audit_events BEGIN SELECT RAISE(ABORT, 'audit events are append-only'); END;

CREATE INDEX idx_git_observations_project_time ON git_observations(project_id,observed_at DESC,id DESC); CREATE INDEX idx_git_assets_fingerprint ON git_observation_assets(fingerprint,first_seen_at);
CREATE TRIGGER git_observations_actor_lineage BEFORE INSERT ON git_observations BEGIN SELECT CASE WHEN (NEW.actor_session_id IS NULL AND (NEW.task_id IS NOT NULL OR NEW.run_id IS NOT NULL)) OR (NEW.actor_session_id IS NOT NULL AND (NEW.task_id IS NULL OR NEW.run_id IS NULL)) THEN RAISE(ABORT,'git observation actor lineage mismatch') END; SELECT CASE WHEN NEW.actor_session_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM agent_sessions s JOIN tasks t ON t.id=NEW.task_id JOIN task_runs r ON r.id=NEW.run_id WHERE s.id=NEW.actor_session_id AND s.project_id=NEW.project_id AND t.project_id=NEW.project_id AND r.task_id=NEW.task_id AND r.session_id=NEW.actor_session_id) THEN RAISE(ABORT,'git observation actor lineage mismatch') END; END;
CREATE TRIGGER git_assets_owner_lineage BEFORE INSERT ON git_observation_assets BEGIN SELECT CASE WHEN NEW.owner_registered=0 AND (NEW.owner_session_id IS NOT NULL OR NEW.owner_task_id IS NOT NULL OR NEW.owner_run_id IS NOT NULL OR NEW.owner_state<>'unknown') THEN RAISE(ABORT,'unregistered git asset owner') END; SELECT CASE WHEN (NEW.owner_session_id IS NOT NULL OR NEW.owner_task_id IS NOT NULL OR NEW.owner_run_id IS NOT NULL) AND NEW.owner_registered<>1 THEN RAISE(ABORT,'unregistered git asset owner') END; SELECT CASE WHEN NEW.owner_session_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM agent_sessions s JOIN git_observations o ON o.id=NEW.observation_id WHERE s.id=NEW.owner_session_id AND s.project_id=o.project_id) THEN RAISE(ABORT,'git asset owner session mismatch') END; SELECT CASE WHEN NEW.owner_task_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM tasks t JOIN git_observations o ON o.id=NEW.observation_id WHERE t.id=NEW.owner_task_id AND t.project_id=o.project_id) THEN RAISE(ABORT,'git asset owner task mismatch') END; SELECT CASE WHEN NEW.owner_run_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM task_runs r JOIN tasks t ON t.id=r.task_id JOIN agent_sessions s ON s.id=r.session_id JOIN git_observations o ON o.id=NEW.observation_id WHERE r.id=NEW.owner_run_id AND t.project_id=o.project_id AND s.project_id=o.project_id AND (NEW.owner_task_id IS NULL OR r.task_id=NEW.owner_task_id) AND (NEW.owner_session_id IS NULL OR r.session_id=NEW.owner_session_id)) THEN RAISE(ABORT,'git asset owner run mismatch') END; END;
CREATE TRIGGER git_assets_registered_owner_required BEFORE INSERT ON git_observation_assets WHEN NEW.owner_registered=1 AND (NEW.owner_session_id IS NULL OR NEW.owner_task_id IS NULL OR NEW.owner_run_id IS NULL) BEGIN SELECT RAISE(ABORT,'registered git asset owner requires complete lineage'); END;
CREATE TRIGGER git_observations_timestamp_valid BEFORE INSERT ON git_observations BEGIN SELECT CASE WHEN COALESCE(strftime('%Y-%m-%dT%H:%M:%S',substr(NEW.observed_at,1,19))=substr(NEW.observed_at,1,19),0)=0 THEN RAISE(ABORT,'invalid git observation timestamp') END; END;
CREATE TRIGGER git_assets_timestamps_valid BEFORE INSERT ON git_observation_assets BEGIN SELECT CASE WHEN COALESCE(strftime('%Y-%m-%dT%H:%M:%S',substr(NEW.first_seen_at,1,19))=substr(NEW.first_seen_at,1,19),0)=0 OR COALESCE(strftime('%Y-%m-%dT%H:%M:%S',substr(NEW.last_seen_at,1,19))=substr(NEW.last_seen_at,1,19),0)=0 THEN RAISE(ABORT,'invalid git asset timestamp') END; END;
CREATE TRIGGER git_observations_no_update BEFORE UPDATE ON git_observations BEGIN SELECT RAISE(ABORT,'git observations are immutable'); END; CREATE TRIGGER git_observations_no_delete BEFORE DELETE ON git_observations BEGIN SELECT RAISE(ABORT,'git observations are retained'); END; CREATE TRIGGER git_assets_no_update BEFORE UPDATE ON git_observation_assets BEGIN SELECT RAISE(ABORT,'git observation assets are immutable'); END; CREATE TRIGGER git_assets_no_delete BEFORE DELETE ON git_observation_assets BEGIN SELECT RAISE(ABORT,'git observation assets are retained'); END;
