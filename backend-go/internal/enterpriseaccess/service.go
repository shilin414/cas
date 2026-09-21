package enterpriseaccess

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
)

const (
	ModeAll       = "all"
	ModeAssigned  = "assigned"
	ModeAdminOnly = "admin_only"
)
const (
	ReasonStaffBypass               = "STAFF_BYPASS"
	ReasonRBACBypass                = "RBAC_BYPASS"
	ReasonResourceNotFound          = "RESOURCE_NOT_FOUND"
	ReasonResourceDisabled          = "RESOURCE_DISABLED"
	ReasonAccessAll                 = "ACCESS_ALL"
	ReasonAccessAdminOnly           = "ACCESS_ADMIN_ONLY"
	ReasonDirectUserMatch           = "DIRECT_USER_MATCH"
	ReasonDirectDepartmentMatch     = "DIRECT_DEPARTMENT_MATCH"
	ReasonLocalGroupUserMatch       = "LOCAL_GROUP_USER_MATCH"
	ReasonLocalGroupDepartmentMatch = "LOCAL_GROUP_DEPARTMENT_MATCH"
	ReasonFeishuGroupMemberMatch    = "FEISHU_GROUP_MEMBER_MATCH"
	ReasonDirectoryNotLinked        = "DIRECTORY_NOT_LINKED"
	ReasonDirectoryUserInactive     = "DIRECTORY_USER_INACTIVE"
	ReasonDirectoryUserResigned     = "DIRECTORY_USER_RESIGNED"
	ReasonNoAssignmentMatch         = "NO_ASSIGNMENT_MATCH"
)

var ErrNotFound = errors.New("enterprise access: not found")
var ErrInvalidGrant = errors.New("enterprise access: invalid grant")

type DepartmentGrant struct {
	DepartmentID    int64  `json:"department_id"`
	Name            string `json:"name"`
	IncludeChildren bool   `json:"include_children"`
	CoveredUsers    int    `json:"covered_users"`
}
type UserGrant struct {
	DirectoryUserID int64    `json:"directory_user_id"`
	Name            string   `json:"name"`
	AvatarURL       string   `json:"avatar_url"`
	Departments     []string `json:"departments"`
}
type GroupGrant struct {
	GroupID           int64  `json:"group_id"`
	Name              string `json:"name"`
	SourceType        string `json:"source_type"`
	ExternalGroupType string `json:"external_group_type"`
	MemberCount       int    `json:"member_count"`
	Enabled           bool   `json:"enabled"`
}
type Policy struct {
	ApplicationID int64             `json:"application_id"`
	AccessMode    string            `json:"access_mode"`
	Departments   []DepartmentGrant `json:"departments"`
	Users         []UserGrant       `json:"users"`
	Groups        []GroupGrant      `json:"groups"`
}
type Update struct {
	AccessMode       string            `json:"access_mode"`
	DepartmentGrants []DepartmentGrant `json:"department_grants"`
	UserGrants       []int64           `json:"user_grants"`
	GroupGrants      []int64           `json:"group_grants"`
}
type DecisionApplication struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Enabled    bool   `json:"enabled"`
	AccessMode string `json:"access_mode"`
}
type DecisionUser struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Active      bool   `json:"active"`
	Resigned    bool   `json:"resigned"`
	Linked      bool   `json:"linked"`
	LocalUserID *int64 `json:"local_user_id,omitempty"`
}
type MatchedGrant struct {
	Type              string `json:"type"`
	ID                int64  `json:"id"`
	Name              string `json:"name"`
	SourceType        string `json:"source_type,omitempty"`
	ExternalGroupType string `json:"external_group_type,omitempty"`
	IncludeChildren   bool   `json:"include_children,omitempty"`
}
type AccessDecision struct {
	Allowed       bool                 `json:"allowed"`
	ReasonCode    string               `json:"reason_code"`
	Application   *DecisionApplication `json:"application,omitempty"`
	User          *DecisionUser        `json:"user,omitempty"`
	MatchedGrants []MatchedGrant       `json:"matched_grants"`
	AccessPath    []string             `json:"access_path"`
}
type Service struct {
	DB          *sql.DB
	RBACEnabled bool
}

