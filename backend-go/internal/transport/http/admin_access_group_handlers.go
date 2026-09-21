package http

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/shilin414/cas/backend-go/internal/accessgroup"
	"github.com/shilin414/cas/backend-go/internal/adminrbac"
	"github.com/shilin414/cas/backend-go/internal/enterpriseaccess"
	genapi "github.com/shilin414/cas/backend-go/internal/gen/api"
)

func groupInput(body genapi.AccessGroupInput) accessgroup.Input {
	code := ""
	if body.Code != nil {
		code = *body.Code
	}
	in := accessgroup.Input{Code: code, Name: body.Name, Description: body.Description, Enabled: body.Enabled, UserGrants: body.UserGrants}
	for _, g := range body.DepartmentGrants {
		in.DepartmentGrants = append(in.DepartmentGrants, accessgroup.DepartmentGrant{DepartmentID: g.DepartmentId, IncludeChildren: g.IncludeChildren})
	}
	return in
}
func (s *Server) ListAdminAccessGroups(w http.ResponseWriter, r *http.Request, params genapi.ListAdminAccessGroupsParams) {
	if s.requireAdminPermission(w, r, adminrbac.PermissionAccessGroupRead) == nil {
		return
	}
	source := ""
	if params.SourceType != nil {
		source = string(*params.SourceType)
	}
	rows, err := s.AccessGroups.List(r.Context(), source)
	if errors.Is(err, accessgroup.ErrInvalid) {
		writeDetail(w, 400, err.Error())
		return
	}
	if err != nil {
		writeSimpleError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, rows)
}
func (s *Server) CreateAdminAccessGroup(w http.ResponseWriter, r *http.Request) {
	caller := s.requireAdminPermission(w, r, adminrbac.PermissionAccessGroupManage)
	if caller == nil {
		return
	}
	var body genapi.AccessGroupInput
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeDetail(w, 400, "invalid request body")
		return
	}
	g, err := s.AccessGroups.Create(r.Context(), caller.ID, groupInput(body))
	if errors.Is(err, accessgroup.ErrInvalid) {
		writeDetail(w, 400, err.Error())
		return
	}
	if err != nil {
		writeSimpleError(w, 500, err.Error())
		return
	}
	writeJSON(w, 201, g)
}
func (s *Server) GetAdminAccessGroup(w http.ResponseWriter, r *http.Request, id int64) {
	if s.requireAdminPermission(w, r, adminrbac.PermissionAccessGroupRead) == nil {
		return
	}
	g, err := s.AccessGroups.Get(r.Context(), id)
	if errors.Is(err, accessgroup.ErrNotFound) {
		writeDetail(w, 404, "group not found")
		return
	}
	if err != nil {
		writeSimpleError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, g)
}
func (s *Server) UpdateAdminAccessGroup(w http.ResponseWriter, r *http.Request, id int64) {
	caller := s.requireAdminPermission(w, r, adminrbac.PermissionAccessGroupManage)
	if caller == nil {
		return
	}
	var body genapi.AccessGroupInput
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeDetail(w, 400, "invalid request body")
		return
	}
	g, err := s.AccessGroups.Update(r.Context(), caller.ID, id, groupInput(body))
	if errors.Is(err, accessgroup.ErrNotFound) {
		writeDetail(w, 404, "group not found")
		return
	}
	if errors.Is(err, accessgroup.ErrInvalid) || errors.Is(err, accessgroup.ErrReadOnly) {
		writeDetail(w, 400, err.Error())
		return
	}
	if err != nil {
		writeSimpleError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, g)
}
func (s *Server) DeleteAdminAccessGroup(w http.ResponseWriter, r *http.Request, id int64) {
	caller := s.requireAdminPermission(w, r, adminrbac.PermissionAccessGroupManage)
	if caller == nil {
		return
	}
	err := s.AccessGroups.Delete(r.Context(), caller.ID, id)
	if errors.Is(err, accessgroup.ErrNotFound) {
		writeDetail(w, 404, "group not found")
		return
	}
	if errors.Is(err, accessgroup.ErrInUse) || errors.Is(err, accessgroup.ErrReadOnly) {
		writeDetail(w, 409, err.Error())
		return
	}
	if err != nil {
		writeSimpleError(w, 500, err.Error())
		return
	}
	w.WriteHeader(204)
}
func (s *Server) ListAdminAccessGroupMembers(w http.ResponseWriter, r *http.Request, id int64) {
	if s.requireAdminPermission(w, r, adminrbac.PermissionAccessGroupRead) == nil {
		return
	}
	rows, err := s.AccessGroups.Members(r.Context(), id)
	if errors.Is(err, accessgroup.ErrNotFound) {
		writeDetail(w, 404, "group not found")
		return
	}
	if err != nil {
		writeSimpleError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, rows)
}
func (s *Server) ListAdminAccessGroupApplications(w http.ResponseWriter, r *http.Request, id int64) {
	if s.requireAdminPermission(w, r, adminrbac.PermissionAccessGroupRead) == nil {
		return
	}
	rows, err := s.AccessGroups.Applications(r.Context(), id)
	if err != nil {
		writeSimpleError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, rows)
}
func (s *Server) DiagnoseAdminAccess(w http.ResponseWriter, r *http.Request) {
	if s.requireAdminPermission(w, r, adminrbac.PermissionAccessDiagnosisRead) == nil {
		return
	}
	var body genapi.AccessDiagnosisInput
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeDetail(w, 400, "invalid request body")
		return
	}
	decision, err := s.EnterpriseAccess.ResolveDirectoryUser(r.Context(), body.ApplicationId, body.DirectoryUserId)
	if errors.Is(err, enterpriseaccess.ErrNotFound) {
		writeDetail(w, 404, "application not found")
		return
	}
	if err != nil {
		writeSimpleError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, decision)
}
