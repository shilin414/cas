package aily

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/shilin414/cas/backend-go/internal/platform/crypto"
	"github.com/shilin414/cas/backend-go/internal/platform/redisx"
	goredis "github.com/redis/go-redis/v9"
)

// All auth tests are offline: every Redis command is intercepted, SQL uses
// sqlmock, and token endpoints are implemented by authTokenAPIStub.
// The shared hook store models separate processes, not a resolver-local mutex.
type authRedisValue struct {
	value   string
	expires time.Time
}
type authRedisStub struct {
	mu                         sync.Mutex
	values                     map[string]authRedisValue
	failCommand                string
	failure                    error
	barrierKey                 string
	barrierCount, barrierReads int
	barrier                    chan struct{}
}

func newAuthRedisStub() *authRedisStub {
	return &authRedisStub{values: make(map[string]authRedisValue)}
}
func (s *authRedisStub) client(t *testing.T) *redisx.Client {
	t.Helper()
	c := goredis.NewClient(&goredis.Options{Addr: "offline.invalid:0", MaxRetries: -1})
	c.AddHook(s)
	t.Cleanup(func() { _ = c.Close() })
	wrapped := redisx.NewWithPrefix("auth-test")
	wrapped.UniversalClient = c
	return wrapped
}
func (s *authRedisStub) DialHook(goredis.DialHook) goredis.DialHook {
	return func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("auth test attempted network access")
	}
}
func (s *authRedisStub) ProcessPipelineHook(goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(context.Context, []goredis.Cmder) error { return errors.New("unexpected auth Redis pipeline") }
}
func (s *authRedisStub) lookup(key string) string {
	v := s.values[key]
	if !v.expires.IsZero() && !time.Now().Before(v.expires) {
		delete(s.values, key)
		return ""
	}
	return v.value
}
func (s *authRedisStub) put(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[key] = authRedisValue{value: value}
}
func (s *authRedisStub) get(key string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lookup(key)
}
func (s *authRedisStub) ProcessHook(goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		if err := ctx.Err(); err != nil {
			cmd.SetErr(err)
			return err
		}
		s.mu.Lock()
		args := cmd.Args()
		key := fmt.Sprint(args[1])
		if cmd.Name() == "get" && key == s.barrierKey && s.barrierReads < s.barrierCount {
			s.barrierReads++
			if s.barrierReads == s.barrierCount {
				close(s.barrier)
			}
			s.mu.Unlock()
			select {
			case <-s.barrier:
				cmd.SetErr(goredis.Nil)
				return goredis.Nil
			case <-ctx.Done():
				cmd.SetErr(ctx.Err())
				return ctx.Err()
			}
		}
		defer s.mu.Unlock()
		if cmd.Name() == s.failCommand {
			cmd.SetErr(s.failure)
			return s.failure
		}
		switch cmd.Name() {
		case "get":
			value := s.lookup(key)
			if value == "" {
				cmd.SetErr(goredis.Nil)
				return goredis.Nil
			}
			cmd.(*goredis.StringCmd).SetVal(value)
		case "set":
			nx := false
			var expiry time.Time
			for i := 3; i < len(args); i++ {
				switch strings.ToLower(fmt.Sprint(args[i])) {
				case "nx":
					nx = true
				case "ex", "px":
					var n int64
					_, _ = fmt.Sscan(fmt.Sprint(args[i+1]), &n)
					unit := time.Second
					if fmt.Sprint(args[i]) == "px" {
						unit = time.Millisecond
					}
					expiry = time.Now().Add(time.Duration(n) * unit)
					i++
				}
			}
			if nx && s.lookup(key) != "" {
				cmd.(*goredis.BoolCmd).SetVal(false)
				return nil
			}
			var value string
			switch v := args[2].(type) {
			case []byte:
				value = string(v)
			default:
				value = fmt.Sprint(v)
			}
			s.values[key] = authRedisValue{value: value, expires: expiry}
			if nx {
				cmd.(*goredis.BoolCmd).SetVal(true)
			} else {
				cmd.(*goredis.StatusCmd).SetVal("OK")
			}
		case "eval":
			// Compare-and-delete and compare-and-publish are the only auth scripts.
			n := args[2].(int)
			lockKey, owner := fmt.Sprint(args[3]), fmt.Sprint(args[3+n])
			result := int64(0)
			if s.lookup(lockKey) == owner {
				result = 1
				if n == 1 {
					delete(s.values, lockKey)
				} else if n == 2 {
					var value string
					switch v := args[6].(type) {
					case []byte:
						value = string(v)
					default:
						value = fmt.Sprint(v)
					}
					var ttl int64
					_, _ = fmt.Sscan(fmt.Sprint(args[7]), &ttl)
					s.values[fmt.Sprint(args[4])] = authRedisValue{value: value, expires: time.Now().Add(time.Duration(ttl) * time.Second)}
				} else {
					return fmt.Errorf("unexpected auth script key count: %d", n)
				}
			}
			cmd.(*goredis.Cmd).SetVal(result)
		default:
			err := fmt.Errorf("unexpected auth Redis command: %s", cmd.Name())
			cmd.SetErr(err)
			return err
		}
		return nil
	}
}

type authTokenAPIStub struct {
	refresh func(context.Context, string) (*TokenResult, error)
	tenant  func(context.Context) (*TokenResult, error)
}

func (s authTokenAPIStub) RefreshUserToken(ctx context.Context, token string) (*TokenResult, error) {
	if s.refresh == nil {
		return nil, errors.New("unexpected user token request")
	}
	return s.refresh(ctx, token)
}
func (s authTokenAPIStub) TenantToken(ctx context.Context) (*TokenResult, error) {
	if s.tenant == nil {
		return nil, errors.New("unexpected tenant token request")
	}
	return s.tenant(ctx)
}
func authUserResolver(t *testing.T, store *authRedisStub) (*AuthResolver, sqlmock.Sqlmock) {
	t.Helper()
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	cipher, err := crypto.NewAESGCM("offline-auth-tests-only")
	if err != nil {
		t.Fatal(err)
	}
	r := &AuthResolver{DB: database, Redis: store.client(t), GCM: cipher, OnRotate: func(context.Context, int64, string, *time.Time) error { return nil }}
	return r, mock
}
func authExpectIdentity(t *testing.T, mock sqlmock.Sqlmock, r *AuthResolver, userID int64, refresh string) {
	t.Helper()
	enc, err := r.GCM.Encrypt(refresh)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	rows := sqlmock.NewRows([]string{"id", "user_id", "open_id", "union_id", "feishu_user_id", "display_name", "avatar_url", "refresh_token_enc", "refresh_token_expires_at", "last_login_at", "created_at", "updated_at"}).AddRow(99, userID, "open", "union", nil, "Test", "", enc, nil, nil, now, now)
	mock.ExpectQuery("GetFeishuIdentityByLocalUser").WithArgs(uint64(userID)).WillReturnRows(rows)
}
