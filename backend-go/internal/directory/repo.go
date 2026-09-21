package directory

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var ErrNotFound = errors.New("directory: not found")
var ErrLeaseLost = errors.New("directory: sync lease lost")
var ErrActiveJobExists = errors.New("directory: active sync job already exists")

type Repo struct{ DB *sql.DB }

func (r *Repo) ListDepartments(ctx context.Context, query string, parentID *int64, includeInactive bool) ([]Department, error) {
	args := []any{}
	where := []string{"1=1"}
	if !includeInactive {
		where = append(where, "d.is_active = 1")
	}
	if strings.TrimSpace(query) != "" {
		where = append(where, "d.name LIKE ?")
		args = append(args, "%"+escapeLike(strings.TrimSpace(query))+"%")
	}
	if parentID != nil {
		where = append(where, "d.parent_id = ?")
		args = append(args, *parentID)
	}
	rows, err := r.DB.QueryContext(ctx, `SELECT d.id,d.open_department_id,d.name,d.parent_id,d.parent_open_department_id,d.order_weight,d.is_active,d.last_synced_at FROM directory_departments d WHERE `+strings.Join(where, " AND ")+` ORDER BY d.parent_id IS NOT NULL,d.order_weight,d.name,d.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Department{}
	for rows.Next() {
		var d Department
		var parent sql.NullInt64
		var last sql.NullTime
		if err := rows.Scan(&d.ID, &d.OpenDepartmentID, &d.Name, &parent, &d.ParentOpenDepartmentID, &d.OrderWeight, &d.IsActive, &last); err != nil {
			return nil, err
		}
		d.ParentID = nullInt64Ptr(parent)
		d.LastSyncedAt = nullTimePtr(last)
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *Repo) ListDepartmentPage(ctx context.Context, query string, includeInactive bool, cursor string, limit int) ([]Department, string, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	lastID := int64(0)
	if cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil {
			return nil, "", fmt.Errorf("invalid department cursor: %w", err)
		}
		lastID, err = strconv.ParseInt(string(raw), 10, 64)
		if err != nil || lastID < 1 {
			return nil, "", fmt.Errorf("invalid department cursor")
		}
	}
	args := []any{lastID}
	where := []string{"d.id > ?"}
	if !includeInactive {
		where = append(where, "d.is_active = 1")
	}
	if strings.TrimSpace(query) != "" {
		where = append(where, "d.name LIKE ?")
		args = append(args, "%"+escapeLike(strings.TrimSpace(query))+"%")
	}
	args = append(args, limit+1)
	rows, err := r.DB.QueryContext(ctx, `SELECT d.id,d.open_department_id,d.name,d.parent_id,d.parent_open_department_id,d.order_weight,d.is_active,d.last_synced_at FROM directory_departments d WHERE `+strings.Join(where, " AND ")+` ORDER BY d.id LIMIT ?`, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	out := []Department{}
	for rows.Next() {
		var d Department
		var parent sql.NullInt64
		var last sql.NullTime
		if err := rows.Scan(&d.ID, &d.OpenDepartmentID, &d.Name, &parent, &d.ParentOpenDepartmentID, &d.OrderWeight, &d.IsActive, &last); err != nil {
			return nil, "", err
		}
		d.ParentID = nullInt64Ptr(parent)
		d.LastSyncedAt = nullTimePtr(last)
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > limit {
		out = out[:limit]
		next = base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(out[len(out)-1].ID, 10)))
	}
	return out, next, nil
}

func (r *Repo) ListUsers(ctx context.Context, query string, departmentID *int64, includeInactive bool, cursor string, limit int) ([]User, string, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	lastID := int64(0)
	if cursor != "" {
		if raw, err := base64.RawURLEncoding.DecodeString(cursor); err == nil {
			lastID, _ = strconv.ParseInt(string(raw), 10, 64)
		}
	}
	args := []any{lastID}
	where := []string{"du.id > ?"}
	if !includeInactive {
		where = append(where, "du.is_active = 1")
	}
	if strings.TrimSpace(query) != "" {
		where = append(where, "du.name LIKE ?")
		args = append(args, "%"+escapeLike(strings.TrimSpace(query))+"%")
	}
	join := ""
	if departmentID != nil {
		join = " JOIN directory_user_departments filter_dud ON filter_dud.directory_user_id=du.id AND filter_dud.department_id=?"
		args = append([]any{*departmentID}, args...)
	}
	args = append(args, limit+1)
	rows, err := r.DB.QueryContext(ctx, `SELECT DISTINCT du.id,du.open_id,du.name,du.avatar_url,du.active_status,du.is_resigned,du.local_user_id,du.is_active,du.last_synced_at FROM directory_users du`+join+` WHERE `+strings.Join(where, " AND ")+` ORDER BY du.id LIMIT ?`, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	out := []User{}
	for rows.Next() {
		var u User
		var local sql.NullInt64
		var last sql.NullTime
		if err := rows.Scan(&u.ID, &u.OpenID, &u.Name, &u.AvatarURL, &u.ActiveStatus, &u.IsResigned, &local, &u.IsActive, &last); err != nil {
			return nil, "", err
		}
		u.LocalUserID = nullInt64Ptr(local)
		u.LastSyncedAt = nullTimePtr(last)
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > limit {
		out = out[:limit]
		next = base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(out[len(out)-1].ID, 10)))
	}
	userIDs := make([]int64, 0, len(out))
	for i := range out {
		userIDs = append(userIDs, out[i].ID)
	}
	departmentsByUser, err := r.userDepartmentsForUsers(ctx, userIDs)
	if err != nil {
		return nil, "", err
	}
	for i := range out {
		out[i].Departments = departmentsByUser[out[i].ID]
		if out[i].Departments == nil {
			out[i].Departments = []DepartmentRef{}
		}
	}
	return out, next, nil
}

func (r *Repo) userDepartmentsForUsers(ctx context.Context, userIDs []int64) (map[int64][]DepartmentRef, error) {
	out := make(map[int64][]DepartmentRef, len(userIDs))
	if len(userIDs) == 0 {
		return out, nil
	}
	args := make([]any, 0, len(userIDs))
	for _, id := range userIDs {
		args = append(args, id)
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(userIDs)), ",")
	rows, err := r.DB.QueryContext(ctx, `SELECT dud.directory_user_id,d.id,d.name,dud.is_primary FROM directory_user_departments dud JOIN directory_departments d ON d.id=dud.department_id WHERE dud.directory_user_id IN (`+placeholders+`) ORDER BY dud.directory_user_id,dud.is_primary DESC,d.name`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var userID int64
		var d DepartmentRef
		if err := rows.Scan(&userID, &d.ID, &d.Name, &d.IsPrimary); err != nil {
			return nil, err
		}
		out[userID] = append(out[userID], d)
	}
	return out, rows.Err()
}

func (r *Repo) userDepartments(ctx context.Context, userID int64) ([]DepartmentRef, error) {
	rows, err := r.DB.QueryContext(ctx, `SELECT d.id,d.name,dud.is_primary FROM directory_user_departments dud JOIN directory_departments d ON d.id=dud.department_id WHERE dud.directory_user_id=? ORDER BY dud.is_primary DESC,d.name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DepartmentRef{}
	for rows.Next() {
		var d DepartmentRef
		if err := rows.Scan(&d.ID, &d.Name, &d.IsPrimary); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *Repo) Stats(ctx context.Context) (*Stats, error) {
	var out Stats
	err := r.DB.QueryRowContext(ctx, `SELECT
	 (SELECT COUNT(*) FROM directory_departments),
	 (SELECT COUNT(*) FROM directory_departments WHERE is_active=1),
	 (SELECT COUNT(*) FROM directory_users),
	 (SELECT COUNT(*) FROM directory_users WHERE is_active=1),
	 (SELECT COUNT(*) FROM directory_users WHERE is_resigned=1),
	 (SELECT COUNT(*) FROM feishu_identities WHERE open_id IS NOT NULL AND open_id<>''),
	 (SELECT COUNT(DISTINCT du.id) FROM directory_users du JOIN feishu_identities fi ON fi.user_id=du.local_user_id AND fi.open_id=du.open_id WHERE du.is_active=1)`).Scan(&out.DepartmentsTotal, &out.DepartmentsActive, &out.UsersTotal, &out.UsersActive, &out.UsersResigned, &out.OAuthUsers, &out.LinkedDirectoryUsers)
	return &out, err
}

func (r *Repo) GetSyncConfig(ctx context.Context) (*SyncConfig, error) {
	row := r.DB.QueryRowContext(ctx, `SELECT enabled,schedule_type,interval_minutes,daily_time,timezone,next_run_at,last_run_at,last_success_at,directory_version,updated_at FROM directory_sync_configs WHERE id=1`)
	var c SyncConfig
	var next, last, success sql.NullTime
	if err := row.Scan(&c.Enabled, &c.ScheduleType, &c.IntervalMinutes, &c.DailyTime, &c.Timezone, &next, &last, &success, &c.DirectoryVersion, &c.UpdatedAt); err != nil {
		return nil, err
	}
	c.NextRunAt = nullTimePtr(next)
	c.LastRunAt = nullTimePtr(last)
	c.LastSuccessAt = nullTimePtr(success)
	return &c, nil
}

func (r *Repo) UpdateSyncConfig(ctx context.Context, c SyncConfig, updatedBy int64, next time.Time) error {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var before SyncConfig
	var beforeNext, beforeLast, beforeSuccess sql.NullTime
	if err = tx.QueryRowContext(ctx, `SELECT enabled,schedule_type,interval_minutes,daily_time,timezone,next_run_at,last_run_at,last_success_at,directory_version,updated_at FROM directory_sync_configs WHERE id=1 FOR UPDATE`).Scan(&before.Enabled, &before.ScheduleType, &before.IntervalMinutes, &before.DailyTime, &before.Timezone, &beforeNext, &beforeLast, &beforeSuccess, &before.DirectoryVersion, &before.UpdatedAt); err != nil {
		return err
	}
	before.NextRunAt = nullTimePtr(beforeNext)
	before.LastRunAt = nullTimePtr(beforeLast)
	before.LastSuccessAt = nullTimePtr(beforeSuccess)
	if _, err = tx.ExecContext(ctx, `UPDATE directory_sync_configs SET enabled=?,schedule_type=?,interval_minutes=?,daily_time=?,timezone=?,next_run_at=?,updated_by=? WHERE id=1`, c.Enabled, c.ScheduleType, c.IntervalMinutes, c.DailyTime, c.Timezone, next, updatedBy); err != nil {
		return err
	}
	after := c
	after.NextRunAt = &next
	detail, err := json.Marshal(map[string]any{"before": before, "after": after})
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO audit_logs(user_id,action,resource,resource_id,detail) VALUES(?,'directory.sync_config.update','enterprise','1',?)`, updatedBy, detail); err != nil {
		return err
	}
	return tx.Commit()
}
func (r *Repo) CreateSyncRun(ctx context.Context, trigger string, createdBy *int64) (*SyncRun, error) {
	return r.CreateTargetRun(ctx, TargetDirectory, trigger, createdBy, nil, "pending")
}
func (r *Repo) GetSyncRun(ctx context.Context, id int64) (*SyncRun, error) {
	row := r.DB.QueryRowContext(ctx, syncRunSelect+` WHERE id=?`, id)
	return scanSyncRun(row)
}
func (r *Repo) ListSyncRuns(ctx context.Context, limit int) ([]SyncRun, error) {
	return r.ListTargetRuns(ctx, "", limit)
}

func (r *Repo) RecoverStaleRuns(ctx context.Context) error {
	_, err := r.DB.ExecContext(ctx, `UPDATE directory_sync_runs SET status='failed',error_code='stale_running_recovered',error_message='scheduler recovered a stale running sync job',finished_at=CURRENT_TIMESTAMP(3) WHERE status='running' AND started_at<DATE_SUB(CURRENT_TIMESTAMP(3),INTERVAL 24 HOUR)`)
	return err
}

func (r *Repo) PeekPendingRun(ctx context.Context) (*SyncRun, error) {
	var id int64
	err := r.DB.QueryRowContext(ctx, `SELECT id FROM directory_sync_runs WHERE status='pending' ORDER BY id LIMIT 1`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return r.GetSyncRun(ctx, id)
}

func (r *Repo) ClaimPendingRunID(ctx context.Context, id int64) (*SyncRun, error) {
	res, err := r.DB.ExecContext(ctx, `UPDATE directory_sync_runs SET status='running',started_at=CURRENT_TIMESTAMP(3),error_code='',error_message='' WHERE id=? AND status='pending'`, id)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, nil
	}
	return r.GetSyncRun(ctx, id)
}

func (r *Repo) ClaimPendingRun(ctx context.Context) (*SyncRun, error) {
	run, err := r.PeekPendingRun(ctx)
	if err != nil || run == nil {
		return run, err
	}
	return r.ClaimPendingRunID(ctx, run.ID)
}

func (r *Repo) FinishRun(ctx context.Context, id int64, status string, dc, uc, auc, mc, amc int, code, msg string) error {
	res, err := r.DB.ExecContext(ctx, `UPDATE directory_sync_runs SET status=?,departments_count=?,users_count=?,active_users_count=?,memberships_count=?,active_memberships_count=?,error_code=?,error_message=?,finished_at=CURRENT_TIMESTAMP(3) WHERE id=? AND status='running'`, status, dc, uc, auc, mc, amc, code, left(msg, 1000), id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("directory sync run %d is no longer running", id)
	}
	return nil
}
func (r *Repo) AcquireLease(ctx context.Context, owner string, d time.Duration) (bool, error) {
	res, err := r.DB.ExecContext(ctx, `UPDATE directory_sync_configs SET lease_owner=?,lease_until=DATE_ADD(CURRENT_TIMESTAMP(3),INTERVAL ? SECOND) WHERE id=1 AND (lease_until IS NULL OR lease_until<CURRENT_TIMESTAMP(3) OR lease_owner=?)`, owner, int(d.Seconds()), owner)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}
func (r *Repo) RenewLease(ctx context.Context, owner string, d time.Duration) error {
	res, err := r.DB.ExecContext(ctx, `UPDATE directory_sync_configs SET lease_until=DATE_ADD(CURRENT_TIMESTAMP(3),INTERVAL ? SECOND) WHERE id=1 AND lease_owner=? AND lease_until>=CURRENT_TIMESTAMP(3)`, int(d.Seconds()), owner)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrLeaseLost
	}
	return nil
}
func (r *Repo) ReleaseLease(ctx context.Context, owner string) {
	_, _ = r.DB.ExecContext(ctx, `UPDATE directory_sync_configs SET lease_owner=NULL,lease_until=NULL WHERE id=1 AND lease_owner=?`, owner)
}
func (r *Repo) MarkScheduleStarted(ctx context.Context, next time.Time) error {
	_, err := r.DB.ExecContext(ctx, `UPDATE directory_sync_configs SET last_run_at=CURRENT_TIMESTAMP(3),next_run_at=? WHERE id=1`, next)
	return err
}
func (r *Repo) ValidateSnapshotSize(ctx context.Context, departments, totalUsers, totalMemberships, activeUsers, activeMemberships int) error {
	var prevDepartments, prevActiveUsers, prevActiveMemberships int
	err := r.DB.QueryRowContext(ctx, `SELECT departments_count,active_users_count,active_memberships_count FROM directory_sync_runs WHERE status='success' AND target_code IN ('directory','legacy_full') ORDER BY (target_code='directory') DESC,id DESC LIMIT 1`).Scan(&prevDepartments, &prevActiveUsers, &prevActiveMemberships)
	if errors.Is(err, sql.ErrNoRows) {
		err = r.DB.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM directory_departments),(SELECT COUNT(*) FROM directory_users WHERE is_active=1),(SELECT COUNT(*) FROM directory_user_departments dud JOIN directory_users du ON du.id=dud.directory_user_id WHERE du.is_active=1)`).Scan(&prevDepartments, &prevActiveUsers, &prevActiveMemberships)
		if err != nil {
			return err
		}
	}
	if err != nil {
		return err
	}
	return validateSnapshotShrink(prevDepartments, prevActiveUsers, prevActiveMemberships, departments, activeUsers, activeMemberships)
}

