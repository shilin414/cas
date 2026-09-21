-- name: ListAIConnections :many
SELECT id, name, adapter, base_url, credential_enc, enabled, timeout_seconds, max_concurrency, version, created_at, updated_at FROM ai_connections ORDER BY created_at,id;

-- name: GetAIConnection :one
SELECT id, name, adapter, base_url, credential_enc, enabled, timeout_seconds, max_concurrency, version, created_at, updated_at FROM ai_connections WHERE id=?;

-- name: LockAIConnection :one
SELECT id, name, adapter, base_url, credential_enc, enabled, timeout_seconds, max_concurrency, version, created_at, updated_at FROM ai_connections WHERE id=? FOR UPDATE;

-- name: CountAIConnectionActive :one
SELECT COUNT(*) FROM ai_invocations WHERE connection_id=? AND status IN ('queued','running');

-- name: CreateAIConnection :exec
INSERT INTO ai_connections(id,name,adapter,base_url,credential_enc,enabled,timeout_seconds,max_concurrency,version,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,1,?,?);

-- name: UpdateAIConnection :execrows
UPDATE ai_connections SET name=?,adapter=?,base_url=?,enabled=?,timeout_seconds=?,max_concurrency=?,version=version+1,updated_at=? WHERE id=? AND version=?;

-- name: SetAIConnectionCredential :execrows
UPDATE ai_connections SET credential_enc=?,version=version+1,updated_at=? WHERE id=?;

-- name: DeleteAIConnection :execrows
DELETE FROM ai_connections WHERE id=?;

-- name: ListAIModels :many
SELECT id, name, model_id, capability_kind, execution_location, connection_id, browser_manifest, capabilities, default_parameters, enabled, version, validation_status, created_at, updated_at FROM ai_models ORDER BY created_at,id;

-- name: GetAIModel :one
SELECT id, name, model_id, capability_kind, execution_location, connection_id, browser_manifest, capabilities, default_parameters, enabled, version, validation_status, created_at, updated_at FROM ai_models WHERE id=?;

-- name: LockAIModel :one
SELECT id, name, model_id, capability_kind, execution_location, connection_id, browser_manifest, capabilities, default_parameters, enabled, version, validation_status, created_at, updated_at FROM ai_models WHERE id=? FOR UPDATE;

-- name: CreateAIModel :exec
INSERT INTO ai_models(id,name,model_id,capability_kind,execution_location,connection_id,browser_manifest,capabilities,default_parameters,enabled,version,validation_status,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,1,'unverified',?,?);

-- name: UpdateAIModel :execrows
UPDATE ai_models SET name=?,model_id=?,capability_kind=?,execution_location=?,connection_id=?,browser_manifest=?,capabilities=?,default_parameters=?,enabled=?,version=version+1,validation_status='unverified',updated_at=? WHERE id=? AND version=?;

-- name: DeleteAIModel :execrows
DELETE FROM ai_models WHERE id=?;

-- name: EnsureAITestOwner :exec
INSERT IGNORE INTO ai_test_owners(owner_id) VALUES (?);

-- name: LockAITestOwner :one
SELECT owner_id FROM ai_test_owners WHERE owner_id=? FOR UPDATE;

-- name: CountAITestSessions :one
SELECT COUNT(*) FROM ai_test_sessions WHERE owner_id=? AND expires_at>? AND deleting=FALSE;

-- name: ListAITestSessions :many
SELECT id, owner_id, model_id, title, deleting, cleanup_after, cleanup_attempts, active_invocation_id, created_at, expires_at FROM ai_test_sessions WHERE owner_id=? AND expires_at>? AND deleting=FALSE ORDER BY created_at DESC,id;

-- name: GetAITestSession :one
SELECT id, owner_id, model_id, title, deleting, cleanup_after, cleanup_attempts, active_invocation_id, created_at, expires_at FROM ai_test_sessions WHERE id=? AND owner_id=? AND expires_at>? AND deleting=FALSE;

-- name: LockAITestSession :one
SELECT id, owner_id, model_id, title, deleting, cleanup_after, cleanup_attempts, active_invocation_id, created_at, expires_at FROM ai_test_sessions WHERE id=? AND owner_id=? AND expires_at>? AND deleting=FALSE FOR UPDATE;

