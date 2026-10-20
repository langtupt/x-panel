-- Commands run after the standard build (e.g. "php artisan migrate --force"),
-- as the account, in the same confined unit as the build.
ALTER TABLE sites ADD COLUMN deploy_script TEXT NOT NULL DEFAULT '';
