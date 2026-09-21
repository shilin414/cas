package adminrbac

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

var (
	ErrNotFound   = errors.New("admin rbac: not found")
	ErrInvalid    = errors.New("admin rbac: invalid input")
	ErrSystemRole = errors.New("admin rbac: system role is read only")
	ErrRoleInUse  = errors.New("admin rbac: role is assigned")
	ErrLastOwner  = errors.New("admin rbac: cannot remove the final platform owner")
)

var roleCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{2,99}$`)

type Service struct {
	DB      *sql.DB
	Enabled bool
}

func (s *Service) HasPermission(ctx context.Context, userID int64, isStaff bool, code string) (bool, error) {
	if isStaff {
		return true, nil
	}
	if !s.Enabled || userID <= 0 || code == "" {
		return false, nil
	}
	var ok bool
	err := s.DB.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM admin_role_assignments a
		JOIN admin_roles r ON r.id=a.role_id AND r.enabled=1
		JOIN admin_role_permissions rp ON rp.role_id=r.id
		JOIN admin_permissions p ON p.id=rp.permission_id
		WHERE a.user_id=? AND a.enabled=1 AND a.scope_type='global'
		  AND (a.expires_at IS NULL OR a.expires_at>CURRENT_TIMESTAMP(3))
		  AND p.code=?
	)`, userID, code).Scan(&ok)
	return ok, err
}

func (s *Service) Me(ctx context.Context, userID int64, isStaff bool) (*Me, error) {
	out := &Me{CanAccessConsole: isStaff, IsSuperAdmin: isStaff, Roles: []Role{}, Permissions: []Permission{}}
	if isStaff {
		permissions, err := s.ListPermissions(ctx)
		if err != nil {
			return nil, err
		}
		out.Permissions = permissions
		return out, nil
	}
	if !s.Enabled {
		return out, nil
	}
	roles, err := s.rolesForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	out.Roles = roles
	seen := map[int64]Permission{}
	for _, role := range roles {
		for _, p := range role.Permissions {
			seen[p.ID] = p
		}
	}
	for _, p := range seen {
		out.Permissions = append(out.Permissions, p)
	}
	sort.Slice(out.Permissions, func(i, j int) bool { return out.Permissions[i].Code < out.Permissions[j].Code })
	out.CanAccessConsole = len(out.Permissions) > 0
	return out, nil
}

