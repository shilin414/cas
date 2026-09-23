-- Secret-free built-in model presets. Operators still set credentials through the write-only API.
SET @glm_connection_id = '5e1bf97e-2c92-4f8c-8f4d-46f1a6a42b01';
SET @glm_model_id = '5e1bf97e-2c92-4f8c-8f4d-46f1a6a42b02';
SET @ocr_model_id = '5e1bf97e-2c92-4f8c-8f4d-46f1a6a42b03';

INSERT INTO ai_connections(
 id,name,adapter,base_url,credential_enc,enabled,timeout_seconds,max_concurrency,version,created_at,updated_at
)
SELECT @glm_connection_id,'GLM-4.6V-FlashX 内网连接','openai_chat','http://192.168.212.121:3000/v1','',TRUE,600,2,1,UTC_TIMESTAMP(3),UTC_TIMESTAMP(3)
FROM DUAL
WHERE NOT EXISTS (SELECT 1 FROM ai_connections WHERE id=@glm_connection_id);

INSERT INTO ai_models(
 id,name,model_id,capability_kind,execution_location,connection_id,browser_manifest,capabilities,default_parameters,enabled,version,validation_status,created_at,updated_at
)
SELECT @glm_model_id,'GLM-4.6V-FlashX','glm-4.6v-flashx','chat','server_remote',@glm_connection_id,
       '{}','{"image":true,"video":false,"pdf":false,"streaming":false}','{"temperature":0.1}',
       TRUE,1,'unverified',UTC_TIMESTAMP(3),UTC_TIMESTAMP(3)
FROM DUAL
WHERE NOT EXISTS (SELECT 1 FROM ai_models WHERE model_id='glm-4.6v-flashx' AND execution_location='server_remote');

-- If an existing GLM model made the seeded connection unnecessary, leave no orphan.
DELETE FROM ai_connections
WHERE id=@glm_connection_id
  AND NOT EXISTS (SELECT 1 FROM ai_models WHERE connection_id=@glm_connection_id);

-- Browser OCR is temporarily disabled (2026-09, no runtime/model assets in the
-- build): seeded DISABLED so it stays out of the usable catalog until re-enabled.
INSERT INTO ai_models(
 id,name,model_id,capability_kind,execution_location,connection_id,browser_manifest,capabilities,default_parameters,enabled,version,validation_status,created_at,updated_at
)
SELECT @ocr_model_id,'内置 OCR（PP-OCRv6 Tiny）','PP-OCRv6_tiny','ocr','browser_local',NULL,
       '{"adapter":"paddleocr_tiny","version":"ppocrv6-tiny-20260921","resources":[{"name":"PP-OCRv6_tiny_det","url":"ocr-assets/ppocrv6-tiny-20260921/PP-OCRv6_tiny_det.tar","sha256":"ff6ab415b0a6e0c488550f2fb5d5046f1719848df220b2dc21b56402a65bc05d","size_bytes":1792000},{"name":"PP-OCRv6_tiny_rec","url":"ocr-assets/ppocrv6-tiny-20260921/PP-OCRv6_tiny_rec.tar","sha256":"1e13b22717b1edd89d4cde4fda272b6c17d5b505c97c2baea99da1a3a2d54b29","size_bytes":4526080}]}',
       '{"image":true,"video":false,"pdf":false,"streaming":false}','{}',
       FALSE,1,'unverified',UTC_TIMESTAMP(3),UTC_TIMESTAMP(3)
FROM DUAL
WHERE NOT EXISTS (SELECT 1 FROM ai_models WHERE model_id='PP-OCRv6_tiny' AND execution_location='browser_local');
