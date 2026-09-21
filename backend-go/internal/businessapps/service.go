// Package businessapps implements deterministic, server-only IT service connectors.
// It never invokes the run/worker/AI plane or persists upstream credentials.
package businessapps

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

var (
	ErrUnavailable = errors.New("接口暂不可用，请稍后联系服务台；账号操作请先确认实际状态，不要重复提交")
	ErrRejected    = errors.New("上游未确认操作成功，请联系服务台核实")
	ErrIdentity    = errors.New("尚未绑定可信工号或无权操作该账号，请联系管理员")
	ErrConfig      = errors.New("该服务尚未配置完成，请联系管理员")
	accountPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,32}$`)
	barcodePattern = regexp.MustCompile(`^[0-9]{20}$`)
	mobilePattern  = regexp.MustCompile(`^1[3-9][0-9]{9}$`)
	eanPattern     = regexp.MustCompile(`^69[0-9]{11}$`)
)

type Config struct {
	EboatURL, BasicAuth, Username, Password, Tenant string
	MaterialURL, MaterialAuth                       string
	OAUnlockURL, OAAppID, OASecret                  string
	TPMURL, TPMUser, TPMPassword                    string
	Accounts                                        map[string]string
	Operators                                       map[string]bool
}

// FromEnv reads only server-owned settings. Empty credentials disable that connector.
func FromEnv() Config {
	c := Config{EboatURL: os.Getenv("BUSINESS_EBOAT_URL"), BasicAuth: os.Getenv("BUSINESS_EBOAT_AUTH"), Username: os.Getenv("BUSINESS_EBOAT_USER"), Password: os.Getenv("BUSINESS_EBOAT_PASSWORD"), Tenant: os.Getenv("BUSINESS_EBOAT_TENANT"), MaterialURL: os.Getenv("BUSINESS_MATERIAL_URL"), MaterialAuth: os.Getenv("BUSINESS_MATERIAL_AUTH"), OAUnlockURL: os.Getenv("BUSINESS_OA_UNLOCK_URL"), OAAppID: os.Getenv("BUSINESS_OA_APP_ID"), OASecret: os.Getenv("BUSINESS_OA_SECRET"), TPMURL: os.Getenv("BUSINESS_TPM_URL"), TPMUser: os.Getenv("BUSINESS_TPM_USER"), TPMPassword: os.Getenv("BUSINESS_TPM_PASSWORD"), Operators: map[string]bool{}}
	// Invalid identity JSON fails closed, never falls back to a display name/user input.
	if json.Unmarshal([]byte(os.Getenv("BUSINESS_ACCOUNT_MAP")), &c.Accounts) != nil {
		c.Accounts = nil
	}
	for _, id := range strings.Split(os.Getenv("BUSINESS_OPERATOR_IDS"), ",") {
		if n, e := strconv.ParseInt(strings.TrimSpace(id), 10, 64); e == nil && n > 0 {
			c.Operators[strconv.FormatInt(n, 10)] = true
		}
	}
	return c
}

type Input struct {
	Action      string `json:"action"`
	Barcode     string `json:"barcode"`
	Query       string `json:"query"`
	UserCode    string `json:"usercode"`
	Password    string `json:"password"`
	Mobile      string `json:"mobile"`
	OfficePhone string `json:"officephone"`
	Fax         string `json:"fax"`
	Confirmed   bool   `json:"confirmed"`
}
type Result struct {
	Data               any           `json:"data"`
	Message            string        `json:"message"`
	Snapshot           *SnapshotInfo `json:"snapshot,omitempty"`
	ForwardUnavailable string        `json:"forward_unavailable,omitempty"`
}
type Status struct {
	Configured      bool   `json:"configured"`
	Account         string `json:"account"`
	CanManageOthers bool   `json:"can_manage_others"`
	CanSubmit       bool   `json:"can_submit"`
	Reason          string `json:"reason,omitempty"`
}
type Service struct {
	config      Config
	client      *http.Client
	tokenMu     sync.Mutex
	refreshGate chan struct{}
	token       string
	expires     time.Time
}

func New(c Config) *Service {
	// Legacy LDAP changes state via GET. net/http can retry GET on a reused
	// connection. Dedicated HTTP/1 connections forbid that replay path; disable
	// HTTP/2 as well so its transport cannot replay a refused stream.
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DisableKeepAlives = true
	transport.ForceAttemptHTTP2 = false
	transport.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
	// A previously initialized DefaultTransport may retain h2 in its cloned
	// TLS ALPN list. Negotiating h2 without its handler breaks HTTPS requests.
	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = &tls.Config{}
	} else {
		transport.TLSClientConfig = transport.TLSClientConfig.Clone()
	}
	transport.TLSClientConfig.NextProtos = []string{"http/1.1"}
	return &Service{config: c, refreshGate: make(chan struct{}, 1), client: &http.Client{
		Transport: transport, Timeout: 15 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}}
}
func IsQuery(key string) bool { return key == "barcode-query" || key == "material-query" }
func Known(key string) bool {
	switch key {
	case "barcode-query", "material-query", "oa-unlock", "oa-password", "ldap-password", "oa-phone", "tpm-account":
		return true
	}
	return false
}
func validURL(raw string) bool {
	u, e := url.Parse(raw)
	return e == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && u.User == nil && u.Fragment == ""
}
func (s *Service) Configured(key string) bool {
	c := s.config
	switch key {
	case "material-query":
		return validURL(c.MaterialURL) && c.MaterialAuth != ""
	case "oa-unlock":
		return validURL(c.OAUnlockURL) && c.OAAppID != "" && c.OASecret != ""
	case "tpm-account":
		return validURL(c.TPMURL) && c.TPMUser != "" && c.TPMPassword != ""
	default:
		return Known(key) && validURL(c.EboatURL) && c.BasicAuth != "" && c.Username != "" && c.Password != "" && c.Tenant != ""
	}
}
func (s *Service) Status(key string, userID int64) Status {
	id := strconv.FormatInt(userID, 10)
	account := s.config.Accounts[id]
	if !accountPattern.MatchString(account) {
		account = ""
	}
	v := Status{Configured: s.Configured(key), Account: account, CanManageOthers: s.config.Operators[id]}
	v.CanSubmit = v.Configured && (IsQuery(key) || v.Account != "" || v.CanManageOthers)
	if !v.Configured {
		v.Reason = ErrConfig.Error()
	} else if !v.CanSubmit {
		v.Reason = ErrIdentity.Error()
	}
	return v
}
func (s *Service) Target(userID int64, requested string) (string, error) {
	id := strconv.FormatInt(userID, 10)
	own := s.config.Accounts[id]
	if requested == "" {
		requested = own
	}
	if !accountPattern.MatchString(requested) || (!s.config.Operators[id] && (own == "" || requested != own)) {
		return "", ErrIdentity
	}
	return requested, nil
}
func Validate(key string, i Input) error {
	bad := func(s string) error { return errors.New(s) }
	if !Known(key) {
		return bad("不支持的应用")
	}
	if IsQuery(key) {
		if key == "barcode-query" {
			if !barcodePattern.MatchString(i.Barcode) || (i.Action != "production" && i.Action != "flow") {
				return bad("请输入20位数字条码并选择查询类型")
			}
		}
		if key == "material-query" {
			if (i.Action != "article" && i.Action != "ean") || len(i.Query) == 0 || len(i.Query) > 100 || strings.EqualFold(i.Query, "a") || strings.ContainsAny(i.Query, "\r\n\x00") {
				return bad("请输入有效货号或69码（a为接口保留值）")
			}
			if i.Action == "ean" && !eanPattern.MatchString(i.Query) {
				return bad("请输入69开头的13位数字")
			}
		}
		return nil
	}
	if !i.Confirmed {
		return bad("请先确认本次账号操作")
	}
	if key == "tpm-account" && i.Action != "unlock" && i.Action != "lock" && i.Action != "reset" {
		return bad("请选择有效的TPM操作")
	}
	if key == "oa-password" || key == "ldap-password" || (key == "tpm-account" && i.Action == "reset") {
		if len(i.Password) < 12 || len(i.Password) > 64 {
			return bad("密码需为12—64位，包含字母和数字")
		}
		letter, digit := false, false
		for _, r := range i.Password {
			if unicode.IsSpace(r) || unicode.IsControl(r) {
				return bad("密码不能包含空白或控制字符")
			}
			letter = letter || unicode.IsLetter(r)
			digit = digit || unicode.IsDigit(r)
		}
		if !letter || !digit {
			return bad("密码必须包含字母和数字")
		}
	}
	if key == "oa-phone" {
		if !mobilePattern.MatchString(i.Mobile) {
			return bad("请输入有效的11位手机号码")
		}
		for _, v := range []string{i.OfficePhone, i.Fax} {
			if len(v) > 32 || strings.ContainsAny(v, "\r\n\x00") {
				return bad("办公电话或传真格式不正确")
			}
		}
	}
	return nil
}

// request deliberately drops URL-bearing transport errors and all upstream error bodies.
// Redirects and automatic retries are forbidden, especially for the legacy GET password API.
func (s *Service) request(ctx context.Context, method, endpoint string, body []byte, headers map[string]string) ([]byte, int, error) {
	req, e := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if e != nil {
		return nil, 0, ErrConfig
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, e := s.client.Do(req)
	if e != nil {
		return nil, 0, ErrUnavailable
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, res.StatusCode, ErrUnavailable
	}
	b, e := io.ReadAll(io.LimitReader(res.Body, 2*1024*1024+1))
	if e != nil || len(b) > 2*1024*1024 {
		return nil, res.StatusCode, ErrUnavailable
	}
	return b, res.StatusCode, nil
}
func (s *Service) cachedToken() string {
	s.tokenMu.Lock()
	defer s.tokenMu.Unlock()
	if time.Now().Before(s.expires) {
		return s.token
	}
	return ""
}
func (s *Service) eboatToken(ctx context.Context) (string, error) {
	if token := s.cachedToken(); token != "" {
		return token, nil
	}
	select {
	case s.refreshGate <- struct{}{}:
	case <-ctx.Done():
		return "", ErrUnavailable
	}
	defer func() { <-s.refreshGate }()
	if token := s.cachedToken(); token != "" {
		return token, nil
	}
	c := s.config
	body := url.Values{"grant_type": {"password"}, "scope": {"all"}, "username": {c.Username}, "password": {c.Password}, "tenantId": {c.Tenant}}
	b, _, e := s.request(ctx, "POST", strings.TrimRight(c.EboatURL, "/")+"/api/blade-auth/oauth/token", []byte(body.Encode()), map[string]string{"Authorization": c.BasicAuth, "Tenant-Id": c.Tenant, "Content-Type": "application/x-www-form-urlencoded"})
	if e != nil {
		return "", e
	}
	var v struct {
		AccessToken string `json:"access_token"`
		Type        string `json:"token_type"`
		Expires     int64  `json:"expires_in"`
	}
	if json.Unmarshal(b, &v) != nil || v.AccessToken == "" || v.Type == "" || v.Expires <= 0 || v.Expires > 31536000 {
		return "", ErrUnavailable
	}
	token := v.Type + " " + v.AccessToken
	ttl := time.Duration(v.Expires) * time.Second
	if ttl > time.Minute {
		ttl -= time.Minute
	} else {
		ttl /= 2
	}
	s.tokenMu.Lock()
	s.token = token
	s.expires = time.Now().Add(ttl)
	s.tokenMu.Unlock()
	return token, nil
}

// Never let a delayed old-token 401 evict a newer successful refresh.
func (s *Service) invalidateToken(used string) {
	if used == "" {
		return
	}
	s.tokenMu.Lock()
	defer s.tokenMu.Unlock()
	if s.token == used {
		s.token = ""
	}
}
func explicitFailure(data any) bool {
	if data == false {
		return true
	}
	if envelope, ok := data.(map[string]any); ok {
		if success, exists := envelope["success"]; exists && success != true {
			return true
		}
		if code, exists := envelope["code"]; exists && code != float64(200) && code != "200" {
			return true
		}
		if nested, exists := envelope["data"]; exists {
			return explicitFailure(nested)
		}
	}
	return false
}
func (s *Service) Execute(ctx context.Context, key string, i Input) (Result, error) {
	if e := Validate(key, i); e != nil {
		return Result{}, e
	}
	if !s.Configured(key) {
		return Result{}, ErrConfig
	}
	if !IsQuery(key) && !accountPattern.MatchString(i.UserCode) {
		return Result{}, ErrIdentity
	}
	// Bound the whole operation including authentication, not just each HTTP exchange.
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	c := s.config
	method := "POST"
	endpoint := ""
	headers := map[string]string{}
	var body []byte
	switch key {
	case "material-query":
		method = "GET"
		u, _ := url.Parse(c.MaterialURL)
		q := u.Query()
		q.Set("hh", "a")
		q.Set("code", "a")
		if i.Action == "article" {
			q.Set("hh", i.Query)
		} else {
			q.Set("code", i.Query)
		}
		u.RawQuery = q.Encode()
		endpoint = u.String()
		headers["Authorization"] = c.MaterialAuth
	case "oa-unlock":
		endpoint = c.OAUnlockURL
		body, _ = json.Marshal(map[string]any{"usercode": i.UserCode, "passwordlock": 0, "appid": c.OAAppID, "secret": c.OASecret})
		headers["Content-Type"] = "application/json"
	case "tpm-account":
		endpoint = c.TPMURL
		headers["usercode"] = c.TPMUser
		headers["password"] = c.TPMPassword
		headers["Content-Type"] = "application/x-www-form-urlencoded"
		lock := "0"
		if i.Action == "lock" {
			lock = "1"
		}
		v := url.Values{"usercode": {i.UserCode}, "lock": {lock}}
		if i.Action == "reset" {
			v.Set("password", i.Password)
		}
		body = []byte(v.Encode())
	default:
		token, e := s.eboatToken(ctx)
		if e != nil {
			return Result{}, e
		}
		headers["Authorization"] = c.BasicAuth
		headers["Blade-Auth"] = token
		base := strings.TrimRight(c.EboatURL, "/")
		switch key {
		case "barcode-query":
			method = "GET"
			path := "queryBarcode"
			if i.Action == "flow" {
				path = "queryBarflow"
			}
			endpoint = base + "/api/blade-ums/feishu/bar/" + path + "?" + url.Values{"barCode": {i.Barcode}}.Encode()
		case "ldap-password":
			method = "GET"
			endpoint = base + "/api/blade-ums/ldap/ldapModefyPwd?" + url.Values{"userCode": {i.UserCode}, "pwd": {i.Password}}.Encode()
		case "oa-password":
			endpoint = base + "/api/blade-ums/feishu/eboat/resetOaPwd"
			body, _ = json.Marshal(map[string]string{"usercode": i.UserCode, "password": i.Password})
		case "oa-phone":
			endpoint = base + "/api/blade-ums/feishu/eboat/modefyPhone"
			v := map[string]string{"usercode": i.UserCode, "mobile": i.Mobile}
			if i.OfficePhone != "" {
				v["officephone"] = i.OfficePhone
			}
			if i.Fax != "" {
				v["fax"] = i.Fax
			}
			body, _ = json.Marshal(v)
		}
		headers["Content-Type"] = "application/json"
	}
	b, status, e := s.request(ctx, method, endpoint, body, headers)
	if status == 401 {
		s.invalidateToken(headers["Blade-Auth"])
	} // Never replay this request.
	if e != nil {
		return Result{}, e
	}
	var data any
	if key == "material-query" {
		// FDL's response schema is not specified. Preserve the entire object;
		// fields named code/data may be material business fields, not an envelope.
		if json.Unmarshal(b, &data) != nil {
			text := strings.TrimSpace(string(b))
			if strings.HasPrefix(strings.ToLower(text), "<!doctype html") || strings.HasPrefix(strings.ToLower(text), "<html") {
				return Result{}, ErrRejected
			}
			data = text
		}
		if obj, ok := data.(map[string]any); ok {
			if success, exists := obj["success"]; exists && success == false {
				return Result{}, ErrRejected
			}
			// Observed FDL envelope: output/code/message. Only interpret status when
			// the message marker is present and code is numeric; preserve its structure.
			if _, hasMessage := obj["message"]; hasMessage {
				if code, ok := obj["code"].(float64); ok && code != 0 && code != 200 {
					return Result{}, ErrRejected
				}
			}
		}
		return Result{Data: data, Message: "查询完成"}, nil
	}
	if json.Unmarshal(b, &data) != nil {
		return Result{}, ErrRejected
	}
	if !IsQuery(key) && explicitFailure(data) {
		return Result{}, ErrRejected
	}
	if envelope, ok := data.(map[string]any); ok {
		if success, exists := envelope["success"]; exists && success != true {
			return Result{}, ErrRejected
		}
		code, hasCode := envelope["code"]
		if hasCode && code != float64(200) && code != "200" && !(key == "material-query" && (code == float64(0) || code == "0")) {
			return Result{}, ErrRejected
		}
		if !IsQuery(key) && !hasCode {
			return Result{}, ErrRejected
		}
		if d, exists := envelope["data"]; exists {
			data = d
		}
	} else if !IsQuery(key) {
		return Result{}, ErrRejected
	}
	if !IsQuery(key) {
		return Result{Message: "操作成功，请使用新信息重新登录或核实账号状态"}, nil
	}
	return Result{Data: data, Message: "查询完成"}, nil
}
