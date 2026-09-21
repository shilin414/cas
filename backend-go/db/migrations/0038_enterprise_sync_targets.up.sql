ALTER TABLE directory_sync_runs
    ADD COLUMN target_code VARCHAR(64) NOT NULL DEFAULT 'legacy_full' AFTER id,
    ADD COLUMN batch_id BIGINT UNSIGNED NULL AFTER target_code,
    ADD COLUMN metrics_json JSON NULL AFTER unknown_users_count,
    ADD COLUMN warnings_json JSON NULL AFTER metrics_json,
    ADD COLUMN target_version BIGINT UNSIGNED NOT NULL DEFAULT 0 AFTER warnings_json,
    ADD KEY idx_directory_sync_runs_target_status (target_code, status, created_at),
    ADD KEY idx_directory_sync_runs_batch (batch_id, id);

CREATE TABLE sync_target_configs (
    target_code VARCHAR(64) NOT NULL,
    enabled TINYINT(1) NOT NULL DEFAULT 0,
    schedule_type VARCHAR(20) NOT NULL DEFAULT 'interval',
    interval_minutes INT UNSIGNED NOT NULL DEFAULT 360,
    daily_time VARCHAR(5) NOT NULL DEFAULT '02:00',
    timezone VARCHAR(64) NOT NULL DEFAULT 'Asia/Shanghai',
    next_run_at DATETIME(3) NULL,
    last_run_at DATETIME(3) NULL,
    last_success_at DATETIME(3) NULL,
    target_version BIGINT UNSIGNED NOT NULL DEFAULT 0,
    last_error_code VARCHAR(100) NOT NULL DEFAULT '',
    last_error_message VARCHAR(1000) NOT NULL DEFAULT '',
    lease_owner VARCHAR(128) NULL,
    lease_until DATETIME(3) NULL,
    updated_by BIGINT UNSIGNED NULL,
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (target_code),
    KEY idx_sync_target_configs_due (enabled, next_run_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

INSERT INTO sync_target_configs(target_code,enabled,schedule_type,interval_minutes,daily_time,timezone,next_run_at,last_run_at,last_success_at,target_version,updated_by)
SELECT 'directory',enabled,schedule_type,interval_minutes,daily_time,timezone,next_run_at,last_run_at,last_success_at,directory_version,updated_by
FROM directory_sync_configs WHERE id=1;

INSERT INTO sync_target_configs(target_code,enabled,schedule_type,interval_minutes,daily_time,timezone)
VALUES ('user_groups',0,'interval',360,'02:00','Asia/Shanghai');

CREATE TABLE sync_batches (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    trigger_type VARCHAR(20) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    requested_targets JSON NOT NULL,
    created_by BIGINT UNSIGNED NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    finished_at DATETIME(3) NULL,
    PRIMARY KEY (id),
    KEY idx_sync_batches_created (created_at, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