-- name: GetAITestSessionInternal :one
SELECT id, owner_id, model_id, title, deleting, cleanup_after, cleanup_attempts, active_invocation_id, created_at, expires_at FROM ai_test_sessions WHERE id=?;

-- name: LockAITestSessionInternal :one
SELECT id, owner_id, model_id, title, deleting, cleanup_after, cleanup_attempts, active_invocation_id, created_at, expires_at FROM ai_test_sessions WHERE id=? FOR UPDATE;

-- name: CreateAITestSession :exec
INSERT INTO ai_test_sessions(id,owner_id,model_id,title,created_at,expires_at) VALUES (?,?,?,?,?,?);

-- name: SetAITestSessionActive :exec
UPDATE ai_test_sessions SET active_invocation_id=? WHERE id=?;

-- name: ReleaseAITestSession :exec
UPDATE ai_test_sessions SET active_invocation_id=NULL WHERE id=? AND active_invocation_id=?;

-- name: DeleteAITestSession :execrows
DELETE FROM ai_test_sessions WHERE id=?;

-- name: ListExpiredAITestSessions :many
SELECT id, owner_id, model_id, title, deleting, cleanup_after, cleanup_attempts, active_invocation_id, created_at, expires_at FROM ai_test_sessions WHERE (expires_at<=sqlc.arg(expiry_cutoff) OR deleting=TRUE) AND active_invocation_id IS NULL AND (cleanup_after IS NULL OR cleanup_after<=sqlc.arg(retry_cutoff)) ORDER BY COALESCE(cleanup_after,expires_at),id LIMIT ?;

-- name: MarkAITestSessionDeleting :execrows
UPDATE ai_test_sessions SET deleting=TRUE,cleanup_after=? WHERE id=? AND active_invocation_id IS NULL;

-- name: RetryAITestSessionCleanup :execrows
UPDATE ai_test_sessions SET cleanup_after=?,cleanup_attempts=cleanup_attempts+1 WHERE id=? AND deleting=TRUE;

-- name: ListAITestMessages :many
SELECT id, session_id, role, text, created_at, sequence_id FROM ai_test_messages WHERE session_id=? ORDER BY sequence_id;

-- name: CreateAITestMessage :exec
INSERT INTO ai_test_messages(id,session_id,role,text,created_at) VALUES (?,?,?,?,?);

-- name: ListAITestAttachments :many
SELECT id, session_id, message_id, name, mime_type, size_bytes, kind, storage_key, created_at FROM ai_test_attachments WHERE session_id=? ORDER BY created_at,id;

-- name: GetAITestAttachment :one
SELECT id, session_id, message_id, name, mime_type, size_bytes, kind, storage_key, created_at FROM ai_test_attachments WHERE id=? AND session_id=?;

-- name: CreateAITestAttachment :exec
INSERT INTO ai_test_attachments(id,session_id,name,mime_type,size_bytes,kind,storage_key,created_at) VALUES (?,?,?,?,?,?,?,?);

-- name: BindAITestAttachment :execrows
UPDATE ai_test_attachments SET message_id=? WHERE id=? AND session_id=? AND message_id IS NULL;

-- name: DeleteAITestAttachment :execrows
DELETE FROM ai_test_attachments WHERE id=? AND session_id=? AND message_id IS NULL;

-- name: DeleteAITestSessionAttachments :exec
DELETE FROM ai_test_attachments WHERE session_id=?;

-- name: GetAIInvocation :one
SELECT id, session_id, owner_id, model_id, request_id, connection_id, request_payload, request_hash, status, output_text, error_code, error_message, duration_ms, input_tokens, output_tokens, cancel_requested, cancellation_confirmed, model_version, connection_version, config_snapshot, created_at, updated_at, completed_at FROM ai_invocations WHERE id=?;

-- name: LockAIInvocation :one
SELECT id, session_id, owner_id, model_id, request_id, connection_id, request_payload, request_hash, status, output_text, error_code, error_message, duration_ms, input_tokens, output_tokens, cancel_requested, cancellation_confirmed, model_version, connection_version, config_snapshot, created_at, updated_at, completed_at FROM ai_invocations WHERE id=? FOR UPDATE;

