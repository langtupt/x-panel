-- Every hosting account connects its own Google Drive (its own OAuth client)
-- and keeps its own encryption passphrase and schedule: nothing is shared.
CREATE TABLE backup_configs (
    user_id        BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    client_id      TEXT NOT NULL DEFAULT '',
    client_secret  TEXT NOT NULL DEFAULT '',  -- sealed
    refresh_token  TEXT NOT NULL DEFAULT '',  -- sealed
    email          TEXT NOT NULL DEFAULT '',
    folder         TEXT NOT NULL DEFAULT '',
    passphrase     TEXT NOT NULL DEFAULT '',  -- sealed
    schedule       TEXT NOT NULL DEFAULT 'off' CHECK (schedule IN ('off', 'daily', 'weekly')),
    hour           INT NOT NULL DEFAULT 2 CHECK (hour BETWEEN 0 AND 23),
    keep           INT NOT NULL DEFAULT 7 CHECK (keep BETWEEN 1 AND 365),
    last_run       DATE,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- The server-wide Google connection of version 1.1 previews is dropped.
DELETE FROM settings WHERE key LIKE 'backup.%';
