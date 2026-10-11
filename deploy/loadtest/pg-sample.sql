-- One sample of Postgres for deploy/loadtest/capacity.py: connections against the
-- limit, then active sessions by wait event (none means on CPU).
SELECT 'S', extract(epoch FROM now())::bigint, count(*), current_setting('max_connections')
FROM pg_stat_activity;
SELECT 'W', extract(epoch FROM now())::bigint, coalesce(wait_event_type, 'CPU'),
       coalesce(wait_event, '-'), coalesce(datname, '-'), count(*)
FROM pg_stat_activity
WHERE state = 'active' AND backend_type = 'client backend' AND pid <> pg_backend_pid()
GROUP BY 3, 4, 5;