func validateSnapshotShrink(prevDepartments, prevUsers, prevMemberships, departments, users, memberships int) error {
	type metric struct {
		name       string
		prev, next int
	}
	for _, m := range []metric{{"departments", prevDepartments, departments}, {"users", prevUsers, users}, {"memberships", prevMemberships, memberships}} {
		if m.prev > 0 && m.next*100 < m.prev*70 {
			return fmt.Errorf("suspicious directory shrink: %s %d -> %d (below 70%%); refusing publication", m.name, m.prev, m.next)
		}
	}
	return nil
}

func (r *Repo) ValidateGroupSnapshotSize(ctx context.Context, groups, members int) error {
	var liveGroups, liveMembers int
	if err := r.DB.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM access_groups WHERE source_type='feishu' AND enabled=1 AND sync_status<>'missing'),(SELECT COUNT(*) FROM access_group_external_members em JOIN access_groups g ON g.id=em.group_id WHERE g.source_type='feishu' AND g.enabled=1 AND g.sync_status<>'missing')`).Scan(&liveGroups, &liveMembers); err != nil {
		return err
	}
	if groups == 0 && (liveGroups > 0 || liveMembers > 0) {
		return fmt.Errorf("empty Feishu group snapshot would replace live groups=%d members=%d; refusing publication", liveGroups, liveMembers)
	}
	var prevGroups, prevMembers int
	err := r.DB.QueryRowContext(ctx, `SELECT groups_count,group_members_count FROM directory_sync_runs WHERE status='success' AND target_code IN ('user_groups','legacy_full') ORDER BY (target_code='user_groups') DESC,id DESC LIMIT 1`).Scan(&prevGroups, &prevMembers)
	if errors.Is(err, sql.ErrNoRows) {
		prevGroups, prevMembers = liveGroups, liveMembers
	} else if err != nil {
		return err
	}
	for _, metric := range []struct {
		name       string
		prev, next int
	}{{"groups", prevGroups, groups}, {"group_members", prevMembers, members}} {
		if metric.prev > 0 && metric.next*100 < metric.prev*70 {
			return fmt.Errorf("suspicious user-group shrink: %s %d -> %d (below 70%%); refusing publication", metric.name, metric.prev, metric.next)
		}
	}
	return nil
}

func (r *Repo) ValidatePerGroupMembershipShrink(ctx context.Context, groups []stagedUserGroup, members []stagedUserGroupMember) error {
	next := make(map[string]int, len(groups))
	for _, group := range groups {
		next[group.ExternalID] = 0
	}
	for _, member := range members {
		next[member.ExternalGroupID]++
	}
	rows, err := r.DB.QueryContext(ctx, `SELECT g.external_group_id,COUNT(em.directory_user_id) FROM access_groups g LEFT JOIN access_group_external_members em ON em.group_id=g.id WHERE g.source_type='feishu' AND g.sync_status<>'missing' GROUP BY g.id,g.external_group_id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	previous := map[string]int{}
	for rows.Next() {
		var externalID string
		var count int
		if err = rows.Scan(&externalID, &count); err != nil {
			return err
		}
		previous[externalID] = count
	}
	if err = rows.Err(); err != nil {
		return err
	}
	return validatePerGroupShrink(previous, next)
}
func validatePerGroupShrink(previous, next map[string]int) error {
	for externalID, oldCount := range previous {
		current, present := next[externalID]
		if !present {
			continue
		}
		if oldCount > 0 && current*100 < oldCount*70 {
			return fmt.Errorf("suspicious Feishu group shrink: %s %d -> %d (below 70%%); refusing publication", externalID, oldCount, current)
		}
	}
	return nil
}

