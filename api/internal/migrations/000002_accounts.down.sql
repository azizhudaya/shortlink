-- Reverses 000002 for local resets only. In production, roll forward: this
-- discards every account and the actor recorded on each takedown.

DROP INDEX IF EXISTS links_user_id_created_at_idx;
CREATE INDEX IF NOT EXISTS idx_links_user_id ON links (user_id) WHERE user_id IS NOT NULL;
ALTER TABLE links DROP CONSTRAINT IF EXISTS links_user_id_fkey;
UPDATE links SET user_id = NULL;
-- 'user:<id>' / 'operator:<name>' have no BIGINT form.
ALTER TABLE links ALTER COLUMN disabled_by TYPE BIGINT USING NULL;

DROP TABLE IF EXISTS link_creations;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS users;
