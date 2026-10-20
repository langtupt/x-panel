-- Backups found on Google Drive that another server (or an earlier install) made.
ALTER TABLE backups DROP CONSTRAINT backups_kind_check;
ALTER TABLE backups ADD CONSTRAINT backups_kind_check CHECK (kind IN ('manual', 'scheduled', 'imported'));
ALTER TABLE backups ADD CONSTRAINT backups_file_id_key UNIQUE (file_id);
