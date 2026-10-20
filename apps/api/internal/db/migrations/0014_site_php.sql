ALTER TABLE sites ADD COLUMN php_config JSONB NOT NULL DEFAULT '{}'::jsonb;
