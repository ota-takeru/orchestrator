ALTER TABLE tasks
ADD COLUMN verification_commands_json TEXT NOT NULL DEFAULT '[]';
