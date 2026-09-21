package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/shilin414/cas/backend-go/internal/aimodel"
	modelruntime "github.com/shilin414/cas/backend-go/internal/aimodel/runtime"
	genapi "github.com/shilin414/cas/backend-go/internal/gen/api"
	"github.com/google/uuid"
)

func writeAIRuntimeError(w http.ResponseWriter, err error) {
	status, code, detail := http.StatusInternalServerError, "internal_error", "AI 测试暂不可用，请稍后重试"
	switch {
	case errors.Is(err, aimodel.ErrNotFound):
		status, code, detail = 404, "not_found", "测试资源不存在或已过期"
	case errors.Is(err, aimodel.ErrInvalid):
		status, code, detail = 400, "invalid_input", "测试参数无效，请检查输入"
	case errors.Is(err, aimodel.ErrConflict), errors.Is(err, aimodel.ErrInUse):
		status, code, detail = 409, "conflict", "资源正在使用或请求内容冲突"
	case errors.Is(err, aimodel.ErrLimit):
		status, code, detail = 429, "limit", "已达到测试限制，请结束当前调用或新建对话"
	case errors.Is(err, aimodel.ErrUnavailable):
		status, code, detail = 503, "unavailable", "模型未启用或测试服务暂不可用"
	default:
		var remoteError *modelruntime.Error
		if errors.As(err, &remoteError) {
			status, code, detail = 400, remoteError.Code, remoteError.Message
		}
	}
	writeJSON(w, status, map[string]string{"code": code, "detail": detail})
}
func (s *Server) SendAITestMessage(w http.ResponseWriter, r *http.Request, id string) {
	caller := s.requireAdminPermission(w, r, aimodel.PermissionTest)
	if caller == nil {
		return
	}
	if s.AIModels == nil || s.AIModelRuntime == nil {
		writeAIRuntimeError(w, aimodel.ErrUnavailable)
		return
	}
	var input aimodel.MessageInput
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeAIRuntimeError(w, aimodel.ErrInvalid)
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeAIRuntimeError(w, aimodel.ErrInvalid)
		return
	}
	invocation, err := s.AIModelRuntime.Submit(r.Context(), caller.ID, id, input)
	if err != nil {
		writeAIRuntimeError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"invocation_id": invocation.ID})
}
func (s *Server) GetAIInvocation(w http.ResponseWriter, r *http.Request, id string) {
	caller := s.requireAdminPermission(w, r, aimodel.PermissionTest)
	if caller == nil {
		return
	}
	if s.AIModels == nil {
		writeAIRuntimeError(w, aimodel.ErrUnavailable)
		return
	}
	result, err := s.AIModels.GetInvocation(r.Context(), caller.ID, id)
	if err != nil {
		writeAIRuntimeError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, result)
}
func (s *Server) ListAIInvocations(w http.ResponseWriter, r *http.Request, params genapi.ListAIInvocationsParams) {
	caller := s.requireAdminPermission(w, r, aimodel.PermissionLogRead)
	if caller == nil {
		return
	}
	if s.AIModels == nil {
		writeAIRuntimeError(w, aimodel.ErrUnavailable)
		return
	}
	modelID := ""
	if params.ModelId != nil {
		modelID = *params.ModelId
	}
	result, err := s.AIModels.ListInvocations(r.Context(), caller.ID, modelID)
	if err != nil {
		writeAIRuntimeError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, result)
}
func (s *Server) CancelAIInvocation(w http.ResponseWriter, r *http.Request, id string) {
	caller := s.requireAdminPermission(w, r, aimodel.PermissionTest)
	if caller == nil {
		return
	}
	if s.AIModelRuntime == nil {
		writeAIRuntimeError(w, aimodel.ErrUnavailable)
		return
	}
	result, err := s.AIModelRuntime.Cancel(r.Context(), caller.ID, id)
	if err != nil {
		writeAIRuntimeError(w, err)
		return
	}
	writeJSON(w, 200, result)
}

