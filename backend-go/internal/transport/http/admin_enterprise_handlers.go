package http

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/shilin414/cas/backend-go/internal/adminrbac"
	"github.com/shilin414/cas/backend-go/internal/directory"
	"github.com/shilin414/cas/backend-go/internal/enterpriseaccess"
	genapi "github.com/shilin414/cas/backend-go/internal/gen/api"
)

func requireStaff(w http.ResponseWriter, r *http.Request) *AuthenticatedUser {
	caller := userFrom(r.Context())
	if caller == nil {
		writeDetail(w, http.StatusUnauthorized, "Authentication credentials were not provided.")
		return nil
	}
	if !caller.IsStaff {
		writeDetail(w, http.StatusForbidden, "只有管理员可以访问企业控制台。")
		return nil
	}
	return caller
}

func (s *Server) ListAdminDirectoryDepartments(w http.ResponseWriter, r *http.Request, params genapi.ListAdminDirectoryDepartmentsParams) {
	if s.requireAdminPermission(w, r, adminrbac.PermissionDirectoryRead) == nil {
		return
	}
	q := ""
	if params.Q != nil {
		q = *params.Q
	}
	inactive := params.IncludeInactive != nil && *params.IncludeInactive
	rows, err := s.DirectoryRepo.ListDepartments(r.Context(), q, params.ParentId, inactive)
	if err != nil {
		writeSimpleError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, rows)
}
func (s *Server) ListAdminDirectoryDepartmentPage(w http.ResponseWriter, r *http.Request, params genapi.ListAdminDirectoryDepartmentPageParams) {
	if s.requireAdminPermission(w, r, adminrbac.PermissionDirectoryRead) == nil {
		return
	}
	q, cursor := "", ""
	limit := 50
	if params.Q != nil {
		q = *params.Q
	}
	if params.Cursor != nil {
		cursor = *params.Cursor
	}
	if params.Limit != nil {
		limit = *params.Limit
	}
	if limit < 1 || limit > 100 {
		writeDetail(w, http.StatusBadRequest, "limit must be between 1 and 100")
		return
	}
	inactive := params.IncludeInactive != nil && *params.IncludeInactive
	rows, next, err := s.DirectoryRepo.ListDepartmentPage(r.Context(), q, inactive, cursor, limit)
	if err != nil {
		if cursor != "" {
			writeDetail(w, http.StatusBadRequest, "invalid cursor")
			return
		}
		writeSimpleError(w, 500, err.Error())
		return
	}
	var nextPtr *string
	if next != "" {
		nextPtr = &next
	}
	writeJSON(w, 200, map[string]any{"results": rows, "next_cursor": nextPtr})
}

func (s *Server) GetAdminDirectoryStats(w http.ResponseWriter, r *http.Request) {
	if s.requireAdminPermission(w, r, adminrbac.PermissionDirectoryRead) == nil {
		return
	}
	stats, err := s.DirectoryRepo.Stats(r.Context())
	if err != nil {
		writeSimpleError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, stats)
}

