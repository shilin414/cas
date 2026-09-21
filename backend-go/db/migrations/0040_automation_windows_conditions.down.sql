ALTER TABLE occurrence_delivery_expectations
 DROP COLUMN condition_text, DROP COLUMN condition_operator;
ALTER TABLE schedule_deliveries
 DROP COLUMN condition_text, DROP COLUMN condition_operator;
