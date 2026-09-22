-- Keyset pages must not sort the complete run history for every admin refresh.
-- Apply manually after checking table size, DDL time and EXPLAIN on the target.
-- No runtime role auto-applies migrations.
ALTER TABLE runs
    ADD KEY idx_runs_created_id (created_at, id),
    ADD KEY idx_runs_status_created_id (status, created_at, id),
    ALGORITHM=INPLACE, LOCK=NONE;
