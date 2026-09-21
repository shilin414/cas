package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/shilin414/cas/backend-go/internal/adminrbac"
	genapi "github.com/shilin414/cas/backend-go/internal/gen/api"
)

func (s *Server) requireAdminPermission(w http.ResponseWriter, r *http.Request, permission string) *AuthenticatedUser {
	caller := userFrom(r.Context())
	if caller == nil {
		writeDetail(w, http.StatusUnauthorized, "Authentication credentials were not provided.")
		return nil
	}
	if s.AdminRBAC == nil {
		if caller.IsStaff {
			return caller
		}
		writeDetail(w, http.StatusForbidden, "没有企业管理权限。")
		return nil
	}
	ok, err := s.AdminRBAC.HasPermission(r.Context(), caller.ID, caller.IsStaff, permission)
	if err != nil {
		writeSimpleError(w, 500, err.Error())
		return nil
	}
	if !ok {
		writeDetail(w, http.StatusForbidden, "没有执行此操作所需的企业管理权限。")
		return nil
	}
	return caller
}

func (s *Server) GetAdminMe(w http.ResponseWriter, r *http.Request) {
	caller := userFrom(r.Context())
	if caller == nil {
		writeDetail(w, 401, "Authentication credentials were not provided.")
		return
	}
	me, err := s.AdminRBAC.Me(r.Context(), caller.ID, caller.IsStaff)
	if err != nil {
		writeSimpleError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, me)
}
func (s *Server) ListAdminPermissions(w http.ResponseWriter, r *http.Request) {
	if s.requireAdminPermission(w, r, adminrbac.PermissionAdminRoleRead) == nil {
		return
	}
	rows, err := s.AdminRBAC.ListPermissions(r.Context())
	if err != nil {
		writeSimpleError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, rows)
}
func (s *Server) ListAdminRoles(w http.ResponseWriter, r *http.Request) {
	if s.requireAdminPermission(w, r, adminrbac.PermissionAdminRoleRead) == nil {
		return
	}
	rows, err := s.AdminRBAC.ListRoles(r.Context())
	if err != nil {
		writeSimpleError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, rows)
}
func (s *Server) GetAdminRole(w http.ResponseWriter, r *http.Request, id int64) {
	if s.requireAdminPermission(w, r, adminrbac.PermissionAdminRoleRead) == nil {
		return
	}
	role, err := s.AdminRBAC.GetRole(r.Context(), id)
	if errors.Is(err, adminrbac.ErrNotFound) {
		writeDetail(w, 404, "role not found")
		return
	}
	if err != nil {
		writeSimpleError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, role)
}
func roleInput(body genapi.AdminRoleInput) adminrbac.RoleInput {
	code := ""
	if body.Code != nil {
		code = *body.Code
	}
	return adminrbac.RoleInput{Code: code, Name: body.Name, Description: body.Description, Enabled: body.Enabled, PermissionCodes: body.PermissionCodes}
}
func (s *Server) CreateAdminRole(w http.ResponseWriter, r *http.Request) {
	caller := s.requireAdminPermission(w, r, adminrbac.PermissionAdminRoleManage)
	if caller == nil {
		return
	}
	var body genapi.AdminRoleInput
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeDetail(w, 400, "invalid request body")
		return
	}
	role, err := s.AdminRBAC.CreateRole(r.Context(), caller.ID, roleInput(body))
	if errors.Is(err, adminrbac.ErrInvalid) {
		writeDetail(w, 400, err.Error())
		return
	}
	if err != nil {
		writeSimpleError(w, 500, err.Error())
		return
	}
	writeJSON(w, 201, role)
}
func (s *Server) UpdateAdminRole(w http.ResponseWriter, r *http.Request, id int64) {
	caller := s.requireAdminPermission(w, r, adminrbac.PermissionAdminRoleManage)
	if caller == nil {
		return
	}
	var body genapi.AdminRoleInput
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeDetail(w, 400, "invalid request body")
		return
	}
	role, err := s.AdminRBAC.UpdateRole(r.Context(), caller.ID, id, roleInput(body))
	if errors.Is(err, adminrbac.ErrNotFound) {
		writeDetail(w, 404, "role not found")
		return
	}
	if errors.Is(err, adminrbac.ErrSystemRole) || errors.Is(err, adminrbac.ErrInvalid) {
		writeDetail(w, 400, err.Error())
		return
	}
	if err != nil {
		writeSimpleError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, role)
}
func (s *Server) DeleteAdminRole(w http.ResponseWriter, r *http.Request, id int64) {
	caller := s.requireAdminPermission(w, r, adminrbac.PermissionAdminRoleManage)
	if caller == nil {
		return
	}
	err := s.AdminRBAC.DeleteRole(r.Context(), caller.ID, id)
	if errors.Is(err, adminrbac.ErrNotFound) {
		writeDetail(w, 404, "role not found")
		return
	}
	if errors.Is(err, adminrbac.ErrSystemRole) || errors.Is(err, adminrbac.ErrRoleInUse) {
		writeDetail(w, 409, err.Error())
		return
	}
	if err != nil {
		writeSimpleError(w, 500, err.Error())
		return
	}
	w.WriteHeader(204)
}
func (s *Server) ListAdminAdministrators(w http.ResponseWriter, r *http.Request) {
	if s.requireAdminPermission(w, r, adminrbac.PermissionAdminUserRead) == nil {
		return
	}
	rows, err := s.AdminRBAC.ListAdministrators(r.Context())
	if err != nil {
		writeSimpleError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, rows)
}
func (s *Server) CreateAdminAdministrator(w http.ResponseWriter, r *http.Request) {
	s.upsertAdministrator(w, r, true, 0)
}
func (s *Server) UpdateAdminAdministrator(w http.ResponseWriter, r *http.Request, id int64) {
	s.upsertAdministrator(w, r, false, id)
}
func (s *Server) upsertAdministrator(w http.ResponseWriter, r *http.Request, creating bool, id int64) {
	caller := s.requireAdminPermission(w, r, adminrbac.PermissionAdminUserManage)
	if caller == nil {
		return
	}
	var body genapi.AdministratorInput
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeDetail(w, 400, "invalid request body")
		return
	}
	userID := id
	if creating {
		userID = body.UserId
	} else if body.UserId != 0 && body.UserId != id {
		writeDetail(w, 400, "user_id must match path")
		return
	}
	var exp *time.Time
	if body.ExpiresAt != nil {
		v := *body.ExpiresAt
		exp = &v
	}
	admin, err := s.AdminRBAC.ReplaceAdministratorRoles(r.Context(), caller.ID, userID, body.RoleIds, exp)
	if errors.Is(err, adminrbac.ErrNotFound) {
		writeDetail(w, 404, "user not found")
		return
	}
	if errors.Is(err, adminrbac.ErrInvalid) || errors.Is(err, adminrbac.ErrLastOwner) {
		writeDetail(w, 400, err.Error())
		return
	}
	if err != nil {
		writeSimpleError(w, 500, err.Error())
		return
	}
	status := 200
	if creating {
		status = 201
	}
	writeJSON(w, status, admin)
}
func (s *Server) DeleteAdminAdministrator(w http.ResponseWriter, r *http.Request, id int64) {
	caller := s.requireAdminPermission(w, r, adminrbac.PermissionAdminUserManage)
	if caller == nil {
		return
	}
	_, err := s.AdminRBAC.ReplaceAdministratorRoles(r.Context(), caller.ID, id, nil, nil)
	if errors.Is(err, adminrbac.ErrLastOwner) {
		writeDetail(w, 409, err.Error())
		return
	}
	if errors.Is(err, adminrbac.ErrNotFound) {
		writeDetail(w, 404, "user not found")
		return
	}
	if err != nil {
		writeSimpleError(w, 500, err.Error())
		return
	}
	w.WriteHeader(204)
}
