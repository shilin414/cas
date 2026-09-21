package aily

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"

	db "github.com/shilin414/cas/backend-go/internal/gen/db"
	"github.com/shilin414/cas/backend-go/internal/platform/crypto"
	"github.com/shilin414/cas/backend-go/internal/platform/redisx"
)

// ProviderKey is the catalog provider key.
const ProviderKey = "feishu_aily"

// TokenEndpoints (Feishu open platform).
const (
	tenantTokenPath = "/open-apis/auth/v3/tenant_access_token/internal"
	userTokenPath   = "/open-apis/authen/v2/oauth/token"
)

// AuthResolver maps studio identity → Aily credentials.
//
// Separation invariants (§12/§13):
//   - Studio sessions never double as provider credentials.
//   - user identity (identity_mode=user) REQUIRES the user's UAT — the
//     real agent rejects app identity (10009).
//   - Refresh tokens live AES-256-GCM encrypted in MySQL; access tokens
//     only in the Redis TTL cache.
type AuthResolver struct {
	DB       *sql.DB
	Redis    *redisx.Client
	Feishu   FeishuTokenAPI
	GCM      *crypto.AESGCM
	AppID    string
	OnRotate func(ctx context.Context, identityID int64, enc string, expires *time.Time) error
}

// FeishuTokenAPI is the subset of the Feishu client needed here.
type FeishuTokenAPI interface {
	RefreshUserToken(ctx context.Context, refreshToken string) (*TokenResult, error)
	TenantToken(ctx context.Context) (*TokenResult, error)
}

// TokenResult mirrors identity.FeishuClient results to avoid an import
// cycle at this boundary.
type TokenResult struct {
	AccessToken           string
	RefreshToken          string
	ExpiresIn             int64
	RefreshTokenExpiresIn int64
}

// AuthContext is the resolved credential (in-memory only).
type AuthContext struct {
	Provider      string
	IdentityMode  string
	SubjectUserID string
	TenantID      string
	CredentialRef string
	Token         string
}

func tokenCacheKey(rdb *redisx.Client, kind, subject string) string {
	slot := crypto.HashToken(kind + ":" + subject)
	return rdb.KeyWithSlot(slot, "provider", "aily", kind, subject)
}

// UserAccessTokenCacheKey is shared with OAuth re-authorization and integration
// fixtures so every UAT writer/invalidator uses the same cluster-safe key.
func UserAccessTokenCacheKey(rdb *redisx.Client, userID int64) string {
	return tokenCacheKey(rdb, "uat", fmt.Sprintf("%d", userID))
}

func (r *AuthResolver) uatKey(userID int64) string {
	return UserAccessTokenCacheKey(r.Redis, userID)
}

func (r *AuthResolver) tatKey(appID string) string {
	return tokenCacheKey(r.Redis, "tat", appID)
}

// Refreshes are coordinated across resolvers/processes sharing this Redis
// namespace. The operation deadline leaves headroom before the lease expires.
// This is a bounded single-Redis lease, NOT a fencing protocol: Redis failover,
// pauses longer than the lease, or other refresh writers (e.g. OAuth login) that
// do not take this lease can still race. Feishu rotation and MySQL persistence
// cannot be made atomic here; a failed/ambiguous rotation may require re-login.
const (
	authRefreshTimeout  = 30 * time.Second
	authRefreshLeaseTTL = 60 * time.Second
	authRefreshPoll     = 50 * time.Millisecond
	authReleaseTimeout  = 2 * time.Second

	authReleaseScript = `if redis.call('GET', KEYS[1]) == ARGV[1] then
 return redis.call('DEL', KEYS[1])
 end
 return 0`
	// The cache key and refresh lease carry the same Redis Cluster hash tag,
	// so this two-key script is valid in both standalone and cluster modes.
	authPublishScript = `if redis.call('GET', KEYS[1]) == ARGV[1] then
 redis.call('SET', KEYS[2], ARGV[2], 'EX', ARGV[3])
 return 1
 end
 return 0`
)

func (r *AuthResolver) cachedToken(ctx context.Context, key string) (string, error) {
	cached, err := r.Redis.Get(ctx, key).Result()
	if errors.Is(err, goredis.Nil) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read feishu token cache: %w", err)
	}
	var record struct {
		Token     string  `json:"token"`
		ExpiresAt float64 `json:"expires_at"`
	}
	if json.Unmarshal([]byte(cached), &record) == nil && record.Token != "" && record.ExpiresAt-float64(time.Now().Unix()) > 30 {
		return record.Token, nil
	}
	return "", nil
}

