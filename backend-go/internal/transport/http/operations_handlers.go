package http

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/go-chi/chi/v5"
	"github.com/shilin414/cas/backend-go/internal/adminrbac"
	"github.com/shilin414/cas/backend-go/internal/operations"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

type operationsBackend interface {
	Overview(context.Context) (*operations.Overview, error)
	ListRuns(context.Context, operations.RunFilter) (*operations.RunPage, error)
	UpdateCapacity(context.Context, int64, string, operations.CapacityInput) (*operations.CapacityResult, error)
}

func (s *Server) registerOperationsRoutes(r chi.Router) {
	r.Get("/api/v2/admin/operations/overview", s.operationsOverview)
	r.Get("/api/v2/admin/operations/runs", s.operationsRuns)
	r.Patch("/api/v2/admin/operations/providers/{provider}/capacity", s.operationsCapacity)
}
func (s *Server) authorizeOperations(w http.ResponseWriter, r *http.Request, permission string) *AuthenticatedUser {
	w.Header().Set("Cache-Control", "no-store")
	caller := userFrom(r.Context())
	if caller == nil || caller.User == nil {
		writeDetail(w, 401, "请先登录。")
		return nil
	}
	allowed := caller.IsStaff
	if s.AdminRBAC != nil {
		var err error
		allowed, err = s.AdminRBAC.HasPermission(r.Context(), caller.ID, caller.IsStaff, permission)
		if err != nil {
			s.writeOperationsError(w, r, err)
			return nil
		}
	}
	if !allowed {
		writeDetail(w, 403, "没有执行此操作所需的企业管理权限。")
		return nil
	}
	if s.Operations == nil {
		writeDetail(w, http.StatusServiceUnavailable, "运行管理暂不可用。")
		return nil
	}
	return caller
}
func (s *Server) operationsOverview(w http.ResponseWriter, r *http.Request) {
	if s.authorizeOperations(w, r, adminrbac.PermissionRunMonitorRead) == nil {
		return
	}
	if r.URL.RawQuery != "" {
		writeDetail(w, 400, "此接口不接受查询参数。")
		return
	}
	out, err := s.Operations.Overview(r.Context())
	if err != nil {
		s.writeOperationsError(w, r, err)
		return
	}
	writeJSON(w, 200, out)
}
func (s *Server) operationsRuns(w http.ResponseWriter, r *http.Request) {
	if s.authorizeOperations(w, r, adminrbac.PermissionRunMonitorRead) == nil {
		return
	}
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		writeDetail(w, 400, "查询参数格式无效。")
		return
	}
	allowed := map[string]bool{"status": true, "provider": true, "owner_user_id": true, "application_id": true, "limit": true, "cursor": true}
	for key, vs := range values {
		if !allowed[key] || len(vs) != 1 {
			writeDetail(w, 400, "查询参数未知或重复。")
			return
		}
	}
	f := operations.RunFilter{Status: values.Get("status"), Provider: values.Get("provider"), OwnerUserID: values.Get("owner_user_id"), ApplicationID: values.Get("application_id"), Cursor: values.Get("cursor"), Limit: 50}
	if raw, ok := values["limit"]; ok {
		n, err := strconv.Atoi(raw[0])
		if err != nil || n < 1 || n > 100 {
			writeDetail(w, 400, "limit 必须是 1 到 100 的整数。")
			return
		}
		f.Limit = n
	}
	out, err := s.Operations.ListRuns(r.Context(), f)
	if err != nil {
		s.writeOperationsError(w, r, err)
		return
	}
	writeJSON(w, 200, out)
}
func (s *Server) operationsCapacity(w http.ResponseWriter, r *http.Request) {
	caller := s.authorizeOperations(w, r, adminrbac.PermissionProviderManage)
	if caller == nil {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var body *struct {
		MaxInflight         *int64  `json:"max_inflight"`
		ExpectedMaxInflight *int64  `json:"expected_max_inflight"`
		Reason              *string `json:"reason"`
	}
	if err := dec.Decode(&body); err != nil {
		var large *http.MaxBytesError
		if errors.As(err, &large) {
			writeDetail(w, 413, "请求内容过大。")
		} else {
			writeDetail(w, 400, "请求 JSON 格式无效。")
		}
		return
	}
	if body == nil || body.MaxInflight == nil || body.ExpectedMaxInflight == nil || body.Reason == nil || dec.Decode(new(any)) != io.EOF {
		writeDetail(w, 400, "必须提供额度、原额度和变更原因，且只能提交一个 JSON 对象。")
		return
	}
	in := operations.CapacityInput{MaxInflight: *body.MaxInflight, ExpectedMaxInflight: *body.ExpectedMaxInflight, Reason: *body.Reason}
	if err := in.Validate(); err != nil {
		s.writeOperationsError(w, r, err)
		return
	}
	out, err := s.Operations.UpdateCapacity(r.Context(), caller.ID, chi.URLParam(r, "provider"), in)
	if err != nil {
		s.writeOperationsError(w, r, err)
		return
	}
	writeJSON(w, 200, out)
}
func (s *Server) writeOperationsError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, operations.ErrInvalid):
		writeDetail(w, 400, err.Error())
	case errors.Is(err, operations.ErrNotFound):
		writeDetail(w, 404, "Provider 不存在。")
	case errors.Is(err, operations.ErrConflict):
		writeDetail(w, 409, "并发额度已发生变化，请刷新后重新确认。")
	default:
		if s.Log != nil {
			s.Log.Warn("operations request failed", "path", normalizeRoute(r), "err", err)
		}
		w.Header().Set("Retry-After", "2")
		writeDetail(w, 503, "运维数据暂不可用，请稍后重试；未确认成功的额度变更请先刷新核对。")
	}
}
