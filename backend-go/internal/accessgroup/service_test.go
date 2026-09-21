package accessgroup

import "testing"

func TestValidateLocalGroupInput(t *testing.T) {
	if err := validate(Input{Code: "bad code", Name: "Beta"}, true); err == nil {
		t.Fatal("expected invalid code")
	}
	if err := validate(Input{Code: "beta_users", Name: "Beta Users", Enabled: true}, true); err != nil {
		t.Fatalf("valid group: %v", err)
	}
}
