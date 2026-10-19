ALTER TABLE databases ADD COLUMN engine TEXT NOT NULL DEFAULT 'mariadb'
    CHECK (engine IN ('mariadb', 'postgresql'));
