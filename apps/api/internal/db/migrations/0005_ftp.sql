-- Pure-FTPd virtual users, each confined to a directory in the owner's home.
CREATE TABLE ftp_accounts (
    id          BIGSERIAL PRIMARY KEY,
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    login       TEXT NOT NULL UNIQUE,
    dir         TEXT NOT NULL DEFAULT '',   -- relative to /home/<user>
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX ftp_accounts_user_idx ON ftp_accounts(user_id);
