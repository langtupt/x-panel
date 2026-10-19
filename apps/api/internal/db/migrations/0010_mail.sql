-- Hosted e-mail: domains, mailboxes and forwarders, each owned by one account.
ALTER TABLE packages ADD COLUMN max_mailboxes INT NOT NULL DEFAULT 0 CHECK (max_mailboxes >= 0);

CREATE TABLE mail_domains (
    id          BIGSERIAL PRIMARY KEY,
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    domain      TEXT NOT NULL UNIQUE,
    dkim_name   TEXT NOT NULL DEFAULT '',
    dkim_value  TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX mail_domains_user_idx ON mail_domains(user_id);

CREATE TABLE mailboxes (
    id            BIGSERIAL PRIMARY KEY,
    domain_id     BIGINT NOT NULL REFERENCES mail_domains(id) ON DELETE CASCADE,
    local_part    TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    quota_mb      INT NOT NULL DEFAULT 0 CHECK (quota_mb >= 0),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (domain_id, local_part)
);

CREATE TABLE mail_aliases (
    id            BIGSERIAL PRIMARY KEY,
    domain_id     BIGINT NOT NULL REFERENCES mail_domains(id) ON DELETE CASCADE,
    local_part    TEXT NOT NULL,
    destinations  TEXT[] NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (domain_id, local_part)
);
