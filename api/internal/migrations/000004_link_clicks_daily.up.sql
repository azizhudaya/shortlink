-- Click counts (D32, FR-26): redirects served per link per UTC day. Counts
-- only: no IP, user agent, referrer or cookie is ever recorded. The redirect
-- path only bumps an in-memory counter; a background job adds the totals here
-- in one batch a minute, so the redirect itself still does a single read.
CREATE TABLE link_clicks_daily (
    short_code VARCHAR(32) NOT NULL REFERENCES links(short_code) ON DELETE CASCADE,
    day        DATE        NOT NULL,
    clicks     BIGINT      NOT NULL CHECK (clicks >= 0),
    PRIMARY KEY (short_code, day)
);
