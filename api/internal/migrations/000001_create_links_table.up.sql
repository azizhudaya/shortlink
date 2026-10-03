-- user_id/disabled_* are pre-created (nullable, unused for now) so adding
-- accounts later needs no backfill. user_id has no FK yet: the migration that
-- creates `users` adds it.

CREATE TABLE IF NOT EXISTS links (
    short_code     VARCHAR(32)  PRIMARY KEY,
    long_url       TEXT         NOT NULL,
    is_custom      BOOLEAN      NOT NULL DEFAULT FALSE,
    user_id        BIGINT       NULL,
    disabled_at    TIMESTAMPTZ  NULL,
    disabled_by    BIGINT       NULL,
    disable_reason TEXT         NULL,
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_links_user_id ON links (user_id) WHERE user_id IS NOT NULL;
