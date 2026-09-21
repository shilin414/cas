-- Test sessions are ephemeral; remove seeded-model sessions before reversing their catalog rows.
DELETE FROM ai_test_sessions
WHERE model_id IN ('5e1bf97e-2c92-4f8c-8f4d-46f1a6a42b02','5e1bf97e-2c92-4f8c-8f4d-46f1a6a42b03');
DELETE FROM ai_models
WHERE id IN ('5e1bf97e-2c92-4f8c-8f4d-46f1a6a42b02','5e1bf97e-2c92-4f8c-8f4d-46f1a6a42b03');
DELETE FROM ai_connections
WHERE id='5e1bf97e-2c92-4f8c-8f4d-46f1a6a42b01'
  AND NOT EXISTS (SELECT 1 FROM ai_models WHERE connection_id='5e1bf97e-2c92-4f8c-8f4d-46f1a6a42b01');
