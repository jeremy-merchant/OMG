-- P4-A advisory reservation ledger. All facts use fixed-width UTC RFC3339
-- nanosecond text so lexical and temporal ordering agree.
PRAGMA foreign_keys = ON;

CREATE TABLE reservations (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id),
    human_id TEXT NOT NULL REFERENCES humans(id),
    session_id TEXT NOT NULL REFERENCES agent_sessions(id),
    task_id TEXT NOT NULL REFERENCES tasks(id),
    run_id TEXT NOT NULL REFERENCES task_runs(id),
    pattern_kind TEXT NOT NULL CHECK(pattern_kind IN ('exact','directory_prefix','glob')),
    normalized_pattern TEXT NOT NULL CHECK(length(trim(normalized_pattern)) > 0),
    case_sensitivity TEXT NOT NULL CHECK(case_sensitivity IN ('sensitive','insensitive')),
    mode TEXT NOT NULL CHECK(mode IN ('shared','exclusive')),
    intent TEXT NOT NULL CHECK(length(trim(intent)) > 0),
    expires_at TEXT NOT NULL CHECK(length(expires_at)=30 AND expires_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]Z' AND COALESCE(strftime('%Y-%m-%dT%H:%M:%SZ',julianday(substr(expires_at,1,19)||'Z'))=substr(expires_at,1,19)||'Z',0)),
    created_at TEXT NOT NULL CHECK(length(created_at)=30 AND created_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]Z' AND COALESCE(strftime('%Y-%m-%dT%H:%M:%SZ',julianday(substr(created_at,1,19)||'Z'))=substr(created_at,1,19)||'Z',0) AND expires_at>created_at)
);

CREATE TABLE reservation_renewals (
    id TEXT PRIMARY KEY,
    reservation_id TEXT NOT NULL REFERENCES reservations(id),
    checkpoint_id TEXT NOT NULL CHECK(length(trim(checkpoint_id)) > 0),
    previous_expires_at TEXT NOT NULL CHECK(length(previous_expires_at)=30 AND previous_expires_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]Z' AND COALESCE(strftime('%Y-%m-%dT%H:%M:%SZ',julianday(substr(previous_expires_at,1,19)||'Z'))=substr(previous_expires_at,1,19)||'Z',0)),
    expires_at TEXT NOT NULL CHECK(length(expires_at)=30 AND expires_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]Z' AND COALESCE(strftime('%Y-%m-%dT%H:%M:%SZ',julianday(substr(expires_at,1,19)||'Z'))=substr(expires_at,1,19)||'Z',0)),
    occurred_at TEXT NOT NULL CHECK(length(occurred_at)=30 AND occurred_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]Z' AND COALESCE(strftime('%Y-%m-%dT%H:%M:%SZ',julianday(substr(occurred_at,1,19)||'Z'))=substr(occurred_at,1,19)||'Z',0))
);
CREATE TABLE reservation_releases (
    id TEXT PRIMARY KEY,
    reservation_id TEXT NOT NULL REFERENCES reservations(id),
    reason TEXT NOT NULL CHECK(length(trim(reason)) > 0),
    occurred_at TEXT NOT NULL CHECK(length(occurred_at)=30 AND occurred_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]Z' AND COALESCE(strftime('%Y-%m-%dT%H:%M:%SZ',julianday(substr(occurred_at,1,19)||'Z'))=substr(occurred_at,1,19)||'Z',0)),
    UNIQUE(reservation_id)
);
CREATE TABLE reservation_overrides (
    id TEXT PRIMARY KEY,
    reservation_id TEXT NOT NULL REFERENCES reservations(id),
    human_id TEXT NOT NULL REFERENCES humans(id),
    reason TEXT NOT NULL CHECK(length(trim(reason)) > 0),
    occurred_at TEXT NOT NULL CHECK(length(occurred_at)=30 AND occurred_at GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9].[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]Z' AND COALESCE(strftime('%Y-%m-%dT%H:%M:%SZ',julianday(substr(occurred_at,1,19)||'Z'))=substr(occurred_at,1,19)||'Z',0)),
    UNIQUE(reservation_id)
);

CREATE INDEX idx_reservations_project_expiry ON reservations(project_id, expires_at);
CREATE INDEX idx_reservations_task_expiry ON reservations(project_id, task_id, expires_at);
CREATE INDEX idx_reservation_renewals_reservation ON reservation_renewals(reservation_id, occurred_at, id);

CREATE TRIGGER reservations_project_lineage BEFORE INSERT ON reservations BEGIN
 SELECT CASE WHEN (SELECT project_id FROM agent_sessions WHERE id=NEW.session_id) <> NEW.project_id THEN RAISE(ABORT,'reservation session project mismatch') END;
 SELECT CASE WHEN (SELECT project_id FROM tasks WHERE id=NEW.task_id) <> NEW.project_id THEN RAISE(ABORT,'reservation task project mismatch') END;
 SELECT CASE WHEN (SELECT task_id FROM task_runs WHERE id=NEW.run_id) <> NEW.task_id OR (SELECT session_id FROM task_runs WHERE id=NEW.run_id) <> NEW.session_id THEN RAISE(ABORT,'reservation run lineage mismatch') END;
 SELECT CASE WHEN (SELECT human_id FROM agent_sessions WHERE id=NEW.session_id) <> NEW.human_id THEN RAISE(ABORT,'reservation human session mismatch') END;
