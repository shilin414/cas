package directory

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/shilin414/cas/backend-go/internal/identity"
)

type FeishuDirectoryClient interface {
	TenantToken(context.Context) (*identity.TenantTokenResult, error)
	ListDirectoryDepartments(context.Context, string, string, string) (*identity.DirectoryDepartmentPage, error)
	ListDirectoryEmployees(context.Context, string, []string, int, string) (*identity.DirectoryEmployeePage, error)
}

type FeishuUserGroupClient interface {
	ListDirectoryUserGroups(context.Context, string, int, string) (*identity.DirectoryUserGroupPage, error)
	ListDirectoryUserGroupMembers(context.Context, string, string, string) (*identity.DirectoryUserGroupMemberPage, error)
}

type Service struct {
	Repo    *Repo
	Feishu  FeishuDirectoryClient
	Log     *slog.Logger
	runners map[string]TargetRunner
}

type targetRunner struct {
	code string
	deps []string
	run  func(context.Context, *SyncRun, string) (Result, error)
}

func (r targetRunner) Code() string           { return r.code }
func (r targetRunner) Dependencies() []string { return append([]string(nil), r.deps...) }
func (r targetRunner) Run(ctx context.Context, job *SyncRun, leaseOwner string) (Result, error) {
	return r.run(ctx, job, leaseOwner)
}

func NewService(repo *Repo, feishu FeishuDirectoryClient, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	s := &Service{Repo: repo, Feishu: feishu, Log: log, runners: map[string]TargetRunner{}}
	s.runners[TargetDirectory] = targetRunner{code: TargetDirectory, run: s.runDirectory}
	s.runners[TargetUserGroups] = targetRunner{code: TargetUserGroups, deps: []string{TargetDirectory}, run: s.runUserGroups}
	return s
}

func (s *Service) Run(ctx context.Context, run *SyncRun) error { return s.RunWithLease(ctx, run, "") }
func (s *Service) RunWithLease(ctx context.Context, run *SyncRun, leaseOwner string) error {
	if run == nil {
		return errors.New("directory: nil sync run")
	}
	if run.TargetCode == "" || run.TargetCode == "legacy_full" {
		run.TargetCode = TargetDirectory
	}
	runner, ok := s.runners[run.TargetCode]
	var err error
	if !ok {
		err = fmt.Errorf("unknown sync target %q", run.TargetCode)
	} else {
		_, err = runner.Run(ctx, run, leaseOwner)
	}
	if err != nil {
		return s.failTarget(ctx, run, err)
	}
	return nil
}

func (s *Service) runDirectory(ctx context.Context, run *SyncRun, leaseOwner string) (Result, error) {
	deps, users, err := s.fetch(ctx)
	if err != nil {
		return Result{}, err
	}
	// Preserve the fetched snapshot for diagnostics when validation rejects it.
	// Publishing remains atomic because live tables are not touched until after
	// all closure and shrink checks pass.
	if err = s.Repo.WriteDirectoryStage(ctx, run.ID, deps, users); err != nil {
		return Result{}, fmt.Errorf("stage: %w", err)
	}
	edges, err := validateAndBuildClosure(deps, users)
	if err != nil {
		return Result{}, fmt.Errorf("validate: %w", err)
	}
	memberships, activeUsers, activeMemberships := 0, 0, 0
	for _, u := range users {
		memberships += len(u.Departments)
		if u.ActiveStatus == 2 && !u.Resigned {
			activeUsers++
			activeMemberships += len(u.Departments)
		}
	}
	if err = s.Repo.ValidateSnapshotSize(ctx, len(deps), len(users), memberships, activeUsers, activeMemberships); err != nil {
		return Result{}, fmt.Errorf("snapshot: %w", err)
	}
	result, err := s.Repo.PublishDirectoryTarget(ctx, run, deps, users, edges, leaseOwner, activeUsers, activeMemberships)
	if err != nil {
		return Result{}, fmt.Errorf("publish: %w", err)
	}
	if err = s.Repo.CleanupStage(ctx, run.ID); err != nil {
		s.Log.Warn("directory stage cleanup failed", "run_id", run.ID, "err", err)
	}
	return result, nil
}

