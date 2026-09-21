// Package config loads runtime configuration for the studio backend.
//
// Precedence: real environment variables win over .env.local / .env files.
// Secrets (DB/Redis/Feishu/encryption keys) must only arrive through the
// environment or a local secret file that is git-ignored.
package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Config is the full runtime configuration of any studio process.
// Each binary (api / stream / worker) reads the same set and uses the
// subset it needs.
type Config struct {
	BasePath      string // Normalized public mount path, including trailing slash.
	PublicBaseURL string // Browser-reachable origin plus configured mount path for read-only result links.
	Env           string // development | production | test
	LogLevel      string
	DevMode       bool

	APIAddr      string // short-request plane (studio-api)
	StreamAddr   string // SSE plane (studio-stream)
	MetricsAddr  string
	PPROFEnabled bool

	Database   DatabaseConfig
	Redis      RedisConfig
	Feishu     FeishuConfig
	Aily       AilyConfig
	Runner     RunnerConfig
	Session    SessionConfig
	Storage    StorageConfig
	Auth       AuthConfig
	OTel       OTelConfig
	SSE        SSEConfig
	Enterprise EnterpriseConfig
}

type DatabaseConfig struct {
	Host            string
	Port            int
	Name            string
	User            string
	Password        string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
	QueryTimeout    time.Duration
}

func (d DatabaseConfig) DSN() string {
	// UTC everywhere: DATETIME columns are written/read in UTC; the app
	// converts to local time only at the presentation edge.
	// time_zone pins the SESSION clock to UTC so DB-side
	// CURRENT_TIMESTAMP/ON UPDATE match the driver's loc=UTC parsing —
	// otherwise DATETIME columns mix server-local DEFAULTs with
	// Go-written UTC values (an 8h skew on CST servers breaks lease
	// expiry comparisons and client timestamps).
	// '+00:00' URL-encoded; appended AFTER Sprintf so the format parser
	// never sees the % escapes.
	const sysVars = "&time_zone=%27%2B00%3A00%27"
	return fmt.Sprintf(
		"%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=true&loc=UTC&multiStatements=true&timeout=10s&readTimeout=60s&writeTimeout=60s",
		d.User, d.Password, d.Host, d.Port, d.Name,
	) + sysVars
}

type RedisMode string

const (
	RedisModeStandalone RedisMode = "standalone"
	RedisModeCluster    RedisMode = "cluster"
)

type RedisConfig struct {
	Mode                RedisMode
	Host                string
	Port                int
	DB                  int
	Password            string
	ClusterNodes        []string
	ClusterMaxRedirects int
	KeyPrefix           string
	PoolSize            int
	MinIdleConns        int
	MaxIdleConns        int
	ConnectTimeout      time.Duration
	Timeout             time.Duration
	PoolTimeout         time.Duration
}

func (r RedisConfig) Addr() string { return net.JoinHostPort(r.Host, strconv.Itoa(r.Port)) }

func (r RedisConfig) Addrs() []string {
	if r.Mode == RedisModeCluster {
		return append([]string(nil), r.ClusterNodes...)
	}
	return []string{r.Addr()}
}

func (r RedisConfig) Target() string {
	if r.Mode == RedisModeCluster {
		return strings.Join(r.ClusterNodes, ",")
	}
	return r.Addr()
}

