package aily

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestUserAccessTokenCacheKeyMatchesResolver(t *testing.T) {
	store := newAuthRedisStub()
	r, _ := authUserResolver(t, store)
	if got, want := UserAccessTokenCacheKey(r.Redis, 7), r.uatKey(7); got != want {
		t.Fatalf("shared UAT key = %q, resolver key = %q", got, want)
	}
}

func TestAuthUserConcurrentMissAcrossResolvers(t *testing.T) {
	store := newAuthRedisStub()
	r1, m1 := authUserResolver(t, store)
	r2, m2 := authUserResolver(t, store)
	// Either resolver can win; leave the losing resolver's expectation unused.
	authExpectIdentity(t, m1, r1, 7, "old-refresh")
	authExpectIdentity(t, m2, r2, 7, "old-refresh")
	store.barrierKey, store.barrierCount, store.barrier = r1.uatKey(7), 2, make(chan struct{})
	var refreshes, rotations atomic.Int32
	api := authTokenAPIStub{refresh: func(ctx context.Context, token string) (*TokenResult, error) {
		if token != "old-refresh" {
			return nil, fmt.Errorf("unexpected refresh token")
		}
		refreshes.Add(1)
		return &TokenResult{AccessToken: "uat", RefreshToken: "new-refresh", ExpiresIn: 3600}, nil
	}}
	rotate := func(context.Context, int64, string, *time.Time) error { rotations.Add(1); return nil }
	r1.Feishu, r2.Feishu, r1.OnRotate, r2.OnRotate = api, api, rotate, rotate
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	results := make(chan error, 2)
	for _, r := range []*AuthResolver{r1, r2} {
		go func(r *AuthResolver) {
			token, err := r.UserAccessToken(ctx, 7)
			if err == nil && token != "uat" {
				err = fmt.Errorf("token = %q", token)
			}
			results <- err
		}(r)
	}
	for range 2 {
		if err := <-results; err != nil {
			t.Error(err)
		}
	}
	if n := refreshes.Load(); n != 1 {
		t.Errorf("same rotating refresh token used %d times; want 1", n)
	}
	if n := rotations.Load(); n != 1 {
		t.Errorf("rotation persisted %d times; want 1", n)
	}
}

func TestAuthTenantConcurrentMissAcrossResolvers(t *testing.T) {
	store := newAuthRedisStub()
	var calls atomic.Int32
	api := authTokenAPIStub{tenant: func(context.Context) (*TokenResult, error) {
		calls.Add(1)
		return &TokenResult{AccessToken: "tat", ExpiresIn: 3600}, nil
	}}
	r1 := &AuthResolver{Redis: store.client(t), Feishu: api}
	r2 := &AuthResolver{Redis: store.client(t), Feishu: api}
	store.barrierKey, store.barrierCount, store.barrier = r1.tatKey("app"), 2, make(chan struct{})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	results := make(chan error, 2)
	for _, r := range []*AuthResolver{r1, r2} {
		go func(r *AuthResolver) {
			token, err := r.TenantAccessToken(ctx, "app")
			if err == nil && token != "tat" {
				err = fmt.Errorf("token = %q", token)
			}
			results <- err
		}(r)
	}
	for range 2 {
		if err := <-results; err != nil {
			t.Error(err)
		}
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("tenant token API called %d times; want 1", n)
	}
}

