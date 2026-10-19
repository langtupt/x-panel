-- Core accounts: admin > reseller > user, packages (quotas), sessions, audit, tasks.
-- Kept compatible with PostgreSQL 13+ (AlmaLinux AppStream).

CREATE TYPE user_role AS ENUM ('admin', 'reseller', 'user');
CREATE TYPE user_status AS ENUM ('active', 'suspended');

CREATE TABLE users (
    id            BIGSERIAL PRIMARY KEY,
    username      TEXT NOT NULL UNIQUE CHECK (username ~ '^[a-z][a-z0-9_]{2,31}$'),
    email         TEXT NOT NULL DEFAULT '',
    password_hash TEXT NOT NULL,
    role          user_role NOT NULL,
    parent_id     BIGINT REFERENCES users(id) ON DELETE RESTRICT,
    package_id    BIGINT,
    linux_uid     INTEGER UNIQUE,
    status        user_status NOT NULL DEFAULT 'active',
    totp_secret   TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((role = 'admin') = (parent_id IS NULL))
);
CREATE INDEX users_parent_idx ON users(parent_id);

-- 0 in any limit column means unlimited.
CREATE TABLE packages (
    id            BIGSERIAL PRIMARY KEY,
    owner_id      BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    kind          user_role NOT NULL CHECK (kind IN ('reseller', 'user')),
    disk_mb       INTEGER NOT NULL DEFAULT 0,
    inodes        INTEGER NOT NULL DEFAULT 0,
    bandwidth_gb  INTEGER NOT NULL DEFAULT 0,
    max_sites     INTEGER NOT NULL DEFAULT 0,
    max_dbs       INTEGER NOT NULL DEFAULT 0,
    max_ftp       INTEGER NOT NULL DEFAULT 0,
    max_cron      INTEGER NOT NULL DEFAULT 0,
    max_users     INTEGER NOT NULL DEFAULT 0,   -- reseller packages only
    cpu_pct       INTEGER NOT NULL DEFAULT 0,   -- systemd CPUQuota, 100 = 1 core
    mem_mb        INTEGER NOT NULL DEFAULT 0,   -- systemd MemoryMax
    php_versions  TEXT[] NOT NULL DEFAULT '{}',
    oversell      BOOLEAN NOT NULL DEFAULT false,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (owner_id, name)
);

ALTER TABLE users ADD CONSTRAINT users_package_fk
    FOREIGN KEY (package_id) REFERENCES packages(id) ON DELETE SET NULL;

CREATE TABLE sessions (
    id          BIGSERIAL PRIMARY KEY,
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash  BYTEA NOT NULL UNIQUE,
    user_agent  TEXT NOT NULL DEFAULT '',
    ip          TEXT NOT NULL DEFAULT '',
    expires_at  TIMESTAMPTZ NOT NULL,
    revoked_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX sessions_user_idx ON sessions(user_id);

CREATE TABLE audit_logs (
    id           BIGSERIAL PRIMARY KEY,
    actor_id     BIGINT REFERENCES users(id) ON DELETE SET NULL,
    action       TEXT NOT NULL,
    target_type  TEXT NOT NULL DEFAULT '',
    target_id    BIGINT,
    ip           TEXT NOT NULL DEFAULT '',
    meta         JSONB NOT NULL DEFAULT '{}',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX audit_logs_actor_idx ON audit_logs(actor_id, created_at DESC);

CREATE TABLE tasks (
    id           BIGSERIAL PRIMARY KEY,
    user_id      BIGINT REFERENCES users(id) ON DELETE SET NULL,
    type         TEXT NOT NULL,
    payload      JSONB NOT NULL DEFAULT '{}',
    status       TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'done', 'failed')),
    log          TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at  TIMESTAMPTZ
);
CREATE INDEX tasks_status_idx ON tasks(status, created_at);
