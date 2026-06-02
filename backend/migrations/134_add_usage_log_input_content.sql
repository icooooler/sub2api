-- Add input_content to usage_logs: text of the last user message per request.
-- NULL for historical rows and for requests where no user text could be extracted.
-- Idempotent: the column was originally added manually in production; IF NOT EXISTS
-- makes this migration a no-op there while still creating it on fresh databases.
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS input_content TEXT;
