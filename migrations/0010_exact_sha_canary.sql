ALTER TABLE handoff_lifecycle_events RENAME TO handoff_lifecycle_events_v9;
DROP INDEX idx_handoff_lifecycle_handoff;
DROP TRIGGER handoff_lifecycle_events_no_update;
DROP TRIGGER handoff_lifecycle_events_no_delete;

CREATE TABLE handoff_lifecycle_events (
  id TEXT PRIMARY KEY,
  handoff_id TEXT NOT NULL REFERENCES handoffs(id),
  state TEXT NOT NULL CHECK(state IN (
    'SUBMITTED','REVIEWING','ACCEPTED','INTEGRATED',
    'CANARY_RUNNING','CANARY_PASSED','CANARY_MOCK_PASSED','CANARY_FAILED','CANARY_SKIPPED','CANARY_INVALIDATED',
    'SOURCE_CLEANED','REJECTED','BLOCKED'
  )),
  actor_session_id TEXT NOT NULL REFERENCES agent_sessions(id),
  source_commit TEXT,
  source_tree TEXT,
  integration_commit TEXT,
  canary_run_id TEXT,
  canary_integration_ref TEXT,
  canary_target_sha TEXT,
  canary_target_tree TEXT,
  canary_result TEXT,
  canary_command TEXT,
  canary_execution_kind TEXT,
  canary_environment_fingerprint TEXT,
  canary_head_before TEXT,
  canary_head_after TEXT,
  canary_ref_fingerprint_before TEXT,
  canary_ref_fingerprint_after TEXT,
  canary_exit_code INTEGER,
  canary_passed_count INTEGER NOT NULL DEFAULT 0 CHECK(canary_passed_count >= 0),
  canary_failed_count INTEGER NOT NULL DEFAULT 0 CHECK(canary_failed_count >= 0),
  canary_skipped_count INTEGER NOT NULL DEFAULT 0 CHECK(canary_skipped_count >= 0),
  canary_started_at TEXT,
  canary_finished_at TEXT,
  canary_evidence_path TEXT,
  source_worktree_cleaned INTEGER NOT NULL DEFAULT 0 CHECK(source_worktree_cleaned IN (0,1)),
  source_branch_cleaned INTEGER NOT NULL DEFAULT 0 CHECK(source_branch_cleaned IN (0,1)),
  note TEXT,
  created_at TEXT NOT NULL
);

INSERT INTO handoff_lifecycle_events(
  id,handoff_id,state,actor_session_id,source_commit,source_tree,integration_commit,
  canary_target_sha,canary_result,source_worktree_cleaned,source_branch_cleaned,note,created_at
)
SELECT
  id,handoff_id,state,actor_session_id,source_commit,source_tree,integration_commit,
  canary_target_sha,canary_result,source_worktree_cleaned,source_branch_cleaned,note,created_at
FROM handoff_lifecycle_events_v9;

DROP TABLE handoff_lifecycle_events_v9;

CREATE INDEX idx_handoff_lifecycle_handoff ON handoff_lifecycle_events(handoff_id,created_at,id);
CREATE UNIQUE INDEX idx_handoff_lifecycle_canary_run_state
  ON handoff_lifecycle_events(handoff_id,canary_run_id,state)
  WHERE canary_run_id IS NOT NULL;
CREATE TRIGGER handoff_lifecycle_events_no_update BEFORE UPDATE ON handoff_lifecycle_events BEGIN SELECT RAISE(ABORT,'handoff lifecycle events are append-only'); END;
CREATE TRIGGER handoff_lifecycle_events_no_delete BEFORE DELETE ON handoff_lifecycle_events BEGIN SELECT RAISE(ABORT,'handoff lifecycle events are append-only'); END;

