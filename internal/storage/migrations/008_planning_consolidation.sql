ALTER TABLE task_groups ADD COLUMN change_request_id TEXT;
ALTER TABLE task_groups ADD COLUMN planning_unit TEXT NOT NULL DEFAULT 'feature_chunk';

CREATE UNIQUE INDEX idx_task_groups_feature_request_open
  ON task_groups(project_id, feature_request_id)
  WHERE feature_request_id IS NOT NULL
    AND status IN ('proposed', 'ready', 'running', 'waiting_for_human');
