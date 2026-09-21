package businessapps

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestValidation(t *testing.T) {
	for _, tc := range []struct {
		k  string
		i  Input
		ok bool
	}{
		{"barcode-query", Input{Action: "production", Barcode: "71018800942761304404"}, true},
		{"barcode-query", Input{Action: "flow", Barcode: "123"}, false},
		{"material-query", Input{Action: "article", Query: "A8305"}, true},
		{"material-query", Input{Action: "article", Query: "a"}, false},
		{"oa-password", Input{Password: "SafePass!234", Confirmed: true}, true},
		{"oa-password", Input{Password: "123456", Confirmed: true}, false},
		{"oa-unlock", Input{}, false},
		{"oa-phone", Input{Mobile: "13800138000", Confirmed: true}, true},
		{"tpm-account", Input{Action: "lock", Confirmed: true}, true},
		{"tpm-account", Input{Action: "bad", Confirmed: true}, false},
	} {
		if err := Validate(tc.k, tc.i); (err == nil) != tc.ok {
			t.Errorf("%s valid=%v err=%v", tc.k, tc.ok, err)
		}
	}
}
func TestTargetCannotBeSpoofed(t *testing.T) {
	s := New(Config{Accounts: map[string]string{"7": "25180220"}, Operators: map[string]bool{"9": true}})
	if _, e := s.Target(7, "other"); e == nil {
		t.Fatal("cross-user accepted")
	}
	if v, e := s.Target(7, ""); e != nil || v != "25180220" {
		t.Fatal(v, e)
	}
	if _, e := s.Target(8, "25180220"); e == nil {
		t.Fatal("unmapped accepted")
	}
	if v, e := s.Target(9, "25180221"); e != nil || v != "25180221" {
		t.Fatal(v, e)
	}
}
func TestTokenCacheAndWireFormat(t *testing.T) {
	tokens := 0
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/blade-auth/oauth/token" {
			tokens++
			_ = r.ParseForm()
			if r.Form.Get("username") != "service" {
				t.Error("username")
			}
			_, _ = w.Write([]byte(`{"access_token":"abc","token_type":"bearer","expires_in":3600}`))
			return
		}
		if r.Header.Get("Blade-Auth") != "bearer abc" {
			t.Error("token")
		}
		if r.URL.Query().Get("barCode") != "71018800942761304404" {
			t.Error("barcode")
		}
		_, _ = w.Write([]byte(`{"code":200,"success":true,"data":"result"}`))
	}))
	defer up.Close()
	s := New(Config{EboatURL: up.URL, BasicAuth: "Basic test", Username: "service", Password: "secret", Tenant: "000000"})
	for i := 0; i < 2; i++ {
		v, e := s.Execute(context.Background(), "barcode-query", Input{Action: "production", Barcode: "71018800942761304404"})
		if e != nil || v.Data == nil {
			t.Fatal(v, e)
		}
	}
	if tokens != 1 {
		t.Fatal(tokens)
	}
}
func TestNoRetryOrLeakOnMutationFailure(t *testing.T) {
	calls := 0
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(500)
		_, _ = w.Write([]byte(`password=secret`))
	}))
	defer up.Close()
	s := New(Config{TPMURL: up.URL, TPMUser: "operator", TPMPassword: "secret"})
	_, e := s.Execute(context.Background(), "tpm-account", Input{Action: "unlock", UserCode: "25180220", Confirmed: true})
	if e == nil || strings.Contains(e.Error(), "secret") || calls != 1 {
		t.Fatal(calls, e)
	}
}
func TestMaterialSentinel(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("hh") != "A8305" || r.URL.Query().Get("code") != "a" {
			t.Error("sentinel")
		}
		_, _ = w.Write([]byte(`[{"物料":"AAA00283"}]`))
	}))
	defer up.Close()
	s := New(Config{MaterialURL: up.URL, MaterialAuth: "AppCode test"})
	if _, e := s.Execute(context.Background(), "material-query", Input{Action: "article", Query: "A8305"}); e != nil {
		t.Fatal(e)
	}
}

