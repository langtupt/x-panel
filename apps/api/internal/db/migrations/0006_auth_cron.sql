-- Impersonation: a session created by an admin/reseller acting as another account.
ALTER TABLE sessions ADD COLUMN impersonator_id BIGINT REFERENCES users(id) ON DELETE CASCADE;

-- Cron jobs, rendered into the owner's crontab by the agent.
CREATE TABLE cron_jobs (
    id          BIGSERIAL PRIMARY KEY,
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    schedule    TEXT NOT NULL,
    command     TEXT NOT NULL,
    enabled     BOOLEAN NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX cron_jobs_user_idx ON cron_jobs(user_id);
