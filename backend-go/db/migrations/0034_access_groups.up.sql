CREATE TABLE access_groups (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    code VARCHAR(100) NOT NULL,
    name VARCHAR(128) NOT NULL,
    description VARCHAR(500) NOT NULL DEFAULT '',
    source_type VARCHAR(32) NOT NULL,
    external_group_id VARCHAR(128) NOT NULL DEFAULT '',
    external_group_type VARCHAR(32) NOT NULL DEFAULT '',
    enabled TINYINT(1) NOT NULL DEFAULT 1,
    sync_status VARCHAR(32) NOT NULL DEFAULT '',
    sync_generation BIGINT UNSIGNED NULL,
    last_synced_at DATETIME(3) NULL,
    created_by BIGINT UNSIGNED NULL,
    updated_by BIGINT UNSIGNED NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uniq_access_group_code (code),
    UNIQUE KEY uniq_access_group_external (source_type, external_group_id),
    KEY idx_access_group_source (source_type, enabled, name),
    KEY idx_access_group_sync (sync_status, last_synced_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE access_group_departments (
    group_id BIGINT UNSIGNED NOT NULL,
    department_id BIGINT UNSIGNED NOT NULL,
    include_children TINYINT(1) NOT NULL DEFAULT 0,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (group_id, department_id),
    KEY idx_access_group_department (department_id, group_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE access_group_users (
    group_id BIGINT UNSIGNED NOT NULL,
    directory_user_id BIGINT UNSIGNED NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (group_id, directory_user_id),
    KEY idx_access_group_user (directory_user_id, group_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE access_group_external_members (
    group_id BIGINT UNSIGNED NOT NULL,
    directory_user_id BIGINT UNSIGNED NOT NULL,
    external_user_id VARCHAR(128) NOT NULL DEFAULT '',
    synced_at DATETIME(3) NOT NULL,
    PRIMARY KEY (group_id, directory_user_id),
    KEY idx_group_external_member_user (directory_user_id, group_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

CREATE TABLE application_group_grants (
    application_id BIGINT UNSIGNED NOT NULL,
    group_id BIGINT UNSIGNED NOT NULL,
    created_by BIGINT UNSIGNED NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (application_id, group_id),
    KEY idx_application_group_group (group_id, application_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