func (r RedisConfig) Validate() error {
	switch r.Mode {
	case RedisModeStandalone:
		if strings.TrimSpace(r.Host) == "" || r.Port < 1 || r.Port > 65535 {
			return fmt.Errorf("standalone mode requires a valid REDIS_HOST and REDIS_PORT")
		}
	case RedisModeCluster:
		if len(r.ClusterNodes) == 0 {
			return fmt.Errorf("cluster mode requires REDIS_CLUSTER_NODES")
		}
		for _, addr := range r.ClusterNodes {
			host, port, err := net.SplitHostPort(addr)
			portNumber, portErr := strconv.Atoi(port)
			if err != nil || strings.TrimSpace(host) == "" || portErr != nil || portNumber < 1 || portNumber > 65535 {
				return fmt.Errorf("invalid Redis cluster node %q: expected host:port", addr)
			}
		}
		if r.DB != 0 {
			return fmt.Errorf("cluster mode requires REDIS_DATABASE=0 (Redis Cluster has no logical databases)")
		}
	default:
		return fmt.Errorf("REDIS_MODE must be %q or %q, got %q", RedisModeStandalone, RedisModeCluster, r.Mode)
	}
	if r.PoolSize < 1 || r.MinIdleConns < 0 || r.MaxIdleConns < 0 {
		return fmt.Errorf("Redis pool sizes must be non-negative and REDIS_POOL_SIZE must be positive")
	}
	if r.MaxIdleConns > r.PoolSize || r.MinIdleConns > r.PoolSize || r.MinIdleConns > r.MaxIdleConns {
		return fmt.Errorf("Redis idle connection bounds must satisfy min-idle <= max-idle <= pool-size")
	}
	return nil
}

func (r RedisConfig) Key(parts ...string) string {
	return r.KeyPrefix + ":" + strings.Join(parts, ":")
}

type EnterpriseConfig struct {
	ACLEnabled  bool
	RBACEnabled bool
}

type FeishuConfig struct {
	AppID       string
	AppSecret   string
	RedirectURI string
	BaseURL     string
}

type AilyConfig struct {
	BaseURL              string
	StartRateLimitPerSec int
	// MaxInflight is the BOOTSTRAP / override value for the provider-wide
	// concurrency cap. The provider catalog row (providers.max_inflight)
	// is authoritative when present — see app.Build.
	MaxInflight    int
	PollBackoff    []time.Duration
	StreamTimeout  time.Duration
	RequestTimeout time.Duration
}

type RunnerConfig struct {
	LeaseSeconds      time.Duration
	HeartbeatInterval time.Duration
	ClaimBatch        int
	WorkerID          string
	Concurrency       int
	ReaperInterval    time.Duration
	RelayInterval     time.Duration
	RequeueDelay      time.Duration
	// PriorityWeights is the weighted fair scheduling share of
	// [interactive, retry, scheduled] class streams.
	PriorityWeights []int

	// User-level admission (评测 P1-7): provider capacity is finite, so an
	// authenticated client must not be able to grow the MySQL backlog
	// without bound. UserRunQPS is a per-user GCRA limit on run creation;
	// UserMaxOutstanding caps that user's queued+running runs;
	// UserMaxSchedules caps how many schedules one user may own.
	UserRunQPS         int
	UserMaxOutstanding int
	UserMaxSchedules   int
	// UserMaxPendingManual caps the run-now pending queue per schedule
	// (复审 P1-3): a run-now behind an active execution creates a PENDING
	// occurrence that no outstanding-run limit sees, so without this cap
	// a loop on /run-now could enqueue unbounded future work.
	// 1 = "already queued one for you" UI semantics.
	UserMaxPendingManual int
}

type SessionConfig struct {
	TTL        time.Duration
	CookieName string
	Secure     bool
}

type StorageConfig struct {
	Driver      string // localfs | s3
	LocalRoot   string
	S3Endpoint  string
	S3Region    string
	S3Bucket    string
	S3AccessKey string
	S3SecretKey string
	S3UseSSL    bool
}

type AuthConfig struct {
	// TokenEncryptionKey encrypts Feishu refresh tokens at rest (AES-256-GCM).
	// 32 bytes base64 or raw passphrase; required in production.
	TokenEncryptionKey string
	// Argon2id parameters for local admin passwords.
	AdminBootstrapUsername string
	AdminBootstrapPassword string
	CSRFCookieName         string
	TrustedProxyCIDRs      []string
}

type OTelConfig struct {
	Endpoint     string
	Insecure     bool
	ServiceName  string
	SamplingRate float64
}

