package accessgroup

import "time"

type DepartmentGrant struct {
	DepartmentID    int64  `json:"department_id"`
	Name            string `json:"name"`
	IncludeChildren bool   `json:"include_children"`
	CoveredUsers    int    `json:"covered_users"`
}

type UserGrant struct {
	DirectoryUserID int64  `json:"directory_user_id"`
	Name            string `json:"name"`
	AvatarURL       string `json:"avatar_url"`
}

type Group struct {
	ID                int64             `json:"id"`
	Code              string            `json:"code"`
	Name              string            `json:"name"`
	Description       string            `json:"description"`
	SourceType        string            `json:"source_type"`
	ExternalGroupID   string            `json:"external_group_id"`
	ExternalGroupType string            `json:"external_group_type"`
	Enabled           bool              `json:"enabled"`
	SyncStatus        string            `json:"sync_status"`
	LastSyncedAt      *time.Time        `json:"last_synced_at"`
	MemberCount       int               `json:"member_count"`
	CoveredUsers      int               `json:"covered_users"`
	ApplicationCount  int               `json:"application_count"`
	Departments       []DepartmentGrant `json:"departments,omitempty"`
	Users             []UserGrant       `json:"users,omitempty"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
}

type Input struct {
	Code             string            `json:"code"`
	Name             string            `json:"name"`
	Description      string            `json:"description"`
	Enabled          bool              `json:"enabled"`
	DepartmentGrants []DepartmentGrant `json:"department_grants"`
	UserGrants       []int64           `json:"user_grants"`
}

type Member struct {
	DirectoryUserID int64    `json:"directory_user_id"`
	Name            string   `json:"name"`
	AvatarURL       string   `json:"avatar_url"`
	ExternalUserID  string   `json:"external_user_id"`
	Departments     []string `json:"departments"`
	Source          string   `json:"source"`
}

type ApplicationRef struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Enabled bool   `json:"enabled"`
}