func TestAuthRotationPersistenceFailureIsNotSuccess(t *testing.T) {
	store := newAuthRedisStub()
	r, mock := authUserResolver(t, store)
	authExpectIdentity(t, mock, r, 7, "old-refresh")
	failure := errors.New("rotation database unavailable")
	r.OnRotate = func(context.Context, int64, string, *time.Time) error { return failure }
	r.Feishu = authTokenAPIStub{refresh: func(context.Context, string) (*TokenResult, error) {
		return &TokenResult{AccessToken: "uat", RefreshToken: "new-refresh", ExpiresIn: 3600}, nil
	}}
	token, err := r.UserAccessToken(context.Background(), 7)
	if !errors.Is(err, failure) {
		t.Errorf("error = %v, want persistence failure", err)
	}
	if token != "" {
		t.Errorf("returned successful access token after failed rotation")
	}
	if store.get(r.uatKey(7)) != "" {
		t.Error("cached successful token after failed rotation")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestAuthWaiterCancellationDoesNotRefresh(t *testing.T) {
	for _, mode := range []string{"user", "tenant"} {
		t.Run(mode, func(t *testing.T) {
			store := newAuthRedisStub()
			r, _ := authUserResolver(t, store)
			key := r.uatKey(7)
			if mode == "tenant" {
				key = r.tatKey("app")
			}
			store.put(key+":refresh", "another-process")
			var calls atomic.Int32
			api := func(context.Context) (*TokenResult, error) {
				calls.Add(1)
				return &TokenResult{AccessToken: "unexpected"}, nil
			}
			r.Feishu = authTokenAPIStub{tenant: api, refresh: func(ctx context.Context, _ string) (*TokenResult, error) { return api(ctx) }}
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
			defer cancel()
			var token string
			var err error
			if mode == "user" {
				token, err = r.UserAccessToken(ctx, 7)
			} else {
				token, err = r.TenantAccessToken(ctx, "app")
			}
			if !errors.Is(err, context.DeadlineExceeded) || token != "" {
				t.Fatalf("token=%q error=%v, want deadline exceeded", token, err)
			}
			if calls.Load() != 0 {
				t.Error("waiter called upstream without owning lease")
			}
			if store.get(key+":refresh") != "another-process" {
				t.Error("waiter deleted another process's lease")
			}
		})
	}
}

func TestAuthRedisFailureFailsClosed(t *testing.T) {
	for _, command := range []string{"get", "set"} {
		t.Run(command, func(t *testing.T) {
			store := newAuthRedisStub()
			failure := errors.New("redis unavailable")
			store.failCommand, store.failure = command, failure
			var calls atomic.Int32
			r := &AuthResolver{Redis: store.client(t), Feishu: authTokenAPIStub{tenant: func(context.Context) (*TokenResult, error) {
				calls.Add(1)
				return &TokenResult{AccessToken: "tat"}, nil
			}}}
			token, err := r.TenantAccessToken(context.Background(), "app")
			if token != "" || !errors.Is(err, failure) {
				t.Errorf("token=%q error=%v; want Redis failure", token, err)
			}
			if calls.Load() != 0 {
				t.Error("called upstream without Redis coordination")
			}
		})
	}
}

func TestAuthLostLeaseCannotPersistPublishOrUnlockSuccessor(t *testing.T) {
	store := newAuthRedisStub()
	r, mock := authUserResolver(t, store)
	authExpectIdentity(t, mock, r, 7, "old-refresh")
	key := r.uatKey(7)
	var rotations atomic.Int32
	r.OnRotate = func(context.Context, int64, string, *time.Time) error { rotations.Add(1); return nil }
	r.Feishu = authTokenAPIStub{refresh: func(context.Context, string) (*TokenResult, error) {
		store.put(key+":refresh", "successor-owner")
		return &TokenResult{AccessToken: "uat", RefreshToken: "new-refresh"}, nil
	}}
	token, err := r.UserAccessToken(context.Background(), 7)
	if token != "" || err == nil {
		t.Errorf("token=%q error=%v; want lost lease failure", token, err)
	}
	if rotations.Load() != 0 {
		t.Error("persisted rotation after losing lease")
	}
	if store.get(key) != "" {
		t.Error("published token after losing lease")
	}
	if store.get(key+":refresh") != "successor-owner" {
		t.Error("deleted successor's lease")
	}
}

func TestAuthMissingRotationHandlerFailsBeforeRefresh(t *testing.T) {
	store := newAuthRedisStub()
	r, mock := authUserResolver(t, store)
	authExpectIdentity(t, mock, r, 7, "old-refresh")
	r.OnRotate = nil
	var calls atomic.Int32
	r.Feishu = authTokenAPIStub{refresh: func(context.Context, string) (*TokenResult, error) {
		calls.Add(1)
		return &TokenResult{AccessToken: "uat", RefreshToken: "new-refresh"}, nil
	}}
	token, err := r.UserAccessToken(context.Background(), 7)
	if token != "" || err == nil {
		t.Errorf("token=%q error=%v; want missing persistence handler error", token, err)
	}
	if calls.Load() != 0 {
		t.Error("risked consuming refresh token without persistence handler")
	}
}

func TestAuthRotationMustFinishBeforeCacheOrFollowers(t *testing.T) {
	store := newAuthRedisStub()
	r, mock := authUserResolver(t, store)
	authExpectIdentity(t, mock, r, 7, "old-refresh")
	entered, finish := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	r.Feishu = authTokenAPIStub{refresh: func(context.Context, string) (*TokenResult, error) {
		calls.Add(1)
		return &TokenResult{AccessToken: "uat", RefreshToken: "new-refresh", ExpiresIn: 3600, RefreshTokenExpiresIn: 7200}, nil
	}}
	r.OnRotate = func(ctx context.Context, id int64, enc string, expires *time.Time) error {
		plain, err := r.GCM.Decrypt(enc)
		if err != nil || plain != "new-refresh" || enc == plain || id != 99 {
			return fmt.Errorf("invalid rotation payload")
		}
		if expires == nil || time.Until(*expires) < 7190*time.Second || time.Until(*expires) > 7210*time.Second {
			return fmt.Errorf("invalid rotation expiry")
		}
		close(entered)
		select {
		case <-finish:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	follower := &AuthResolver{DB: r.DB, Redis: store.client(t), GCM: r.GCM, Feishu: r.Feishu, OnRotate: r.OnRotate}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		token, err := r.UserAccessToken(ctx, 7)
		if err == nil && token != "uat" {
			err = fmt.Errorf("token=%q", token)
		}
		done <- err
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("leader did not reach persistence")
	}
	if store.get(r.uatKey(7)) != "" {
		t.Error("access token became visible before rotation persistence")
	}
	waitCtx, stop := context.WithTimeout(ctx, 25*time.Millisecond)
	defer stop()
	if _, err := follower.UserAccessToken(waitCtx, 7); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("follower bypassed ongoing persistence: %v", err)
	}
	close(finish)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if token, err := follower.UserAccessToken(ctx, 7); err != nil || token != "uat" {
		t.Fatalf("follower cache hit: token=%q err=%v", token, err)
	}
	if calls.Load() != 1 {
		t.Error("follower repeated upstream rotation")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestAuthLeaderCancellationReleasesLease(t *testing.T) {
	store := newAuthRedisStub()
	r := &AuthResolver{Redis: store.client(t)}
	r.Feishu = authTokenAPIStub{tenant: func(ctx context.Context) (*TokenResult, error) { <-ctx.Done(); return nil, ctx.Err() }}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if token, err := r.TenantAccessToken(ctx, "app"); token != "" || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("token=%q error=%v", token, err)
	}
	key := r.tatKey("app")
	if store.get(key+":refresh") != "" {
		t.Error("caller cancellation prevented lease cleanup")
	}
	r.Feishu = authTokenAPIStub{tenant: func(context.Context) (*TokenResult, error) { return &TokenResult{AccessToken: "retry"}, nil }}
	if token, err := r.TenantAccessToken(context.Background(), "app"); token != "retry" || err != nil {
		t.Fatalf("successor token=%q error=%v", token, err)
	}
}

func TestAuthDifferentKeysDoNotBlockEachOther(t *testing.T) {
	store := newAuthRedisStub()
	r := &AuthResolver{Redis: store.client(t), Feishu: authTokenAPIStub{tenant: func(context.Context) (*TokenResult, error) { return &TokenResult{AccessToken: "other-app"}, nil }}}
	store.put(r.tatKey("busy")+":refresh", "another-owner")
	store.put(r.uatKey(7)+":refresh", "another-owner")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if token, err := r.TenantAccessToken(ctx, "free"); token != "other-app" || err != nil {
		t.Fatalf("independent app blocked: token=%q error=%v", token, err)
	}
}

func TestAuthCacheValidationAndDefaultTTL(t *testing.T) {
	for _, mode := range []string{"user", "tenant"} {
		for _, cached := range []string{"", "malformed", `{"token":"stale","expires_at":1}`, fmt.Sprintf(`{"token":"","expires_at":%d}`, time.Now().Unix()+3600), fmt.Sprintf(`{"token":"near-expiry","expires_at":%d}`, time.Now().Unix()+20), fmt.Sprintf(`{"token":"cached","expires_at":%d}`, time.Now().Unix()+3600)} {
			t.Run(mode+"/"+cached, func(t *testing.T) {
				store := newAuthRedisStub()
				r, mock := authUserResolver(t, store)
				key, ttl := r.uatKey(7), int64(7200)
				cacheHit := strings.Contains(cached, `"cached"`)
				if mode == "tenant" {
					key, ttl = r.tatKey("app"), 7000
				}
				if cached != "" {
					store.put(key, cached)
				}
				if mode == "user" && !cacheHit {
					authExpectIdentity(t, mock, r, 7, "old-refresh")
				}
				var calls atomic.Int32
				result := func(context.Context) (*TokenResult, error) {
					calls.Add(1)
					return &TokenResult{AccessToken: "fresh", RefreshToken: "old-refresh"}, nil
				}
				r.Feishu = authTokenAPIStub{tenant: result, refresh: func(ctx context.Context, _ string) (*TokenResult, error) { return result(ctx) }}
				r.OnRotate = func(context.Context, int64, string, *time.Time) error {
					t.Error("unchanged token should not be persisted")
					return nil
				}
				var token string
				var err error
				if mode == "user" {
					token, err = r.UserAccessToken(context.Background(), 7)
				} else {
					token, err = r.TenantAccessToken(context.Background(), "app")
				}
				want, wantCalls := "fresh", int32(1)
				if cacheHit {
					want, wantCalls = "cached", 0
				}
				if err != nil || token != want || calls.Load() != wantCalls {
					t.Fatalf("token=%q calls=%d error=%v", token, calls.Load(), err)
				}
				if !cacheHit {
					var record struct {
						Token     string `json:"token"`
						ExpiresAt int64  `json:"expires_at"`
					}
					if err := json.Unmarshal([]byte(store.get(key)), &record); err != nil {
						t.Fatal(err)
					}
					remaining := record.ExpiresAt - time.Now().Unix()
					if remaining < ttl-2 || remaining > ttl {
						t.Errorf("remaining TTL=%d, want %d", remaining, ttl)
					}
				}
				if err := mock.ExpectationsWereMet(); err != nil {
					t.Error(err)
				}
			})
		}
	}
}

func TestAuthInvalidResponseAndProviderFailure(t *testing.T) {
	upstreamErr := errors.New("upstream unavailable")
	for _, mode := range []string{"user", "tenant"} {
		for _, tc := range []struct {
			name   string
			result *TokenResult
			err    error
		}{
			{name: "nil response"}, {name: "empty access token", result: &TokenResult{}}, {name: "provider error", err: upstreamErr},
		} {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				store := newAuthRedisStub()
				r, mock := authUserResolver(t, store)
				key := r.uatKey(7)
				if mode == "user" {
					authExpectIdentity(t, mock, r, 7, "old-refresh")
				} else {
					key = r.tatKey("app")
				}
				r.Feishu = authTokenAPIStub{tenant: func(context.Context) (*TokenResult, error) { return tc.result, tc.err }, refresh: func(context.Context, string) (*TokenResult, error) { return tc.result, tc.err }}
				var token string
				var err error
				if mode == "user" {
					token, err = r.UserAccessToken(context.Background(), 7)
				} else {
					token, err = r.TenantAccessToken(context.Background(), "app")
				}
				if token != "" || err == nil {
					t.Errorf("token=%q error=%v", token, err)
				}
				if tc.err != nil && !errors.Is(err, tc.err) {
					t.Errorf("lost provider error: %v", err)
				}
				if store.get(key) != "" || store.get(key+":refresh") != "" {
					t.Error("cached failure or leaked lease")
				}
				if err := mock.ExpectationsWereMet(); err != nil {
					t.Error(err)
				}
			})
		}
	}
}

func TestAuthLeaseLossAtPublication(t *testing.T) {
	store := newAuthRedisStub()
	r := &AuthResolver{Redis: store.client(t)}
	key := r.tatKey("app")
	r.Feishu = authTokenAPIStub{tenant: func(context.Context) (*TokenResult, error) {
		store.put(key+":refresh", "successor")
		store.put(key, `{"token":"successor-token"}`)
		return &TokenResult{AccessToken: "stale-owner-token"}, nil
	}}
	if token, err := r.TenantAccessToken(context.Background(), "app"); token != "" || err == nil {
		t.Fatalf("token=%q error=%v", token, err)
	}
	if store.get(key) != `{"token":"successor-token"}` || store.get(key+":refresh") != "successor" {
		t.Fatal("stale owner modified successor state")
	}
}

func TestAuthWorkHasBoundedDeadline(t *testing.T) {
	store := newAuthRedisStub()
	r := &AuthResolver{Redis: store.client(t)}
	r.Feishu = authTokenAPIStub{tenant: func(ctx context.Context) (*TokenResult, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > authRefreshTimeout || time.Until(deadline) <= 0 {
			return nil, errors.New("missing bounded operation deadline")
		}
		store.mu.Lock()
		lease := store.values[r.tatKey("app")+":refresh"]
		store.mu.Unlock()
		if !lease.expires.After(deadline) || lease.value == "" {
			return nil, errors.New("lease does not outlast operation deadline")
		}
		return &TokenResult{AccessToken: "tat"}, nil
	}}
	if _, err := r.TenantAccessToken(context.Background(), "app"); err != nil {
		t.Fatal(err)
	}
}