func TestAllWriteWireFormats(t *testing.T) {
	for _, key := range []string{"oa-password", "ldap-password", "oa-phone", "oa-unlock", "tpm-account"} {
		t.Run(key, func(t *testing.T) {
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "oauth/token") {
					_, _ = w.Write([]byte(`{"access_token":"abc","token_type":"bearer","expires_in":3600}`))
					return
				}
				switch key {
				case "ldap-password":
					if r.Method != "GET" || r.URL.Query().Get("userCode") != "001" || r.URL.Query().Get("pwd") != "SafePassword12!" {
						t.Error("LDAP payload")
					}
				case "tpm-account":
					_ = r.ParseForm()
					if r.Header.Get("usercode") != "operator" || r.Form.Get("lock") != "0" || r.Form.Get("password") != "SafePassword12!" {
						t.Error("TPM payload")
					}
				default:
					var body map[string]any
					if json.NewDecoder(r.Body).Decode(&body) != nil || body["usercode"] != "001" {
						t.Error("JSON payload")
					}
					if key == "oa-phone" {
						if body["mobile"] != "13800138000" {
							t.Error("phone")
						}
						if _, ok := body["officephone"]; ok {
							t.Error("blank optional field sent")
						}
					}
					if key == "oa-unlock" && body["passwordlock"] != float64(0) {
						t.Error("unlock")
					}
				}
				_, _ = w.Write([]byte(`{"code":200,"success":true,"data":"must-not-leak"}`))
			}))
			defer up.Close()
			s := New(Config{EboatURL: up.URL, BasicAuth: "Basic test", Username: "service", Password: "secret", Tenant: "000000", OAUnlockURL: up.URL, OAAppID: "app", OASecret: "secret", TPMURL: up.URL, TPMUser: "operator", TPMPassword: "secret"})
			res, e := s.Execute(context.Background(), key, Input{Action: "reset", UserCode: "001", Password: "SafePassword12!", Mobile: "13800138000", Confirmed: true})
			if e != nil || res.Data != nil {
				t.Fatal(res, e)
			}
		})
	}
}
func TestRejectsMalformedAndAmbiguousSuccess(t *testing.T) {
	for _, body := range []string{`{}`, `{"code":500,"msg":"secret"}`, `{"code":200,"success":false}`, `[]`, `invalid`, `{"success":true}`} {
		t.Run(body, func(t *testing.T) {
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
			defer up.Close()
			s := New(Config{OAUnlockURL: up.URL, OAAppID: "app", OASecret: "secret"})
			if _, e := s.Execute(context.Background(), "oa-unlock", Input{UserCode: "001", Confirmed: true}); e == nil {
				t.Fatal("ambiguous response accepted")
			}
		})
	}
}
func TestRedirectNeverForwardsCredentials(t *testing.T) {
	calls := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	defer target.Close()
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) }))
	defer up.Close()
	s := New(Config{OAUnlockURL: up.URL, OAAppID: "app", OASecret: "secret"})
	_, e := s.Execute(context.Background(), "oa-unlock", Input{UserCode: "001", Confirmed: true})
	if e == nil || calls != 0 {
		t.Fatal(calls, e)
	}
}
func TestEnvironmentStatusFailsClosed(t *testing.T) {
	t.Setenv("BUSINESS_ACCOUNT_MAP", `{"7":"001"}`)
	t.Setenv("BUSINESS_OPERATOR_IDS", "9,invalid")
	t.Setenv("BUSINESS_MATERIAL_URL", "https://example.test/material")
	t.Setenv("BUSINESS_MATERIAL_AUTH", "AppCode test")
	s := New(FromEnv())
	if !s.Status("material-query", 7).CanSubmit {
		t.Fatal("query not enabled")
	}
	if s.Status("oa-unlock", 7).CanSubmit {
		t.Fatal("missing config accepted")
	}
	if !s.Status("oa-unlock", 9).CanManageOthers {
		t.Fatal("operator")
	}
	t.Setenv("BUSINESS_ACCOUNT_MAP", `invalid`)
	s = New(FromEnv())
	if _, e := s.Target(7, "001"); e == nil {
		t.Fatal("bad config open")
	}
}
func TestValidationBoundaries(t *testing.T) {
	for _, tc := range []struct {
		k string
		i Input
	}{
		{"unknown", Input{}}, {"material-query", Input{Action: "ean", Query: "123"}}, {"material-query", Input{Action: "article", Query: "x\n"}},
		{"oa-password", Input{Confirmed: true, Password: "abcdefghijkl"}}, {"oa-password", Input{Confirmed: true, Password: "abcdefghij12 "}}, // gitleaks:allow -- synthetic invalid-password fixtures
		{"oa-phone", Input{Confirmed: true, Mobile: "123"}}, {"oa-phone", Input{Confirmed: true, Mobile: "13800138000", Fax: "x\n"}},
	} {
		if Validate(tc.k, tc.i) == nil {
			t.Fatal(tc.k)
		}
	}
	s := New(Config{})
	if _, e := s.Execute(context.Background(), "oa-unlock", Input{Confirmed: true}); e != ErrConfig {
		t.Fatal(e)
	}
}

