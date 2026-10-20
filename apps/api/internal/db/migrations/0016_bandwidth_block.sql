-- Set to the first day of the month in which the account used up its
-- package's bandwidth; its sites answer 503 while it equals the current month.
ALTER TABLE users ADD COLUMN bw_blocked_month DATE;
