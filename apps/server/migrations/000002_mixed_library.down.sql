-- Postgres cannot remove enum values safely; leave 'mixed' in place on downgrade.
SELECT 1;