-- name: GetAIInvocationByRequest :one
SELECT id, session_id, owner_id, model_id, request_id, connection_id, request_payload, request_hash, status, output_text, error_code, error_message, duration_ms, input_tokens, output_tokens, cancel_requested, cancellation_confirmed, model_version, connection_version, config_snapshot, created_at, updated_at, completed_at FROM ai_invocations WHERE owner_id=? AND request_id=?;

-- name: ListAIInvocations :many
SELECT i.id, i.session_id, i.owner_id, i.model_id, i.request_id, i.connection_id, i.request_payload, i.request_hash, i.status, i.output_text, i.error_code, i.error_message, i.duration_ms, i.input_tokens, i.output_tokens, i.cancel_requested, i.cancellation_confirmed, i.model_version, i.connection_version, i.config_snapshot, i.created_at, i.updated_at, i.completed_at FROM ai_invocations i JOIN ai_test_sessions s ON s.id=i.session_id WHERE i.owner_id=? AND s.expires_at>? AND s.deleting=FALSE AND (sqlc.arg(model_filter) = '' OR i.model_id=sqlc.arg(model_filter)) ORDER BY i.created_at DESC,i.id LIMIT 200;

-- name: CreateAIInvocation :exec
INSERT INTO ai_invocations(id,session_id,owner_id,model_id,request_id,connection_id,request_payload,request_hash,status,output_text,model_version,connection_version,config_snapshot,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,'queued','',?,?,?,?,?);

-- name: StartAIInvocation :execrows
UPDATE ai_invocations SET status='running',updated_at=? WHERE id=? AND status='queued' AND cancel_requested=FALSE;

-- name: UpdateAIInvocationOutput :execrows
UPDATE ai_invocations SET output_text=?,updated_at=? WHERE id=? AND status='running';

-- name: CompleteAIInvocation :execrows
UPDATE ai_invocations SET status=?,output_text=?,error_code=?,error_message=?,duration_ms=?,input_tokens=?,output_tokens=?,cancellation_confirmed=?,updated_at=?,completed_at=? WHERE id=? AND status IN ('queued','running');

-- name: RequestAIInvocationCancellation :execrows
UPDATE ai_invocations SET cancel_requested=TRUE WHERE id=? AND status IN ('queued','running');

-- name: ListStaleAIInvocations :many
SELECT id, session_id, owner_id, model_id, request_id, connection_id, request_payload, request_hash, status, output_text, error_code, error_message, duration_ms, input_tokens, output_tokens, cancel_requested, cancellation_confirmed, model_version, connection_version, config_snapshot, created_at, updated_at, completed_at FROM ai_invocations WHERE status IN ('queued','running') AND updated_at<? ORDER BY updated_at LIMIT 200;

-- name: CreateAIObjectDeletion :exec
INSERT INTO ai_object_deletions(storage_key,cleanup_after,created_at) VALUES (?,?,?);

-- name: GetAIObjectDeletion :one
SELECT storage_key, cleanup_after, cleanup_attempts, created_at FROM ai_object_deletions WHERE storage_key=?;

-- name: LockAIObjectDeletion :one
SELECT storage_key, cleanup_after, cleanup_attempts, created_at FROM ai_object_deletions WHERE storage_key=? FOR UPDATE;

-- name: ListAIObjectDeletions :many
SELECT storage_key, cleanup_after, cleanup_attempts, created_at FROM ai_object_deletions WHERE cleanup_after<=? ORDER BY cleanup_after,storage_key LIMIT ?;

-- name: LeaseAIObjectDeletion :execrows
UPDATE ai_object_deletions SET cleanup_after=? WHERE storage_key=?;

-- name: RetryAIObjectDeletion :execrows
UPDATE ai_object_deletions SET cleanup_after=?,cleanup_attempts=cleanup_attempts+1 WHERE storage_key=?;

-- name: DeleteAIObjectDeletion :exec
DELETE FROM ai_object_deletions WHERE storage_key=?;

-- name: CreateAIAdminAudit :exec
-- Non-sensitive configuration mutation metadata only; use the same transaction as the write.
INSERT INTO audit_logs (user_id, action, resource, resource_id, detail) VALUES (?, ?, ?, ?, ?);