func (s *Service) runUserGroups(ctx context.Context, run *SyncRun, leaseOwner string) (Result, error) {
	version, err := s.Repo.DirectoryVersion(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("directory dependency: %w", err)
	}
	if version < 1 {
		return Result{}, errors.New("directory dependency has no successful published version")
	}
	groups, members, err := s.fetchUserGroups(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("user groups: %w", err)
	}
	known, unknown, err := s.Repo.KnownGroupMembers(ctx, members)
	if err != nil {
		return Result{}, fmt.Errorf("map live directory users: %w", err)
	}
	if len(members) > 0 && unknown*5 > len(members) {
		return Result{}, fmt.Errorf("user groups reference %d/%d users outside the live directory scope", unknown, len(members))
	}
	warnings := []Warning{}
	if unknown > 0 {
		warnings = append(warnings, Warning{Code: "unmapped_directory_users", Message: "some group members are not present in the live directory and were skipped", Count: int64(unknown)})
	}
	if len(groups) == 0 {
		warnings = append(warnings, Warning{Code: "empty_group_snapshot", Message: "Feishu returned no visible user groups"})
		s.Log.Warn("Feishu user-group sync returned no visible groups", "run_id", run.ID, "hint", "enable contact:group:readonly and grant all-employee directory scope")
	}
	if err = s.Repo.ValidateGroupSnapshotSize(ctx, len(groups), len(known)); err != nil {
		return Result{}, fmt.Errorf("user group snapshot: %w", err)
	}
	if err = s.Repo.ValidatePerGroupMembershipShrink(ctx, groups, known); err != nil {
		return Result{}, fmt.Errorf("user group membership: %w", err)
	}
	if err = s.Repo.WriteGroupStage(ctx, run.ID, groups, known); err != nil {
		return Result{}, fmt.Errorf("stage: %w", err)
	}
	result, err := s.Repo.PublishUserGroupsTarget(ctx, run, groups, known, warnings, unknown, leaseOwner)
	if err != nil {
		return Result{}, fmt.Errorf("publish: %w", err)
	}
	if err = s.Repo.CleanupStage(ctx, run.ID); err != nil {
		s.Log.Warn("user-group stage cleanup failed", "run_id", run.ID, "err", err)
	}
	return result, nil
}

func (s *Service) failTarget(ctx context.Context, run *SyncRun, err error) error {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	code := "sync_target_failed"
	var apiErr *identity.FeishuAPIError
	if errors.As(err, &apiErr) {
		code = fmt.Sprintf("feishu_%d", apiErr.Code)
	}
	if errors.Is(err, ErrLeaseLost) {
		code = "lease_lost"
	}
	if markErr := s.Repo.FinishTargetFailure(cleanup, run, code, err); markErr != nil {
		s.Log.Error("mark sync target failed", "run_id", run.ID, "target", run.TargetCode, "err", markErr)
	}
	return err
}

func (s *Service) fetchUserGroups(ctx context.Context) ([]stagedUserGroup, []stagedUserGroupMember, error) {
	client, ok := s.Feishu.(FeishuUserGroupClient)
	if !ok {
		return nil, nil, errors.New("directory client does not support Feishu user groups")
	}
	token, err := s.Feishu.TenantToken(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("tenant token: %w", err)
	}
	groups := []stagedUserGroup{}
	members := []stagedUserGroupMember{}
	for _, requestedType := range []int{1, 2} {
		pageToken := ""
		seenTokens := map[string]struct{}{}
		for {
			page, err := client.ListDirectoryUserGroups(ctx, token.TenantAccessToken, requestedType, pageToken)
			if err != nil {
				return nil, nil, fmt.Errorf("type %d: %w", requestedType, err)
			}
			for _, group := range page.Groups {
				externalID := group.ID
				if externalID == "" {
					externalID = group.GroupID
				}
				groupType := "normal"
				if group.Type == 2 {
					groupType = "dynamic"
				} else if group.Type != 1 {
					groupType = "unknown"
				}
				groups = append(groups, stagedUserGroup{ExternalID: externalID, Name: group.Name, Description: group.Description, GroupType: groupType})
				memberToken := ""
				memberSeen := map[string]struct{}{}
				for {
					mp, err := client.ListDirectoryUserGroupMembers(ctx, token.TenantAccessToken, externalID, memberToken)
					if err != nil {
						return nil, nil, fmt.Errorf("group %s members: %w", externalID, err)
					}
					for _, member := range mp.Members {
						if member.MemberType == "" || member.MemberType == "user" {
							members = append(members, stagedUserGroupMember{ExternalGroupID: externalID, ExternalUserID: member.MemberID})
						}
					}
					if !mp.HasMore {
						break
					}
					if mp.PageToken == "" {
						return nil, nil, fmt.Errorf("group %s members has_more without page token", externalID)
					}
					if _, dup := memberSeen[mp.PageToken]; dup {
						return nil, nil, fmt.Errorf("group %s repeated member page token", externalID)
					}
					memberSeen[mp.PageToken] = struct{}{}
					memberToken = mp.PageToken
				}
			}
			if !page.HasMore {
				break
			}
			if page.PageToken == "" {
				return nil, nil, fmt.Errorf("user groups type %d has_more without page token", requestedType)
			}
			if _, dup := seenTokens[page.PageToken]; dup {
				return nil, nil, fmt.Errorf("user groups type %d repeated page token", requestedType)
			}
			seenTokens[page.PageToken] = struct{}{}
			pageToken = page.PageToken
		}
	}
	return groups, members, nil
}

