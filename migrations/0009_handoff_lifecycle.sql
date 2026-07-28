CREATE TABLE handoff_lifecycle_events (
  id TEXT PRIMARY KEY,
  handoff_id TEXT NOT NULL REFERENCES handoffs(id),
  state TEXT NOT NULL CHECK(state IN ('SUBMITTED','REVIEWING','ACCEPTED','INTEGRATED','CANARY_PASSED','SOURCE_CLEANED','REJECTED','BLOCKED')),
  actor_session_id TEXT NOT NULL REFERENCES agent_sessions(id),
  source_commit TEXT,
  source_tree TEXT,
  integration_commit TEXT,
  canary_target_sha TEXT,
  canary_result TEXT,
  source_worktree_cleaned INTEGER NOT NULL DEFAULT 0 CHECK(source_worktree_cleaned IN (0,1)),
  source_branch_cleaned INTEGER NOT NULL DEFAULT 0 CHECK(source_branch_cleaned IN (0,1)),
  note TEXT,
  created_at TEXT NOT NULL
);
CREATE INDEX idx_handoff_lifecycle_handoff ON handoff_lifecycle_events(handoff_id,created_at,id);
CREATE TRIGGER handoff_lifecycle_events_no_update BEFORE UPDATE ON handoff_lifecycle_events BEGIN SELECT RAISE(ABORT,'handoff lifecycle events are append-only'); END;
CREATE TRIGGER handoff_lifecycle_events_no_delete BEFORE DELETE ON handoff_lifecycle_events BEGIN SELECT RAISE(ABORT,'handoff lifecycle events are append-only'); END;

