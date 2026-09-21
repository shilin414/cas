ALTER TABLE conversation_shares
  DROP INDEX uniq_conversation_shares_source_run,
  DROP COLUMN source_run_id;