func (s *Service) Get(ctx context.Context, appID int64) (*Policy, error) {
	var mode string
	if err := s.DB.QueryRowContext(ctx, `SELECT access_mode FROM applications WHERE id=?`, appID).Scan(&mode); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	p := &Policy{ApplicationID: appID, AccessMode: mode, Departments: []DepartmentGrant{}, Users: []UserGrant{}, Groups: []GroupGrant{}}
	rows, err := s.DB.QueryContext(ctx, `SELECT d.id,d.name,g.include_children,(SELECT COUNT(DISTINCT dud.directory_user_id) FROM directory_department_closure c JOIN directory_user_departments dud ON dud.department_id=c.descendant_id JOIN directory_users du ON du.id=dud.directory_user_id AND du.is_active=1 AND du.is_resigned=0 WHERE c.ancestor_id=d.id AND (g.include_children=1 OR c.depth=0)) FROM application_department_grants g JOIN directory_departments d ON d.id=g.department_id WHERE g.application_id=? ORDER BY d.name,d.id`, appID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var g DepartmentGrant
		if err = rows.Scan(&g.DepartmentID, &g.Name, &g.IncludeChildren, &g.CoveredUsers); err != nil {
			rows.Close()
			return nil, err
		}
		p.Departments = append(p.Departments, g)
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	rows, err = s.DB.QueryContext(ctx, `SELECT du.id,du.name,du.avatar_url FROM application_user_grants g JOIN directory_users du ON du.id=g.directory_user_id WHERE g.application_id=? ORDER BY du.name,du.id`, appID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var g UserGrant
		if err = rows.Scan(&g.DirectoryUserID, &g.Name, &g.AvatarURL); err != nil {
			rows.Close()
			return nil, err
		}
		g.Departments, err = s.departmentNames(ctx, g.DirectoryUserID)
		if err != nil {
			rows.Close()
			return nil, err
		}
		p.Users = append(p.Users, g)
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	rows, err = s.DB.QueryContext(ctx, `SELECT g.id,g.name,g.source_type,g.external_group_type,g.enabled,CASE WHEN g.source_type='feishu' THEN (SELECT COUNT(*) FROM access_group_external_members em WHERE em.group_id=g.id) ELSE (SELECT COUNT(*) FROM access_group_users gu WHERE gu.group_id=g.id)+(SELECT COUNT(*) FROM access_group_departments gd WHERE gd.group_id=g.id) END FROM application_group_grants ag JOIN access_groups g ON g.id=ag.group_id WHERE ag.application_id=? ORDER BY g.name,g.id`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var g GroupGrant
		if err = rows.Scan(&g.GroupID, &g.Name, &g.SourceType, &g.ExternalGroupType, &g.Enabled, &g.MemberCount); err != nil {
			return nil, err
		}
		p.Groups = append(p.Groups, g)
	}
	return p, rows.Err()
}

func (s *Service) Replace(ctx context.Context, appID, actorID int64, in Update) (*Policy, error) {
	if !validMode(in.AccessMode) {
		return nil, fmt.Errorf("%w: invalid access_mode", ErrInvalidGrant)
	}
	depMap := map[int64]bool{}
	for _, g := range in.DepartmentGrants {
		if g.DepartmentID <= 0 {
			return nil, ErrInvalidGrant
		}
		depMap[g.DepartmentID] = g.IncludeChildren
	}
	userMap := map[int64]struct{}{}
	for _, id := range in.UserGrants {
		if id <= 0 {
			return nil, ErrInvalidGrant
		}
		userMap[id] = struct{}{}
	}
	groupMap := map[int64]struct{}{}
	for _, id := range in.GroupGrants {
		if id <= 0 {
			return nil, ErrInvalidGrant
		}
		groupMap[id] = struct{}{}
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var before string
	if err = tx.QueryRowContext(ctx, `SELECT access_mode FROM applications WHERE id=? FOR UPDATE`, appID).Scan(&before); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	beforeSnapshot, err := accessSnapshotTx(ctx, tx, appID, before)
	if err != nil {
		return nil, err
	}
	for id := range depMap {
		var n int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM directory_departments WHERE id=? AND is_active=1`, id).Scan(&n); err != nil {
			return nil, err
		}
		if n != 1 {
			return nil, fmt.Errorf("%w: department %d inactive", ErrInvalidGrant, id)
		}
	}
	for id := range userMap {
		var n int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM directory_users WHERE id=? AND is_active=1 AND is_resigned=0`, id).Scan(&n); err != nil {
			return nil, err
		}
		if n != 1 {
			return nil, fmt.Errorf("%w: user %d unavailable", ErrInvalidGrant, id)
		}
	}
	groupIDs := make([]int64, 0, len(groupMap))
	for id := range groupMap {
		groupIDs = append(groupIDs, id)
	}
	sort.Slice(groupIDs, func(i, j int) bool { return groupIDs[i] < groupIDs[j] })
	for _, id := range groupIDs {
		var lockedID int64
		if err = tx.QueryRowContext(ctx, `SELECT id FROM access_groups WHERE id=? AND enabled=1 FOR UPDATE`, id).Scan(&lockedID); errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: group %d unavailable", ErrInvalidGrant, id)
		} else if err != nil {
			return nil, err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE applications SET access_mode=?,is_public=? WHERE id=?`, in.AccessMode, in.AccessMode == ModeAll, appID); err != nil {
		return nil, err
	}
	for _, table := range []string{"application_department_grants", "application_user_grants", "application_group_grants"} {
		if _, err = tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE application_id=?`, appID); err != nil {
			return nil, err
		}
	}
	for id, children := range depMap {
		if _, err = tx.ExecContext(ctx, `INSERT INTO application_department_grants(application_id,department_id,include_children,created_by) VALUES(?,?,?,?)`, appID, id, children, actorID); err != nil {
			return nil, err
		}
	}
	for id := range userMap {
		if _, err = tx.ExecContext(ctx, `INSERT INTO application_user_grants(application_id,directory_user_id,created_by) VALUES(?,?,?)`, appID, id, actorID); err != nil {
			return nil, err
		}
	}
	for id := range groupMap {
		if _, err = tx.ExecContext(ctx, `INSERT INTO application_group_grants(application_id,group_id,created_by) VALUES(?,?,?)`, appID, id, actorID); err != nil {
			return nil, err
		}
	}
	afterSnapshot := accessSnapshotFromInput(in.AccessMode, depMap, userMap, groupMap)
	detail, _ := json.Marshal(map[string]any{"before": beforeSnapshot, "after": afterSnapshot})
	if _, err = tx.ExecContext(ctx, `INSERT INTO audit_logs(user_id,action,resource,resource_id,detail) VALUES(?,'application.access.update','application',?,?)`, actorID, strconv.FormatInt(appID, 10), detail); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return s.Get(ctx, appID)
}

func (s *Service) Allowed(ctx context.Context, appID, userID int64, isStaff bool) (bool, error) {
	d, err := s.ResolveLocalUser(ctx, appID, userID, isStaff)
	if err != nil {
		return false, err
	}
	return d.Allowed, nil
}
func (s *Service) ResolveLocalUser(ctx context.Context, appID, userID int64, isStaff bool) (*AccessDecision, error) {
	app, err := s.application(ctx, appID)
	if err != nil {
		return nil, err
	}
	d := baseDecision(app)
	if !app.Enabled {
		return deny(d, ReasonResourceDisabled, "resource disabled"), nil
	}
	if isStaff {
		return bypass(d, ReasonStaffBypass, "is_staff bypass"), nil
	}
	if s.RBACEnabled {
		ok, err := s.hasBypass(ctx, userID)
		if err != nil {
			return nil, err
		}
		if ok {
			return bypass(d, ReasonRBACBypass, "resource.access.bypass"), nil
		}
	}
	var directoryID int64
	if err = s.DB.QueryRowContext(ctx, `SELECT id FROM directory_users WHERE local_user_id=?`, userID).Scan(&directoryID); errors.Is(err, sql.ErrNoRows) {
		if app.AccessMode == ModeAll {
			return bypass(d, ReasonAccessAll, "access_mode=all"), nil
		}
		if app.AccessMode == ModeAdminOnly {
			return deny(d, ReasonAccessAdminOnly, "access_mode=admin_only"), nil
		}
		return deny(d, ReasonDirectoryNotLinked, "directory user not linked"), nil
	} else if err != nil {
		return nil, err
	}
	return s.resolveDirectory(ctx, d, directoryID)
}
func (s *Service) ResolveDirectoryUser(ctx context.Context, appID, directoryUserID int64) (*AccessDecision, error) {
	app, err := s.application(ctx, appID)
	if err != nil {
		return nil, err
	}
	d := baseDecision(app)
	if !app.Enabled {
		return deny(d, ReasonResourceDisabled, "resource disabled"), nil
	}
	return s.resolveDirectory(ctx, d, directoryUserID)
}
func (s *Service) resolveDirectory(ctx context.Context, d *AccessDecision, directoryUserID int64) (*AccessDecision, error) {
	var u DecisionUser
	var activeStatus int
	var local sql.NullInt64
	if err := s.DB.QueryRowContext(ctx, `SELECT id,name,is_active,is_resigned,active_status,local_user_id FROM directory_users WHERE id=?`, directoryUserID).Scan(&u.ID, &u.Name, &u.Active, &u.Resigned, &activeStatus, &local); errors.Is(err, sql.ErrNoRows) {
		return deny(d, ReasonDirectoryNotLinked, "directory user not found"), nil
	} else if err != nil {
		return nil, err
	}
	u.Linked = local.Valid
	if local.Valid {
		v := local.Int64
		u.LocalUserID = &v
		var staff bool
		if err := s.DB.QueryRowContext(ctx, `SELECT is_staff FROM users WHERE id=? AND is_active=1`, v).Scan(&staff); err == nil && staff {
			d.User = &u
			return bypass(d, ReasonStaffBypass, "is_staff bypass"), nil
		}
		if s.RBACEnabled {
			ok, err := s.hasBypass(ctx, v)
			if err != nil {
				return nil, err
			}
			if ok {
				d.User = &u
				return bypass(d, ReasonRBACBypass, "resource.access.bypass"), nil
			}
		}
	}
	d.User = &u
	if !u.Active || activeStatus != 2 {
		return deny(d, ReasonDirectoryUserInactive, "directory user inactive"), nil
	}
	if u.Resigned {
		return deny(d, ReasonDirectoryUserResigned, "directory user resigned"), nil
	}
	if d.Application.AccessMode == ModeAll {
		return bypass(d, ReasonAccessAll, "access_mode=all"), nil
	}
	if d.Application.AccessMode == ModeAdminOnly {
		return deny(d, ReasonAccessAdminOnly, "access_mode=admin_only"), nil
	}
	var id int64
	var name string
	if err := s.DB.QueryRowContext(ctx, `SELECT du.id,du.name FROM application_user_grants ug JOIN directory_users du ON du.id=ug.directory_user_id WHERE ug.application_id=? AND ug.directory_user_id=? LIMIT 1`, d.Application.ID, u.ID).Scan(&id, &name); err == nil {
		return allow(d, ReasonDirectUserMatch, MatchedGrant{Type: "user", ID: id, Name: name}), nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	var children bool
	if err := s.DB.QueryRowContext(ctx, `SELECT dep.id,dep.name,dg.include_children FROM directory_user_departments dud JOIN directory_department_closure c ON c.descendant_id=dud.department_id JOIN application_department_grants dg ON dg.department_id=c.ancestor_id AND (dg.include_children=1 OR c.depth=0) JOIN directory_departments dep ON dep.id=dg.department_id WHERE dud.directory_user_id=? AND dg.application_id=? LIMIT 1`, u.ID, d.Application.ID).Scan(&id, &name, &children); err == nil {
		return allow(d, ReasonDirectDepartmentMatch, MatchedGrant{Type: "department", ID: id, Name: name, IncludeChildren: children}), nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	var source, groupType string
	if err := s.DB.QueryRowContext(ctx, `SELECT g.id,g.name,g.source_type,g.external_group_type FROM application_group_grants ag JOIN access_groups g ON g.id=ag.group_id AND g.enabled=1 JOIN access_group_users gu ON gu.group_id=g.id WHERE ag.application_id=? AND g.source_type='local' AND gu.directory_user_id=? LIMIT 1`, d.Application.ID, u.ID).Scan(&id, &name, &source, &groupType); err == nil {
		return allow(d, ReasonLocalGroupUserMatch, MatchedGrant{Type: "local_group", ID: id, Name: name, SourceType: source, ExternalGroupType: groupType}), nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if err := s.DB.QueryRowContext(ctx, `SELECT g.id,g.name,g.source_type,g.external_group_type,gd.include_children FROM application_group_grants ag JOIN access_groups g ON g.id=ag.group_id AND g.enabled=1 JOIN access_group_departments gd ON gd.group_id=g.id JOIN directory_department_closure c ON c.ancestor_id=gd.department_id AND (gd.include_children=1 OR c.depth=0) JOIN directory_user_departments dud ON dud.department_id=c.descendant_id WHERE ag.application_id=? AND g.source_type='local' AND dud.directory_user_id=? LIMIT 1`, d.Application.ID, u.ID).Scan(&id, &name, &source, &groupType, &children); err == nil {
		return allow(d, ReasonLocalGroupDepartmentMatch, MatchedGrant{Type: "local_group", ID: id, Name: name, SourceType: source, ExternalGroupType: groupType, IncludeChildren: children}), nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if err := s.DB.QueryRowContext(ctx, `SELECT g.id,g.name,g.source_type,g.external_group_type FROM application_group_grants ag JOIN access_groups g ON g.id=ag.group_id AND g.enabled=1 JOIN access_group_external_members em ON em.group_id=g.id WHERE ag.application_id=? AND g.source_type='feishu' AND em.directory_user_id=? LIMIT 1`, d.Application.ID, u.ID).Scan(&id, &name, &source, &groupType); err == nil {
		return allow(d, ReasonFeishuGroupMemberMatch, MatchedGrant{Type: "feishu_group", ID: id, Name: name, SourceType: source, ExternalGroupType: groupType}), nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	return deny(d, ReasonNoAssignmentMatch, "no grant matched"), nil
}
func (s *Service) hasBypass(ctx context.Context, userID int64) (bool, error) {
	var ok bool
	err := s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM admin_role_assignments a JOIN admin_roles r ON r.id=a.role_id AND r.enabled=1 JOIN admin_role_permissions rp ON rp.role_id=r.id JOIN admin_permissions p ON p.id=rp.permission_id AND p.code='resource.access.bypass' JOIN users u ON u.id=a.user_id AND u.is_active=1 WHERE a.user_id=? AND a.enabled=1 AND a.scope_type='global' AND (a.expires_at IS NULL OR a.expires_at>CURRENT_TIMESTAMP(3)))`, userID).Scan(&ok)
	return ok, err
}
func (s *Service) application(ctx context.Context, id int64) (*DecisionApplication, error) {
	var a DecisionApplication
	if err := s.DB.QueryRowContext(ctx, `SELECT id,name,enabled,access_mode FROM applications WHERE id=?`, id).Scan(&a.ID, &a.Name, &a.Enabled, &a.AccessMode); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	return &a, nil
}
func baseDecision(a *DecisionApplication) *AccessDecision {
	return &AccessDecision{Application: a, MatchedGrants: []MatchedGrant{}, AccessPath: []string{a.Name, a.AccessMode}}
}
func allow(d *AccessDecision, reason string, g MatchedGrant) *AccessDecision {
	d.Allowed = true
	d.ReasonCode = reason
	d.MatchedGrants = append(d.MatchedGrants, g)
	d.AccessPath = append(d.AccessPath, g.Name, "ALLOW")
	return d
}
func bypass(d *AccessDecision, reason, path string) *AccessDecision {
	d.Allowed = true
	d.ReasonCode = reason
	d.AccessPath = append(d.AccessPath, path, "ALLOW")
	return d
}
func deny(d *AccessDecision, reason, path string) *AccessDecision {
	d.Allowed = false
	d.ReasonCode = reason
	d.AccessPath = append(d.AccessPath, path, "DENY")
	return d
}
func (s *Service) departmentNames(ctx context.Context, userID int64) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT d.name FROM directory_user_departments dud JOIN directory_departments d ON d.id=dud.department_id WHERE dud.directory_user_id=? ORDER BY dud.is_primary DESC,d.name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var n string
		if err = rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}
func accessSnapshotTx(ctx context.Context, tx *sql.Tx, appID int64, mode string) (map[string]any, error) {
	deps := []map[string]any{}
	rows, err := tx.QueryContext(ctx, `SELECT department_id,include_children FROM application_department_grants WHERE application_id=? ORDER BY department_id`, appID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		var children bool
		if err = rows.Scan(&id, &children); err != nil {
			rows.Close()
			return nil, err
		}
		deps = append(deps, map[string]any{"department_id": id, "include_children": children})
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	users := []int64{}
	rows, err = tx.QueryContext(ctx, `SELECT directory_user_id FROM application_user_grants WHERE application_id=? ORDER BY directory_user_id`, appID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		users = append(users, id)
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	groups := []int64{}
	rows, err = tx.QueryContext(ctx, `SELECT group_id FROM application_group_grants WHERE application_id=? ORDER BY group_id`, appID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		groups = append(groups, id)
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	return map[string]any{"access_mode": mode, "department_grants": deps, "user_grants": users, "group_grants": groups}, nil
}
func accessSnapshotFromInput(mode string, deps map[int64]bool, users map[int64]struct{}, groups map[int64]struct{}) map[string]any {
	depIDs := make([]int64, 0, len(deps))
	for id := range deps {
		depIDs = append(depIDs, id)
	}
	sort.Slice(depIDs, func(i, j int) bool { return depIDs[i] < depIDs[j] })
	depRows := make([]map[string]any, 0, len(depIDs))
	for _, id := range depIDs {
		depRows = append(depRows, map[string]any{"department_id": id, "include_children": deps[id]})
	}
	userIDs := make([]int64, 0, len(users))
	for id := range users {
		userIDs = append(userIDs, id)
	}
	sort.Slice(userIDs, func(i, j int) bool { return userIDs[i] < userIDs[j] })
	groupIDs := make([]int64, 0, len(groups))
	for id := range groups {
		groupIDs = append(groupIDs, id)
	}
	sort.Slice(groupIDs, func(i, j int) bool { return groupIDs[i] < groupIDs[j] })
	return map[string]any{"access_mode": mode, "department_grants": depRows, "user_grants": userIDs, "group_grants": groupIDs}
}
func validMode(v string) bool { return v == ModeAll || v == ModeAssigned || v == ModeAdminOnly }
