package http

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/shilin414/cas/backend-go/internal/aimodel"
	genapi "github.com/shilin414/cas/backend-go/internal/gen/api"
)

// aiModelCaller reuses RBAC decisions, but never returns database error text.
func (s *Server) aiModelCaller(w http.ResponseWriter, r *http.Request, permission string) *AuthenticatedUser {
	user := userFrom(r.Context())
	if user == nil {
		writeDetail(w, 401, "Authentication credentials were not provided.")
		return nil
	}
	allowed := user.IsStaff
	if s.AdminRBAC != nil {
		var err error
		allowed, err = s.AdminRBAC.HasPermission(r.Context(), user.ID, user.IsStaff, permission)
		if err != nil {
			writeDetail(w, 503, "AI model permission service unavailable")
			return nil
		}
	}
	if !allowed {
		writeDetail(w, 403, "没有执行此操作所需的权限。")
		return nil
	}
	if s.AIModels == nil {
		writeDetail(w, 503, "AI model service unavailable")
		return nil
	}
	return user
}
func writeAIAdminError(w http.ResponseWriter, err error) {
	status := 503
	code := "unavailable"
	detail := "AI model service unavailable"
	switch {
	case errors.Is(err, aimodel.ErrInvalid):
		status = 400
		code = "invalid_input"
		detail = "AI model input is invalid"
	case errors.Is(err, aimodel.ErrNotFound):
		status = 404
		code = "not_found"
		detail = "AI model resource not found"
	case errors.Is(err, aimodel.ErrConflict):
		status = 409
		code = "conflict"
		detail = "AI model resource conflict"
	case errors.Is(err, aimodel.ErrInUse):
		status = 409
		code = "in_use"
		detail = "AI model resource is in use"
	case errors.Is(err, aimodel.ErrLimit):
		status = 429
		code = "limit_exceeded"
		detail = "AI model resource limit exceeded"
	}
	writeJSON(w, status, map[string]string{"detail": detail, "code": code})
}
func aiDecode(w http.ResponseWriter, r *http.Request, out any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		writeDetail(w, 400, "invalid request body")
		return false
	}
	if err := d.Decode(new(any)); err != io.EOF {
		writeDetail(w, 400, "invalid request body")
		return false
	}
	return true
}

