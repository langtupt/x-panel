CREATE TABLE sites (
    id              BIGSERIAL PRIMARY KEY,
    user_id         BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    domain          TEXT NOT NULL UNIQUE,
    aliases         TEXT[] NOT NULL DEFAULT '{}',
    php_version     TEXT NOT NULL DEFAULT '',   -- '' = static site
    doc_root        TEXT NOT NULL DEFAULT '',   -- relative to /home/<user>/www/<domain>
    ssl_status      TEXT NOT NULL DEFAULT 'none' CHECK (ssl_status IN ('none', 'pending', 'active', 'failed')),
    ssl_expires_at  TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX sites_user_idx ON sites(user_id);
-- Aliases must be unique across all sites too; enforced in the API, indexed for the lookup.
CREATE INDEX sites_aliases_idx ON sites USING GIN (aliases);

ALTER TABLE tasks ADD COLUMN target TEXT NOT NULL DEFAULT '';
ALTER TABLE tasks ADD COLUMN started_at TIMESTAMPTZ;
