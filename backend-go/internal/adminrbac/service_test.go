package adminrbac

import "testing"

func TestValidateRoleInputRejectsRoleNamesAsCodes(t *testing.T) {
	if err := validateRoleInput(RoleInput{Code: "Access Administrator", Name: "Access Administrator"}, true); err == nil {
		t.Fatal("expected invalid role code")
	}
	if err := validateRoleInput(RoleInput{Code: "access_admin_custom", Name: "Access Administrator", Enabled: true}, true); err != nil {
		t.Fatalf("valid role: %v", err)
	}
}