func (s *Service) ListPermissions(ctx context.Context) ([]Permission, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,code,category,name,description,created_at FROM admin_permissions ORDER BY category,code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Permission{}
	for rows.Next() {
		var p Permission
		if err = rows.Scan(&p.ID, &p.Code, &p.Category, &p.Name, &p.Description, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Service) ListRoles(ctx context.Context) ([]Role, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,code,name,description,is_system,enabled,created_at,updated_at FROM admin_roles ORDER BY is_system DESC,name,id`)
	if err != nil {
		return nil, err
	}
	out := []Role{}
	roleIDs := []int64{}
	for rows.Next() {
		var r Role
		if err = rows.Scan(&r.ID, &r.Code, &r.Name, &r.Description, &r.IsSystem, &r.Enabled, &r.CreatedAt, &r.UpdatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		r.Permissions = []Permission{}
		roleIDs = append(roleIDs, r.ID)
		out = append(out, r)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	permissionsByRole, err := s.permissionsForRoles(ctx, roleIDs)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Permissions = permissionsByRole[out[i].ID]
		if out[i].Permissions == nil {
			out[i].Permissions = []Permission{}
		}
	}
	return out, nil
}

func (s *Service) GetRole(ctx context.Context, id int64) (*Role, error) {
	var r Role
	err := s.DB.QueryRowContext(ctx, `SELECT id,code,name,description,is_system,enabled,created_at,updated_at FROM admin_roles WHERE id=?`, id).
		Scan(&r.ID, &r.Code, &r.Name, &r.Description, &r.IsSystem, &r.Enabled, &r.CreatedAt, &r.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	r.Permissions, err = s.permissionsForRole(ctx, id)
	return &r, err
}

func (s *Service) CreateRole(ctx context.Context, actorID int64, in RoleInput) (*Role, error) {
	if err := validateRoleInput(in, true); err != nil {
		return nil, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `INSERT INTO admin_roles(code,name,description,is_system,enabled,created_by) VALUES(?,?,?,0,?,?)`, in.Code, in.Name, in.Description, in.Enabled, actorID)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	if err = s.replaceRolePermissionsTx(ctx, tx, id, in.PermissionCodes); err != nil {
		return nil, err
	}
	afterSnapshot, err := roleSnapshotTx(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if err = writeAuditTx(ctx, tx, actorID, "admin.role.create", fmt.Sprint(id), map[string]any{"before": nil, "after": afterSnapshot}); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetRole(ctx, id)
}

func (s *Service) UpdateRole(ctx context.Context, actorID, id int64, in RoleInput) (*Role, error) {
	if err := validateRoleInput(in, false); err != nil {
		return nil, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var system, beforeEnabled bool
	var code, beforeName, beforeDescription string
	if err = tx.QueryRowContext(ctx, `SELECT is_system,code,name,description,enabled FROM admin_roles WHERE id=? FOR UPDATE`, id).Scan(&system, &code, &beforeName, &beforeDescription, &beforeEnabled); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	if system {
		return nil, ErrSystemRole
	}
	if in.Code == "" {
		in.Code = code
	}
	if !roleCodePattern.MatchString(in.Code) {
		return nil, fmt.Errorf("%w: invalid role code", ErrInvalid)
	}
	beforePermissions, err := rolePermissionCodesTx(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE admin_roles SET code=?,name=?,description=?,enabled=? WHERE id=?`, in.Code, strings.TrimSpace(in.Name), in.Description, in.Enabled, id); err != nil {
		return nil, err
	}
	if err = s.replaceRolePermissionsTx(ctx, tx, id, in.PermissionCodes); err != nil {
		return nil, err
	}
	if err = writeAuditTx(ctx, tx, actorID, "admin.role.update", fmt.Sprint(id), map[string]any{"before": map[string]any{"code": code, "name": beforeName, "description": beforeDescription, "enabled": beforeEnabled, "permission_codes": beforePermissions}, "after": map[string]any{"code": in.Code, "name": strings.TrimSpace(in.Name), "description": in.Description, "enabled": in.Enabled, "permission_codes": in.PermissionCodes}}); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetRole(ctx, id)
}

func (s *Service) DeleteRole(ctx context.Context, actorID, id int64) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var system bool
	if err = tx.QueryRowContext(ctx, `SELECT is_system FROM admin_roles WHERE id=? FOR UPDATE`, id).Scan(&system); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	if system {
		return ErrSystemRole
	}
	beforeSnapshot, err := roleSnapshotTx(ctx, tx, id)
	if err != nil {
		return err
	}
	var n int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_role_assignments WHERE role_id=?`, id).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return ErrRoleInUse
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM admin_role_permissions WHERE role_id=?`, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM admin_roles WHERE id=?`, id); err != nil {
		return err
	}
	if err = writeAuditTx(ctx, tx, actorID, "admin.role.delete", fmt.Sprint(id), map[string]any{"before": beforeSnapshot, "after": nil}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) ListAdministrators(ctx context.Context) ([]Administrator, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT DISTINCT u.id,u.username,u.display_name,u.email,u.auth_source,u.is_staff,u.is_active,fi.last_login_at
		FROM users u LEFT JOIN admin_role_assignments a ON a.user_id=u.id LEFT JOIN feishu_identities fi ON fi.user_id=u.id
		WHERE u.is_staff=1 OR a.id IS NOT NULL ORDER BY u.is_staff DESC,u.display_name,u.id`)
	if err != nil {
		return nil, err
	}
	out := []Administrator{}
	userIDs := []int64{}
	for rows.Next() {
		var a Administrator
		var last sql.NullTime
		if err = rows.Scan(&a.UserID, &a.Username, &a.DisplayName, &a.Email, &a.AuthSource, &a.IsStaff, &a.IsActive, &last); err != nil {
			rows.Close()
			return nil, err
		}
		if last.Valid {
			v := last.Time
			a.LastLoginAt = &v
		}
		a.Assignments = []Assignment{}
		userIDs = append(userIDs, a.UserID)
		out = append(out, a)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	assignmentsByUser, err := s.assignmentsForUsers(ctx, userIDs)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Assignments = assignmentsByUser[out[i].UserID]
		if out[i].Assignments == nil {
			out[i].Assignments = []Assignment{}
		}
	}
	return out, nil
}