// Bound in-memory multipart preprocessing independently of invocation slots.
func (s *Server) acquireAIUpload() (func(), bool) {
	s.aiUploadOnce.Do(func() { s.aiUploadSlots = make(chan struct{}, 4) })
	select {
	case s.aiUploadSlots <- struct{}{}:
		return func() { <-s.aiUploadSlots }, true
	default:
		return nil, false
	}
}
func (s *Server) UploadAITestAttachment(w http.ResponseWriter, r *http.Request, id string) {
	caller := s.requireAdminPermission(w, r, aimodel.PermissionTest)
	if caller == nil {
		return
	}
	if s.AIModels == nil || s.Storage == nil {
		writeAIRuntimeError(w, aimodel.ErrUnavailable)
		return
	}
	release, acquired := s.acquireAIUpload()
	if !acquired {
		writeAIRuntimeError(w, aimodel.ErrLimit)
		return
	}
	defer release()
	cfg, err := s.AIModels.RuntimeConfig(r.Context(), caller.ID, id)
	if err != nil {
		writeAIRuntimeError(w, err)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, aimodel.MaxAttachmentBytes+(1<<20))
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		writeAIRuntimeError(w, aimodel.ErrInvalid)
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeAIRuntimeError(w, aimodel.ErrInvalid)
		return
	}
	defer file.Close()
	totalFiles := 0
	for _, files := range r.MultipartForm.File {
		totalFiles += len(files)
	}
	if totalFiles != 1 {
		writeAIRuntimeError(w, aimodel.ErrInvalid)
		return
	}
	data, err := io.ReadAll(io.LimitReader(file, aimodel.MaxAttachmentBytes+1))
	if err != nil {
		writeAIRuntimeError(w, aimodel.ErrInvalid)
		return
	}
	kind, mimeType, err := modelruntime.ValidateFile(data)
	if err != nil {
		writeAIRuntimeError(w, err)
		return
	}
	supported := kind == "image" && cfg.Model.Capabilities.Image || kind == "video" && cfg.Model.Capabilities.Video || kind == "pdf" && cfg.Model.Capabilities.PDF
	if !supported || (cfg.Connection.Adapter == "openai_chat" && kind != "image") {
		writeAIRuntimeError(w, &modelruntime.Error{Code: "unsupported_input", Message: "所选模型和协议不支持此文件类型，文件未保存或外发"})
		return
	}
	name := path.Base(strings.ReplaceAll(header.Filename, "\\", "/"))
	if name == "." || len(name) > 240 || strings.ContainsAny(name, "\r\n\x00") {
		writeAIRuntimeError(w, aimodel.ErrInvalid)
		return
	}
	key := fmt.Sprintf("ai-tests/%d/%s/%s", caller.ID, id, uuid.NewString())
	if _, err := s.Storage.Put(r.Context(), key, bytes.NewReader(data), mimeType); err != nil {
		writeAIRuntimeError(w, aimodel.ErrUnavailable)
		return
	}
	attachment, err := s.AIModels.AddAttachment(r.Context(), caller.ID, id, aimodel.AttachmentInput{Name: name, MIMEType: mimeType, SizeBytes: int64(len(data)), Kind: kind, StorageKey: key})
	if err != nil {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = s.Storage.Delete(cleanupCtx, key)
		cleanupCancel()
		writeAIRuntimeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, attachment)
}
func (s *Server) GetAITestAttachment(w http.ResponseWriter, r *http.Request, id, attachmentId string) {
	caller := s.requireAdminPermission(w, r, aimodel.PermissionTest)
	if caller == nil {
		return
	}
	if s.AIModels == nil || s.Storage == nil {
		writeAIRuntimeError(w, aimodel.ErrUnavailable)
		return
	}
	attachment, err := s.AIModels.GetAttachment(r.Context(), caller.ID, id, attachmentId)
	if err != nil {
		writeAIRuntimeError(w, err)
		return
	}
	file, object, err := s.Storage.Open(r.Context(), attachment.StorageKey)
	if err != nil {
		writeAIRuntimeError(w, aimodel.ErrNotFound)
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", attachment.MIMEType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": attachment.Name}))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
	http.ServeContent(w, r, attachment.Name, object.LastModified, file)
}
func (s *Server) DeleteAITestAttachment(w http.ResponseWriter, r *http.Request, id, attachmentId string) {
	caller := s.requireAdminPermission(w, r, aimodel.PermissionTest)
	if caller == nil {
		return
	}
	if s.AIModels == nil {
		writeAIRuntimeError(w, aimodel.ErrUnavailable)
		return
	}
	if err := s.AIModels.DeleteAttachment(r.Context(), caller.ID, id, attachmentId); err != nil {
		writeAIRuntimeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
