package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/shilin414/cas/backend-go/internal/accessgroup"
	"github.com/shilin414/cas/backend-go/internal/adminrbac"
	"github.com/shilin414/cas/backend-go/internal/catalog"
	"github.com/shilin414/cas/backend-go/internal/enterpriseaccess"
	"github.com/shilin414/cas/backend-go/internal/platform/config"
	"github.com/shilin414/cas/backend-go/internal/platform/database"
)

func TestEnterpriseRBACAndGroupACL(t *testing.T) {
	if os.Getenv("STUDIO_TEST_DB") != "1" {
		t.Skip("set STUDIO_TEST_DB=1")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	db, err := database.Open(context.Background(), cfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	// Remove only stale fixtures from interrupted runs of this test.
	_, _ = db.ExecContext(ctx, `DELETE FROM application_group_grants WHERE application_id IN (SELECT id FROM applications WHERE name LIKE 'itest RBAC Group %')`)
	_, _ = db.ExecContext(ctx, `DELETE FROM applications WHERE name LIKE 'itest RBAC Group %'`)
	_, _ = db.ExecContext(ctx, `DELETE FROM access_group_external_members WHERE group_id IN (SELECT id FROM access_groups WHERE code LIKE 'itest\_feishu\_%' ESCAPE '\\')`)
	_, _ = db.ExecContext(ctx, `DELETE FROM access_group_users WHERE group_id IN (SELECT id FROM access_groups WHERE code LIKE 'itest\_local\_%' ESCAPE '\\')`)
	_, _ = db.ExecContext(ctx, `DELETE FROM access_groups WHERE code LIKE 'itest\_local\_%' ESCAPE '\\' OR code LIKE 'itest\_feishu\_%' ESCAPE '\\'`)
	_, _ = db.ExecContext(ctx, `DELETE FROM directory_users WHERE open_id LIKE 'ou-itest-rbac-%'`)
	_, _ = db.ExecContext(ctx, `DELETE FROM admin_role_assignments WHERE user_id IN (SELECT id FROM users WHERE username LIKE 'itest-rbac-%')`)
	_, _ = db.ExecContext(ctx, `DELETE FROM users WHERE username LIKE 'itest-rbac-%'`)
	suffix := fmt.Sprint(time.Now().UnixNano())
	userRes, err := db.ExecContext(ctx, `INSERT INTO users(username,password_hash,display_name,display_id,email,role,auth_source,is_staff,is_active) VALUES(?,'',?,'','', 'creator','feishu',0,1)`, "itest-rbac-"+suffix, "ITest RBAC")
	if err != nil {
		t.Fatal(err)
	}
	userID, _ := userRes.LastInsertId()
	duRes, err := db.ExecContext(ctx, `INSERT INTO directory_users(open_id,name,active_status,is_resigned,local_user_id,is_active) VALUES(?,?,2,0,?,1)`, "ou-itest-rbac-"+suffix, "ITest RBAC", userID)
	if err != nil {
		t.Fatal(err)
	}
	directoryUserID, _ := duRes.LastInsertId()
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = db.ExecContext(bg, `DELETE FROM directory_users WHERE id=?`, directoryUserID)
		_, _ = db.ExecContext(bg, `DELETE FROM users WHERE id=?`, userID)
	})
	catalogSvc := &catalog.Service{DB: db, ACLEnabled: true}
	app, _, err := catalogSvc.Create(ctx, &catalog.CreateInput{Name: "itest RBAC Group " + suffix, Kind: "custom", RendererKey: "itest-rbac", CreatorID: userID, IsStaff: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `UPDATE applications SET enabled=1 WHERE id=?`, app.ID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = db.ExecContext(bg, `DELETE FROM application_group_grants WHERE application_id=?`, app.ID)
		_, _ = db.ExecContext(bg, `DELETE FROM audit_logs WHERE resource_id=?`, fmt.Sprint(app.ID))
		_ = catalogSvc.Delete(bg, app.ID, userID, true)
	})
	groupSvc := &accessgroup.Service{DB: db}
	local, err := groupSvc.Create(ctx, userID, accessgroup.Input{Code: "itest_local_" + suffix, Name: "ITest Local", Enabled: true, UserGrants: []int64{directoryUserID}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = db.ExecContext(bg, `DELETE FROM access_group_users WHERE group_id=?`, local.ID)
		_, _ = db.ExecContext(bg, `DELETE FROM access_groups WHERE id=?`, local.ID)
	})
	accessSvc := &enterpriseaccess.Service{DB: db, RBACEnabled: true}
	if _, err = accessSvc.Replace(ctx, app.ID, userID, enterpriseaccess.Update{AccessMode: enterpriseaccess.ModeAssigned, GroupGrants: []int64{local.ID}}); err != nil {
		t.Fatal(err)
	}
	decision, err := accessSvc.ResolveLocalUser(ctx, app.ID, userID, false)
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Allowed || decision.ReasonCode != enterpriseaccess.ReasonLocalGroupUserMatch {
		t.Fatalf("local decision=%#v", decision)
	}

	var auditRaw []byte
	if err = db.QueryRowContext(ctx, `SELECT detail FROM audit_logs WHERE action='application.access.update' AND resource_id=? ORDER BY id DESC LIMIT 1`, fmt.Sprint(app.ID)).Scan(&auditRaw); err != nil {
		t.Fatal(err)
	}
	var auditDetail map[string]any
	if err = json.Unmarshal(auditRaw, &auditDetail); err != nil {
		t.Fatal(err)
	}
	afterAudit, auditOK := auditDetail["after"].(map[string]any)
	if !auditOK || len(afterAudit["group_grants"].([]any)) != 1 {
		t.Fatalf("policy audit detail=%s", auditRaw)
	}
	feishuRes, err := db.ExecContext(ctx, `INSERT INTO access_groups(code,name,source_type,external_group_id,external_group_type,enabled,sync_status) VALUES(?,?,'feishu',?,'dynamic',1,'synced')`, "itest_feishu_"+suffix, "ITest Feishu", "g-"+suffix)
	if err != nil {
		t.Fatal(err)
	}
	feishuID, _ := feishuRes.LastInsertId()
	if _, err = db.ExecContext(ctx, `INSERT INTO access_group_external_members(group_id,directory_user_id,external_user_id,synced_at) VALUES(?,?,?,CURRENT_TIMESTAMP(3))`, feishuID, directoryUserID, "ou-itest-rbac-"+suffix); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = db.ExecContext(bg, `DELETE FROM access_group_external_members WHERE group_id=?`, feishuID)
		_, _ = db.ExecContext(bg, `DELETE FROM access_groups WHERE id=?`, feishuID)
	})
	if _, err = accessSvc.Replace(ctx, app.ID, userID, enterpriseaccess.Update{AccessMode: enterpriseaccess.ModeAssigned, GroupGrants: []int64{feishuID}}); err != nil {
		t.Fatal(err)
	}
	decision, err = accessSvc.ResolveLocalUser(ctx, app.ID, userID, false)
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Allowed || decision.ReasonCode != enterpriseaccess.ReasonFeishuGroupMemberMatch {
		t.Fatalf("feishu decision=%#v", decision)
	}
	var ownerRoleID int64
	if err = db.QueryRowContext(ctx, `SELECT id FROM admin_roles WHERE code='platform_owner'`).Scan(&ownerRoleID); err != nil {
		t.Fatal(err)
	}
	rbac := &adminrbac.Service{DB: db, Enabled: true}
	if _, err = rbac.ReplaceAdministratorRoles(ctx, userID, userID, []int64{ownerRoleID}, nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM admin_role_assignments WHERE user_id=?`, userID)
	})
	ok, err := rbac.HasPermission(ctx, userID, false, adminrbac.PermissionAccessPolicyManage)
	if err != nil || !ok {
		t.Fatalf("rbac permission=%v err=%v", ok, err)
	}
	if _, err = accessSvc.Replace(ctx, app.ID, userID, enterpriseaccess.Update{AccessMode: enterpriseaccess.ModeAdminOnly}); err != nil {
		t.Fatal(err)
	}
	decision, err = accessSvc.ResolveLocalUser(ctx, app.ID, userID, false)
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Allowed || decision.ReasonCode != enterpriseaccess.ReasonRBACBypass {
		t.Fatalf("rbac bypass decision=%#v", decision)
	}
	runtimeRepo := &catalog.Repo{DB: db, ACLEnabled: true, RBACEnabled: true}
	runtimeAllowed, err := runtimeRepo.AccessAllowed(ctx, app.ID, userID, false)
	if err != nil || !runtimeAllowed {
		t.Fatalf("runtime SQL rbac bypass=%v err=%v", runtimeAllowed, err)
	}
	flagOffRepo := &catalog.Repo{DB: db, ACLEnabled: true, RBACEnabled: false}
	flagOffAllowed, err := flagOffRepo.AccessAllowed(ctx, app.ID, userID, false)
	if err != nil || flagOffAllowed {
		t.Fatalf("rbac flag-off runtime access=%v err=%v", flagOffAllowed, err)
	}
	if _, err = accessSvc.Replace(ctx, app.ID, userID, enterpriseaccess.Update{AccessMode: enterpriseaccess.ModeAll}); err != nil {
		t.Fatal(err)
	}
	unlinkedRes, err := db.ExecContext(ctx, `INSERT INTO users(username,password_hash,display_name,display_id,email,role,auth_source,is_staff,is_active) VALUES(?,'',?,'','', 'creator','local',0,1)`, "itest-rbac-unlinked-"+suffix, "ITest Unlinked")
	if err != nil {
		t.Fatal(err)
	}
	unlinkedID, _ := unlinkedRes.LastInsertId()
	t.Cleanup(func() { _, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id=?`, unlinkedID) })
	page, err := runtimeRepo.ListApplicationPage(ctx, catalog.ApplicationPageQuery{Scope: "accessible", Mode: catalog.PageModeConsume, Kind: catalog.PageQueryKindFixed, Search: suffix, Limit: 10, CallerID: unlinkedID})
	if err != nil || len(page) != 1 {
		t.Fatalf("unlinked access_mode=all page=%d err=%v", len(page), err)
	}
	if _, err = db.ExecContext(ctx, `DELETE FROM admin_role_assignments WHERE user_id=?`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `UPDATE directory_users SET is_resigned=1,is_active=0 WHERE id=?`, directoryUserID); err != nil {
		t.Fatal(err)
	}
	page, err = runtimeRepo.ListApplicationPage(ctx, catalog.ApplicationPageQuery{Scope: "accessible", Mode: catalog.PageModeConsume, Kind: catalog.PageQueryKindFixed, Search: suffix, Limit: 10, CallerID: userID})
	if err != nil || len(page) != 0 {
		t.Fatalf("resigned access_mode=all page=%d err=%v", len(page), err)
	}
}