func (s *Service) fetch(ctx context.Context) ([]stagedDepartment, []stagedUser, error) {
	token, err := s.Feishu.TenantToken(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("tenant token: %w", err)
	}
	deps := []stagedDepartment{}
	depSeen := map[string]struct{}{}
	parents := []string{"0"}
	type departmentResult struct {
		items []identity.DirectoryDepartment
		err   error
	}
	for len(parents) > 0 {
		level := parents
		parents = nil
		results := make(chan departmentResult, len(level))
		sem := make(chan struct{}, 2)
		var wg sync.WaitGroup
		for _, parent := range level {
			parent := parent
			wg.Add(1)
			go func() {
				defer wg.Done()
				select {
				case sem <- struct{}{}:
				case <-ctx.Done():
					results <- departmentResult{err: ctx.Err()}
					return
				}
				defer func() { <-sem }()
				items := []identity.DirectoryDepartment{}
				pageToken := ""
				for {
					page, e := s.Feishu.ListDirectoryDepartments(ctx, token.TenantAccessToken, parent, pageToken)
					if e != nil {
						results <- departmentResult{err: e}
						return
					}
					if e = validateAbnormals("departments", page.Abnormals); e != nil {
						results <- departmentResult{err: e}
						return
					}
					items = append(items, page.Departments...)
					if !page.HasMore {
						break
					}
					if page.PageToken == "" {
						results <- departmentResult{err: errors.New("departments has_more without page_token")}
						return
					}
					pageToken = page.PageToken
				}
				results <- departmentResult{items: items}
			}()
		}
		wg.Wait()
		close(results)
		for result := range results {
			if result.err != nil {
				return nil, nil, fmt.Errorf("departments: %w", result.err)
			}
			for _, item := range result.items {
				id := item.DepartmentID
				if id == "" {
					return nil, nil, errors.New("department response missing department_id; check directory department field permissions")
				}
				if _, ok := depSeen[id]; ok {
					continue
				}
				depSeen[id] = struct{}{}
				active := true
				if item.EnabledStatus != nil {
					active = *item.EnabledStatus
				}
				deps = append(deps, stagedDepartment{OpenID: id, Name: item.Name.Display(), ParentOpenID: item.ParentDepartmentID, OrderWeight: item.OrderWeight, Active: active})
				parents = append(parents, id)
			}
		}
	}
	usersByID := map[string]*stagedUser{}
	departmentIDs := make([]string, 0, len(deps))
	for _, d := range deps {
		departmentIDs = append(departmentIDs, d.OpenID)
	}
	for offset := 0; offset < len(departmentIDs); offset += 100 {
		end := offset + 100
		if end > len(departmentIDs) {
			end = len(departmentIDs)
		}
		batch := departmentIDs[offset:end]
		for _, staffStatus := range []int{1, 5} {
			pageToken := ""
			for {
				page, err := s.Feishu.ListDirectoryEmployees(ctx, token.TenantAccessToken, batch, staffStatus, pageToken)
				if err != nil {
					return nil, nil, fmt.Errorf("employees: %w", err)
				}
				if err = validateAbnormals("employees", page.Abnormals); err != nil {
					return nil, nil, err
				}
				for _, item := range page.Employees {
					id := item.OpenID()
					if id == "" {
						return nil, nil, errors.New("employee response missing open_id; check directory employee field permissions")
					}
					if item.DisplayName() == "" {
						return nil, nil, errors.New("employee response missing name; grant directory:employee.base.name.name:read")
					}
					if item.BaseInfo.ActiveStatus < 1 || item.BaseInfo.ActiveStatus > 5 {
						return nil, nil, errors.New("employee response missing active_status; grant directory:employee.base.active_status:read")
					}
					if len(item.BaseInfo.Departments) == 0 {
						return nil, nil, errors.New("employee response missing departments; grant directory:employee.base.department:read")
					}
					if item.BaseInfo.IsResigned == nil {
						return nil, nil, errors.New("employee response missing is_resigned; grant directory:employee.base.is_resigned:read")
					}
					u := usersByID[id]
					if u == nil {
						u = &stagedUser{OpenID: id, Name: item.DisplayName(), AvatarURL: item.AvatarURL(), ActiveStatus: item.BaseInfo.ActiveStatus, Resigned: *item.BaseInfo.IsResigned}
						usersByID[id] = u
					}
					seen := map[string]struct{}{}
					for _, d := range u.Departments {
						seen[d] = struct{}{}
					}
					for _, d := range item.BaseInfo.Departments {
						if d.DepartmentID != "" {
							if _, ok := seen[d.DepartmentID]; !ok {
								u.Departments = append(u.Departments, d.DepartmentID)
								seen[d.DepartmentID] = struct{}{}
							}
						}
					}
				}
				if !page.HasMore {
					break
				}
				if page.PageToken == "" {
					return nil, nil, errors.New("employees has_more without page_token")
				}
				pageToken = page.PageToken
			}
		}
	}
	users := make([]stagedUser, 0, len(usersByID))
	for _, u := range usersByID {
		users = append(users, *u)
	}
	sort.Slice(users, func(i, j int) bool { return users[i].OpenID < users[j].OpenID })
	if len(deps) == 0 {
		return nil, nil, errors.New("directory snapshot contains no departments; refusing to replace the live snapshot")
	}
	if len(users) == 0 {
		return nil, nil, errors.New("directory snapshot contains no employees; refusing to replace the live snapshot")
	}
	memberships := 0
	for _, u := range users {
		memberships += len(u.Departments)
	}
	if memberships < len(users) {
		return nil, nil, fmt.Errorf("directory snapshot has %d users but only %d memberships; refusing to publish incomplete employee fields", len(users), memberships)
	}
	return deps, users, nil
}
func validateAbnormals(kind string, rows []identity.DirectoryAbnormal) error {
	for _, a := range rows {
		if a.RowError != 0 || len(a.FieldErrors) > 0 {
			return fmt.Errorf("%s field permission error for %s: row=%d fields=%v", kind, a.ID, a.RowError, a.FieldErrors)
		}
	}
	return nil
}
func validateAndBuildClosure(deps []stagedDepartment, users []stagedUser) ([]ClosureEdge, error) {
	parents := make(map[string]string, len(deps))
	for _, d := range deps {
		if d.OpenID == "" {
			return nil, errors.New("empty department id")
		}
		if _, ok := parents[d.OpenID]; ok {
			return nil, fmt.Errorf("duplicate department %s", d.OpenID)
		}
		p := d.ParentOpenID
		if p == "" {
			p = "0"
		}
		parents[d.OpenID] = p
	}
	for id, p := range parents {
		if p != "0" {
			if _, ok := parents[p]; !ok {
				return nil, fmt.Errorf("department %s parent %s missing", id, p)
			}
		}
	}
	for _, u := range users {
		for _, d := range u.Departments {
			if _, ok := parents[d]; !ok {
				return nil, fmt.Errorf("employee %s references missing department %s", u.OpenID, d)
			}
		}
	}
	ids := make([]string, 0, len(parents))
	for id := range parents {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	edges := make([]ClosureEdge, 0, len(ids)*3)
	for _, desc := range ids {
		current := desc
		depth := 0
		path := map[string]struct{}{}
		for current != "0" {
			if _, seen := path[current]; seen {
				return nil, fmt.Errorf("department cycle at %s", current)
			}
			path[current] = struct{}{}
			edges = append(edges, ClosureEdge{AncestorOpenID: current, DescendantOpenID: desc, Depth: depth})
			current = parents[current]
			depth++
			if depth > len(parents) {
				return nil, fmt.Errorf("department cycle from %s", desc)
			}
		}
	}
	return edges, nil
}
