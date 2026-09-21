ALTER TABLE directory_sync_configs DROP COLUMN directory_version;
ALTER TABLE directory_sync_runs
    DROP COLUMN unknown_users_count,
    DROP COLUMN member_mapping_errors,
    DROP COLUMN group_fetch_warnings,
    DROP COLUMN group_members_count,
    DROP COLUMN groups_count;
DROP TABLE IF EXISTS directory_sync_user_group_member_stage;
DROP TABLE IF EXISTS directory_sync_user_group_stage;