func (r *Repo) RecordGroupSyncDiagnostics(ctx context.Context, runID int64, groups, members, warnings, mappingErrors, unknownUsers int) error {
	_, err := r.DB.ExecContext(ctx, `UPDATE directory_sync_runs SET groups_count=?,group_members_count=?,group_fetch_warnings=?,member_mapping_errors=?,unknown_users_count=? WHERE id=?`, groups, members, warnings, mappingErrors, unknownUsers, runID)
	return err
}

func (r *Repo) WriteStage(ctx context.Context, runID int64, deps []stagedDepartment, users []stagedUser, groups []stagedUserGroup, groupMembers []stagedUserGroupMember) error {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM directory_sync_department_stage WHERE run_id=?`, runID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM directory_sync_user_stage WHERE run_id=?`, runID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM directory_sync_user_department_stage WHERE run_id=?`, runID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM directory_sync_user_group_member_stage WHERE run_id=?`, runID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM directory_sync_user_group_stage WHERE run_id=?`, runID); err != nil {
		return err
	}
	for _, d := range deps {
		if _, err = tx.ExecContext(ctx, `INSERT INTO directory_sync_department_stage(run_id,open_department_id,name,parent_open_department_id,order_weight) VALUES(?,?,?,?,?)`, runID, d.OpenID, d.Name, d.ParentOpenID, d.OrderWeight); err != nil {
			return err
		}
	}
	for _, u := range users {
		if _, err = tx.ExecContext(ctx, `INSERT INTO directory_sync_user_stage(run_id,open_id,name,avatar_url,active_status,is_resigned) VALUES(?,?,?,?,?,?)`, runID, u.OpenID, u.Name, u.AvatarURL, u.ActiveStatus, u.Resigned); err != nil {
			return err
		}
		for i, dep := range u.Departments {
			if _, err = tx.ExecContext(ctx, `INSERT INTO directory_sync_user_department_stage(run_id,user_open_id,department_open_id,is_primary) VALUES(?,?,?,?)`, runID, u.OpenID, dep, i == 0); err != nil {
				return err
			}
		}
	}
	for _, group := range groups {
		if _, err = tx.ExecContext(ctx, `INSERT INTO directory_sync_user_group_stage(run_id,external_group_id,name,description,group_type) VALUES(?,?,?,?,?)`, runID, group.ExternalID, group.Name, group.Description, group.GroupType); err != nil {
			return err
		}
	}
	for _, member := range groupMembers {
		if _, err = tx.ExecContext(ctx, `INSERT INTO directory_sync_user_group_member_stage(run_id,external_group_id,external_user_id) VALUES(?,?,?)`, runID, member.ExternalGroupID, member.ExternalUserID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *Repo) CleanupStage(ctx context.Context, runID int64) error {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, table := range []string{"directory_sync_user_group_member_stage", "directory_sync_user_group_stage", "directory_sync_user_department_stage", "directory_sync_user_stage", "directory_sync_department_stage"} {
		if _, err = tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE run_id=?", runID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *Repo) Publish(ctx context.Context, runID int64, deps []stagedDepartment, users []stagedUser, edges []ClosureEdge, leaseOwner string, activeUsers, activeMemberships int) error {
	return r.PublishWithGroups(ctx, runID, deps, users, nil, nil, edges, leaseOwner, activeUsers, activeMemberships)
}

func (r *Repo) PublishWithGroups(ctx context.Context, runID int64, deps []stagedDepartment, users []stagedUser, groups []stagedUserGroup, groupMembers []stagedUserGroupMember, edges []ClosureEdge, leaseOwner string, activeUsers, activeMemberships int) error {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if leaseOwner != "" {
		var owner sql.NullString
		var until sql.NullTime
		var now time.Time
		if err = tx.QueryRowContext(ctx, `SELECT lease_owner,lease_until,CURRENT_TIMESTAMP(3) FROM directory_sync_configs WHERE id=1 FOR UPDATE`).Scan(&owner, &until, &now); err != nil {
			return err
		}
		if !owner.Valid || owner.String != leaseOwner || !until.Valid || until.Time.Before(now) {
			return ErrLeaseLost
		}
	}
	// Set-based publication keeps the live-snapshot transaction bounded even
	// for 20k+ employees. Per-row round trips previously exceeded the driver's
	// read timeout while holding the transaction open.
	if _, err = tx.ExecContext(ctx, `INSERT INTO directory_departments(open_department_id,name,parent_open_department_id,order_weight,is_active,sync_generation,last_synced_at)
		SELECT open_department_id,name,parent_open_department_id,order_weight,1,run_id,CURRENT_TIMESTAMP(3)
		FROM directory_sync_department_stage WHERE run_id=?
		ON DUPLICATE KEY UPDATE name=VALUES(name),parent_open_department_id=VALUES(parent_open_department_id),order_weight=VALUES(order_weight),is_active=1,sync_generation=VALUES(sync_generation),last_synced_at=VALUES(last_synced_at)`, runID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE directory_departments child LEFT JOIN directory_departments parent ON parent.open_department_id=child.parent_open_department_id SET child.parent_id=CASE WHEN child.parent_open_department_id IN ('','0') THEN NULL ELSE parent.id END WHERE child.sync_generation=?`, runID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE directory_departments SET is_active=0 WHERE sync_generation IS NULL OR sync_generation<>?`, runID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO directory_users(open_id,name,avatar_url,active_status,is_resigned,is_active,sync_generation,last_synced_at)
		SELECT open_id,name,avatar_url,active_status,is_resigned,(active_status=2 AND is_resigned=0),run_id,CURRENT_TIMESTAMP(3)
		FROM directory_sync_user_stage WHERE run_id=?
		ON DUPLICATE KEY UPDATE name=VALUES(name),avatar_url=VALUES(avatar_url),active_status=VALUES(active_status),is_resigned=VALUES(is_resigned),is_active=VALUES(is_active),sync_generation=VALUES(sync_generation),last_synced_at=VALUES(last_synced_at)`, runID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE directory_users du JOIN feishu_identities fi ON fi.open_id=du.open_id SET du.local_user_id=fi.user_id WHERE du.sync_generation=?`, runID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE directory_users SET is_active=0 WHERE sync_generation IS NULL OR sync_generation<>?`, runID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM directory_user_departments`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO directory_user_departments(directory_user_id,department_id,is_primary) SELECT u.id,d.id,s.is_primary FROM directory_sync_user_department_stage s JOIN directory_users u ON u.open_id=s.user_open_id JOIN directory_departments d ON d.open_department_id=s.department_open_id WHERE s.run_id=?`, runID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM directory_department_closure`); err != nil {
		return err
	}
	if err = insertClosureEdgesTx(ctx, tx, edges); err != nil {
		return err
	}
	var codeCollisions int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM directory_sync_user_group_stage s JOIN access_groups g ON g.code=CONCAT('feishu_',SHA1(s.external_group_id)) WHERE s.run_id=? AND (g.source_type<>'feishu' OR g.external_group_id<>s.external_group_id)`, runID).Scan(&codeCollisions); err != nil {
		return err
	}
	if codeCollisions > 0 {
		return fmt.Errorf("Feishu group code namespace collision: %d", codeCollisions)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO access_groups(code,name,description,source_type,external_group_id,external_group_type,enabled,sync_status,sync_generation,last_synced_at) SELECT CONCAT('feishu_',SHA1(external_group_id)),name,description,'feishu',external_group_id,group_type,1,'synced',run_id,CURRENT_TIMESTAMP(3) FROM directory_sync_user_group_stage WHERE run_id=? ON DUPLICATE KEY UPDATE name=VALUES(name),description=VALUES(description),external_group_type=VALUES(external_group_type),sync_status='synced',sync_generation=VALUES(sync_generation),last_synced_at=VALUES(last_synced_at)`, runID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE access_groups SET enabled=0,sync_status='missing' WHERE source_type='feishu' AND (sync_generation IS NULL OR sync_generation<>?)`, runID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE em FROM access_group_external_members em JOIN access_groups g ON g.id=em.group_id WHERE g.source_type='feishu'`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO access_group_external_members(group_id,directory_user_id,external_user_id,synced_at) SELECT g.id,u.id,m.external_user_id,CURRENT_TIMESTAMP(3) FROM directory_sync_user_group_member_stage m JOIN access_groups g ON g.source_type='feishu' AND g.external_group_id=m.external_group_id JOIN directory_users u ON u.open_id=m.external_user_id WHERE m.run_id=?`, runID); err != nil {
		return err
	}
	var publishedGroups, publishedMembers int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM access_groups WHERE source_type='feishu' AND sync_generation=?`, runID).Scan(&publishedGroups); err != nil {
		return err
	}
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM access_group_external_members em JOIN access_groups g ON g.id=em.group_id WHERE g.source_type='feishu' AND g.sync_generation=?`, runID).Scan(&publishedMembers); err != nil {
		return err
	}
	if publishedGroups != len(groups) || publishedMembers != len(groupMembers) {
		return fmt.Errorf("published user-group snapshot mismatch: groups %d/%d members %d/%d", publishedGroups, len(groups), publishedMembers, len(groupMembers))
	}
	memberships := 0
	for _, u := range users {
		memberships += len(u.Departments)
	}
	result, err := tx.ExecContext(ctx, `UPDATE directory_sync_runs SET status='success',departments_count=?,users_count=?,active_users_count=?,memberships_count=?,active_memberships_count=?,groups_count=?,group_members_count=?,group_fetch_warnings=0,member_mapping_errors=0,unknown_users_count=0,error_code='',error_message='',finished_at=CURRENT_TIMESTAMP(3) WHERE id=? AND status='running'`, len(deps), len(users), activeUsers, memberships, activeMemberships, len(groups), len(groupMembers), runID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return fmt.Errorf("directory sync run %d is no longer running", runID)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE directory_sync_configs SET last_success_at=CURRENT_TIMESTAMP(3),directory_version=directory_version+1 WHERE id=1`); err != nil {
		return err
	}
	detail, err := json.Marshal(map[string]any{"departments": len(deps), "users": len(users), "active_users": activeUsers, "memberships": memberships, "active_memberships": activeMemberships, "groups": len(groups), "group_members": len(groupMembers)})
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO audit_logs(user_id,action,resource,resource_id,detail) SELECT created_by,'directory.sync.success','enterprise',?,? FROM directory_sync_runs WHERE id=?`, strconv.FormatInt(runID, 10), detail, runID); err != nil {
		return err
	}
	return tx.Commit()
}

