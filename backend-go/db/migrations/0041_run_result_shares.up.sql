-- One immutable, revocable result share per run, reused across recipients and retries.
-- NULL preserves all existing manually-created shares. No foreign key to keep
-- the existing explicit conversation cleanup behavior.
ALTER TABLE conversation_shares
  ADD COLUMN source_run_id BINARY(16) NULL,
  ADD UNIQUE KEY uniq_conversation_shares_source_run (source_run_id);
