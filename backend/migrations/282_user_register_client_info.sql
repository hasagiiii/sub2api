-- Client IP and User-Agent captured when a user self-registers. Admin- and
-- IAM-created users, and users registered before this migration, stay empty.
ALTER TABLE users ADD COLUMN IF NOT EXISTS register_ip VARCHAR(64) NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN IF NOT EXISTS register_user_agent TEXT NOT NULL DEFAULT '';