// SSEConfig bounds the process-local SSE Hub (Batch 4 — SSE Hub).
//
// Every field is a memory bound on a long-lived process, so a malformed or
// non-positive value falls back to its default rather than being read as
// "unlimited": an unbounded replay cache or subscriber queue is exactly the
// failure the Hub's bounds exist to prevent.
//
// The defaults mirror sse.DefaultHubOptions (the same numbers, for the same
// reasons); sse.HubOptions.normalized applies them a second time so a
// programmatically constructed config cannot disable a bound either.
//
//	2048 durable cache events / run      8 MiB durable cache / run
//	1024 pending live frames / subscriber  4 MiB pending queue / subscriber
//	30s idle retention
type SSEConfig struct {
	// HubCacheEvents / HubCacheBytes bound the per-run durable replay cache.
	// BOTH are enforced: one content.chunk can carry a large slice of an
	// answer, so an event-count-only bound does not bound memory.
	HubCacheEvents int
	HubCacheBytes  int64
	// SubscriberEvents / SubscriberBytes bound one connection's pending live
	// queue. Exceeding either drops THAT connection only.
	SubscriberEvents int
	SubscriberBytes  int64
	// HubIdleTTL is how long a hub survives with no subscribers. The browser
	// reconnects ~2s after a drop, so evicting immediately would rebuild the
	// Redis subscription on every blip.
	HubIdleTTL time.Duration
}

