-- Restore the compatibility directory configuration before removing target state.
UPDATE directory_sync_configs legacy
JOIN sync_target_configs target ON target.target_code='directory'
SET legacy.enabled=target.enabled,
    legacy.schedule_type=target.schedule_type,
    legacy.interval_minutes=target.interval_minutes,
    legacy.daily_time=target.daily_time,
    legacy.timezone=target.timezone,
    legacy.next_run_at=target.next_run_at,
    legacy.last_run_at=target.last_run_at,
    legacy.last_success_at=target.last_success_at,
    legacy.directory_version=target.target_version,
    legacy.lease_owner=NULL,
    legacy.lease_until=NULL,
    legacy.updated_by=target.updated_by,
    legacy.updated_at=target.updated_at;

-- The legacy schema cannot represent ANY target-specific v2 job without
-- silently changing its meaning. Delete all v2 rows, including directory jobs
-- in pending/running states, and retain only true pre-0038 legacy_full history.
DELETE FROM directory_sync_runs
WHERE target_code <> 'legacy_full';

UPDATE directory_sync_runs
SET status='failed',
    error_code='migration_rollback',
    error_message='job normalized while rolling back enterprise sync targets',
    finished_at=COALESCE(finished_at,CURRENT_TIMESTAMP(3))
WHERE status='blocked';

DROP TABLE IF EXISTS sync_batches;
DROP TABLE IF EXISTS sync_target_configs;
ALTER TABLE directory_sync_runs
    DROP KEY idx_directory_sync_runs_batch,
    DROP KEY idx_directory_sync_runs_target_status,
    DROP COLUMN target_version,
    DROP COLUMN warnings_json,
    DROP COLUMN metrics_json,
    DROP COLUMN batch_id,
    DROP COLUMN target_code;