func insertClosureEdgesTx(ctx context.Context, tx *sql.Tx, edges []ClosureEdge) error {
	const batch = 300
	for start := 0; start < len(edges); start += batch {
		end := start + batch
		if end > len(edges) {
			end = len(edges)
		}
		var b strings.Builder
		b.WriteString(`INSERT INTO directory_department_closure(ancestor_id,descendant_id,depth) SELECT a.id,d.id,e.depth FROM (`)
		args := make([]any, 0, (end-start)*3)
		for i, e := range edges[start:end] {
			if i > 0 {
				b.WriteString(` UNION ALL `)
			}
			if i == 0 {
				b.WriteString(`SELECT ? AS ancestor_open_id, ? AS descendant_open_id, ? AS depth`)
			} else {
				b.WriteString(`SELECT ?, ?, ?`)
			}
			args = append(args, e.AncestorOpenID, e.DescendantOpenID, e.Depth)
		}
		b.WriteString(`) e JOIN directory_departments a ON a.open_department_id=e.ancestor_open_id JOIN directory_departments d ON d.open_department_id=e.descendant_open_id`)
		if _, err := tx.ExecContext(ctx, b.String(), args...); err != nil {
			return err
		}
	}
	return nil
}

func WriteAudit(ctx context.Context, db *sql.DB, userID *int64, action, resourceID string, detail any) error {
	raw, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	var uid any
	if userID != nil {
		uid = *userID
	}
	_, err = db.ExecContext(ctx, `INSERT INTO audit_logs(user_id,action,resource,resource_id,detail) VALUES(?,?,'enterprise',?,?)`, uid, action, resourceID, raw)
	return err
}
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
func left(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
func ValidateSyncConfig(c SyncConfig) error {
	if c.ScheduleType != "interval" && c.ScheduleType != "daily" {
		return fmt.Errorf("schedule_type must be interval or daily")
	}
	if c.IntervalMinutes < 15 || c.IntervalMinutes > 10080 {
		return fmt.Errorf("interval_minutes must be between 15 and 10080")
	}
	parts := strings.Split(c.DailyTime, ":")
	if len(parts) != 2 {
		return fmt.Errorf("daily_time must be HH:MM")
	}
	h, e1 := strconv.Atoi(parts[0])
	m, e2 := strconv.Atoi(parts[1])
	if e1 != nil || e2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return fmt.Errorf("daily_time must be HH:MM")
	}
	if _, err := time.LoadLocation(c.Timezone); err != nil {
		return fmt.Errorf("invalid timezone")
	}
	return nil
}

