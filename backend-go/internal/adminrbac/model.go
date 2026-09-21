package adminrbac

import "time"

type Permission struct {
	ID          int64     `json:"id"`
	Code        string    `json:"code"`
	Category    string    `json:"category"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

type Role struct {
	ID          int64        `json:"id"`
	Code        string       `json:"code"`
	Name        string       `json:"name"`
	Description string       `json:"description"`
	IsSystem    bool         `json:"is_system"`
	Enabled     bool         `json:"enabled"`
	Permissions []Permission `json:"permissions"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
}

type RoleInput struct {
	Code            string   `json:"code"`
	Name            string   `json:"name"`
	Description     string   `json:"description"`
	Enabled         bool     `json:"enabled"`
	PermissionCodes []string `json:"permission_codes"`
}

type Assignment struct {
	ID        int64      `json:"id"`
	RoleID    int64      `json:"role_id"`
	RoleCode  string     `json:"role_code"`
	RoleName  string     `json:"role_name"`
	Enabled   bool       `json:"enabled"`
	ExpiresAt *time.Time `json:"expires_at"`
}

type Administrator struct {
	UserID      int64        `json:"user_id"`
	Username    string       `json:"username"`
	DisplayName string       `json:"display_name"`
	Email       string       `json:"email"`
	AuthSource  string       `json:"auth_source"`
	IsStaff     bool         `json:"is_staff"`
	IsActive    bool         `json:"is_active"`
	LastLoginAt *time.Time   `json:"last_login_at"`
	Assignments []Assignment `json:"assignments"`
}

type Me struct {
	CanAccessConsole bool         `json:"can_access_console"`
	IsSuperAdmin     bool         `json:"is_super_admin"`
	Roles            []Role       `json:"roles"`
	Permissions      []Permission `json:"permissions"`
}
