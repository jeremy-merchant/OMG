ALTER TABLE migration_approvals
  ADD COLUMN authorization_kind TEXT NOT NULL DEFAULT 'human'
  CHECK(authorization_kind IN ('human','automatic_safe_policy'));
