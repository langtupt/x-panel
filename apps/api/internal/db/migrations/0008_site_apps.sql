ALTER TABLE sites
 ADD COLUMN app_type TEXT NOT NULL DEFAULT 'php' CHECK (app_type IN ('html','php','laravel','nodejs','nextjs','go')),
 ADD COLUMN runtime TEXT NOT NULL DEFAULT '',
 ADD COLUMN app_port INTEGER UNIQUE CHECK (app_port BETWEEN 10000 AND 59999),
 ADD COLUMN env JSONB NOT NULL DEFAULT '{}';
UPDATE sites SET app_type='html' WHERE php_version='';
