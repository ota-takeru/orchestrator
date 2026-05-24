ALTER TABLE semantic_behavior_diffs
  ADD COLUMN category TEXT NOT NULL DEFAULT 'non_user_visible'
  CHECK (category IN ('user_visible', 'non_user_visible', 'risk', 'test_change'));

ALTER TABLE semantic_behavior_diffs
  ADD COLUMN summary TEXT NOT NULL DEFAULT '';

ALTER TABLE semantic_behavior_diffs
  ADD COLUMN confidence TEXT NOT NULL DEFAULT 'medium'
  CHECK (confidence IN ('high', 'medium', 'low'));

CREATE INDEX idx_semantic_behavior_diffs_category
  ON semantic_behavior_diffs(project_id, task_id, category);
