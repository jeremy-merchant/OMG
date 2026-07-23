-- Immutable canonical P3 human ownership migration.
-- Existing rows remain NULL and are intentionally invisible to project-scoped reads.
ALTER TABLE humans ADD COLUMN project_id TEXT REFERENCES projects(id);
CREATE INDEX humans_project_id_idx ON humans(project_id, id);
