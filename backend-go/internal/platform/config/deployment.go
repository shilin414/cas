package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var basePathPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+(/[A-Za-z0-9_-]+)*$`)

// deploymentBasePath shares its source with the Vite and nginx builds. An explicit
// APP_BASE_PATH is useful for packaged deployments without a source checkout.
func deploymentBasePath() (string, error) {
	value, overridden := os.LookupEnv("APP_BASE_PATH")
	if !overridden {
		var data []byte
		var err error
		if file := os.Getenv("DEPLOYMENT_CONFIG"); file != "" {
			data, err = os.ReadFile(file)
		} else {
			dir, cwdErr := os.Getwd()
			if cwdErr != nil {
				return "", cwdErr
			}
			for {
				data, err = os.ReadFile(filepath.Join(dir, "deployment.json"))
				if err == nil || !os.IsNotExist(err) {
					break
				}
				parent := filepath.Dir(dir)
				if parent == dir {
					break
				}
				dir = parent
			}
		}
		if err != nil {
			return "", fmt.Errorf("read deployment.json (or set DEPLOYMENT_CONFIG / APP_BASE_PATH): %w", err)
		}
		var deployment struct {
			BasePath *string `json:"basePath"`
		}
		if err := json.Unmarshal(data, &deployment); err != nil {
			return "", fmt.Errorf("deployment.json: %w", err)
		}
		if deployment.BasePath == nil {
			return "", fmt.Errorf("deployment.json: basePath is required")
		}
		value = *deployment.BasePath
	}
	if value == "" || value == "/" {
		return "/", nil
	}
	path := strings.TrimSuffix(strings.TrimPrefix(value, "/"), "/")
	if !basePathPattern.MatchString(path) {
		return "", fmt.Errorf("invalid APP_BASE_PATH/basePath %q: use slash-separated letters, digits, underscores or hyphens", value)
	}
	return "/" + path + "/", nil
}

func configureDeployment(cfg *Config) error {
	base, err := deploymentBasePath()
	if err != nil {
		return err
	}
	cfg.BasePath = base
	// Legacy full URLs contribute only their origin. Path ownership belongs to
	// deployment.json so changing its prefix never leaves stale OAuth/share URLs.
	origin := getEnv("PUBLIC_ORIGIN", cfg.PublicBaseURL)
	if origin == "" {
		origin = "http://localhost:3030"
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("PUBLIC_ORIGIN must be an absolute HTTP(S) origin")
	}
	if os.Getenv("PUBLIC_ORIGIN") != "" && u.Path != "" && u.Path != "/" {
		return fmt.Errorf("PUBLIC_ORIGIN must not include a path; configure basePath in deployment.json")
	}
	cfg.PublicBaseURL = u.Scheme + "://" + u.Host + strings.TrimSuffix(base, "/")
	cfg.Feishu.RedirectURI = cfg.PublicBaseURL + "/auth/feishu/callback"
	return nil
}

// CookiePath also supports manually constructed configs in transport tests.
func (c *Config) CookiePath() string {
	if c.BasePath == "" {
		return "/"
	}
	return c.BasePath
}
