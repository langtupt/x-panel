-- Encrypted account backups stored on Google Drive.
CREATE TABLE backups (
    id          BIGSERIAL PRIMARY KEY,
    user_id     BIGINT REFERENCES users(id) ON DELETE SET NULL,
    username    TEXT NOT NULL,
    file_id     TEXT NOT NULL,
    file_name   TEXT NOT NULL,
    size_bytes  BIGINT NOT NULL,
    kind        TEXT NOT NULL CHECK (kind IN ('manual', 'scheduled')),
    contents    JSONB NOT NULL,    -- {"databases":[...],"mail_domains":[...]}
    created_by  BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX backups_user_idx ON backups(user_id, created_at DESC);
CREATE INDEX backups_username_idx ON backups(username, created_at DESC);
