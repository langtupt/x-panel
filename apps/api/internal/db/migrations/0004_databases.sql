-- Customer MariaDB databases; each has its own MariaDB user of the same name.
CREATE TABLE databases (
    id          BIGSERIAL PRIMARY KEY,
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name        TEXT NOT NULL UNIQUE,
    db_user     TEXT NOT NULL UNIQUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX databases_user_idx ON databases(user_id);