END;
CREATE TRIGGER reservations_no_update BEFORE UPDATE ON reservations BEGIN SELECT RAISE(ABORT,'reservations are immutable'); END;
CREATE TRIGGER reservations_no_delete BEFORE DELETE ON reservations BEGIN SELECT RAISE(ABORT,'reservations are retained'); END;

CREATE TRIGGER reservation_renewals_validate BEFORE INSERT ON reservation_renewals BEGIN
 SELECT CASE WHEN EXISTS(SELECT 1 FROM reservation_releases WHERE reservation_id=NEW.reservation_id) OR EXISTS(SELECT 1 FROM reservation_overrides WHERE reservation_id=NEW.reservation_id) THEN RAISE(ABORT,'terminal reservation fact already exists') END;
 SELECT CASE WHEN NEW.previous_expires_at <> COALESCE((SELECT expires_at FROM reservation_renewals WHERE reservation_id=NEW.reservation_id ORDER BY occurred_at DESC, id DESC LIMIT 1),(SELECT expires_at FROM reservations WHERE id=NEW.reservation_id)) THEN RAISE(ABORT,'renewal previous expiry is not current') END;
 SELECT CASE WHEN NEW.expires_at <= NEW.previous_expires_at THEN RAISE(ABORT,'renewal must extend expiry') END;
 SELECT CASE WHEN NEW.occurred_at >= NEW.previous_expires_at THEN RAISE(ABORT,'renewal must occur before current expiry') END;
 SELECT CASE WHEN NEW.occurred_at <= COALESCE((SELECT occurred_at FROM reservation_renewals WHERE reservation_id=NEW.reservation_id ORDER BY occurred_at DESC, id DESC LIMIT 1),(SELECT created_at FROM reservations WHERE id=NEW.reservation_id)) THEN RAISE(ABORT,'renewal chronology is invalid') END;
END;
CREATE TRIGGER reservation_releases_validate BEFORE INSERT ON reservation_releases BEGIN
 SELECT CASE WHEN EXISTS(SELECT 1 FROM reservation_overrides WHERE reservation_id=NEW.reservation_id) THEN RAISE(ABORT,'terminal reservation fact already exists') END;
 SELECT CASE WHEN NEW.occurred_at >= COALESCE((SELECT expires_at FROM reservation_renewals WHERE reservation_id=NEW.reservation_id ORDER BY occurred_at DESC, id DESC LIMIT 1),(SELECT expires_at FROM reservations WHERE id=NEW.reservation_id)) THEN RAISE(ABORT,'release must occur before current expiry') END;
 SELECT CASE WHEN NEW.occurred_at <= COALESCE((SELECT occurred_at FROM reservation_renewals WHERE reservation_id=NEW.reservation_id ORDER BY occurred_at DESC, id DESC LIMIT 1),(SELECT created_at FROM reservations WHERE id=NEW.reservation_id)) THEN RAISE(ABORT,'release chronology is invalid') END;
END;
CREATE TRIGGER reservation_overrides_validate BEFORE INSERT ON reservation_overrides BEGIN
 SELECT CASE WHEN EXISTS(SELECT 1 FROM reservation_releases WHERE reservation_id=NEW.reservation_id) THEN RAISE(ABORT,'terminal reservation fact already exists') END;
 SELECT CASE WHEN NEW.occurred_at >= COALESCE((SELECT expires_at FROM reservation_renewals WHERE reservation_id=NEW.reservation_id ORDER BY occurred_at DESC, id DESC LIMIT 1),(SELECT expires_at FROM reservations WHERE id=NEW.reservation_id)) THEN RAISE(ABORT,'override must occur before current expiry') END;
 SELECT CASE WHEN NEW.occurred_at <= COALESCE((SELECT occurred_at FROM reservation_renewals WHERE reservation_id=NEW.reservation_id ORDER BY occurred_at DESC, id DESC LIMIT 1),(SELECT created_at FROM reservations WHERE id=NEW.reservation_id)) THEN RAISE(ABORT,'override chronology is invalid') END;
END;
CREATE TRIGGER reservation_renewals_no_update BEFORE UPDATE ON reservation_renewals BEGIN SELECT RAISE(ABORT,'reservation renewal facts are append-only'); END;
CREATE TRIGGER reservation_renewals_no_delete BEFORE DELETE ON reservation_renewals BEGIN SELECT RAISE(ABORT,'reservation renewal facts are append-only'); END;
CREATE TRIGGER reservation_releases_no_update BEFORE UPDATE ON reservation_releases BEGIN SELECT RAISE(ABORT,'reservation release facts are append-only'); END;
CREATE TRIGGER reservation_releases_no_delete BEFORE DELETE ON reservation_releases BEGIN SELECT RAISE(ABORT,'reservation release facts are append-only'); END;
CREATE TRIGGER reservation_overrides_no_update BEFORE UPDATE ON reservation_overrides BEGIN SELECT RAISE(ABORT,'reservation override facts are append-only'); END;
CREATE TRIGGER reservation_overrides_no_delete BEFORE DELETE ON reservation_overrides BEGIN SELECT RAISE(ABORT,'reservation override facts are append-only'); END;
