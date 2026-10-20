-- Resource use per hosting account, sampled every 5 minutes, kept 35 days.
CREATE TABLE resource_samples (
    user_id      BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    at           TIMESTAMPTZ NOT NULL,
    cpu_pct      REAL,            -- 100 = one core; NULL for the first sample
    mem_mb       INT NOT NULL,
    tasks        INT NOT NULL,
    disk_mb      INT,             -- NULL when quotas are off
    db_mb        INT NOT NULL,
    mail_mb      INT NOT NULL,
    bw_bytes     BIGINT NOT NULL, -- served since the previous sample
    requests     BIGINT NOT NULL,
    PRIMARY KEY (user_id, at)
);
CREATE INDEX resource_samples_at_idx ON resource_samples(at);
