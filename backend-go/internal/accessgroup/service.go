package accessgroup

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var (
	ErrNotFound = errors.New("access group: not found")
	ErrInvalid  = errors.New("access group: invalid input")
	ErrReadOnly = errors.New("access group: external group membership is read only")
	ErrInUse    = errors.New("access group: group is used by applications")
)

var codePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{2,99}$`)

type Service struct{ DB *sql.DB }

func (s *Service) List(ctx context.Context, source string) ([]Group, error) {
	args := []any{}
	where := ""
	if source != "" {
		if source != "local" && source != "feishu" {
			return nil, ErrInvalid
		}
		where = "WHERE g.source_type=?"
		args = append(args, source)
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT g.id,g.code,g.name,g.description,g.source_type,g.external_group_id,g.external_group_type,g.enabled,g.sync_status,g.last_synced_at,g.created_at,g.updated_at,
		COALESCE(mc.member_count,0),COALESCE(ac.application_count,0),COALESCE(cu.covered_users,0)
		FROM access_groups g
		LEFT JOIN (
			SELECT group_id,COUNT(*) member_count FROM (
				SELECT group_id,directory_user_id member_key FROM access_group_external_members
				UNION ALL SELECT group_id,directory_user_id FROM access_group_users
				UNION ALL SELECT group_id,department_id FROM access_group_departments
			) members GROUP BY group_id
		) mc ON mc.group_id=g.id
		LEFT JOIN (SELECT group_id,COUNT(*) application_count FROM application_group_grants GROUP BY group_id) ac ON ac.group_id=g.id
		LEFT JOIN (
			SELECT memberships.group_id,COUNT(DISTINCT memberships.directory_user_id) covered_users
			FROM (
				SELECT em.group_id,em.directory_user_id FROM access_group_external_members em
				UNION ALL SELECT gu.group_id,gu.directory_user_id FROM access_group_users gu
				UNION ALL
				SELECT gd.group_id,dud.directory_user_id FROM access_group_departments gd
				JOIN directory_department_closure c ON c.ancestor_id=gd.department_id AND (gd.include_children=1 OR c.depth=0)
				JOIN directory_user_departments dud ON dud.department_id=c.descendant_id
			) memberships
			JOIN directory_users du ON du.id=memberships.directory_user_id AND du.is_active=1 AND du.is_resigned=0
			GROUP BY memberships.group_id
		) cu ON cu.group_id=g.id `+where+` ORDER BY g.source_type,g.name,g.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Group{}
	for rows.Next() {
		g, err := scanGroup(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *g)
	}
	return out, rows.Err()
}

// scanner is implemented by *sql.Row and *sql.Rows.
type scanner interface{ Scan(...any) error }

func scanGroup(row scanner) (*Group, error) {
	var g Group
	var synced sql.NullTime
	var memberCount, applicationCount, coveredUsers int
	err := row.Scan(&g.ID, &g.Code, &g.Name, &g.Description, &g.SourceType, &g.ExternalGroupID, &g.ExternalGroupType, &g.Enabled, &g.SyncStatus, &synced, &g.CreatedAt, &g.UpdatedAt, &memberCount, &applicationCount, &coveredUsers)
	if err != nil {
		return nil, err
	}
	if synced.Valid {
		v := synced.Time
		g.LastSyncedAt = &v
	}
	g.MemberCount = memberCount
	g.ApplicationCount = applicationCount
	g.CoveredUsers = coveredUsers
	return &g, nil
}

func (s *Service) Get(ctx context.Context, id int64) (*Group, error) {
	row := s.DB.QueryRowContext(ctx, `SELECT g.id,g.code,g.name,g.description,g.source_type,g.external_group_id,g.external_group_type,g.enabled,g.sync_status,g.last_synced_at,g.created_at,g.updated_at,
	CASE WHEN g.source_type='feishu' THEN (SELECT COUNT(*) FROM access_group_external_members em WHERE em.group_id=g.id) ELSE (SELECT COUNT(*) FROM access_group_users gu WHERE gu.group_id=g.id)+(SELECT COUNT(*) FROM access_group_departments gd WHERE gd.group_id=g.id) END,
	(SELECT COUNT(*) FROM application_group_grants ag WHERE ag.group_id=g.id),0 FROM access_groups g WHERE g.id=?`, id)
	g, err := scanGroup(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	g.CoveredUsers, err = s.coveredUsers(ctx, id, g.SourceType)
	if err != nil {
		return nil, err
	}
	if g.SourceType == "local" {
		g.Departments, err = s.departments(ctx, id)
		if err != nil {
			return nil, err
		}
		g.Users, err = s.users(ctx, id)
		if err != nil {
			return nil, err
		}
	}
	return g, nil
}

func (s *Service) Create(ctx context.Context, actorID int64, in Input) (*Group, error) {
	if err := validate(in, true); err != nil {
		return nil, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `INSERT INTO access_groups(code,name,description,source_type,external_group_id,enabled,sync_status,created_by,updated_by) VALUES(?,?,?,'local',?,?,'local',?,?)`, in.Code, strings.TrimSpace(in.Name), in.Description, in.Code, in.Enabled, actorID, actorID)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	if err = s.replaceMembersTx(ctx, tx, id, in); err != nil {
		return nil, err
	}
	if err = writeAuditTx(ctx, tx, actorID, "access_group.create", fmt.Sprint(id), map[string]any{"before": nil, "after": in}); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

func (s *Service) Update(ctx context.Context, actorID, id int64, in Input) (*Group, error) {
	if err := validate(in, false); err != nil {
		return nil, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var source, code string
	if err = tx.QueryRowContext(ctx, `SELECT source_type,code FROM access_groups WHERE id=? FOR UPDATE`, id).Scan(&source, &code); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	beforeSnapshot, err := groupSnapshotTx(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if in.Code == "" {
		in.Code = code
	}
	if source == "feishu" {
		if _, err = tx.ExecContext(ctx, `UPDATE access_groups SET enabled=?,updated_by=? WHERE id=?`, in.Enabled, actorID, id); err != nil {
			return nil, err
		}
	} else {
		if !codePattern.MatchString(in.Code) || strings.HasPrefix(in.Code, "feishu_") {
			return nil, ErrInvalid
		}
		if _, err = tx.ExecContext(ctx, `UPDATE access_groups SET code=?,external_group_id=?,name=?,description=?,enabled=?,updated_by=? WHERE id=?`, in.Code, in.Code, strings.TrimSpace(in.Name), in.Description, in.Enabled, actorID, id); err != nil {
			return nil, err
		}
		if err = s.replaceMembersTx(ctx, tx, id, in); err != nil {
			return nil, err
		}
	}
	if err = writeAuditTx(ctx, tx, actorID, "access_group.update", fmt.Sprint(id), map[string]any{"before": beforeSnapshot, "after": in}); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

func (s *Service) Delete(ctx context.Context, actorID, id int64) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var source string
	if err = tx.QueryRowContext(ctx, `SELECT source_type FROM access_groups WHERE id=? FOR UPDATE`, id).Scan(&source); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	if source != "local" {
		return ErrReadOnly
	}
	beforeSnapshot, err := groupSnapshotTx(ctx, tx, id)
	if err != nil {
		return err
	}
	var n int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM application_group_grants WHERE group_id=?`, id).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return ErrInUse
	}
	for _, table := range []string{"access_group_users", "access_group_departments"} {
		if _, err = tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE group_id=?`, id); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM access_groups WHERE id=?`, id); err != nil {
		return err
	}
	if err = writeAuditTx(ctx, tx, actorID, "access_group.delete", fmt.Sprint(id), map[string]any{"before": beforeSnapshot, "after": nil}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) Members(ctx context.Context, id int64) ([]Member, error) {
	var source string
	if err := s.DB.QueryRowContext(ctx, `SELECT source_type FROM access_groups WHERE id=?`, id).Scan(&source); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	query := `SELECT du.id,du.name,du.avatar_url,'' AS external_user_id,'local' AS source FROM access_group_users gu JOIN directory_users du ON du.id=gu.directory_user_id WHERE gu.group_id=?`
	if source == "feishu" {
		query = `SELECT du.id,du.name,du.avatar_url,em.external_user_id,'feishu' AS source FROM access_group_external_members em JOIN directory_users du ON du.id=em.directory_user_id WHERE em.group_id=?`
	}
	rows, err := s.DB.QueryContext(ctx, query, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Member{}
	ids := []int64{}
	for rows.Next() {
		var m Member
		if err = rows.Scan(&m.DirectoryUserID, &m.Name, &m.AvatarURL, &m.ExternalUserID, &m.Source); err != nil {
			return nil, err
		}
		m.Departments = []string{}
		ids = append(ids, m.DirectoryUserID)
		out = append(out, m)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return out, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	depRows, err := s.DB.QueryContext(ctx, `SELECT dud.directory_user_id,d.name FROM directory_user_departments dud JOIN directory_departments d ON d.id=dud.department_id WHERE dud.directory_user_id IN (`+placeholders+`) ORDER BY dud.directory_user_id,dud.is_primary DESC,d.name`, args...)
	if err != nil {
		return nil, err
	}
	defer depRows.Close()
	byID := map[int64][]string{}
	for depRows.Next() {
		var id int64
		var name string
		if err = depRows.Scan(&id, &name); err != nil {
			return nil, err
		}
		byID[id] = append(byID[id], name)
	}
	if err = depRows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Departments = byID[out[i].DirectoryUserID]
	}
	return out, nil
}
func (s *Service) Applications(ctx context.Context, id int64) ([]ApplicationRef, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT a.id,a.name,a.kind,a.enabled FROM application_group_grants ag JOIN applications a ON a.id=ag.application_id WHERE ag.group_id=? ORDER BY a.name,a.id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ApplicationRef{}
	for rows.Next() {
		var a ApplicationRef
		if err = rows.Scan(&a.ID, &a.Name, &a.Kind, &a.Enabled); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
func (s *Service) replaceMembersTx(ctx context.Context, tx *sql.Tx, id int64, in Input) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM access_group_departments WHERE group_id=?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM access_group_users WHERE group_id=?`, id); err != nil {
		return err
	}
	deps := map[int64]bool{}
	for _, g := range in.DepartmentGrants {
		if g.DepartmentID <= 0 {
			return ErrInvalid
		}
		deps[g.DepartmentID] = g.IncludeChildren
	}
	for dep, children := range deps {
		res, err := tx.ExecContext(ctx, `INSERT INTO access_group_departments(group_id,department_id,include_children) SELECT ?,id,? FROM directory_departments WHERE id=? AND is_active=1`, id, children, dep)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			return fmt.Errorf("%w: department %d unavailable", ErrInvalid, dep)
		}
	}
	users := map[int64]struct{}{}
	for _, uid := range in.UserGrants {
		if uid <= 0 {
			return ErrInvalid
		}
		users[uid] = struct{}{}
	}
	for uid := range users {
		res, err := tx.ExecContext(ctx, `INSERT INTO access_group_users(group_id,directory_user_id) SELECT ?,id FROM directory_users WHERE id=? AND is_active=1 AND is_resigned=0`, id, uid)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			return fmt.Errorf("%w: user %d unavailable", ErrInvalid, uid)
		}
	}
	return nil
}
func (s *Service) departments(ctx context.Context, id int64) ([]DepartmentGrant, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT d.id,d.name,gd.include_children,(SELECT COUNT(DISTINCT dud.directory_user_id) FROM directory_department_closure c JOIN directory_user_departments dud ON dud.department_id=c.descendant_id JOIN directory_users du ON du.id=dud.directory_user_id AND du.is_active=1 AND du.is_resigned=0 WHERE c.ancestor_id=d.id AND (gd.include_children=1 OR c.depth=0)) FROM access_group_departments gd JOIN directory_departments d ON d.id=gd.department_id WHERE gd.group_id=? ORDER BY d.name`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DepartmentGrant{}
	for rows.Next() {
		var g DepartmentGrant
		if err = rows.Scan(&g.DepartmentID, &g.Name, &g.IncludeChildren, &g.CoveredUsers); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
func (s *Service) users(ctx context.Context, id int64) ([]UserGrant, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT du.id,du.name,du.avatar_url FROM access_group_users gu JOIN directory_users du ON du.id=gu.directory_user_id WHERE gu.group_id=? ORDER BY du.name`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []UserGrant{}
	for rows.Next() {
		var u UserGrant
		if err = rows.Scan(&u.DirectoryUserID, &u.Name, &u.AvatarURL); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
func (s *Service) coveredUsers(ctx context.Context, id int64, source string) (int, error) {
	var n int
	if source == "feishu" {
		return n, s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM access_group_external_members em JOIN directory_users du ON du.id=em.directory_user_id WHERE em.group_id=? AND du.is_active=1 AND du.is_resigned=0`, id).Scan(&n)
	}
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(DISTINCT x.directory_user_id) FROM (SELECT gu.directory_user_id FROM access_group_users gu WHERE gu.group_id=? UNION SELECT dud.directory_user_id FROM access_group_departments gd JOIN directory_department_closure c ON c.ancestor_id=gd.department_id AND (gd.include_children=1 OR c.depth=0) JOIN directory_user_departments dud ON dud.department_id=c.descendant_id WHERE gd.group_id=?) x JOIN directory_users du ON du.id=x.directory_user_id WHERE du.is_active=1 AND du.is_resigned=0`, id, id).Scan(&n)
	return n, err
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
func groupSnapshotTx(ctx context.Context, tx *sql.Tx, id int64) (map[string]any, error) {
	var code, name, description, source string
	var enabled bool
	if err := tx.QueryRowContext(ctx, `SELECT code,name,description,source_type,enabled FROM access_groups WHERE id=?`, id).Scan(&code, &name, &description, &source, &enabled); err != nil {
		return nil, err
	}
	deps := []map[string]any{}
	rows, err := tx.QueryContext(ctx, `SELECT department_id,include_children FROM access_group_departments WHERE group_id=? ORDER BY department_id`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var depID int64
		var children bool
		if err = rows.Scan(&depID, &children); err != nil {
			rows.Close()
			return nil, err
		}
		deps = append(deps, map[string]any{"department_id": depID, "include_children": children})
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	users := []int64{}
	rows, err = tx.QueryContext(ctx, `SELECT directory_user_id FROM access_group_users WHERE group_id=? ORDER BY directory_user_id`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var userID int64
		if err = rows.Scan(&userID); err != nil {
			rows.Close()
			return nil, err
		}
		users = append(users, userID)
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	return map[string]any{"code": code, "name": name, "description": description, "source_type": source, "enabled": enabled, "department_grants": deps, "user_grants": users}, nil
}
func validate(in Input, requireCode bool) error {
	if requireCode && (!codePattern.MatchString(in.Code) || strings.HasPrefix(in.Code, "feishu_")) {
		return fmt.Errorf("%w: invalid code", ErrInvalid)
	}
	if strings.TrimSpace(in.Name) == "" {
		return fmt.Errorf("%w: name required", ErrInvalid)
	}
	return nil
}
func writeAuditTx(ctx context.Context, tx *sql.Tx, actorID int64, action, resourceID string, detail any) error {
	raw, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO audit_logs(user_id,action,resource,resource_id,detail) VALUES(?,?,'access_group',?,?)`, actorID, action, resourceID, raw)
	return err
}