// Load reads configuration from the selected environment profile. Real
// process environment variables always win over dotenv files.
func Load(searchPaths ...string) (*Config, error) {
	profile, explicitProfile, err := selectedEnvironmentProfile()
	if err != nil {
		return nil, err
	}
	loadDotEnv(profile, searchPaths...)

	runtimeEnv := profile
	if !explicitProfile {
		runtimeEnv, err = normalizeEnvironmentProfile(getEnv("APP_ENV", profile))
		if err != nil {
			return nil, err
		}
	}

	cfg := &Config{
		Env:           runtimeEnv,
		LogLevel:      getEnv("LOG_LEVEL", "info"),
		PublicBaseURL: publicBaseURL(),
		APIAddr:       getEnv("API_ADDR", ":8080"),
		StreamAddr:    getEnv("STREAM_ADDR", ":8081"),
		MetricsAddr:   getEnv("METRICS_ADDR", ":9090"),
		PPROFEnabled:  getEnvBool("PPROF_ENABLED", false),
		Database: DatabaseConfig{
			Host:            getEnv("DB_HOST", "127.0.0.1"),
			Port:            getEnvInt("DB_PORT", 3306),
			Name:            getEnv("DB_NAME", "xiaoan"),
			User:            getEnv("DB_USER", "root"),
			Password:        getEnv("DB_PASSWORD", ""),
			MaxOpenConns:    getEnvInt("DB_MAX_OPEN_CONNS", 40),
			MaxIdleConns:    getEnvInt("DB_MAX_IDLE_CONNS", 10),
			ConnMaxLifetime: getEnvDuration("DB_CONN_MAX_LIFETIME", 30*time.Minute),
			ConnMaxIdleTime: getEnvDuration("DB_CONN_MAX_IDLE_TIME", 5*time.Minute),
			QueryTimeout:    getEnvDuration("DB_QUERY_TIMEOUT", 15*time.Second),
		},
		Redis: RedisConfig{
			Mode:                RedisMode(strings.ToLower(strings.TrimSpace(getEnv("REDIS_MODE", string(RedisModeStandalone))))),
			Host:                getEnv("REDIS_HOST", "127.0.0.1"),
			Port:                getEnvInt("REDIS_PORT", 6379),
			DB:                  getEnvInt("REDIS_DATABASE", getEnvInt("REDIS_DB", 0)),
			Password:            getEnv("REDIS_PASSWORD", ""),
			ClusterNodes:        parseCSV(getEnv("REDIS_CLUSTER_NODES", "")),
			ClusterMaxRedirects: getEnvPositiveInt("REDIS_CLUSTER_MAX_REDIRECTS", 3),
			KeyPrefix:           getEnv("REDIS_KEY_PREFIX", "xiaoan3"),
			PoolSize:            getEnvPositiveInt("REDIS_POOL_SIZE", 20),
			MinIdleConns:        getEnvPositiveInt("REDIS_MIN_IDLE_CONNS", 5),
			MaxIdleConns:        getEnvPositiveInt("REDIS_MAX_IDLE_CONNS", 10),
			ConnectTimeout:      getEnvPositiveDuration("REDIS_CONNECT_TIMEOUT", 10*time.Second),
			Timeout:             getEnvPositiveDuration("REDIS_TIMEOUT", 5*time.Second),
			PoolTimeout:         getEnvPositiveDuration("REDIS_POOL_TIMEOUT", 3*time.Second),
		},
		Feishu: FeishuConfig{
			AppID:       getEnv("FEISHU_APP_ID", ""),
			AppSecret:   getEnv("FEISHU_APP_SECRET", ""),
			RedirectURI: getEnv("FEISHU_REDIRECT_URI", ""),
			BaseURL:     getEnv("FEISHU_BASE_URL", "https://open.feishu.cn"),
		},
		Aily: AilyConfig{
			BaseURL:              getEnv("AILY_BASE_URL", "https://open.feishu.cn/open-apis"),
			StartRateLimitPerSec: getEnvInt("AILY_START_RATE_LIMIT", 10),
			MaxInflight:          getEnvInt("AILY_MAX_INFLIGHT", 100),
			PollBackoff:          parseBackoff(getEnv("AILY_POLL_BACKOFF_SECONDS", "1,2,3,5")),
			StreamTimeout:        getEnvDuration("AILY_STREAM_TIMEOUT", 330*time.Second),
			RequestTimeout:       getEnvDuration("AILY_REQUEST_TIMEOUT", 30*time.Second),
		},
		Runner: RunnerConfig{
			LeaseSeconds:         getEnvDuration("RUN_LEASE_SECONDS", 120*time.Second),
			HeartbeatInterval:    getEnvDuration("RUN_LEASE_HEARTBEAT_SECONDS", 30*time.Second),
			ClaimBatch:           getEnvInt("RUN_CLAIM_BATCH_SIZE", 10),
			WorkerID:             getEnv("WORKER_ID", ""),
			Concurrency:          getEnvInt("WORKER_CONCURRENCY", 10),
			ReaperInterval:       getEnvDuration("RUN_REAPER_INTERVAL", 20*time.Second),
			RelayInterval:        getEnvDuration("OUTBOX_RELAY_INTERVAL", 500*time.Millisecond),
			RequeueDelay:         getEnvDuration("RUN_REQUEUE_DELAY", 5*time.Second),
			PriorityWeights:      parseWeights(getEnv("RUN_PRIORITY_WEIGHTS", "7,1,2")),
			UserRunQPS:           getEnvInt("RUN_USER_QPS", 5),
			UserMaxOutstanding:   getEnvInt("RUN_USER_MAX_OUTSTANDING", 20),
			UserMaxSchedules:     getEnvInt("SCHEDULE_USER_MAX", 50),
			UserMaxPendingManual: getEnvInt("SCHEDULE_MAX_PENDING_MANUAL", 1),
		},
		Session: SessionConfig{
			TTL:        getEnvDuration("SESSION_TTL", 12*time.Hour),
			CookieName: getEnv("SESSION_COOKIE_NAME", "studio_session"),
			Secure:     runtimeEnv == "production",
		},
		Storage: StorageConfig{
			Driver:      getEnv("STORAGE_DRIVER", "localfs"),
			LocalRoot:   getEnv("STORAGE_LOCAL_ROOT", "./data/storage"),
			S3Endpoint:  getEnv("S3_ENDPOINT", ""),
			S3Region:    getEnv("S3_REGION", "us-east-1"),
			S3Bucket:    getEnv("S3_BUCKET", ""),
			S3AccessKey: getEnv("S3_ACCESS_KEY", ""),
			S3SecretKey: getEnv("S3_SECRET_KEY", ""),
			S3UseSSL:    getEnvBool("S3_USE_SSL", true),
		},
		Auth: AuthConfig{
			TokenEncryptionKey:     getEnv("TOKEN_ENCRYPTION_KEY", ""),
			AdminBootstrapUsername: getEnv("ADMIN_BOOTSTRAP_USERNAME", "admin"),
			AdminBootstrapPassword: getEnv("ADMIN_BOOTSTRAP_PASSWORD", ""),
			CSRFCookieName:         getEnv("CSRF_COOKIE_NAME", "studio_csrf"),
			TrustedProxyCIDRs: parseCSV(getEnv("TRUSTED_PROXY_CIDRS",
				"127.0.0.1/32,::1/128,172.16.0.0/12")),
		},
		OTel: OTelConfig{
			Endpoint:     getEnv("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
			Insecure:     getEnvBool("OTEL_INSECURE", true),
			ServiceName:  getEnv("OTEL_SERVICE_NAME", "studio-backend"),
			SamplingRate: getEnvFloat("OTEL_SAMPLING_RATE", 0.1),
		},
		Enterprise: EnterpriseConfig{
			ACLEnabled:  getEnvBool("ENTERPRISE_ACL_ENABLED", false),
			RBACEnabled: getEnvBool("ENTERPRISE_RBAC_ENABLED", false),
		},
		SSE: SSEConfig{
			HubCacheEvents:   getEnvPositiveInt("SSE_HUB_CACHE_EVENTS", 2048),
			HubCacheBytes:    getEnvPositiveInt64("SSE_HUB_CACHE_BYTES", 8<<20),
			SubscriberEvents: getEnvPositiveInt("SSE_SUBSCRIBER_QUEUE_EVENTS", 1024),
			SubscriberBytes:  getEnvPositiveInt64("SSE_SUBSCRIBER_QUEUE_BYTES", 4<<20),
			HubIdleTTL:       getEnvPositiveDuration("SSE_HUB_IDLE_TTL", 30*time.Second),
		},
	}

	if err := configureDeployment(cfg); err != nil {
		return nil, err
	}
	if err := cfg.Redis.Validate(); err != nil {
		return nil, fmt.Errorf("redis config: %w", err)
	}

	if cfg.Runner.WorkerID == "" {
		host, _ := os.Hostname()
		cfg.Runner.WorkerID = fmt.Sprintf("%s-%d", host, os.Getpid())
	}
	if cfg.Env == "production" {
		if err := cfg.validateProduction(); err != nil {
			return nil, err
		}
	}
	return cfg, nil
}

func (c *Config) validateProduction() error {
	var missing []string
	if c.Feishu.AppSecret == "" {
		missing = append(missing, "FEISHU_APP_SECRET")
	}
	if c.Database.Password == "" && os.Getenv("DB_PASSWORD") == "" {
		missing = append(missing, "DB_PASSWORD")
	}
	if c.Auth.TokenEncryptionKey == "" {
		missing = append(missing, "TOKEN_ENCRYPTION_KEY")
	}
	if len(missing) > 0 {
		return fmt.Errorf("production config missing secrets: %s", strings.Join(missing, ", "))
	}
	return nil
}

func normalizeEnvironmentProfile(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "dev", "development":
		return "development", nil
	case "test":
		return "test", nil
	case "prod", "production":
		return "production", nil
	default:
		return "", fmt.Errorf("STUDIO_ENV/APP_ENV must be dev, test, or prod; got %q", raw)
	}
}

