-- Standalone AI model test data; MySQL 5.7-compatible. UUIDs are canonical ASCII.
CREATE TABLE ai_connections (
 id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,
 name VARCHAR(128) NOT NULL,
 adapter VARCHAR(32) NOT NULL,
 base_url VARCHAR(2048) NOT NULL,
 credential_enc TEXT NOT NULL,
 enabled BOOLEAN NOT NULL DEFAULT TRUE,
 timeout_seconds INT NOT NULL DEFAULT 60,
 max_concurrency INT NOT NULL DEFAULT 2,
 version BIGINT NOT NULL DEFAULT 1,
 created_at DATETIME(3) NOT NULL,
 updated_at DATETIME(3) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
CREATE TABLE ai_models (
 id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,
 name VARCHAR(128) NOT NULL,
 model_id VARCHAR(255) NOT NULL,
 capability_kind VARCHAR(16) NOT NULL,
 execution_location VARCHAR(32) NOT NULL,
 connection_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NULL,
 browser_manifest JSON NOT NULL,
 capabilities JSON NOT NULL,
 default_parameters JSON NOT NULL,
 enabled BOOLEAN NOT NULL DEFAULT TRUE,
 version BIGINT NOT NULL DEFAULT 1,
 validation_status VARCHAR(32) NOT NULL DEFAULT 'unverified',
 created_at DATETIME(3) NOT NULL,
 updated_at DATETIME(3) NOT NULL,
 CONSTRAINT fk_ai_model_connection FOREIGN KEY (connection_id) REFERENCES ai_connections(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
-- Stable per-owner lock serializes session-quota admission, including an empty session set.
CREATE TABLE ai_test_owners (
 owner_id BIGINT NOT NULL PRIMARY KEY
) ENGINE=InnoDB;
CREATE TABLE ai_test_sessions (
 id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,
 owner_id BIGINT NOT NULL,
 model_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 title VARCHAR(255) NOT NULL DEFAULT '',
 deleting BOOLEAN NOT NULL DEFAULT FALSE,
 cleanup_after DATETIME(3) NULL,
 cleanup_attempts INT NOT NULL DEFAULT 0,
 active_invocation_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NULL,
 created_at DATETIME(3) NOT NULL,
 expires_at DATETIME(3) NOT NULL,
 KEY idx_ai_session_owner (owner_id,expires_at),
 KEY idx_ai_session_expiry (expires_at),
 KEY idx_ai_session_cleanup (deleting,cleanup_after,expires_at),
 CONSTRAINT fk_ai_session_model FOREIGN KEY (model_id) REFERENCES ai_models(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
CREATE TABLE ai_test_messages (
 id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,
 session_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 role VARCHAR(16) NOT NULL,
 text MEDIUMTEXT NOT NULL,
 created_at DATETIME(3) NOT NULL,
 -- Monotonic sequence preserves history ordering even at equal millisecond timestamps.
 sequence_id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT UNIQUE,
 KEY idx_ai_message_session (session_id,sequence_id),
 CONSTRAINT fk_ai_message_session FOREIGN KEY (session_id) REFERENCES ai_test_sessions(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
CREATE TABLE ai_test_attachments (
 id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,
 session_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 message_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NULL,
 name VARCHAR(255) NOT NULL,
 mime_type VARCHAR(128) NOT NULL,
 size_bytes BIGINT NOT NULL,
 kind VARCHAR(16) NOT NULL,
 storage_key VARCHAR(512) NOT NULL,
 created_at DATETIME(3) NOT NULL,
 UNIQUE KEY uniq_ai_attachment_storage (storage_key),
 KEY idx_ai_attachment_session (session_id),
 CONSTRAINT fk_ai_attachment_session FOREIGN KEY (session_id) REFERENCES ai_test_sessions(id) ON DELETE CASCADE,
 CONSTRAINT fk_ai_attachment_message FOREIGN KEY (message_id) REFERENCES ai_test_messages(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
-- Transactional tombstones for individually removed, unbound attachments.
-- Metadata removal and object-cleanup admission commit together; storage I/O follows commit.
CREATE TABLE ai_object_deletions (
 storage_key VARCHAR(512) NOT NULL PRIMARY KEY,
 cleanup_after DATETIME(3) NOT NULL,
 cleanup_attempts INT NOT NULL DEFAULT 0,
 created_at DATETIME(3) NOT NULL,
 KEY idx_ai_object_cleanup (cleanup_after)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
CREATE TABLE ai_invocations (
 id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,
 session_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 owner_id BIGINT NOT NULL,
 model_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 request_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 connection_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 request_payload JSON NOT NULL,
 request_hash CHAR(64) CHARACTER SET ascii NOT NULL,
 status VARCHAR(24) NOT NULL,
 output_text MEDIUMTEXT NOT NULL,
 error_code VARCHAR(64) NOT NULL DEFAULT '',
 error_message VARCHAR(512) NOT NULL DEFAULT '',
 duration_ms BIGINT NOT NULL DEFAULT 0,
 input_tokens BIGINT NULL,
 output_tokens BIGINT NULL,
 cancel_requested BOOLEAN NOT NULL DEFAULT FALSE,
 cancellation_confirmed BOOLEAN NOT NULL DEFAULT FALSE,
 model_version BIGINT NOT NULL,
 connection_version BIGINT NOT NULL,
 config_snapshot JSON NOT NULL,
 created_at DATETIME(3) NOT NULL,
 updated_at DATETIME(3) NOT NULL,
 completed_at DATETIME(3) NULL,
 UNIQUE KEY uniq_ai_invocation_request (owner_id,request_id),
 KEY idx_ai_invocation_session (session_id,created_at),
 KEY idx_ai_invocation_owner (owner_id,model_id,created_at),
 KEY idx_ai_invocation_connection (connection_id,status),
 KEY idx_ai_invocation_stale (status,updated_at),
 CONSTRAINT fk_ai_invocation_connection FOREIGN KEY (connection_id) REFERENCES ai_connections(id) ON DELETE RESTRICT,
 CONSTRAINT fk_ai_invocation_session FOREIGN KEY (session_id) REFERENCES ai_test_sessions(id) ON DELETE CASCADE,
 CONSTRAINT fk_ai_invocation_model FOREIGN KEY (model_id) REFERENCES ai_models(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;

INSERT INTO admin_permissions(code,category,name,description) VALUES
 ('ai.model.read','ai_model','查看 AI 模型','查看非敏感连接与模型配置'),
 ('ai.model.write','ai_model','管理 AI 模型','创建修改删除连接与模型'),
 ('ai.connection.secret.write','ai_model','管理 AI 连接凭据','单独授权写入连接凭据；永不回显'),
 ('ai.model.test','ai_model','测试 AI 模型','管理本人测试会话与调用'),
 ('ai.model.log.read','ai_model','查看 AI 调用记录','查看本人脱敏调用元数据');
INSERT INTO admin_role_permissions(role_id,permission_id)
 SELECT r.id,p.id FROM admin_roles r CROSS JOIN admin_permissions p
 WHERE r.code='platform_owner' AND p.code IN ('ai.model.read','ai.model.write','ai.connection.secret.write','ai.model.test','ai.model.log.read');