func (s *Server) ListAdminDirectoryUsers(w http.ResponseWriter, r *http.Request, params genapi.ListAdminDirectoryUsersParams) {
	if s.requireAdminPermission(w, r, adminrbac.PermissionDirectoryRead) == nil {
		return
	}
	q, cursor := "", ""
	limit := 50
	if params.Q != nil {
		q = *params.Q
	}
	if params.Cursor != nil {
		cursor = *params.Cursor
	}
	if params.Limit != nil {
		limit = *params.Limit
	}
	inactive := params.IncludeInactive != nil && *params.IncludeInactive
	rows, next, err := s.DirectoryRepo.ListUsers(r.Context(), q, params.DepartmentId, inactive, cursor, limit)
	if err != nil {
		writeSimpleError(w, 500, err.Error())
		return
	}
	var nextPtr *string
	if next != "" {
		nextPtr = &next
	}
	writeJSON(w, 200, map[string]any{"results": rows, "next_cursor": nextPtr})
}
func (s *Server) GetAdminDirectorySyncConfig(w http.ResponseWriter, r *http.Request) {
	if s.requireAdminPermission(w, r, adminrbac.PermissionDirectorySyncRead) == nil {
		return
	}
	targetCfg, err := s.DirectoryRepo.GetTargetConfig(r.Context(), directory.TargetDirectory)
	if err != nil {
		writeSimpleError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, directory.SyncConfig{Enabled: targetCfg.Enabled, ScheduleType: targetCfg.ScheduleType, IntervalMinutes: targetCfg.IntervalMinutes, DailyTime: targetCfg.DailyTime, Timezone: targetCfg.Timezone, NextRunAt: targetCfg.NextRunAt, LastRunAt: targetCfg.LastRunAt, LastSuccessAt: targetCfg.LastSuccessAt, DirectoryVersion: targetCfg.TargetVersion, UpdatedAt: targetCfg.UpdatedAt})
}
func (s *Server) UpdateAdminDirectorySyncConfig(w http.ResponseWriter, r *http.Request) {
	caller := s.requireAdminPermission(w, r, adminrbac.PermissionDirectorySyncManage)
	if caller == nil {
		return
	}
	body, err := decodeTargetConfigInput(w, r)
	if err != nil {
		writeDetail(w, http.StatusBadRequest, err.Error())
		return
	}
	cfg := directory.SyncConfig{Enabled: body.Enabled, ScheduleType: body.ScheduleType, IntervalMinutes: body.IntervalMinutes, DailyTime: body.DailyTime, Timezone: body.Timezone}
	if err := directory.ValidateSyncConfig(cfg); err != nil {
		writeDetail(w, 400, err.Error())
		return
	}
	next, err := directory.NextRunAt(cfg, time.Now().UTC())
	if err != nil {
		writeDetail(w, 400, err.Error())
		return
	}
	if err = s.DirectoryRepo.UpdateTargetConfig(r.Context(), directory.TargetDirectory, directory.TargetConfig{TargetCode: directory.TargetDirectory, Enabled: cfg.Enabled, ScheduleType: cfg.ScheduleType, IntervalMinutes: cfg.IntervalMinutes, DailyTime: cfg.DailyTime, Timezone: cfg.Timezone}, caller.ID, next); err != nil {
		writeSimpleError(w, 500, err.Error())
		return
	}
	after, err := s.DirectoryRepo.GetTargetConfig(r.Context(), directory.TargetDirectory)
	if err != nil {
		writeSimpleError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, directory.SyncConfig{Enabled: after.Enabled, ScheduleType: after.ScheduleType, IntervalMinutes: after.IntervalMinutes, DailyTime: after.DailyTime, Timezone: after.Timezone, NextRunAt: after.NextRunAt, LastRunAt: after.LastRunAt, LastSuccessAt: after.LastSuccessAt, DirectoryVersion: after.TargetVersion, UpdatedAt: after.UpdatedAt})
}
func (s *Server) TriggerAdminDirectorySync(w http.ResponseWriter, r *http.Request) {
	caller := s.requireAdminPermission(w, r, adminrbac.PermissionDirectorySyncManage)
	if caller == nil {
		return
	}
	run, err := s.DirectoryRepo.CreateTargetRun(r.Context(), directory.TargetDirectory, "manual", &caller.ID, nil, "pending")
	if errors.Is(err, directory.ErrActiveJobExists) {
		writeDetail(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		writeSimpleError(w, 500, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, run)
}
func (s *Server) ListAdminDirectorySyncRuns(w http.ResponseWriter, r *http.Request, params genapi.ListAdminDirectorySyncRunsParams) {
	if s.requireAdminPermission(w, r, adminrbac.PermissionDirectorySyncRead) == nil {
		return
	}
	limit := 50
	if params.Limit != nil {
		limit = *params.Limit
	}
	rows, err := s.DirectoryRepo.ListLegacySyncRuns(r.Context(), limit)
	if err != nil {
		writeSimpleError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, rows)
}
func (s *Server) GetAdminDirectorySyncRun(w http.ResponseWriter, r *http.Request, id int64) {
	if s.requireAdminPermission(w, r, adminrbac.PermissionDirectorySyncRead) == nil {
		return
	}
	run, err := s.DirectoryRepo.GetSyncRun(r.Context(), id)
	if errors.Is(err, directory.ErrNotFound) {
		writeDetail(w, 404, "sync run not found")
		return
	}
	if err != nil {
		writeSimpleError(w, 500, err.Error())
		return
	}
	if run.TargetCode != directory.TargetDirectory && run.TargetCode != "legacy_full" {
		writeDetail(w, 404, "sync run not found")
		return
	}
	writeJSON(w, 200, run)
}

func (s *Server) GetAdminApplicationAccess(w http.ResponseWriter, r *http.Request, id int64) {
	if s.requireAdminPermission(w, r, adminrbac.PermissionAccessPolicyRead) == nil {
		return
	}
	policy, err := s.EnterpriseAccess.Get(r.Context(), id)
	if errors.Is(err, enterpriseaccess.ErrNotFound) {
		writeDetail(w, 404, "application not found")
		return
	}
	if err != nil {
		writeSimpleError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, policy)
}
func (s *Server) UpdateAdminApplicationAccess(w http.ResponseWriter, r *http.Request, id int64) {
	caller := s.requireAdminPermission(w, r, adminrbac.PermissionAccessPolicyManage)
	if caller == nil {
		return
	}
	var body genapi.ApplicationAccessUpdate
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeDetail(w, 400, "invalid request body")
		return
	}
	in := enterpriseaccess.Update{AccessMode: string(body.AccessMode), UserGrants: body.UserGrants, GroupGrants: body.GroupGrants}
	for _, g := range body.DepartmentGrants {
		in.DepartmentGrants = append(in.DepartmentGrants, enterpriseaccess.DepartmentGrant{DepartmentID: g.DepartmentId, IncludeChildren: g.IncludeChildren})
	}
	policy, err := s.EnterpriseAccess.Replace(r.Context(), id, caller.ID, in)
	if errors.Is(err, enterpriseaccess.ErrNotFound) {
		writeDetail(w, 404, "application not found")
		return
	}
	if errors.Is(err, enterpriseaccess.ErrInvalidGrant) {
		writeDetail(w, 400, err.Error())
		return
	}
	if err != nil {
		writeSimpleError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, policy)
}

func (s *Server) ListAdminAuditLogs(w http.ResponseWriter, r *http.Request, params genapi.ListAdminAuditLogsParams) {
	if s.requireAdminPermission(w, r, adminrbac.PermissionAuditRead) == nil {
		return
	}
	limit := 100
	if params.Limit != nil {
		limit = *params.Limit
	}
	rows, err := s.DB.QueryContext(r.Context(), `SELECT id,user_id,action,resource,resource_id,detail,created_at FROM audit_logs ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		writeSimpleError(w, 500, err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id int64
		var uid sql.NullInt64
		var action, resource, resourceID string
		var raw []byte
		var created time.Time
		if err = rows.Scan(&id, &uid, &action, &resource, &resourceID, &raw, &created); err != nil {
			writeSimpleError(w, 500, err.Error())
			return
		}
		detail := map[string]any{}
		_ = json.Unmarshal(raw, &detail)
		var user any
		if uid.Valid {
			user = uid.Int64
		}
		out = append(out, map[string]any{"id": id, "user_id": user, "action": action, "resource_type": resource, "resource_id": resourceID, "detail": detail, "created_at": created})
	}
	if err = rows.Err(); err != nil {
		writeSimpleError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, out)
}
