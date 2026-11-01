-- Hosting expiry: the account is suspended (reason "expired") once the date
-- has passed; reminders are mailed before. suspend_reason tells an expiry
-- from a manual suspension so renewing re-activates only the former.
ALTER TABLE users ADD COLUMN expires_at DATE;
ALTER TABLE users ADD COLUMN suspend_reason TEXT NOT NULL DEFAULT '';
-- Last reminder sent for the current expires_at ("7", "3", "1", "expired").
ALTER TABLE users ADD COLUMN expiry_notice TEXT NOT NULL DEFAULT '';

-- Backups can go to an FTP/FTPS server instead of Google Drive.
ALTER TABLE backup_configs ADD COLUMN destination TEXT NOT NULL DEFAULT 'drive';
ALTER TABLE backup_configs ADD COLUMN ftp_host TEXT NOT NULL DEFAULT '';
ALTER TABLE backup_configs ADD COLUMN ftp_port INT NOT NULL DEFAULT 21;
ALTER TABLE backup_configs ADD COLUMN ftp_user TEXT NOT NULL DEFAULT '';
ALTER TABLE backup_configs ADD COLUMN ftp_password TEXT NOT NULL DEFAULT ''; -- sealed
ALTER TABLE backup_configs ADD COLUMN ftp_dir TEXT NOT NULL DEFAULT '';
ALTER TABLE backup_configs ADD COLUMN ftp_tls BOOLEAN NOT NULL DEFAULT true;
ALTER TABLE backup_configs ADD COLUMN ftp_insecure BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE backups ADD COLUMN destination TEXT NOT NULL DEFAULT 'drive';
