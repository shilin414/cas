package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/shilin414/cas/backend-go/internal/businessapps"
	"github.com/shilin414/cas/backend-go/internal/catalog"
	"github.com/shilin414/cas/backend-go/internal/execution"
	"github.com/go-chi/chi/v5"
)

func (s *Server) registerBusinessAppRoutes(r chi.Router) {
	if s.BusinessApps == nil {
		s.BusinessApps = businessapps.New(businessapps.FromEnv())
	}
	if s.BusinessSnapshots == nil && s.Redis != nil && s.Redis.UniversalClient != nil {
		s.BusinessSnapshots = &businessapps.RedisSnapshotStore{Client: s.Redis}
	}
	s.BusinessForwardLimiter = execution.NewRateLimiter(s.Redis, "business-forward", 10, time.Minute)
	r.Get("/api/v2/applications/{id}/business/snapshots/{token}", s.getBusinessQuerySnapshot)
	r.Post("/api/v2/applications/{id}/business/forward", s.forwardBusinessQuery)
	s.BusinessAppLimiter = execution.NewRateLimiter(s.Redis, "business-apps", 5, time.Minute)
	r.Get("/api/v2/applications/{id}/business/status", s.businessAppStatus)
	r.Post("/api/v2/applications/{id}/business/execute", s.businessAppExecute)
}

// Both endpoints resolve an application ID, not an arbitrary upstream URL or renderer.
// Even staff cannot execute disabled apps; catalog ACL and account authority are separate.
func (s *Server) businessAppAccess(w http.ResponseWriter, r *http.Request) (*catalog.Application, *AuthenticatedUser) {
	w.Header().Set("Cache-Control", "no-store")
	caller := userFrom(r.Context())
	if caller == nil {
		writeDetail(w, 401, "请先登录")
		return nil, nil
	}
	id, e := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if e != nil || id <= 0 {
		writeDetail(w, 404, "应用不可用")
		return nil, nil
	}
	bundle, e := s.CatalogRepo.ConsumptionBundleByID(r.Context(), id)
	if e != nil {
		if errors.Is(e, catalog.ErrNotFound) {
			writeDetail(w, 404, "应用不可用")
		} else {
			writeDetail(w, 503, "应用信息暂不可用")
		}
		return nil, nil
	}
	app := bundle.Application
	if !app.Enabled || !businessapps.Known(app.RendererKey) || (app.Kind != "page" && app.Kind != "form") {
		writeDetail(w, 404, "应用不可用")
		return nil, nil
	}
	allowed, e := s.CatalogRepo.AccessAllowed(r.Context(), app.ID, caller.ID, caller.IsStaff)
	if e != nil {
		writeDetail(w, 503, "权限校验暂不可用")
		return nil, nil
	}
	if !allowed {
		writeDetail(w, 404, "应用不可用")
		return nil, nil
	}
	return app, caller
}
// callerFeishuUserID returns the OAuth-verified Feishu user_id (the employee
// account number); empty for local-admin sessions without a Feishu identity.
func callerFeishuUserID(caller *AuthenticatedUser) string {
	if caller == nil || caller.Identity == nil {
		return ""
	}
	return caller.Identity.FeishuUserID
}

func (s *Server) businessAppStatus(w http.ResponseWriter, r *http.Request) {
	app, caller := s.businessAppAccess(w, r)
	if app == nil {
		return
	}
	writeJSON(w, 200, s.BusinessApps.Status(app.RendererKey, caller.ID, callerFeishuUserID(caller)))
}
func (s *Server) businessAppExecute(w http.ResponseWriter, r *http.Request) {
	app, caller := s.businessAppAccess(w, r)
	if app == nil {
		return
	}
	var input businessapps.Input
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	dec.DisallowUnknownFields()
	if e := dec.Decode(&input); e != nil {
		writeDetail(w, 400, "请求格式不正确")
		return
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		writeDetail(w, 400, "请求格式不正确")
		return
	}
	if e := businessapps.Validate(app.RendererKey, input); e != nil {
		writeDetail(w, 400, e.Error())
		return
	}
	mutation := !businessapps.IsQuery(app.RendererKey)
	if mutation {
		target, e := s.BusinessApps.Target(caller.ID, callerFeishuUserID(caller), input.UserCode)
		if e != nil {
			writeDetail(w, 403, e.Error())
			return
		}
		input.UserCode = target
	}
	if !s.BusinessApps.Configured(app.RendererKey) {
		writeDetail(w, 503, businessapps.ErrConfig.Error())
		return
	}
	ok, _, e := s.BusinessAppLimiter.AllowKey(r.Context(), "business-apps:user:"+strconv.FormatInt(caller.ID, 10))
	if e != nil || !ok {
		w.Header().Set("Retry-After", "60")
		writeDetail(w, 429, "操作过于频繁，请一分钟后再试")
		return
	}
	if mutation {
		// Shared account budget prevents different operators flooding the same employee.
		ok, _, e = s.BusinessAppLimiter.AllowKey(r.Context(), "business-apps:account:"+input.UserCode)
		if e != nil || !ok {
			w.Header().Set("Retry-After", "60")
			writeDetail(w, 429, "该账号操作过于频繁，请稍后核实状态")
			return
		}
		// Refuse to make a sensitive change when its intent cannot be durably audited.
		if e = s.auditBusinessApp(r.Context(), caller.ID, app.ID, input, "requested"); e != nil {
			writeDetail(w, 503, "审计服务不可用，未提交账号操作")
			return
		}
	}
	result, e := s.BusinessApps.Execute(r.Context(), app.RendererKey, input)
	if mutation {
		outcome := "succeeded"
		if e != nil {
			outcome = "unconfirmed"
		}
		auditCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 3*time.Second)
		if auditErr := s.auditBusinessApp(auditCtx, caller.ID, app.ID, input, outcome); auditErr != nil {
			s.Log.Error("business app outcome audit failed", "application_id", app.ID, "user_id", caller.ID)
		}
		cancel()
	}
	if e != nil {
		writeDetail(w, 502, e.Error())
		return
	}
	if !mutation {
		s.attachBusinessQuerySnapshot(r, app, caller, input, &result)
	}
	writeJSON(w, 200, result)
}
func (s *Server) auditBusinessApp(ctx context.Context, userID, appID int64, input businessapps.Input, outcome string) error {
	// No password, phone, upstream token, URL or raw response belongs in audit detail.
	detail, _ := json.Marshal(map[string]string{"action": input.Action, "target_account": input.UserCode, "outcome": outcome})
	_, e := s.DB.ExecContext(ctx, `INSERT INTO audit_logs (user_id,action,resource,resource_id,detail) VALUES (?, 'business_app.execute', 'application', ?, ?)`, userID, strconv.FormatInt(appID, 10), string(detail))
	return e
}