func selectedEnvironmentProfile() (profile string, explicit bool, err error) {
	if raw := strings.TrimSpace(os.Getenv("STUDIO_ENV")); raw != "" {
		profile, err = normalizeEnvironmentProfile(raw)
		return profile, true, err
	}
	if raw := strings.TrimSpace(os.Getenv("APP_ENV")); raw != "" {
		profile, err = normalizeEnvironmentProfile(raw)
		return profile, true, err
	}
	return "development", false, nil
}

func dotEnvCandidates(profile string) []string {
	switch profile {
	case "test":
		return []string{".env.test.local", ".env.development.local", ".env.local", ".env.test", ".env.development", ".env"}
	case "production":
		return []string{".env.production.local", ".env.local", ".env.production", ".env"}
	default:
		return []string{".env.development.local", ".env.local", ".env.development", ".env"}
	}
}

// loadDotEnv applies KEY=VALUE files to the process environment without
// overriding variables that are already set. Files are searched in the
// working directory and up to four parent directories. Earlier profile files
// win; later files only fill missing keys.
func loadDotEnv(profile string, searchPaths ...string) {
	var dirs []string
	dir, _ := os.Getwd()
	for i := 0; i < 5 && dir != ""; i++ {
		dirs = append(dirs, dir)
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	candidates := dotEnvCandidates(profile)
	if len(searchPaths) > 0 {
		candidates = searchPaths
	}
	for _, name := range candidates {
		var data []byte
		var err error
		for _, d := range dirs {
			data, err = os.ReadFile(filepath.Join(d, name))
			if err == nil {
				break
			}
			data = nil
		}
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			k, v, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			k = strings.TrimSpace(k)
			v = strings.Trim(strings.TrimSpace(v), `"'`)
			if _, exists := os.LookupEnv(k); !exists {
				_ = os.Setenv(k, v)
			}
		}
		_ = filepath.Base(name)
	}
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getEnvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func getEnvFloat(key string, def float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

func getEnvBool(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		switch strings.ToLower(v) {
		case "1", "true", "yes", "on":
			return true
		case "0", "false", "no", "off":
			return false
		}
	}
	return def
}

func getEnvDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
		if secs, err := strconv.ParseFloat(v, 64); err == nil {
			return time.Duration(secs * float64(time.Second))
		}
	}
	return def
}

