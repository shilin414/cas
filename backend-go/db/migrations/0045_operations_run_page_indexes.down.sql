ALTER TABLE runs
    DROP KEY idx_runs_status_created_id,
    DROP KEY idx_runs_created_id,
    ALGORITHM=INPLACE, LOCK=NONE;
