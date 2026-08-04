ALTER TABLE tasks ADD COLUMN completion_policy TEXT NOT NULL DEFAULT 'INDEPENDENT'
    CHECK(completion_policy IN ('INDEPENDENT','ALL_REQUIRED_CHILDREN_VERIFIED'));

ALTER TABLE tasks ADD COLUMN parent_requirement TEXT NOT NULL DEFAULT 'OPTIONAL'
    CHECK(parent_requirement IN ('REQUIRED','OPTIONAL'));

CREATE INDEX idx_tasks_project_parent_display
    ON tasks(project_id,parent_task_id,display_number,id);
