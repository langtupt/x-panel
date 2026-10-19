-- Certificate origin: only Let's Encrypt certificates are renewed automatically.
ALTER TABLE sites ADD COLUMN ssl_type TEXT NOT NULL DEFAULT ''
    CHECK (ssl_type IN ('', 'letsencrypt', 'custom', 'selfsigned'));
ALTER TABLE sites ADD COLUMN ssl_issuer TEXT NOT NULL DEFAULT '';
UPDATE sites SET ssl_type = 'letsencrypt', ssl_issuer = 'Let''s Encrypt' WHERE ssl_status = 'active';

-- Server-wide settings (panel hostname certificate, mail hostname…).
CREATE TABLE settings (
    key         TEXT PRIMARY KEY,
    value       TEXT NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
