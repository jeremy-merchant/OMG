ALTER TABLE command_receipts ADD COLUMN operation TEXT NOT NULL DEFAULT 'legacy.unknown';
CREATE INDEX idx_command_receipts_project_created ON command_receipts(project_id,created_at DESC,id DESC);