func (s *Service) ReplaceAdministratorRoles(ctx context.Context, actorID, userID int64, roleIDs []int64, expiresAt *time.Time) (*Administrator, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// Serialize all owner-set mutations through the shared platform_owner row.
	var ownerRoleLock int64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM admin_roles WHERE code='platform_owner' FOR UPDATE`).Scan(&ownerRoleLock); err != nil {
		return nil, err
	}
	var active bool
	if err = tx.QueryRowContext(ctx, `SELECT is_active FROM users WHERE id=? FOR UPDATE`, userID).Scan(&active); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	if !active {
		return nil, fmt.Errorf("%w: user inactive", ErrInvalid)
	}
	beforeAssignments, err := assignmentSnapshotTx(ctx, tx, userID)
	if err != nil {
		return nil, err
	}
	uniq := map[int64]struct{}{}
	for _, id := range roleIDs {
		if id <= 0 {
			return nil, ErrInvalid
		}
		uniq[id] = struct{}{}
	}
	orderedRoleIDs := make([]int64, 0, len(uniq))
	for id := range uniq {
		orderedRoleIDs = append(orderedRoleIDs, id)
	}
	sort.Slice(orderedRoleIDs, func(i, j int) bool { return orderedRoleIDs[i] < orderedRoleIDs[j] })
	for _, id := range orderedRoleIDs {
		var enabled bool
		if err = tx.QueryRowContext(ctx, `SELECT enabled FROM admin_roles WHERE id=? FOR UPDATE`, id).Scan(&enabled); err != nil || !enabled {
			if errors.Is(err, sql.ErrNoRows) || !enabled {
				return nil, fmt.Errorf("%w: role %d unavailable", ErrInvalid, id)
			}
			return nil, err
		}
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM admin_role_assignments WHERE user_id=?`, userID); err != nil {
		return nil, err
	}
	for _, id := range orderedRoleIDs {
		if _, err = tx.ExecContext(ctx, `INSERT INTO admin_role_assignments(user_id,role_id,scope_type,scope_id,enabled,expires_at,created_by) VALUES(?,?,'global','',1,?,?)`, userID, id, expiresAt, actorID); err != nil {
			return nil, err
		}
	}
	if err = s.ensureOwnerTx(ctx, tx); err != nil {
		return nil, err
	}
	if err = writeAuditTx(ctx, tx, actorID, "admin.assignment.replace", fmt.Sprint(userID), map[string]any{"before": beforeAssignments, "after": map[string]any{"role_ids": orderedRoleIDs, "expires_at": expiresAt}}); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	admins, err := s.ListAdministrators(ctx)
	if err != nil {
		return nil, err
	}
	for i := range admins {
		if admins[i].UserID == userID {
			return &admins[i], nil
		}
	}
	return &Administrator{UserID: userID, Assignments: []Assignment{}}, nil
}

func sqlPlaceholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func (s *Service) permissionsForRoles(ctx context.Context, roleIDs []int64) (map[int64][]Permission, error) {
	out := make(map[int64][]Permission, len(roleIDs))
	if len(roleIDs) == 0 {
		return out, nil
	}
	args := make([]any, 0, len(roleIDs))
	for _, id := range roleIDs {
		args = append(args, id)
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT rp.role_id,p.id,p.code,p.category,p.name,p.description,p.created_at FROM admin_role_permissions rp JOIN admin_permissions p ON p.id=rp.permission_id WHERE rp.role_id IN (`+sqlPlaceholders(len(roleIDs))+`) ORDER BY rp.role_id,p.category,p.code`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var roleID int64
		var p Permission
		if err = rows.Scan(&roleID, &p.ID, &p.Code, &p.Category, &p.Name, &p.Description, &p.CreatedAt); err != nil {
			return nil, err
		}
		out[roleID] = append(out[roleID], p)
	}
	return out, rows.Err()
}

func (s *Service) assignmentsForUsers(ctx context.Context, userIDs []int64) (map[int64][]Assignment, error) {
	out := make(map[int64][]Assignment, len(userIDs))
	if len(userIDs) == 0 {
		return out, nil
	}
	args := make([]any, 0, len(userIDs))
	for _, id := range userIDs {
		args = append(args, id)
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT a.user_id,a.id,r.id,r.code,r.name,a.enabled,a.expires_at FROM admin_role_assignments a JOIN admin_roles r ON r.id=a.role_id WHERE a.user_id IN (`+sqlPlaceholders(len(userIDs))+`) ORDER BY a.user_id,r.name`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var userID int64
		var a Assignment
		var exp sql.NullTime
		if err = rows.Scan(&userID, &a.ID, &a.RoleID, &a.RoleCode, &a.RoleName, &a.Enabled, &exp); err != nil {
			return nil, err
		}
		if exp.Valid {
			v := exp.Time
			a.ExpiresAt = &v
		}
		out[userID] = append(out[userID], a)
	}
	return out, rows.Err()
}

