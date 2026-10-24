-- M2: Google-bound accounts, server-side sessions, and the creation ledger
-- behind the new-account quota. Plan decisions are referenced as Dn.

CREATE TABLE users (
    id             BIGSERIAL   PRIMARY KEY,
    google_sub     TEXT        NOT NULL UNIQUE,                 -- D3
    email          TEXT        NOT NULL,
    is_active      BOOLEAN     NOT NULL DEFAULT TRUE,
    is_whitelisted BOOLEAN     NOT NULL DEFAULT FALSE,          -- D29
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_login_at  TIMESTAMPTZ
);
CREATE INDEX users_created_at_idx ON users (created_at);       -- D26 list-recent

CREATE TABLE sessions (
    token_hash BYTEA       PRIMARY KEY,
    user_id    BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    user_agent TEXT,
    ip         INET
);
CREATE INDEX sessions_user_id_idx    ON sessions (user_id);
CREATE INDEX sessions_expires_at_idx ON sessions (expires_at);

CREATE TABLE link_creations (                                   -- D24: survives deletes
    user_id    BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX link_creations_user_time_idx ON link_creations (user_id, created_at);

ALTER TABLE links ALTER COLUMN disabled_by TYPE TEXT USING disabled_by::text;   -- D5
ALTER TABLE links ADD CONSTRAINT links_user_id_fkey
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;
DROP INDEX IF EXISTS idx_links_user_id;
CREATE INDEX links_user_id_created_at_idx ON links (user_id, created_at DESC, short_code DESC);  -- D15
