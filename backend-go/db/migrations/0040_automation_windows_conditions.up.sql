-- Effective windows live in schedules.trigger_config (no schema change).
-- Old targets/snapshots remain unconditional; never backfill from mutable config.
ALTER TABLE schedule_deliveries
 ADD COLUMN condition_operator VARCHAR(20) NOT NULL DEFAULT 'always',
 ADD COLUMN condition_text TEXT NULL;
ALTER TABLE occurrence_delivery_expectations
 ADD COLUMN condition_operator VARCHAR(20) NOT NULL DEFAULT 'always',
 ADD COLUMN condition_text TEXT NULL;
