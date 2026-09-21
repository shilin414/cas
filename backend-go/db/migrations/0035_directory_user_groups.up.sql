CREATE TABLE directory_sync_user_group_stage (
    run_id BIGINT UNSIGNED NOT NULL,
    external_group_id VARCHAR(128) NOT NULL,
    name VARCHAR(128) NOT NULL DEFAULT '',
    description VARCHAR(500) NOT NULL DEFAULT '',
    group_type VARCHAR(32) NOT NULL DEFAULT 'unknown',
    PRIMARY KEY (run_id, external_group_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE directory_sync_user_group_member_stage (
    run_id BIGINT UNSIGNED NOT NULL,
    external_group_id VARCHAR(128) NOT NULL,
    external_user_id VARCHAR(128) NOT NULL,
    PRIMARY KEY (run_id, external_group_id, external_user_id),
    KEY idx_directory_group_member_user (run_id, external_user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

ALTER TABLE directory_sync_runs
    ADD COLUMN groups_count INT UNSIGNED NOT NULL DEFAULT 0 AFTER active_memberships_count,
    ADD COLUMN group_members_count INT UNSIGNED NOT NULL DEFAULT 0 AFTER groups_count,
    ADD COLUMN group_fetch_warnings INT UNSIGNED NOT NULL DEFAULT 0 AFTER group_members_count,
    ADD COLUMN member_mapping_errors INT UNSIGNED NOT NULL DEFAULT 0 AFTER group_fetch_warnings,
    ADD COLUMN unknown_users_count INT UNSIGNED NOT NULL DEFAULT 0 AFTER member_mapping_errors;

ALTER TABLE directory_sync_configs
    ADD COLUMN directory_version BIGINT UNSIGNED NOT NULL DEFAULT 0 AFTER last_success_at;
