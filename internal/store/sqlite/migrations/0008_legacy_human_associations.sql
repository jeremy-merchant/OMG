-- Preserves the project bounds of pre-v6 humans without granting new scopes.
-- A NULL owner remains legacy-only unless its historical sessions prove a project association.
CREATE TABLE legacy_human_projects (
    human_id TEXT NOT NULL REFERENCES humans(id),
    project_id TEXT NOT NULL REFERENCES projects(id),
    PRIMARY KEY (human_id, project_id)
) WITHOUT ROWID;

INSERT OR IGNORE INTO projects(id, created_at)
SELECT s.project_id, MIN(s.started_at)
FROM agent_sessions AS s
JOIN humans AS h ON h.id = s.human_id
WHERE h.project_id IS NULL
GROUP BY s.project_id;

INSERT OR IGNORE INTO legacy_human_projects(human_id, project_id)
SELECT h.id, s.project_id
FROM humans AS h
JOIN agent_sessions AS s ON s.human_id = h.id
WHERE h.project_id IS NULL
GROUP BY h.id, s.project_id;

-- The original immutability trigger is briefly replaced inside this migration
-- so the one unambiguous historical owner can be materialized permanently.
DROP TRIGGER humans_no_update;
UPDATE humans
SET project_id = (
    SELECT project_id
    FROM legacy_human_projects AS a
    WHERE a.human_id = humans.id
)
WHERE project_id IS NULL
  AND (SELECT COUNT(*) FROM legacy_human_projects AS a WHERE a.human_id = humans.id) = 1;
CREATE TRIGGER humans_no_update BEFORE UPDATE ON humans BEGIN SELECT RAISE(ABORT,'humans are immutable'); END;

CREATE INDEX legacy_human_projects_project_idx ON legacy_human_projects(project_id, human_id);