// getEnvPositiveInt / getEnvPositiveInt64 / getEnvPositiveDuration are the
// loaders for bounds that have no meaningful "zero" reading.
//
// They differ from the plain getEnv* helpers in one deliberate way: a value
// that PARSES but is non-positive is treated exactly like unparsable input and
// falls back to the default. For a limit such as a cache size or a queue
// depth, `SSE_HUB_CACHE_EVENTS=0` is a misconfiguration, not a request for an
// unbounded buffer — and the plain helpers would happily return the zero and
// let the caller interpret it.
func getEnvPositiveInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}

func getEnvPositiveInt64(key string, def int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return def
}

func getEnvPositiveDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
		if secs, err := strconv.ParseFloat(v, 64); err == nil && secs > 0 {
			return time.Duration(secs * float64(time.Second))
		}
	}
	return def
}

func parseCSV(s string) []string {
	out := make([]string, 0)
	for _, part := range strings.Split(s, ",") {
		if value := strings.TrimSpace(part); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func parseBackoff(s string) []time.Duration {
	var out []time.Duration
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if f, err := strconv.ParseFloat(part, 64); err == nil {
			out = append(out, time.Duration(f*float64(time.Second)))
		}
	}
	if len(out) == 0 {
		out = []time.Duration{1 * time.Second, 2 * time.Second, 3 * time.Second, 5 * time.Second}
	}
	return out
}

// parseWeights parses "interactive,retry,scheduled" shares (RUN_PRIORITY_WEIGHTS)
// in execution.Worker's class order.
//
// Every class must keep a STRICTLY POSITIVE share. The platform invariant is
// that scheduled work can never starve and interactive work always leads, so
// a zero weight (a class silently disabled by a typo or a "0,1,2" copy/paste)
// is rejected exactly like non-numeric input and falls back to 7/1/2.
// Disabling a class deliberately is a future feature, not a config accident.
func parseWeights(s string) []int {
	parts := strings.Split(s, ",")
	if len(parts) != 3 {
		return []int{7, 1, 2}
	}
	out := make([]int, 0, 3)
	for _, part := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || n < 1 {
			return []int{7, 1, 2}
		}
		out = append(out, n)
	}
	return out
}

// publicBaseURL uses the configured browser origin; the explicit value also
// supports reverse-proxy path prefixes. Deployments should set PUBLIC_BASE_URL.
func publicBaseURL() string {
	if base := getEnv("PUBLIC_BASE_URL", ""); base != "" {
		return strings.TrimRight(base, "/")
	}
	callback := getEnv("FEISHU_REDIRECT_URI", "http://localhost:3030/auth/feishu/callback")
	if u, err := url.Parse(callback); err == nil && u.Host != "" && (u.Scheme == "http" || u.Scheme == "https") {
		return u.Scheme + "://" + u.Host
	}
	return ""
}