func TestLDAPDisconnectDoesNotReplay(t *testing.T) {
	var writes atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "oauth/token") {
			_, _ = w.Write([]byte(`{"access_token":"abc","token_type":"bearer","expires_in":3600}`))
			return
		}
		writes.Add(1)
		conn, _, e := w.(http.Hijacker).Hijack()
		if e != nil {
			t.Error(e)
			return
		}
		_ = conn.Close()
	}))
	defer up.Close()
	s := New(Config{EboatURL: up.URL, BasicAuth: "Basic test", Username: "service", Password: "secret", Tenant: "000000"})
	_, e := s.Execute(context.Background(), "ldap-password", Input{UserCode: "001", Password: "SafePassword12!", Confirmed: true})
	if writes.Load() != 1 || e == nil {
		t.Fatalf("writes=%d err=%v", writes.Load(), e)
	}
}
func TestTokenWaitRespectsCancellation(t *testing.T) {
	s := New(Config{})
	s.refreshGate <- struct{}{}
	defer func() { <-s.refreshGate }()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, e := s.eboatToken(ctx); done <- e }()
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("cancel accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled waiter remained blocked")
	}
}
func TestOldUnauthorizedCannotEvictNewToken(t *testing.T) {
	s := New(Config{})
	s.token = "new"
	s.expires = time.Now().Add(time.Hour)
	s.invalidateToken("old")
	if s.cachedToken() != "new" {
		t.Fatal("stale 401 evicted new token")
	}
	s.invalidateToken("")
	if s.cachedToken() != "new" {
		t.Fatal("non-EBOAT 401 evicted token")
	}
	s.invalidateToken("new")
	if s.cachedToken() != "" {
		t.Fatal("failed to invalidate current token")
	}
}
func TestNestedExplicitFailures(t *testing.T) {
	for _, body := range []string{`{"code":200,"data":false}`, `{"code":200,"success":true,"data":{"success":false,"code":500}}`} {
		up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
		s := New(Config{OAUnlockURL: up.URL, OAAppID: "app", OASecret: "secret"})
		_, e := s.Execute(context.Background(), "oa-unlock", Input{UserCode: "001", Confirmed: true})
		up.Close()
		if e != ErrRejected {
			t.Fatal(e)
		}
	}
}
func TestMaterialPreservesUnknownSchemas(t *testing.T) {
	for _, body := range []string{`material A8305`, `{"code":"AAA00283","data":{"name":"material"},"unit":"件"}`, `{"output":[{"hh":"A8305"}],"code":200,"message":"success"}`} {
		up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
		s := New(Config{MaterialURL: up.URL, MaterialAuth: "AppCode test"})
		res, e := s.Execute(context.Background(), "material-query", Input{Action: "article", Query: "A8305"})
		up.Close()
		if e != nil {
			t.Fatal(e)
		}
		if strings.HasPrefix(body, "{") {
			got, _ := json.Marshal(res.Data)
			var want any
			_ = json.Unmarshal([]byte(body), &want)
			expected, _ := json.Marshal(want)
			if string(got) != string(expected) {
				t.Fatal("lost schema", string(got))
			}
		} else if res.Data != body {
			t.Fatal(res.Data)
		}
	}
}

func TestHTTPSWithHTTP2ServerUsesHTTP1(t *testing.T) {
	calls := 0
	up := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.ProtoMajor != 1 {
			t.Errorf("unexpected protocol %s", r.Proto)
		}
		if strings.Contains(r.URL.Path, "oauth/token") {
			_, _ = w.Write([]byte(`{"access_token":"abc","token_type":"bearer","expires_in":3600}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":200,"success":true,"data":{}}`))
	}))
	up.EnableHTTP2 = true
	up.StartTLS()
	defer up.Close()
	s := New(Config{EboatURL: up.URL, BasicAuth: "Basic test", Username: "service", Password: "secret", Tenant: "000000"})
	roots := x509.NewCertPool()
	roots.AddCert(up.Certificate())
	transport := s.client.Transport.(*http.Transport)
	transport.TLSClientConfig.RootCAs = roots
	if len(transport.TLSClientConfig.NextProtos) != 1 || transport.TLSClientConfig.NextProtos[0] != "http/1.1" {
		t.Fatal("ALPN advertises unsupported protocol")
	}
	_, e := s.Execute(context.Background(), "ldap-password", Input{UserCode: "001", Password: "SafePassword12!", Confirmed: true})
	if e != nil || calls != 2 {
		t.Fatal(calls, e)
	}
}

