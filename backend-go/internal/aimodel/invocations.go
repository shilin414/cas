package aimodel

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"

	db "github.com/shilin414/cas/backend-go/internal/gen/db"
	"github.com/google/uuid"
)

// BeginInvocation commits request deduplication, session/connection admission,
// the non-secret configuration snapshot, user message and attachment binding together.
// Config is an internal snapshot only and never part of request identity.
func (s *Service) BeginInvocation(ctx context.Context, owner int64, sessionID string, in MessageInput) (*Invocation, bool, error) {
	if e := validateMessage(in); e != nil {
		return nil, false, e
	}
	payload, hash := requestIdentity(in)
	var out *Invocation
	created := false
	e := s.Repo.transaction(ctx, func(q *db.Queries) error {
		session, e := s.lockSession(ctx, q, owner, sessionID)
		if e != nil {
			return e
		}
		previous, e := q.GetAIInvocationByRequest(ctx, db.GetAIInvocationByRequestParams{OwnerID: owner, RequestID: in.RequestID})
		if e == nil {
			if previous.SessionID != sessionID || previous.RequestHash != hash {
				return ErrConflict
			}
			out = invocationFrom(previous)
			return nil
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		if session.ActiveInvocationID != "" {
			return ErrInUse
		}
		// Lock the model then connection. All multi-row invocation operations lock session first.
		current, e := q.LockAIModel(ctx, session.ModelID)
		if e != nil {
			return e
		}
		if !current.Enabled {
			return ErrUnavailable
		}
		cfg := in.Config
		if cfg == nil {
			cfg, e = s.resolveRemote(ctx, q, session.ModelID)
			if e != nil {
				return e
			}
		}
		if cfg.Model == nil || cfg.Connection == nil || cfg.Model.ID != session.ModelID || cfg.Model.ConnectionID == nil || *cfg.Model.ConnectionID != cfg.Connection.ID || cfg.Model.ExecutionLocation != "server_remote" {
			return invalid("runtime configuration")
		}
		if cfg.Model.Version != current.Version || !current.ConnectionID.Valid || current.ConnectionID.String != cfg.Connection.ID {
			return ErrConflict
		}
		connection, e := q.LockAIConnection(ctx, cfg.Connection.ID)
		if e != nil {
			return e
		}
		if !connection.Enabled {
			return ErrUnavailable
		}
		if cfg.Connection.Version != connection.Version {
			return ErrConflict
		}
		active, e := q.CountAIConnectionActive(ctx, cfg.Connection.ID)
		if e != nil {
			return e
		}
		if active >= int64(connection.MaxConcurrency) {
			return ErrLimit
		}
		history, e := q.ListAITestMessages(ctx, sessionID)
		if e != nil {
			return e
		}
		if len(history)+2 > MaxHistoryMessages {
			return ErrLimit
		}
		all, e := q.ListAITestAttachments(ctx, sessionID)
		if e != nil {
			return e
		}
		selected := map[string]bool{}
		for _, id := range in.AttachmentIDs {
			selected[id] = true
		}
		var total int64
		found := 0
		for _, a := range all {
			if a.MessageID.Valid || selected[a.ID] {
				total += a.SizeBytes
				if !supportsAttachment(cfg, a.Kind) {
					return invalid("unsupported attachment capability")
				}
			}
			if selected[a.ID] {
				if a.MessageID.Valid {
					return ErrInUse
				}
				found++
			}
		}
		if found != len(selected) {
			return ErrNotFound
		}
		if total > MaxHistoryBytes {
			return ErrLimit
		}
		now := s.now()
		id := uuid.NewString()
		snapshot := encoded(cfg)
		e = q.CreateAIInvocation(ctx, db.CreateAIInvocationParams{ID: id, SessionID: sessionID, OwnerID: owner, ModelID: session.ModelID, RequestID: in.RequestID, ConnectionID: cfg.Connection.ID, RequestPayload: payload, RequestHash: hash, ModelVersion: cfg.Model.Version, ConnectionVersion: cfg.Connection.Version, ConfigSnapshot: snapshot, CreatedAt: now, UpdatedAt: now})
		if e != nil {
			return e
		}
		mid := uuid.NewString()
		if e = q.CreateAITestMessage(ctx, db.CreateAITestMessageParams{ID: mid, SessionID: sessionID, Role: "user", Text: in.Text, CreatedAt: now}); e != nil {
			return e
		}
		for _, aid := range in.AttachmentIDs {
			if e = affected(q.BindAITestAttachment(ctx, db.BindAITestAttachmentParams{ID: aid, SessionID: sessionID, MessageID: sql.NullString{String: mid, Valid: true}})); e != nil {
				return e
			}
		}
		if e = q.SetAITestSessionActive(ctx, db.SetAITestSessionActiveParams{ID: sessionID, ActiveInvocationID: sql.NullString{String: id, Valid: true}}); e != nil {
			return e
		}
		out = &Invocation{ID: id, SessionID: sessionID, OwnerID: owner, ModelID: session.ModelID, RequestID: in.RequestID, ConnectionID: cfg.Connection.ID, RequestPayload: payload, RequestHash: hash, Status: "queued", ModelVersion: cfg.Model.Version, ConnectionVersion: cfg.Connection.Version, ConfigSnapshot: snapshot, CreatedAt: now, UpdatedAt: now}
		created = true
		return nil
	})
	if e != nil {
		return nil, false, e
	}
	return out, created, nil
}
func (s *Service) GetInvocation(ctx context.Context, owner int64, id string) (*Invocation, error) {
	if owner <= 0 || !validID(id) {
		return nil, ErrNotFound
	}
	v, e := s.Repo.q().GetAIInvocation(ctx, id)
	if e != nil {
		return nil, dbError(e)
	}
	if v.OwnerID != owner {
		return nil, ErrNotFound
	}
	if _, e = s.ownedSession(ctx, owner, v.SessionID); e != nil {
		return nil, e
	}
	return invocationFrom(v), nil
}
func (s *Service) ListInvocations(ctx context.Context, owner int64, modelID string) ([]InvocationLog, error) {
	if owner <= 0 {
		return nil, ErrNotFound
	}
	if modelID != "" && !validID(modelID) {
		return nil, invalid("model_id")
	}
	rows, e := s.Repo.q().ListAIInvocations(ctx, db.ListAIInvocationsParams{OwnerID: owner, ExpiresAt: s.now(), ModelFilter: modelID})
	if e != nil {
		return nil, dbError(e)
	}
	out := make([]InvocationLog, 0, len(rows))
	for _, v := range rows {
		if modelID == "" || v.ModelID == modelID {
			out = append(out, InvocationLog{ID: v.ID, SessionID: v.SessionID, ModelID: v.ModelID, Status: v.Status, ErrorCode: v.ErrorCode, DurationMs: v.DurationMs, InputTokens: numberPtr(v.InputTokens), OutputTokens: numberPtr(v.OutputTokens), CreatedAt: v.CreatedAt, CompletedAt: timePtr(v.CompletedAt)})
		}
	}
	return out, nil
}
func (s *Service) StartInvocation(ctx context.Context, id string) (bool, error) {
	if !validID(id) {
		return false, ErrNotFound
	}
	n, e := s.Repo.q().StartAIInvocation(ctx, db.StartAIInvocationParams{ID: id, UpdatedAt: s.now()})
	return n > 0, dbError(e)
}
func (s *Service) UpdateInvocationOutput(ctx context.Context, id, text string) error {
	if !validID(id) {
		return ErrNotFound
	}
	if !validText(text, MaxOutputBytes) {
		return invalid("output size")
	}
	n, e := s.Repo.q().UpdateAIInvocationOutput(ctx, db.UpdateAIInvocationOutputParams{ID: id, OutputText: text, UpdatedAt: s.now()})
	if e != nil {
		return dbError(e)
	}
	if n == 0 {
		return ErrConflict
	}
	return nil
}
func (s *Service) RequestCancellation(ctx context.Context, owner int64, id string) (*Invocation, error) {
	inv, e := s.GetInvocation(ctx, owner, id)
	if e != nil {
		return nil, e
	}
	if _, e = s.Repo.q().RequestAIInvocationCancellation(ctx, id); e != nil {
		return nil, dbError(e)
	}
	// Queued calls have not left the process; a cancellation that wins the lock can be confirmed.
	if inv.Status == "queued" {
		if e = s.cancelQueued(ctx, inv); e != nil {
			return nil, e
		}
	}
	return s.GetInvocation(ctx, owner, id)
}
func (s *Service) cancelQueued(ctx context.Context, inv *Invocation) error {
	return s.Repo.transaction(ctx, func(q *db.Queries) error {
		if _, e := q.LockAITestSessionInternal(ctx, inv.SessionID); e != nil {
			return e
		}
		row, e := q.LockAIInvocation(ctx, inv.ID)
		if e != nil {
			return e
		}
		if row.Status != "queued" {
			return nil
		}
		return s.complete(ctx, q, row, Completion{Status: "cancelled", CancellationConfirmed: true})
	})
}
func validateCompletion(c Completion) error {
	switch c.Status {
	case "succeeded", "failed", "cancelled", "indeterminate":
	default:
		return invalid("completion status")
	}
	if !validText(c.OutputText, MaxOutputBytes) || c.DurationMs < 0 || (c.InputTokens != nil && *c.InputTokens < 0) || (c.OutputTokens != nil && *c.OutputTokens < 0) {
		return invalid("completion")
	}
	if c.CancellationConfirmed && c.Status != "cancelled" {
		return invalid("cancellation confirmation")
	}
	return nil
}

// error messages are centrally allowlisted, never persisted from upstream exceptions.
func safeCompletionError(code string) (string, string) {
	messages := map[string]string{
		"upstream_auth": "模型服务认证失败，请检查连接凭据", "upstream_rate_limit": "模型服务限流，请稍后重试", "upstream_protocol": "模型服务协议不兼容", "empty_response": "模型服务返回空响应", "redirect_blocked": "模型服务重定向已阻止", "input_limit": "模型输入超过限制", "attachment_unavailable": "测试附件不可用", "request_preparation": "测试请求准备失败", "invalid_model": "模型标识无效", "invalid_parameter": "模型参数无效",
		"internal_error": "模型调用失败", "local_capacity": "测试服务繁忙，请稍后重试", "execution_start": "无法启动测试调用",
		"network_error": "无法连接模型服务", "invalid_endpoint": "模型连接地址无效", "blocked_endpoint": "模型服务地址不符合出网策略",
		"invalid_credential": "模型凭据无效", "upstream_error": "模型服务返回错误", "upstream_rejected": "模型服务拒绝请求",
		"upstream_unconfirmed": "请求已中断，上游是否停止或产生用量尚未确认", "upstream_incomplete": "模型响应读取中断",
		"unsupported_input": "模型不支持此输入类型", "output_limit": "模型输出超过限制", "state_write_failed": "无法保存测试输出，调用已中断",
		"stale_invocation": "服务重启或调用超时，上游完成状态未确认", "invalid_response": "模型服务响应无效",
	}
	if message, ok := messages[code]; ok {
		return code, message
	}
	return "internal_error", messages["internal_error"]
}
func (s *Service) FinishInvocation(ctx context.Context, id string, c Completion) error {
	if !validID(id) {
		return ErrNotFound
	}
	if e := validateCompletion(c); e != nil {
		return e
	}
	v, e := s.Repo.q().GetAIInvocation(ctx, id)
	if e != nil {
		return dbError(e)
	}
	return s.Repo.transaction(ctx, func(q *db.Queries) error {
		if _, e := q.LockAITestSessionInternal(ctx, v.SessionID); e != nil {
			return e
		}
		row, e := q.LockAIInvocation(ctx, id)
		if e != nil {
			return e
		}
		if row.Status != "queued" && row.Status != "running" {
			return nil
		}
		return s.complete(ctx, q, row, c)
	})
}
func (s *Service) complete(ctx context.Context, q *db.Queries, v db.AiInvocation, c Completion) error {
	now := s.now()
	code, message := "", ""
	if c.Status == "failed" || c.Status == "indeterminate" {
		code, message = safeCompletionError(c.ErrorCode)
	}
	if e := affected(q.CompleteAIInvocation(ctx, db.CompleteAIInvocationParams{ID: v.ID, Status: c.Status, OutputText: c.OutputText, ErrorCode: code, ErrorMessage: message, DurationMs: c.DurationMs, InputTokens: nullNumber(c.InputTokens), OutputTokens: nullNumber(c.OutputTokens), CancellationConfirmed: c.CancellationConfirmed, UpdatedAt: now, CompletedAt: sql.NullTime{Time: now, Valid: true}})); e != nil {
		return e
	}
	if c.Status == "succeeded" {
		if e := q.CreateAITestMessage(ctx, db.CreateAITestMessageParams{ID: uuid.NewString(), SessionID: v.SessionID, Role: "assistant", Text: c.OutputText, CreatedAt: now}); e != nil {
			return e
		}
	}
	return q.ReleaseAITestSession(ctx, db.ReleaseAITestSessionParams{ID: v.SessionID, ActiveInvocationID: sql.NullString{String: v.ID, Valid: true}})
}
func (s *Service) RecoverStaleInvocations(ctx context.Context, olderThan time.Time) (int, error) {
	if olderThan.IsZero() || !olderThan.Before(s.now()) {
		return 0, invalid("recovery cutoff")
	}
	rows, e := s.Repo.q().ListStaleAIInvocations(ctx, olderThan)
	if e != nil {
		return 0, dbError(e)
	}
	count := 0
	for _, v := range rows {
		recovered := false
		e = s.Repo.transaction(ctx, func(q *db.Queries) error {
			if _, e := q.LockAITestSessionInternal(ctx, v.SessionID); e != nil {
				return e
			}
			row, e := q.LockAIInvocation(ctx, v.ID)
			if e != nil {
				return e
			}
			if (row.Status != "queued" && row.Status != "running") || !row.UpdatedAt.Before(olderThan) {
				return nil
			}
			c := Completion{Status: "indeterminate", OutputText: row.OutputText, ErrorCode: "stale_invocation", DurationMs: s.now().Sub(row.CreatedAt).Milliseconds()}
			if e = s.complete(ctx, q, row, c); e != nil {
				return e
			}
			recovered = true
			return nil
		})
		if e != nil {
			return count, e
		}
		if recovered {
			count++
		}
	}
	return count, nil
}

// requestIdentity intentionally excludes Config, which is internal and may change between retries.
func requestIdentity(in MessageInput) ([]byte, string) {
	if in.AttachmentIDs == nil {
		in.AttachmentIDs = []string{}
	}
	payload := encoded(in)
	digest := sha256.Sum256(payload)
	return payload, hex.EncodeToString(digest[:])
}

// LookupInvocation is checked before resolving a live model so retries still work
// after an administrator disables or edits configuration. BeginInvocation repeats
// this check transactionally; this read alone is not admission.
func (s *Service) LookupInvocation(ctx context.Context, owner int64, sessionID string, in MessageInput) (*Invocation, error) {
	if e := validateMessage(in); e != nil {
		return nil, e
	}
	if _, e := s.ownedSession(ctx, owner, sessionID); e != nil {
		return nil, e
	}
	row, e := s.Repo.q().GetAIInvocationByRequest(ctx, db.GetAIInvocationByRequestParams{OwnerID: owner, RequestID: in.RequestID})
	if errors.Is(e, sql.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, dbError(e)
	}
	_, hash := requestIdentity(in)
	if row.SessionID != sessionID || row.RequestHash != hash {
		return nil, ErrConflict
	}
	return invocationFrom(row), nil
}