const syncRunSelect = `SELECT id,target_code,batch_id,trigger_type,status,departments_count,users_count,active_users_count,memberships_count,active_memberships_count,groups_count,group_members_count,group_fetch_warnings,member_mapping_errors,unknown_users_count,metrics_json,warnings_json,target_version,started_at,finished_at,error_code,error_message,created_by,created_at FROM directory_sync_runs`

type rowScanner interface{ Scan(...any) error }

func scanSyncRun(row rowScanner) (*SyncRun, error) {
	var v SyncRun
	var batch, by sql.NullInt64
	var st, ft sql.NullTime
	var metricsRaw, warningsRaw []byte
	err := row.Scan(&v.ID, &v.TargetCode, &batch, &v.TriggerType, &v.Status, &v.DepartmentsCount, &v.UsersCount, &v.ActiveUsersCount, &v.MembershipsCount, &v.ActiveMembershipsCount, &v.GroupsCount, &v.GroupMembersCount, &v.GroupFetchWarnings, &v.MemberMappingErrors, &v.UnknownUsersCount, &metricsRaw, &warningsRaw, &v.TargetVersion, &st, &ft, &v.ErrorCode, &v.ErrorMessage, &by, &v.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	v.BatchID = nullInt64Ptr(batch)
	v.CreatedBy = nullInt64Ptr(by)
	v.StartedAt = nullTimePtr(st)
	v.FinishedAt = nullTimePtr(ft)
	v.Metrics = map[string]int64{}
	v.Warnings = []Warning{}
	if len(metricsRaw) > 0 {
		_ = json.Unmarshal(metricsRaw, &v.Metrics)
	}
	if len(warningsRaw) > 0 {
		_ = json.Unmarshal(warningsRaw, &v.Warnings)
	}
	return &v, nil
}

func (r *Repo) GetTargetConfig(ctx context.Context, target string) (*TargetConfig, error) {
	if err := ValidateTarget(target); err != nil {
		return nil, err
	}
	row := r.DB.QueryRowContext(ctx, `SELECT target_code,enabled,schedule_type,interval_minutes,daily_time,timezone,next_run_at,last_run_at,last_success_at,target_version,last_error_code,last_error_message,updated_at FROM sync_target_configs WHERE target_code=?`, target)
	var c TargetConfig
	var next, last, success sql.NullTime
	if err := row.Scan(&c.TargetCode, &c.Enabled, &c.ScheduleType, &c.IntervalMinutes, &c.DailyTime, &c.Timezone, &next, &last, &success, &c.TargetVersion, &c.LastErrorCode, &c.LastErrorMessage, &c.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	c.NextRunAt = nullTimePtr(next)
	c.LastRunAt = nullTimePtr(last)
	c.LastSuccessAt = nullTimePtr(success)
	return &c, nil
}

func (r *Repo) ListTargetViews(ctx context.Context) ([]SyncTargetView, error) {
	defs := TargetDefinitions()
	out := make([]SyncTargetView, 0, len(defs))
	for _, d := range defs {
		c, err := r.GetTargetConfig(ctx, d.Code)
		if err != nil {
			return nil, err
		}
		out = append(out, SyncTargetView{TargetDefinition: d, Config: c})
	}
	return out, nil
}

func (r *Repo) UpdateTargetConfig(ctx context.Context, target string, c TargetConfig, updatedBy int64, next time.Time) error {
	if err := ValidateTarget(target); err != nil {
		return err
	}
	sc := SyncConfig{Enabled: c.Enabled, ScheduleType: c.ScheduleType, IntervalMinutes: c.IntervalMinutes, DailyTime: c.DailyTime, Timezone: c.Timezone}
	if err := ValidateSyncConfig(sc); err != nil {
		return err
	}
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var before TargetConfig
	var beforeNext, beforeLast, beforeSuccess sql.NullTime
	if err = tx.QueryRowContext(ctx, `SELECT target_code,enabled,schedule_type,interval_minutes,daily_time,timezone,next_run_at,last_run_at,last_success_at,target_version,last_error_code,last_error_message,updated_at FROM sync_target_configs WHERE target_code=? FOR UPDATE`, target).Scan(&before.TargetCode, &before.Enabled, &before.ScheduleType, &before.IntervalMinutes, &before.DailyTime, &before.Timezone, &beforeNext, &beforeLast, &beforeSuccess, &before.TargetVersion, &before.LastErrorCode, &before.LastErrorMessage, &before.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	before.NextRunAt = nullTimePtr(beforeNext)
	before.LastRunAt = nullTimePtr(beforeLast)
	before.LastSuccessAt = nullTimePtr(beforeSuccess)
	if _, err = tx.ExecContext(ctx, `UPDATE sync_target_configs SET enabled=?,schedule_type=?,interval_minutes=?,daily_time=?,timezone=?,next_run_at=?,updated_by=? WHERE target_code=?`, c.Enabled, c.ScheduleType, c.IntervalMinutes, c.DailyTime, c.Timezone, next, updatedBy, target); err != nil {
		return err
	}
	after := c
	after.NextRunAt = &next
	detail, err := json.Marshal(map[string]any{"before": before, "after": after})
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO audit_logs(user_id,action,resource,resource_id,detail) VALUES(?,'sync.target_config.update','enterprise',?,?)`, updatedBy, target, detail); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repo) CreateTargetRun(ctx context.Context, target, trigger string, createdBy *int64, batchID *int64, status string) (*SyncRun, error) {
	if err := ValidateTarget(target); err != nil {
		return nil, err
	}
	if status == "" {
		status = "pending"
	}
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if batchID == nil && (status == "pending" || status == "running") {
		var active int64
		err = tx.QueryRowContext(ctx, `SELECT id FROM directory_sync_runs WHERE (target_code=? OR (target_code='legacy_full' AND ?='directory')) AND status IN ('pending','blocked','running') ORDER BY id LIMIT 1 FOR UPDATE`, target, target).Scan(&active)
		if err == nil {
			return nil, ErrActiveJobExists
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	var by, batch any
	if createdBy != nil {
		by = *createdBy
	}
	if batchID != nil {
		batch = *batchID
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO directory_sync_runs(target_code,batch_id,trigger_type,status,created_by) VALUES(?,?,?,?,?)`, target, batch, trigger, status, by)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	detail, _ := json.Marshal(map[string]any{"run_id": id, "target_code": target, "batch_id": batchID, "trigger_type": trigger})
	if _, err = tx.ExecContext(ctx, `INSERT INTO audit_logs(user_id,action,resource,resource_id,detail) VALUES(?,'sync.job.create','enterprise',?,?)`, by, strconv.FormatInt(id, 10), detail); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetSyncRun(ctx, id)
}

func (r *Repo) ListTargetRuns(ctx context.Context, target string, limit int) ([]SyncRun, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	query := syncRunSelect
	args := []any{}
	if target != "" {
		if err := ValidateTarget(target); err != nil {
			return nil, err
		}
		query += ` WHERE target_code=?`
		args = append(args, target)
	}
	query += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := r.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SyncRun{}
	for rows.Next() {
		v, err := scanSyncRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

func (r *Repo) ListLegacySyncRuns(ctx context.Context, limit int) ([]SyncRun, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	rows, err := r.DB.QueryContext(ctx, syncRunSelect+` WHERE target_code IN ('directory','legacy_full') ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SyncRun{}
	for rows.Next() {
		v, e := scanSyncRun(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

func (r *Repo) CreateBatch(ctx context.Context, targets []string, createdBy *int64) (*SyncBatch, error) {
	ordered, err := OrderedTargets(targets)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(ordered)
	if err != nil {
		return nil, err
	}
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var by any
	if createdBy != nil {
		by = *createdBy
	}
	for _, target := range ordered {
		var active int64
		err = tx.QueryRowContext(ctx, `SELECT id FROM directory_sync_runs WHERE (target_code=? OR (target_code='legacy_full' AND ?='directory')) AND status IN ('pending','blocked','running') ORDER BY id LIMIT 1 FOR UPDATE`, target, target).Scan(&active)
		if err == nil {
			return nil, fmt.Errorf("%w: %s", ErrActiveJobExists, target)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO sync_batches(trigger_type,status,requested_targets,created_by) VALUES('manual','pending',?,?)`, raw, by)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	hasDirectory := false
	for _, target := range ordered {
		if target == TargetDirectory {
			hasDirectory = true
		}
	}
	jobIDs := []int64{}
	for _, target := range ordered {
		status := "pending"
		if target == TargetUserGroups && hasDirectory {
			status = "blocked"
		}
		res, err = tx.ExecContext(ctx, `INSERT INTO directory_sync_runs(target_code,batch_id,trigger_type,status,created_by) VALUES(?,?,'manual',?,?)`, target, id, status, by)
		if err != nil {
			return nil, err
		}
		jobID, _ := res.LastInsertId()
		jobIDs = append(jobIDs, jobID)
		jobDetail, _ := json.Marshal(map[string]any{"run_id": jobID, "target_code": target, "batch_id": id, "trigger_type": "manual"})
		if _, err = tx.ExecContext(ctx, `INSERT INTO audit_logs(user_id,action,resource,resource_id,detail) VALUES(?,'sync.job.create','enterprise',?,?)`, by, strconv.FormatInt(jobID, 10), jobDetail); err != nil {
			return nil, err
		}
	}
	detail, _ := json.Marshal(map[string]any{"batch_id": id, "targets": ordered, "job_ids": jobIDs})
	if _, err = tx.ExecContext(ctx, `INSERT INTO audit_logs(user_id,action,resource,resource_id,detail) VALUES(?,'sync.batch.create','enterprise',?,?)`, by, strconv.FormatInt(id, 10), detail); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetBatch(ctx, id)
}

func (r *Repo) GetBatch(ctx context.Context, id int64) (*SyncBatch, error) {
	row := r.DB.QueryRowContext(ctx, `SELECT id,trigger_type,status,requested_targets,created_by,created_at,finished_at FROM sync_batches WHERE id=?`, id)
	var b SyncBatch
	var raw []byte
	var by sql.NullInt64
	var ft sql.NullTime
	if err := row.Scan(&b.ID, &b.TriggerType, &b.Status, &raw, &by, &b.CreatedAt, &ft); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(raw, &b.RequestedTargets)
	b.CreatedBy = nullInt64Ptr(by)
	b.FinishedAt = nullTimePtr(ft)
	rows, err := r.DB.QueryContext(ctx, syncRunSelect+` WHERE batch_id=? ORDER BY id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		v, e := scanSyncRun(rows)
		if e != nil {
			return nil, e
		}
		b.Jobs = append(b.Jobs, *v)
	}
	return &b, rows.Err()
}

func (r *Repo) ReconcileBatch(ctx context.Context, batchID *int64) error {
	if batchID == nil {
		return nil
	}
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var directoryStatus string
	directoryErr := tx.QueryRowContext(ctx, `SELECT status FROM directory_sync_runs WHERE batch_id=? AND target_code='directory'`, *batchID).Scan(&directoryStatus)
	if directoryErr != nil && !errors.Is(directoryErr, sql.ErrNoRows) {
		return directoryErr
	}
	if errors.Is(directoryErr, sql.ErrNoRows) {
		if _, err = tx.ExecContext(ctx, `UPDATE directory_sync_runs SET status='failed',error_code='dependency_missing',error_message='required directory job is missing from batch',finished_at=CURRENT_TIMESTAMP(3) WHERE batch_id=? AND target_code='user_groups' AND status='blocked'`, *batchID); err != nil {
			return err
		}
	}
	if directoryStatus == "success" {
		_, err = tx.ExecContext(ctx, `UPDATE directory_sync_runs SET status='pending' WHERE batch_id=? AND target_code='user_groups' AND status='blocked'`, *batchID)
		if err != nil {
			return err
		}
	}
	if directoryStatus == "failed" {
		_, err = tx.ExecContext(ctx, `UPDATE directory_sync_runs SET status='failed',error_code='dependency_failed',error_message='directory dependency failed',finished_at=CURRENT_TIMESTAMP(3) WHERE batch_id=? AND target_code='user_groups' AND status='blocked'`, *batchID)
		if err != nil {
			return err
		}
	}
	var total, success, failed, running int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*),SUM(status='success'),SUM(status='failed'),SUM(status IN ('pending','blocked','running')) FROM directory_sync_runs WHERE batch_id=?`, *batchID).Scan(&total, &success, &failed, &running); err != nil {
		return err
	}
	status := "running"
	finished := false
	if running == 0 {
		finished = true
		if success == total {
			status = "success"
		} else if success > 0 {
			status = "partial_success"
		} else {
			status = "failed"
		}
	}
	if finished {
		_, err = tx.ExecContext(ctx, `UPDATE sync_batches SET status=?,finished_at=CURRENT_TIMESTAMP(3) WHERE id=?`, status, *batchID)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE sync_batches SET status=? WHERE id=?`, status, *batchID)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repo) ReconcileOpenBatches(ctx context.Context) error {
	rows, err := r.DB.QueryContext(ctx, `SELECT id FROM sync_batches WHERE status IN ('pending','running') ORDER BY id`)
	if err != nil {
		return err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err = rows.Close(); err != nil {
		return err
	}
	for _, id := range ids {
		if err = r.ReconcileBatch(ctx, &id); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repo) EnqueueDueTargetRun(ctx context.Context, target, owner string) (bool, error) {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var cfg TargetConfig
	var due sql.NullTime
	var leaseOwner sql.NullString
	var leaseUntil sql.NullTime
	var dbNow time.Time
	if err = tx.QueryRowContext(ctx, `SELECT enabled,schedule_type,interval_minutes,daily_time,timezone,next_run_at,lease_owner,lease_until,CURRENT_TIMESTAMP(3) FROM sync_target_configs WHERE target_code=? FOR UPDATE`, target).Scan(&cfg.Enabled, &cfg.ScheduleType, &cfg.IntervalMinutes, &cfg.DailyTime, &cfg.Timezone, &due, &leaseOwner, &leaseUntil, &dbNow); err != nil {
		return false, err
	}
	if !cfg.Enabled || (due.Valid && due.Time.After(dbNow)) || !leaseOwner.Valid || leaseOwner.String != owner || !leaseUntil.Valid || leaseUntil.Time.Before(dbNow) {
		return false, nil
	}
	next, err := NextTargetRunAt(cfg, dbNow)
	if err != nil {
		return false, err
	}
	var active int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM directory_sync_runs WHERE (target_code=? OR (target_code='legacy_full' AND ?='directory')) AND status IN ('pending','blocked','running') ORDER BY id LIMIT 1 FOR UPDATE`, target, target).Scan(&active)
	if err == nil {
		if _, err = tx.ExecContext(ctx, `UPDATE sync_target_configs SET last_run_at=CURRENT_TIMESTAMP(3),next_run_at=? WHERE target_code=?`, next, target); err != nil {
			return false, err
		}
		return false, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO directory_sync_runs(target_code,trigger_type,status) VALUES(?,'scheduled','pending')`, target)
	if err != nil {
		return false, err
	}
	id, _ := res.LastInsertId()
	if _, err = tx.ExecContext(ctx, `UPDATE sync_target_configs SET last_run_at=CURRENT_TIMESTAMP(3),next_run_at=? WHERE target_code=?`, next, target); err != nil {
		return false, err
	}
	detail, _ := json.Marshal(map[string]any{"run_id": id, "target_code": target, "trigger_type": "scheduled"})
	if _, err = tx.ExecContext(ctx, `INSERT INTO audit_logs(user_id,action,resource,resource_id,detail) VALUES(NULL,'sync.job.create','enterprise',?,?)`, strconv.FormatInt(id, 10), detail); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func (r *Repo) ListDueTargetConfigs(ctx context.Context, now time.Time) ([]TargetConfig, error) {
	rows, err := r.DB.QueryContext(ctx, `SELECT target_code,enabled,schedule_type,interval_minutes,daily_time,timezone,next_run_at,last_run_at,last_success_at,target_version,last_error_code,last_error_message,updated_at FROM sync_target_configs WHERE enabled=1 AND (next_run_at IS NULL OR next_run_at<=?) ORDER BY target_code`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TargetConfig{}
	for rows.Next() {
		var c TargetConfig
		var next, last, success sql.NullTime
		if err = rows.Scan(&c.TargetCode, &c.Enabled, &c.ScheduleType, &c.IntervalMinutes, &c.DailyTime, &c.Timezone, &next, &last, &success, &c.TargetVersion, &c.LastErrorCode, &c.LastErrorMessage, &c.UpdatedAt); err != nil {
			return nil, err
		}
		c.NextRunAt = nullTimePtr(next)
		c.LastRunAt = nullTimePtr(last)
		c.LastSuccessAt = nullTimePtr(success)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *Repo) AcquireTargetLease(ctx context.Context, target, owner string, d time.Duration) (bool, error) {
	res, err := r.DB.ExecContext(ctx, `UPDATE sync_target_configs SET lease_owner=?,lease_until=DATE_ADD(CURRENT_TIMESTAMP(3),INTERVAL ? SECOND) WHERE target_code=? AND (lease_until IS NULL OR lease_until<CURRENT_TIMESTAMP(3) OR lease_owner=?)`, owner, int(d.Seconds()), target, owner)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}
func (r *Repo) RenewTargetLease(ctx context.Context, target, owner string, d time.Duration) error {
	res, err := r.DB.ExecContext(ctx, `UPDATE sync_target_configs SET lease_until=DATE_ADD(CURRENT_TIMESTAMP(3),INTERVAL ? SECOND) WHERE target_code=? AND lease_owner=? AND lease_until>=CURRENT_TIMESTAMP(3)`, int(d.Seconds()), target, owner)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrLeaseLost
	}
	return nil
}
func (r *Repo) ReleaseTargetLease(ctx context.Context, target, owner string) {
	_, _ = r.DB.ExecContext(ctx, `UPDATE sync_target_configs SET lease_owner=NULL,lease_until=NULL WHERE target_code=? AND lease_owner=?`, target, owner)
}
func (r *Repo) MarkTargetScheduleStarted(ctx context.Context, target string, next time.Time) error {
	_, err := r.DB.ExecContext(ctx, `UPDATE sync_target_configs SET last_run_at=CURRENT_TIMESTAMP(3),next_run_at=? WHERE target_code=?`, next, target)
	return err
}

func (r *Repo) FinishTargetFailure(ctx context.Context, run *SyncRun, code string, errValue error) error {
	msg := left(errValue.Error(), 1000)
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE directory_sync_runs SET status='failed',error_code=?,error_message=?,finished_at=CURRENT_TIMESTAMP(3) WHERE id=? AND status='running'`, code, msg, run.ID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return fmt.Errorf("sync job %d is no longer running", run.ID)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE sync_target_configs SET last_run_at=CURRENT_TIMESTAMP(3),last_error_code=?,last_error_message=? WHERE target_code=?`, code, msg, run.TargetCode); err != nil {
		return err
	}
	detail, _ := json.Marshal(map[string]any{"target_code": run.TargetCode, "error_code": code, "error": msg})
	var by any
	if run.CreatedBy != nil {
		by = *run.CreatedBy
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO audit_logs(user_id,action,resource,resource_id,detail) VALUES(?,'sync.target.failed','enterprise',?,?)`, by, strconv.FormatInt(run.ID, 10), detail); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return r.ReconcileBatch(ctx, run.BatchID)
}

func (r *Repo) KnownGroupMembers(ctx context.Context, members []stagedUserGroupMember) ([]stagedUserGroupMember, int, error) {
	known := make(map[string]bool)
	rows, err := r.DB.QueryContext(ctx, `SELECT open_id FROM directory_users WHERE is_active=1`)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, 0, err
		}
		known[id] = true
	}
	if err = rows.Err(); err != nil {
		return nil, 0, err
	}
	out := make([]stagedUserGroupMember, 0, len(members))
	unknown := 0
	for _, m := range members {
		if known[m.ExternalUserID] {
			out = append(out, m)
		} else {
			unknown++
		}
	}
	return out, unknown, nil
}

func (r *Repo) DirectoryVersion(ctx context.Context) (int64, error) {
	var v int64
	err := r.DB.QueryRowContext(ctx, `SELECT target_version FROM sync_target_configs WHERE target_code='directory'`).Scan(&v)
	return v, err
}

func (r *Repo) WriteDirectoryStage(ctx context.Context, runID int64, deps []stagedDepartment, users []stagedUser) error {
	return r.WriteStage(ctx, runID, deps, users, nil, nil)
}
func (r *Repo) WriteGroupStage(ctx context.Context, runID int64, groups []stagedUserGroup, members []stagedUserGroupMember) error {
	return r.WriteStage(ctx, runID, nil, nil, groups, members)
}

func checkTargetLeaseTx(ctx context.Context, tx *sql.Tx, target, owner string) error {
	if owner == "" {
		return nil
	}
	var got sql.NullString
	var until sql.NullTime
	var now time.Time
	if err := tx.QueryRowContext(ctx, `SELECT lease_owner,lease_until,CURRENT_TIMESTAMP(3) FROM sync_target_configs WHERE target_code=? FOR UPDATE`, target).Scan(&got, &until, &now); err != nil {
		return err
	}
	if !got.Valid || got.String != owner || !until.Valid || until.Time.Before(now) {
		return ErrLeaseLost
	}
	return nil
}

func (r *Repo) PublishDirectoryTarget(ctx context.Context, run *SyncRun, deps []stagedDepartment, users []stagedUser, edges []ClosureEdge, leaseOwner string, activeUsers, activeMemberships int) (Result, error) {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return Result{}, err
	}
	defer tx.Rollback()
	if err = checkTargetLeaseTx(ctx, tx, TargetDirectory, leaseOwner); err != nil {
		return Result{}, err
	}
	id := run.ID
	statements := []struct {
		q    string
		args []any
	}{
		{`INSERT INTO directory_departments(open_department_id,name,parent_open_department_id,order_weight,is_active,sync_generation,last_synced_at) SELECT open_department_id,name,parent_open_department_id,order_weight,1,run_id,CURRENT_TIMESTAMP(3) FROM directory_sync_department_stage WHERE run_id=? ON DUPLICATE KEY UPDATE name=VALUES(name),parent_open_department_id=VALUES(parent_open_department_id),order_weight=VALUES(order_weight),is_active=1,sync_generation=VALUES(sync_generation),last_synced_at=VALUES(last_synced_at)`, []any{id}},
		{`UPDATE directory_departments child LEFT JOIN directory_departments parent ON parent.open_department_id=child.parent_open_department_id SET child.parent_id=CASE WHEN child.parent_open_department_id IN ('','0') THEN NULL ELSE parent.id END WHERE child.sync_generation=?`, []any{id}},
		{`UPDATE directory_departments SET is_active=0 WHERE sync_generation IS NULL OR sync_generation<>?`, []any{id}},
		{`INSERT INTO directory_users(open_id,name,avatar_url,active_status,is_resigned,is_active,sync_generation,last_synced_at) SELECT open_id,name,avatar_url,active_status,is_resigned,(active_status=2 AND is_resigned=0),run_id,CURRENT_TIMESTAMP(3) FROM directory_sync_user_stage WHERE run_id=? ON DUPLICATE KEY UPDATE name=VALUES(name),avatar_url=VALUES(avatar_url),active_status=VALUES(active_status),is_resigned=VALUES(is_resigned),is_active=VALUES(is_active),sync_generation=VALUES(sync_generation),last_synced_at=VALUES(last_synced_at)`, []any{id}},
		{`UPDATE directory_users du JOIN feishu_identities fi ON fi.open_id=du.open_id SET du.local_user_id=fi.user_id WHERE du.sync_generation=?`, []any{id}},
		{`UPDATE directory_users SET is_active=0 WHERE sync_generation IS NULL OR sync_generation<>?`, []any{id}},
		{`DELETE FROM directory_user_departments`, nil},
		{`INSERT INTO directory_user_departments(directory_user_id,department_id,is_primary) SELECT u.id,d.id,s.is_primary FROM directory_sync_user_department_stage s JOIN directory_users u ON u.open_id=s.user_open_id JOIN directory_departments d ON d.open_department_id=s.department_open_id WHERE s.run_id=?`, []any{id}},
		{`DELETE FROM directory_department_closure`, nil},
	}
	for _, st := range statements {
		if _, err = tx.ExecContext(ctx, st.q, st.args...); err != nil {
			return Result{}, err
		}
	}
	if err = insertClosureEdgesTx(ctx, tx, edges); err != nil {
		return Result{}, err
	}
	memberships := 0
	for _, u := range users {
		memberships += len(u.Departments)
	}
	result := Result{Metrics: map[string]int64{"departments": int64(len(deps)), "users": int64(len(users)), "active_users": int64(activeUsers), "memberships": int64(memberships), "active_memberships": int64(activeMemberships)}}
	metrics, warnings, _ := encodeResult(result)
	var version int64
	if err = tx.QueryRowContext(ctx, `SELECT target_version+1 FROM sync_target_configs WHERE target_code='directory' FOR UPDATE`).Scan(&version); err != nil {
		return Result{}, err
	}
	result.Version = version
	res, err := tx.ExecContext(ctx, `UPDATE directory_sync_runs SET status='success',departments_count=?,users_count=?,active_users_count=?,memberships_count=?,active_memberships_count=?,metrics_json=?,warnings_json=?,target_version=?,error_code='',error_message='',finished_at=CURRENT_TIMESTAMP(3) WHERE id=? AND status='running'`, len(deps), len(users), activeUsers, memberships, activeMemberships, metrics, warnings, version, id)
	if err != nil {
		return Result{}, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return Result{}, fmt.Errorf("sync job %d is no longer running", id)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE sync_target_configs SET last_run_at=CURRENT_TIMESTAMP(3),last_success_at=CURRENT_TIMESTAMP(3),target_version=?,last_error_code='',last_error_message='' WHERE target_code='directory'`, version); err != nil {
		return Result{}, err
	}
	detail, _ := json.Marshal(map[string]any{"target_code": TargetDirectory, "metrics": result.Metrics, "version": version})
	var by any
	if run.CreatedBy != nil {
		by = *run.CreatedBy
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO audit_logs(user_id,action,resource,resource_id,detail) VALUES(?,'sync.target.success','enterprise',?,?)`, by, strconv.FormatInt(id, 10), detail); err != nil {
		return Result{}, err
	}
	if err = tx.Commit(); err != nil {
		return Result{}, err
	}
	return result, nil
}

func (r *Repo) PublishUserGroupsTarget(ctx context.Context, run *SyncRun, groups []stagedUserGroup, members []stagedUserGroupMember, warningsList []Warning, unknown int, leaseOwner string) (Result, error) {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return Result{}, err
	}
	defer tx.Rollback()
	if err = checkTargetLeaseTx(ctx, tx, TargetUserGroups, leaseOwner); err != nil {
		return Result{}, err
	}
	id := run.ID
	var collisions int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM directory_sync_user_group_stage s JOIN access_groups g ON g.code=CONCAT('feishu_',SHA1(s.external_group_id)) WHERE s.run_id=? AND (g.source_type<>'feishu' OR g.external_group_id<>s.external_group_id)`, id).Scan(&collisions); err != nil {
		return Result{}, err
	}
	if collisions > 0 {
		return Result{}, fmt.Errorf("Feishu group code namespace collision: %d", collisions)
	}
	statements := []struct {
		q    string
		args []any
	}{
		{`INSERT INTO access_groups(code,name,description,source_type,external_group_id,external_group_type,enabled,sync_status,sync_generation,last_synced_at) SELECT CONCAT('feishu_',SHA1(external_group_id)),name,description,'feishu',external_group_id,group_type,1,'synced',run_id,CURRENT_TIMESTAMP(3) FROM directory_sync_user_group_stage WHERE run_id=? ON DUPLICATE KEY UPDATE name=VALUES(name),description=VALUES(description),external_group_type=VALUES(external_group_type),enabled=1,sync_status='synced',sync_generation=VALUES(sync_generation),last_synced_at=VALUES(last_synced_at)`, []any{id}},
		{`UPDATE access_groups SET enabled=0,sync_status='missing' WHERE source_type='feishu' AND (sync_generation IS NULL OR sync_generation<>?)`, []any{id}},
		{`DELETE em FROM access_group_external_members em JOIN access_groups g ON g.id=em.group_id WHERE g.source_type='feishu'`, nil},
		{`INSERT INTO access_group_external_members(group_id,directory_user_id,external_user_id,synced_at) SELECT g.id,u.id,m.external_user_id,CURRENT_TIMESTAMP(3) FROM directory_sync_user_group_member_stage m JOIN access_groups g ON g.source_type='feishu' AND g.external_group_id=m.external_group_id JOIN directory_users u ON u.open_id=m.external_user_id WHERE m.run_id=?`, []any{id}},
	}
	for _, st := range statements {
		if _, err = tx.ExecContext(ctx, st.q, st.args...); err != nil {
			return Result{}, err
		}
	}
	var publishedGroups, publishedMembers int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM access_groups WHERE source_type='feishu' AND sync_generation=?`, id).Scan(&publishedGroups); err != nil {
		return Result{}, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM access_group_external_members em JOIN access_groups g ON g.id=em.group_id WHERE g.source_type='feishu' AND g.sync_generation=?`, id).Scan(&publishedMembers); err != nil {
		return Result{}, err
	}
	if publishedGroups != len(groups) || publishedMembers != len(members) {
		return Result{}, fmt.Errorf("published user-group snapshot mismatch: groups %d/%d members %d/%d", publishedGroups, len(groups), publishedMembers, len(members))
	}
	result := Result{Metrics: map[string]int64{"groups": int64(len(groups)), "group_members": int64(len(members)), "unknown_users": int64(unknown)}, Warnings: warningsList}
	metrics, warningsRaw, _ := encodeResult(result)
	var version int64
	if err = tx.QueryRowContext(ctx, `SELECT target_version+1 FROM sync_target_configs WHERE target_code='user_groups' FOR UPDATE`).Scan(&version); err != nil {
		return Result{}, err
	}
	result.Version = version
	res, err := tx.ExecContext(ctx, `UPDATE directory_sync_runs SET status='success',groups_count=?,group_members_count=?,group_fetch_warnings=?,member_mapping_errors=?,unknown_users_count=?,metrics_json=?,warnings_json=?,target_version=?,error_code='',error_message='',finished_at=CURRENT_TIMESTAMP(3) WHERE id=? AND status='running'`, len(groups), len(members), len(warningsList), unknown, unknown, metrics, warningsRaw, version, id)
	if err != nil {
		return Result{}, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return Result{}, fmt.Errorf("sync job %d is no longer running", id)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE sync_target_configs SET last_run_at=CURRENT_TIMESTAMP(3),last_success_at=CURRENT_TIMESTAMP(3),target_version=?,last_error_code='',last_error_message='' WHERE target_code='user_groups'`, version); err != nil {
		return Result{}, err
	}
	detail, _ := json.Marshal(map[string]any{"target_code": TargetUserGroups, "metrics": result.Metrics, "warnings": warningsList, "version": version})
	var by any
	if run.CreatedBy != nil {
		by = *run.CreatedBy
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO audit_logs(user_id,action,resource,resource_id,detail) VALUES(?,'sync.target.success','enterprise',?,?)`, by, strconv.FormatInt(id, 10), detail); err != nil {
		return Result{}, err
	}
	if err = tx.Commit(); err != nil {
		return Result{}, err
	}
	return result, nil
}
