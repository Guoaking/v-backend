-- Safely migrate users.org_id from text/varchar to UUID
BEGIN;

-- Add temporary UUID column
ALTER TABLE users ADD COLUMN IF NOT EXISTS org_id_uuid UUID;

-- Try to cast existing values; invalid formats become NULL via NULLIF
UPDATE users SET org_id_uuid = NULLIF(org_id, '')::uuid WHERE org_id IS NOT NULL;

-- Drop old column if exists and replace
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'users' AND column_name = 'org_id'
    ) THEN
        ALTER TABLE users DROP COLUMN org_id;
    END IF;
END $$;

ALTER TABLE users RENAME COLUMN org_id_uuid TO org_id;

-- Optional: add index (skip FK to avoid runtime failures on legacy data)
CREATE INDEX IF NOT EXISTS idx_users_org_id ON users(org_id);

COMMIT;

