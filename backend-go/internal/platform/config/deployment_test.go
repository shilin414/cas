package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDeploymentPath(t *testing.T) {
	for _, base := range []string{"/xiaoan-platform/", "/other/nested/", "/"} {
		t.Run(base, func(t *testing.T) {
			t.Setenv("APP_BASE_PATH", base)
			t.Setenv("PUBLIC_ORIGIN", "https://studio.example")
			cfg := &Config{PublicBaseURL: "http://localhost:3030", Feishu: FeishuConfig{RedirectURI: "http://localhost:3030/auth/feishu/callback"}}
			if err := configureDeployment(cfg); err != nil {
				t.Fatal(err)
			}
			if cfg.BasePath != base {
				t.Fatalf("base = %q", cfg.BasePath)
			}
			if cfg.PublicBaseURL+"/" != "https://studio.example"+base {
				t.Fatalf("public = %q", cfg.PublicBaseURL)
			}
			if cfg.Feishu.RedirectURI != "https://studio.example"+base+"auth/feishu/callback" {
				t.Fatalf("callback = %q", cfg.Feishu.RedirectURI)
			}
		})
	}
}

func TestDeploymentRejectsUnsafePaths(t *testing.T) {
	for _, base := range []string{"//evil", "/a/../b", "/a?x", "/a#x", "/a%2fb", "/a b", "/a;foo"} {
		t.Run(base, func(t *testing.T) {
			t.Setenv("APP_BASE_PATH", base)
			if err := configureDeployment(&Config{}); err == nil {
				t.Fatalf("accepted %q", base)
			}
		})
	}
}

func TestDeploymentConfigFileAndOverride(t *testing.T) {
	file := filepath.Join(t.TempDir(), "deployment.json")
	if err := os.WriteFile(file, []byte(`{"basePath":"/future/platform/"}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEPLOYMENT_CONFIG", file)
	// t.Setenv restores the previous environment; unset to exercise JSON precedence.
	t.Setenv("APP_BASE_PATH", "")
	if err := os.Unsetenv("APP_BASE_PATH"); err != nil {
		t.Fatal(err)
	}
	base, err := deploymentBasePath()
	if err != nil || base != "/future/platform/" {
		t.Fatalf("file base = %q, %v", base, err)
	}
	t.Setenv("APP_BASE_PATH", "/")
	base, err = deploymentBasePath()
	if err != nil || base != "/" {
		t.Fatalf("override = %q, %v", base, err)
	}
}

func TestDeploymentRejectsMissingOrMalformedFile(t *testing.T) {
	t.Setenv("APP_BASE_PATH", "")
	if err := os.Unsetenv("APP_BASE_PATH"); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "deployment.json")
	t.Setenv("DEPLOYMENT_CONFIG", file)
	if _, err := deploymentBasePath(); err == nil {
		t.Fatal("missing explicit file silently accepted")
	}
	for _, contents := range []string{`{}`, `{"basePath":null}`, `{"basePath":12}`, `invalid`} {
		if err := os.WriteFile(file, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := deploymentBasePath(); err == nil {
			t.Fatalf("invalid file %s accepted", contents)
		}
	}
}

func TestDeploymentOriginValidationAndLegacyMigration(t *testing.T) {
	t.Setenv("APP_BASE_PATH", "/next/")
	for _, origin := range []string{"https://user:pass@example.com", "//example.com", "javascript:alert(1)", "https://example.com/stale", "https://example.com?query=yes"} {
		t.Setenv("PUBLIC_ORIGIN", origin)
		if err := configureDeployment(&Config{}); err == nil {
			t.Fatalf("invalid origin %q accepted", origin)
		}
	}
	t.Setenv("PUBLIC_ORIGIN", "")
	cfg := &Config{PublicBaseURL: "https://example.com/old", Feishu: FeishuConfig{RedirectURI: "https://example.com/old/auth/feishu/callback"}}
	if err := configureDeployment(cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.PublicBaseURL != "https://example.com/next" || cfg.Feishu.RedirectURI != "https://example.com/next/auth/feishu/callback" {
		t.Fatalf("stale path survived: %+v", cfg)
	}
}