func (s *Service) permissionsForRole(ctx context.Context, roleID int64) ([]Permission, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT p.id,p.code,p.category,p.name,p.description,p.created_at FROM admin_permissions p JOIN admin_role_permissions rp ON rp.permission_id=p.id WHERE rp.role_id=? ORDER BY p.category,p.code`, roleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Permission{}
	for rows.Next() {
		var p Permission
		if err = rows.Scan(&p.ID, &p.Code, &p.Category, &p.Name, &p.Description, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func (s *Service) rolesForUser(ctx context.Context, userID int64) ([]Role, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT r.id,r.code,r.name,r.description,r.is_system,r.enabled,r.created_at,r.updated_at FROM admin_role_assignments a JOIN admin_roles r ON r.id=a.role_id WHERE a.user_id=? AND a.enabled=1 AND r.enabled=1 AND a.scope_type='global' AND (a.expires_at IS NULL OR a.expires_at>CURRENT_TIMESTAMP(3)) ORDER BY r.name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Role{}
	for rows.Next() {
		var r Role
		if err = rows.Scan(&r.ID, &r.Code, &r.Name, &r.Description, &r.IsSystem, &r.Enabled, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		r.Permissions, err = s.permissionsForRole(ctx, r.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (s *Service) assignmentsForUser(ctx context.Context, userID int64) ([]Assignment, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT a.id,r.id,r.code,r.name,a.enabled,a.expires_at FROM admin_role_assignments a JOIN admin_roles r ON r.id=a.role_id WHERE a.user_id=? ORDER BY r.name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Assignment{}
	for rows.Next() {
		var a Assignment
		var exp sql.NullTime
		if err = rows.Scan(&a.ID, &a.RoleID, &a.RoleCode, &a.RoleName, &a.Enabled, &exp); err != nil {
			return nil, err
		}
		if exp.Valid {
			v := exp.Time
			a.ExpiresAt = &v
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
func (s *Service) replaceRolePermissionsTx(ctx context.Context, tx *sql.Tx, roleID int64, codes []string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM admin_role_permissions WHERE role_id=?`, roleID); err != nil {
		return err
	}
	seen := map[string]struct{}{}
	for _, code := range codes {
		code = strings.TrimSpace(code)
		if code == "" {
			continue
		}
		if _, ok := seen[code]; ok {
			continue
		}
		seen[code] = struct{}{}
		res, err := tx.ExecContext(ctx, `INSERT INTO admin_role_permissions(role_id,permission_id) SELECT ?,id FROM admin_permissions WHERE code=?`, roleID, code)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			return fmt.Errorf("%w: unknown permission %s", ErrInvalid, code)
		}
	}
	return nil
}
func (s *Service) ensureOwnerTx(ctx context.Context, tx *sql.Tx) error {
	var staffCount, permanentOwnerCount int
	err := tx.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM users WHERE is_staff=1 AND is_active=1),(SELECT COUNT(DISTINCT a.user_id) FROM admin_role_assignments a JOIN admin_roles r ON r.id=a.role_id AND r.code='platform_owner' AND r.enabled=1 JOIN users u ON u.id=a.user_id AND u.is_active=1 WHERE a.enabled=1 AND a.scope_type='global' AND a.expires_at IS NULL)`).Scan(&staffCount, &permanentOwnerCount)
	if err != nil {
		return err
	}
	if staffCount < 1 && permanentOwnerCount < 1 {
		return ErrLastOwner
	}
	return nil
}
func validateRoleInput(in RoleInput, requireCode bool) error {
	if requireCode && !roleCodePattern.MatchString(in.Code) {
		return fmt.Errorf("%w: invalid role code", ErrInvalid)
	}
	if strings.TrimSpace(in.Name) == "" {
		return fmt.Errorf("%w: role name required", ErrInvalid)
	}
	return nil
}
func roleSnapshotTx(ctx context.Context, tx *sql.Tx, roleID int64) (map[string]any, error) {
	var code, name, description string
	var system, enabled bool
	if err := tx.QueryRowContext(ctx, `SELECT code,name,description,is_system,enabled FROM admin_roles WHERE id=?`, roleID).Scan(&code, &name, &description, &system, &enabled); err != nil {
		return nil, err
	}
	permissions, err := rolePermissionCodesTx(ctx, tx, roleID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"code": code, "name": name, "description": description, "is_system": system, "enabled": enabled, "permission_codes": permissions}, nil
}
func rolePermissionCodesTx(ctx context.Context, tx *sql.Tx, roleID int64) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT p.code FROM admin_role_permissions rp JOIN admin_permissions p ON p.id=rp.permission_id WHERE rp.role_id=? ORDER BY p.code`, roleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var code string
		if err = rows.Scan(&code); err != nil {
			return nil, err
		}
		out = append(out, code)
	}
	return out, rows.Err()
}
func assignmentSnapshotTx(ctx context.Context, tx *sql.Tx, userID int64) ([]map[string]any, error) {
	rows, err := tx.QueryContext(ctx, `SELECT role_id,enabled,expires_at FROM admin_role_assignments WHERE user_id=? ORDER BY role_id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var roleID int64
		var enabled bool
		var expires sql.NullTime
		if err = rows.Scan(&roleID, &enabled, &expires); err != nil {
			return nil, err
		}
		var expiresValue any
		if expires.Valid {
			expiresValue = expires.Time
		}
		out = append(out, map[string]any{"role_id": roleID, "enabled": enabled, "expires_at": expiresValue})
	}
	return out, rows.Err()
}
func writeAuditTx(ctx context.Context, tx *sql.Tx, actorID int64, action, resourceID string, detail any) error {
	raw, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO audit_logs(user_id,action,resource,resource_id,detail) VALUES(?,?,'admin_rbac',?,?)`, actorID, action, resourceID, raw)
	return err
}