// Merge only supplied top-level fields. Strict decoding rejects fields absent
// from the domain input; PATCH cannot bypass the independent credential permission.
func aiMergePatch(w http.ResponseWriter, r *http.Request, current, out any, connection bool) bool {
	var patch map[string]json.RawMessage
	if !aiDecode(w, r, &patch) {
		return false
	}
	if patch == nil {
		writeDetail(w, 400, "invalid request body")
		return false
	}
	if _, ok := patch["credential"]; connection && ok {
		writeDetail(w, 400, "use separate credential endpoint")
		return false
	}
	encoded, err := json.Marshal(current)
	if err != nil {
		writeAIAdminError(w, aimodel.ErrUnavailable)
		return false
	}
	var merged map[string]json.RawMessage
	if json.Unmarshal(encoded, &merged) != nil {
		writeAIAdminError(w, aimodel.ErrUnavailable)
		return false
	}
	for key, value := range patch {
		merged[key] = value
	}
	encoded, err = json.Marshal(merged)
	if err != nil {
		writeDetail(w, 400, "invalid request body")
		return false
	}
	d := json.NewDecoder(bytes.NewReader(encoded))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil {
		writeDetail(w, 400, "invalid request body")
		return false
	}
	return true
}
func (s *Server) ListAIConnections(w http.ResponseWriter, r *http.Request) {
	if s.aiModelCaller(w, r, aimodel.PermissionRead) == nil {
		return
	}
	rows, e := s.AIModels.ListConnections(r.Context())
	if e != nil {
		writeAIAdminError(w, e)
		return
	}
	writeJSON(w, 200, rows)
}
func (s *Server) CreateAIConnection(w http.ResponseWriter, r *http.Request) {
	caller := s.aiModelCaller(w, r, aimodel.PermissionWrite)
	if caller == nil {
		return
	}
	ctx := aimodel.WithActor(r.Context(), caller.ID)
	var in aimodel.ConnectionInput
	if !aiDecode(w, r, &in) {
		return
	}
	if in.Credential != nil && s.aiModelCaller(w, r, aimodel.PermissionSecretWrite) == nil {
		return
	}
	out, e := s.AIModels.CreateConnection(ctx, in)
	if e != nil {
		writeAIAdminError(w, e)
		return
	}
	writeJSON(w, 201, out)
}
func (s *Server) UpdateAIConnection(w http.ResponseWriter, r *http.Request, id string) {
	caller := s.aiModelCaller(w, r, aimodel.PermissionWrite)
	if caller == nil {
		return
	}
	ctx := aimodel.WithActor(r.Context(), caller.ID)
	old, e := s.AIModels.GetConnection(ctx, id)
	if e != nil {
		writeAIAdminError(w, e)
		return
	}
	current := aimodel.ConnectionInput{Name: old.Name, Adapter: old.Adapter, BaseURL: old.BaseURL, Enabled: old.Enabled, TimeoutSeconds: old.TimeoutSeconds, MaxConcurrency: old.MaxConcurrency}
	var in aimodel.ConnectionInput
	if !aiMergePatch(w, r, current, &in, true) {
		return
	}
	in.ExpectedVersion = old.Version
	out, e := s.AIModels.UpdateConnection(ctx, id, in)
	if e != nil {
		writeAIAdminError(w, e)
		return
	}
	writeJSON(w, 200, out)
}
func (s *Server) DeleteAIConnection(w http.ResponseWriter, r *http.Request, id string) {
	caller := s.aiModelCaller(w, r, aimodel.PermissionWrite)
	if caller == nil {
		return
	}
	ctx := aimodel.WithActor(r.Context(), caller.ID)
	if e := s.AIModels.DeleteConnection(ctx, id); e != nil {
		writeAIAdminError(w, e)
		return
	}
	w.WriteHeader(204)
}
func (s *Server) SetAIConnectionCredential(w http.ResponseWriter, r *http.Request, id string) {
	caller := s.aiModelCaller(w, r, aimodel.PermissionSecretWrite)
	if caller == nil {
		return
	}
	ctx := aimodel.WithActor(r.Context(), caller.ID)
	var in struct {
		Credential *string `json:"credential"`
	}
	if !aiDecode(w, r, &in) {
		return
	}
	if in.Credential == nil {
		writeDetail(w, 400, "credential is required")
		return
	}
	if e := s.AIModels.SetConnectionCredential(ctx, id, *in.Credential); e != nil {
		writeAIAdminError(w, e)
		return
	}
	w.WriteHeader(204)
}
func (s *Server) ListAIModels(w http.ResponseWriter, r *http.Request) {
	if s.aiModelCaller(w, r, aimodel.PermissionRead) == nil {
		return
	}
	rows, e := s.AIModels.ListModels(r.Context())
	if e != nil {
		writeAIAdminError(w, e)
		return
	}
	writeJSON(w, 200, rows)
}
func (s *Server) CreateAIModel(w http.ResponseWriter, r *http.Request) {
	caller := s.aiModelCaller(w, r, aimodel.PermissionWrite)
	if caller == nil {
		return
	}
	ctx := aimodel.WithActor(r.Context(), caller.ID)
	var in aimodel.ModelInput
	if !aiDecode(w, r, &in) {
		return
	}
	out, e := s.AIModels.CreateModel(ctx, in)
	if e != nil {
		writeAIAdminError(w, e)
		return
	}
	writeJSON(w, 201, out)
}
func (s *Server) UpdateAIModel(w http.ResponseWriter, r *http.Request, id string) {
	caller := s.aiModelCaller(w, r, aimodel.PermissionWrite)
	if caller == nil {
		return
	}
	ctx := aimodel.WithActor(r.Context(), caller.ID)
	old, e := s.AIModels.GetModel(ctx, id)
	if e != nil {
		writeAIAdminError(w, e)
		return
	}
	var in aimodel.ModelInput
	if !aiMergePatch(w, r, old.ModelInput, &in, false) {
		return
	}
	in.ExpectedVersion = old.Version
	out, e := s.AIModels.UpdateModel(ctx, id, in)
	if e != nil {
		writeAIAdminError(w, e)
		return
	}
	writeJSON(w, 200, out)
}
func (s *Server) DeleteAIModel(w http.ResponseWriter, r *http.Request, id string) {
	caller := s.aiModelCaller(w, r, aimodel.PermissionWrite)
	if caller == nil {
		return
	}
	ctx := aimodel.WithActor(r.Context(), caller.ID)
	if e := s.AIModels.DeleteModel(ctx, id); e != nil {
		writeAIAdminError(w, e)
		return
	}
	w.WriteHeader(204)
}
func (s *Server) ListAITestSessions(w http.ResponseWriter, r *http.Request, params genapi.ListAITestSessionsParams) {
	user := s.aiModelCaller(w, r, aimodel.PermissionTest)
	if user == nil {
		return
	}
	modelID := ""
	if params.ModelId != nil {
		modelID = *params.ModelId
	}
	rows, e := s.AIModels.ListSessions(r.Context(), user.ID, modelID)
	if e != nil {
		writeAIAdminError(w, e)
		return
	}
	writeJSON(w, 200, rows)
}
func (s *Server) CreateAITestSession(w http.ResponseWriter, r *http.Request) {
	user := s.aiModelCaller(w, r, aimodel.PermissionTest)
	if user == nil {
		return
	}
	var in struct {
		ModelID string `json:"model_id"`
		Title   string `json:"title"`
	}
	if !aiDecode(w, r, &in) {
		return
	}
	out, e := s.AIModels.CreateSession(r.Context(), user.ID, in.ModelID, in.Title)
	if e != nil {
		writeAIAdminError(w, e)
		return
	}
	writeJSON(w, 201, out)
}
func (s *Server) GetAITestSession(w http.ResponseWriter, r *http.Request, id string) {
	user := s.aiModelCaller(w, r, aimodel.PermissionTest)
	if user == nil {
		return
	}
	out, e := s.AIModels.GetSession(r.Context(), user.ID, id)
	if e != nil {
		writeAIAdminError(w, e)
		return
	}
	writeJSON(w, 200, out)
}
func (s *Server) DeleteAITestSession(w http.ResponseWriter, r *http.Request, id string) {
	user := s.aiModelCaller(w, r, aimodel.PermissionTest)
	if user == nil {
		return
	}
	if e := s.AIModels.DeleteSession(r.Context(), user.ID, id); e != nil {
		writeAIAdminError(w, e)
		return
	}
	w.WriteHeader(204)
}