// resolveToken fails closed when Redis coordination is unavailable. In
// particular, treating a Redis error as a miss could consume a rotating token
// concurrently. Waiters re-read the cache and can cancel independently.
func (r *AuthResolver) resolveToken(ctx context.Context, key string, defaultTTL int64, refresh func(context.Context, func() error) (*TokenResult, error)) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, authRefreshTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if token, err := r.cachedToken(ctx, key); err != nil || token != "" {
		return token, err
	}
	owner, err := crypto.RandomToken()
	if err != nil {
		return "", fmt.Errorf("create feishu refresh lease: %w", err)
	}
	lockKey := key + ":refresh"
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		acquired, err := r.Redis.SetNX(ctx, lockKey, owner, authRefreshLeaseTTL).Result()
		if err != nil {
			return "", fmt.Errorf("acquire feishu refresh lease: %w", err)
		}
		if acquired {
			break
		}
		timer := time.NewTimer(authRefreshPoll)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", ctx.Err()
		case <-timer.C:
		}
		if token, err := r.cachedToken(ctx, key); err != nil || token != "" {
			return token, err
		}
	}
	// Caller cancellation must not prevent cleanup; compare-and-delete never
	// removes a successor's lease. If cleanup fails, Redis TTL bounds its lifetime.
	defer func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), authReleaseTimeout)
		defer stop()
		_ = r.Redis.Eval(cleanup, authReleaseScript, []string{lockKey}, owner).Err()
	}()
	checkLease := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		current, err := r.Redis.Get(ctx, lockKey).Result()
		if errors.Is(err, goredis.Nil) || (err == nil && current != owner) {
			return errors.New("feishu refresh lease lost")
		}
		if err != nil {
			return fmt.Errorf("check feishu refresh lease: %w", err)
		}
		return ctx.Err()
	}
	// Another process may have refreshed between the initial miss and SET NX.
	// In the user path the DB read is inside refresh, AFTER this second lookup.
	if token, err := r.cachedToken(ctx, key); err != nil || token != "" {
		return token, err
	}
	if err := checkLease(); err != nil {
		return "", err
	}
	tokens, err := refresh(ctx, checkLease)
	if err != nil {
		return "", err
	}
	if tokens == nil || tokens.AccessToken == "" {
		return "", errors.New("feishu token response has no access token")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	ttl := tokens.ExpiresIn
	if ttl <= 0 {
		ttl = defaultTTL
	}
	record, _ := json.Marshal(map[string]any{
		"token":      tokens.AccessToken,
		"expires_at": time.Now().Unix() + ttl,
	})
	// Only the owner can publish. Keep the lease until persistence and cache
	// publication both finish, so followers never observe unpersisted rotation.
	published, err := r.Redis.Eval(ctx, authPublishScript, []string{lockKey, key}, owner, record, ttl).Int64()
	if err != nil {
		return "", fmt.Errorf("publish feishu token cache: %w", err)
	}
	if published != 1 {
		return "", errors.New("feishu refresh lease lost before cache publication")
	}
	return tokens.AccessToken, nil
}

// UserAccessToken returns a cached or freshly refreshed UAT.
func (r *AuthResolver) UserAccessToken(ctx context.Context, userID int64) (string, error) {
	return r.resolveToken(ctx, r.uatKey(userID), 7200, func(ctx context.Context, checkLease func() error) (*TokenResult, error) {
		row, err := db.New(r.DB).GetFeishuIdentityByLocalUser(ctx, uint64(userID))
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("user has no feishu identity; re-login through Feishu OAuth")
		}
		if err != nil {
			return nil, err
		}
		refreshPlain, err := r.GCM.Decrypt(row.RefreshTokenEnc.String)
		if err != nil || refreshPlain == "" {
			return nil, fmt.Errorf("user has no stored refresh token; re-login through Feishu OAuth")
		}
		// Do not consume a potentially rotating refresh token if there is no way
		// to persist its successor. Cache hits do not require a rotation callback.
		if r.OnRotate == nil {
			return nil, errors.New("feishu refresh token persistence is not configured")
		}
		if err := checkLease(); err != nil {
			return nil, err
		}
		tokens, err := r.Feishu.RefreshUserToken(ctx, refreshPlain)
		if err != nil {
			return nil, fmt.Errorf("feishu token refresh failed: %w", err)
		}
		if tokens == nil {
			return nil, errors.New("feishu token response is nil")
		}
		if err := checkLease(); err != nil {
			return nil, err
		}
		// Persist rotation before exposing or caching the access token. A valid
		// successor must be saved even if the provider omitted the access token.
		if tokens.RefreshToken != "" && tokens.RefreshToken != refreshPlain {
			enc, err := r.GCM.Encrypt(tokens.RefreshToken)
			if err != nil {
				return nil, fmt.Errorf("encrypt feishu refresh token rotation: %w", err)
			}
			var expires *time.Time
			if tokens.RefreshTokenExpiresIn > 0 {
				t := time.Now().UTC().Add(time.Duration(tokens.RefreshTokenExpiresIn) * time.Second)
				expires = &t
			}
			if err := r.OnRotate(ctx, int64(row.ID), enc, expires); err != nil {
				return nil, fmt.Errorf("persist feishu refresh token rotation: %w", err)
			}
		}
		return tokens, nil
	})
}

// TenantAccessToken returns the app TAT (tenant-mode bindings only).
func (r *AuthResolver) TenantAccessToken(ctx context.Context, appID string) (string, error) {
	return r.resolveToken(ctx, r.tatKey(appID), 7000, func(ctx context.Context, _ func() error) (*TokenResult, error) {
		return r.Feishu.TenantToken(ctx)
	})
}

// Build resolves the AuthContext for a run's identity mode.
func (r *AuthResolver) Build(ctx context.Context, userID int64, identityMode, appID string) (*AuthContext, error) {
	if identityMode == "tenant" {
		token, err := r.TenantAccessToken(ctx, appID)
		if err != nil {
			return nil, err
		}
		return &AuthContext{
			Provider:      ProviderKey,
			IdentityMode:  "tenant",
			TenantID:      appID,
			CredentialRef: "feishu_tat:" + appID,
			Token:         token,
		}, nil
	}
	token, err := r.UserAccessToken(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &AuthContext{
		Provider:      ProviderKey,
		IdentityMode:  "user",
		SubjectUserID: fmt.Sprintf("%d", userID),
		TenantID:      appID,
		CredentialRef: fmt.Sprintf("feishu_uat:%d", userID),
		Token:         token,
	}, nil
}
