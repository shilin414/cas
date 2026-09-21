package directory

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/shilin414/cas/backend-go/internal/identity"
)

type fakeDirectoryClient struct{ failEmployees bool }

func (f *fakeDirectoryClient) TenantToken(context.Context) (*identity.TenantTokenResult, error) {
	return &identity.TenantTokenResult{TenantAccessToken: "t"}, nil
}
func (f *fakeDirectoryClient) ListDirectoryDepartments(_ context.Context, _ string, parent, page string) (*identity.DirectoryDepartmentPage, error) {
	if page != "" {
		t := &identity.DirectoryDepartmentPage{}
		return t, nil
	}
	switch parent {
	case "0":
		return &identity.DirectoryDepartmentPage{Departments: []identity.DirectoryDepartment{{DepartmentID: "a", Name: identity.DirectoryI18nText{DefaultValue: "A"}, ParentDepartmentID: "0"}}}, nil
	case "a":
		return &identity.DirectoryDepartmentPage{Departments: []identity.DirectoryDepartment{{DepartmentID: "b", Name: identity.DirectoryI18nText{DefaultValue: "B"}, ParentDepartmentID: "a"}}}, nil
	default:
		return &identity.DirectoryDepartmentPage{}, nil
	}
}
func (f *fakeDirectoryClient) ListDirectoryEmployees(_ context.Context, _ string, _ []string, status int, _ string) (*identity.DirectoryEmployeePage, error) {
	if f.failEmployees {
		return nil, errors.New("page 2 failed")
	}
	if status != 1 {
		return &identity.DirectoryEmployeePage{}, nil
	}
	return &identity.DirectoryEmployeePage{Employees: []identity.DirectoryEmployee{{BaseInfo: identity.DirectoryEmployeeBaseInfo{EmployeeID: "ou-1", Name: identity.DirectoryEmployeeName{Name: identity.DirectoryI18nText{DefaultValue: "张三"}}, Departments: []identity.DirectoryEmployeeDepartment{{DepartmentID: "b"}}, ActiveStatus: 2, IsResigned: boolPtr(false)}}}}, nil
}
func TestFetchTraversesDepartmentTreeAndMergesEmployees(t *testing.T) {
	svc := NewService(nil, &fakeDirectoryClient{}, nil)
	deps, users, err := svc.fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) != 2 || deps[1].ParentOpenID != "a" {
		t.Fatalf("deps=%#v", deps)
	}
	if len(users) != 1 || users[0].OpenID != "ou-1" || len(users[0].Departments) != 1 {
		t.Fatalf("users=%#v", users)
	}
}
func TestFetchPageFailureReturnsNoSnapshot(t *testing.T) {
	svc := NewService(nil, &fakeDirectoryClient{failEmployees: true}, nil)
	deps, users, err := svc.fetch(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if deps != nil || users != nil {
		t.Fatalf("partial snapshot leaked deps=%v users=%v", deps, users)
	}
}

func TestFetchRejectsFieldlessEmployeeRows(t *testing.T) {
	client := &fieldlessEmployeeClient{fakeDirectoryClient: fakeDirectoryClient{}}
	svc := NewService(nil, client, nil)
	if _, _, err := svc.fetch(context.Background()); err == nil {
		t.Fatal("expected missing field error")
	}
}

type fieldlessEmployeeClient struct{ fakeDirectoryClient }

func (f *fieldlessEmployeeClient) ListDirectoryEmployees(context.Context, string, []string, int, string) (*identity.DirectoryEmployeePage, error) {
	return &identity.DirectoryEmployeePage{Employees: []identity.DirectoryEmployee{{BaseInfo: identity.DirectoryEmployeeBaseInfo{EmployeeID: "ou-empty"}}}}, nil
}

func boolPtr(v bool) *bool { return &v }

type fakeDirectoryGroupClient struct {
	fakeDirectoryClient
	requestedTypes []int
}

func (f *fakeDirectoryGroupClient) ListDirectoryUserGroups(_ context.Context, _ string, groupType int, page string) (*identity.DirectoryUserGroupPage, error) {
	if page == "" {
		f.requestedTypes = append(f.requestedTypes, groupType)
	}
	if page != "" {
		return &identity.DirectoryUserGroupPage{}, nil
	}
	if groupType == 1 {
		return &identity.DirectoryUserGroupPage{Groups: []identity.DirectoryUserGroup{{ID: "g-1", Name: "普通组", Type: 1}}}, nil
	}
	return &identity.DirectoryUserGroupPage{Groups: []identity.DirectoryUserGroup{{ID: "g-2", Name: "动态组", Type: 2}}}, nil
}
func (f *fakeDirectoryGroupClient) ListDirectoryUserGroupMembers(_ context.Context, _ string, groupID, page string) (*identity.DirectoryUserGroupMemberPage, error) {
	if page != "" {
		return &identity.DirectoryUserGroupMemberPage{}, nil
	}
	return &identity.DirectoryUserGroupMemberPage{Members: []identity.DirectoryUserGroupMember{{MemberID: "ou-1", MemberType: "user"}}}, nil
}
func TestFetchUserGroupsConsumesPagesAndMapsTypes(t *testing.T) {
	client := &fakeDirectoryGroupClient{}
	svc := NewService(nil, client, nil)
	groups, members, err := svc.fetchUserGroups(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(client.requestedTypes) != 2 || client.requestedTypes[0] != 1 || client.requestedTypes[1] != 2 {
		t.Fatalf("requested types=%v", client.requestedTypes)
	}
	if len(groups) != 2 || groups[0].GroupType != "normal" || groups[1].GroupType != "dynamic" {
		t.Fatalf("groups=%#v", groups)
	}
	if len(members) != 2 || members[0].ExternalUserID != "ou-1" {
		t.Fatalf("members=%#v", members)
	}
}

func TestValidatePerGroupShrinkAllowsMissingGroupButRejectsCollapsedPresentGroup(t *testing.T) {
	if err := validatePerGroupShrink(map[string]int{"deleted": 100}, map[string]int{}); err != nil {
		t.Fatalf("deleted group should publish as missing: %v", err)
	}
	if err := validatePerGroupShrink(map[string]int{"present": 100}, map[string]int{"present": 10}); err == nil {
		t.Fatal("expected present-group shrink rejection")
	}
}

type isolationClient struct {
	directoryCalls int
	groupCalls     int
}

func (c *isolationClient) TenantToken(context.Context) (*identity.TenantTokenResult, error) {
	return &identity.TenantTokenResult{TenantAccessToken: "t"}, nil
}
func (c *isolationClient) ListDirectoryDepartments(_ context.Context, _ string, parent, page string) (*identity.DirectoryDepartmentPage, error) {
	c.directoryCalls++
	if parent == "0" && page == "" {
		return &identity.DirectoryDepartmentPage{Departments: []identity.DirectoryDepartment{{DepartmentID: "d", Name: identity.DirectoryI18nText{DefaultValue: "D"}, ParentDepartmentID: "0"}}}, nil
	}
	return &identity.DirectoryDepartmentPage{}, nil
}
func (c *isolationClient) ListDirectoryEmployees(_ context.Context, _ string, _ []string, status int, _ string) (*identity.DirectoryEmployeePage, error) {
	c.directoryCalls++
	if status == 1 {
		return &identity.DirectoryEmployeePage{Employees: []identity.DirectoryEmployee{{BaseInfo: identity.DirectoryEmployeeBaseInfo{EmployeeID: "u", Name: identity.DirectoryEmployeeName{Name: identity.DirectoryI18nText{DefaultValue: "U"}}, Departments: []identity.DirectoryEmployeeDepartment{{DepartmentID: "d"}}, ActiveStatus: 2, IsResigned: boolPtr(false)}}}}, nil
	}
	return &identity.DirectoryEmployeePage{}, nil
}
func (c *isolationClient) ListDirectoryUserGroups(_ context.Context, _ string, kind int, _ string) (*identity.DirectoryUserGroupPage, error) {
	c.groupCalls++
	return &identity.DirectoryUserGroupPage{Groups: []identity.DirectoryUserGroup{{ID: fmt.Sprintf("g-%d", kind), Name: "G", Type: kind}}}, nil
}
func (c *isolationClient) ListDirectoryUserGroupMembers(_ context.Context, _ string, groupID, page string) (*identity.DirectoryUserGroupMemberPage, error) {
	c.groupCalls++
	return &identity.DirectoryUserGroupMemberPage{Members: []identity.DirectoryUserGroupMember{{MemberID: "u", MemberType: "user"}}}, nil
}

func TestDirectoryFetchDoesNotCallUserGroupAPIs(t *testing.T) {
	c := &isolationClient{}
	svc := NewService(nil, c, nil)
	if _, _, err := svc.fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c.groupCalls != 0 {
		t.Fatalf("directory target called user-group APIs %d times", c.groupCalls)
	}
}
func TestUserGroupFetchDoesNotCallDirectoryAPIs(t *testing.T) {
	c := &isolationClient{}
	svc := NewService(nil, c, nil)
	if _, _, err := svc.fetchUserGroups(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c.directoryCalls != 0 {
		t.Fatalf("user_groups target called directory APIs %d times", c.directoryCalls)
	}
}
func TestOrderedTargetsUsesDependencyOrder(t *testing.T) {
	got, err := OrderedTargets([]string{TargetUserGroups, TargetDirectory})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != TargetDirectory || got[1] != TargetUserGroups {
		t.Fatalf("order=%v", got)
	}
}
