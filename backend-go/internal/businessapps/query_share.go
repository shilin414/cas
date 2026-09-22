package businessapps

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shilin414/cas/backend-go/internal/feishucard"
	"github.com/shilin414/cas/backend-go/internal/platform/redisx"
)

const QuerySnapshotTTL = 24 * time.Hour

var ErrSnapshotUnavailable = errors.New("查询快照已过期或暂不可用，请重新查询")
var snapshotTokenPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func ValidSnapshotToken(s string) bool { return snapshotTokenPattern.MatchString(s) }

type QuerySnapshot struct {
	Token         string          `json:"token"`
	OwnerID       int64           `json:"owner_id"`
	ApplicationID int64           `json:"application_id"`
	RendererKey   string          `json:"renderer_key"`
	Slug          string          `json:"slug"`
	Title         string          `json:"title"`
	QueryLabel    string          `json:"query_label"`
	QueryValue    string          `json:"query_value"`
	QueriedAt     time.Time       `json:"queried_at"`
	ExpiresAt     time.Time       `json:"expires_at"`
	Data          json.RawMessage `json:"data"`
}
type SnapshotInfo struct {
	Token      string    `json:"token"`
	QueryLabel string    `json:"query_label"`
	QueryValue string    `json:"query_value"`
	QueriedAt  time.Time `json:"queried_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	CanForward bool      `json:"can_forward"`
}

func (s *QuerySnapshot) Info(ownerID int64) *SnapshotInfo {
	return &SnapshotInfo{Token: s.Token, QueryLabel: s.QueryLabel, QueryValue: s.QueryValue, QueriedAt: s.QueriedAt, ExpiresAt: s.ExpiresAt, CanForward: s.OwnerID == ownerID}
}
func NewQuerySnapshot(owner, appID int64, key, slug, title string, input Input, data json.RawMessage, now time.Time) (*QuerySnapshot, error) {
	if !IsQuery(key) || Validate(key, input) != nil || !json.Valid(data) {
		return nil, ErrSnapshotUnavailable
	}
	var random [32]byte
	if _, e := rand.Read(random[:]); e != nil {
		return nil, ErrSnapshotUnavailable
	}
	label, value := "货号", input.Query
	if key == "barcode-query" {
		label = "生产信息 · 条码"
		if input.Action == "flow" {
			label = "流向记录 · 条码"
		}
		value = input.Barcode
	} else if input.Action == "ean" {
		label = "69码"
	}
	return &QuerySnapshot{Token: hex.EncodeToString(random[:]), OwnerID: owner, ApplicationID: appID, RendererKey: key, Slug: slug, Title: title, QueryLabel: label, QueryValue: value, QueriedAt: now.UTC(), ExpiresAt: now.UTC().Add(QuerySnapshotTTL), Data: append(json.RawMessage(nil), data...)}, nil
}

// SnapshotStore keeps business data out of public sharing and the AI run plane.
// Claim returns "sent", "busy", or "claimed" with an opaque lease owner.
// An abandoned/uncertain send stays blocked for 24h; provider UUID dedupe alone expires sooner.
type SnapshotStore interface {
	Put(context.Context, *QuerySnapshot) error
	Get(context.Context, string) (*QuerySnapshot, error)
	Claim(context.Context, string, string) (string, string, error)
	Finish(context.Context, string, string, string, string) error
}
type RedisSnapshotStore struct{ Client *redisx.Client }

func (s *RedisSnapshotStore) key(token string) string { return s.Client.Key("business-query", token) }
func (s *RedisSnapshotStore) Put(ctx context.Context, snapshot *QuerySnapshot) error {
	if s.Client == nil || s.Client.UniversalClient == nil {
		return ErrSnapshotUnavailable
	}
	raw, e := json.Marshal(snapshot)
	if e != nil {
		return ErrSnapshotUnavailable
	}
	// The snapshot and its delivery states share one expiring hash. Eviction
	// cannot remove dedupe state while leaving a forwardable snapshot behind.
	return s.Client.Eval(ctx, `redis.call('HSET',KEYS[1],'snapshot',ARGV[1]);redis.call('EXPIRE',KEYS[1],86400);return 1`, []string{s.key(snapshot.Token)}, string(raw)).Err()
}
func (s *RedisSnapshotStore) Get(ctx context.Context, token string) (*QuerySnapshot, error) {
	if !ValidSnapshotToken(token) || s.Client == nil || s.Client.UniversalClient == nil {
		return nil, ErrSnapshotUnavailable
	}
	raw, e := s.Client.HGet(ctx, s.key(token), "snapshot").Bytes()
	if e != nil {
		return nil, ErrSnapshotUnavailable
	}
	var snapshot QuerySnapshot
	if json.Unmarshal(raw, &snapshot) != nil || snapshot.Token != token || !snapshot.ExpiresAt.After(time.Now()) || !IsQuery(snapshot.RendererKey) {
		return nil, ErrSnapshotUnavailable
	}
	return &snapshot, nil
}
func (s *RedisSnapshotStore) deliveryKey(token, target string) string {
	sum := sha256.Sum256([]byte(target))
	return "delivery:" + hex.EncodeToString(sum[:])
}
func (s *RedisSnapshotStore) Claim(ctx context.Context, token, target string) (string, string, error) {
	var random [16]byte
	if _, e := rand.Read(random[:]); e != nil {
		return "", "", e
	}
	lease := hex.EncodeToString(random[:])
	state, e := s.Client.Eval(ctx, `if not redis.call('HGET',KEYS[1],'snapshot') then return 'expired' end;local v=redis.call('HGET',KEYS[1],ARGV[1]);local now=tonumber(redis.call('TIME')[1]);
if v=='sent' then return 'sent' end;if v=='uncertain' then return 'uncertain' end;
if v then local owner,started=string.match(v,'^processing:([^:]+):(%d+)$');if not started or now-tonumber(started)>90 then return 'uncertain' end;return 'busy' end;
redis.call('HSET',KEYS[1],ARGV[1],'processing:'..ARGV[2]..':'..now);return 'claimed'`, []string{s.key(token)}, s.deliveryKey(token, target), lease).Text()
	return state, lease, e
}
func (s *RedisSnapshotStore) Finish(ctx context.Context, token, target, lease, outcome string) error {
	if outcome != "sent" && outcome != "failed" && outcome != "uncertain" {
		return errors.New("invalid delivery outcome")
	}
	return s.Client.Eval(ctx, `local v=redis.call('HGET',KEYS[1],ARGV[1]);if not v or string.sub(v,1,string.len(ARGV[2])+12)~='processing:'..ARGV[2]..':' then return 0 end;
 if ARGV[3]=='failed' then redis.call('HDEL',KEYS[1],ARGV[1]) else redis.call('HSET',KEYS[1],ARGV[1],ARGV[3]) end;return 1`, []string{s.key(token)}, s.deliveryKey(token, target), lease, outcome).Err()
}
func QuerySendUUID(token, target string) string {
	sum := sha256.Sum256([]byte(token + ":" + target))
	return hex.EncodeToString(sum[:16])
}
func QuerySnapshotURL(base string, snapshot *QuerySnapshot) (string, error) {
	u, e := url.Parse(base)
	if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("public site URL is not configured")
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/app/" + snapshot.Slug
	u.RawQuery = url.Values{"result": {snapshot.Token}}.Encode()
	return u.String(), nil
}
func QueryResultCard(snapshot *QuerySnapshot, sender, link string) map[string]any {
	return feishucard.Build(QueryResultCardOptions(snapshot, sender, link))
}
func QueryResultCardOptions(snapshot *QuerySnapshot, sender, link string) feishucard.Options {
	preview := queryPreview(snapshot.Data)
	return feishucard.Options{Kind: feishucard.Query, Title: snapshot.Title, Subtitle: sender + " 转发 · 查询结果快照", URL: link, Sections: []feishucard.Section{
		{Label: "查询条件", Text: snapshot.QueryLabel + "：" + snapshot.QueryValue + "\n查询时间：" + snapshot.QueriedAt.In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04:05") + "（北京时间）"},
		{Label: "结果预览", Text: preview},
	}}
}
func queryPreview(raw json.RawMessage) string {
	if string(raw) == "null" || string(raw) == `""` {
		return "未查询到相关记录"
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return strings.NewReplacer(`\r\n`, "\n", `\n`, "\n", "\r\n", "\n").Replace(text)
	}
	// UseNumber preserves long identifiers while preparing the human-readable card.
	var value any
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil {
		return "暂无可预览内容"
	}
	if object, ok := value.(map[string]any); ok {
		if output, exists := object["output"]; exists {
			value = output
		}
	}
	return formatQueryValue(value)
}

const queryPreviewLimit = 12000

type queryText struct {
	lines []string
	runes int
}

func (q *queryText) add(line string) bool {
	line = strings.TrimSpace(strings.Map(func(r rune) rune {
		if utf8.RuneLen(r) < 0 || (r < 32 && r != '\n' && r != '\t') {
			return -1
		}
		return r
	}, line))
	lineRunes := utf8.RuneCountInString(line)
	separator := 0
	if len(q.lines) > 0 {
		separator = 1
	}
	if q.runes+separator+lineRunes > queryPreviewLimit {
		return false
	}
	q.lines = append(q.lines, line)
	q.runes += separator + lineRunes
	return true
}

func (q *queryText) String() string { return strings.Join(q.lines, "\n") }

func formatQueryValue(value any) string {
	if value == nil {
		return "未查询到相关记录"
	}
	var out queryText
	switch typed := value.(type) {
	case []any:
		if len(typed) == 0 {
			return "未查询到相关记录"
		}
		out.add(fmt.Sprintf("共 %d 条记录", len(typed)))
		for index, row := range typed {
			if !out.add("") || !out.add(fmt.Sprintf("记录 %d", index+1)) || !appendQueryRecord(&out, row) {
				out.add("… 其余记录已省略")
				break
			}
		}
	case map[string]any:
		if len(typed) == 0 {
			return "未查询到相关记录"
		}
		appendQueryMap(&out, typed)
	default:
		return queryFieldText(typed)
	}
	return out.String()
}

func appendQueryRecord(out *queryText, value any) bool {
	if record, ok := value.(map[string]any); ok {
		return appendQueryMap(out, record)
	}
	return out.add(queryFieldText(value))
}

func appendQueryMap(out *queryText, record map[string]any) bool {
	keys := make([]string, 0, len(record))
	for key := range record {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if !out.add(key + "：" + queryFieldText(record[key])) {
			return false
		}
	}
	return true
}

func queryFieldText(value any) string {
	if value == nil {
		return "—"
	}
	if text, ok := value.(string); ok {
		text = strings.NewReplacer(`\r\n`, "\n", `\n`, "\n", "\r\n", "\n").Replace(text)
		return clipQueryField(text)
	}
	switch value.(type) {
	case json.Number, bool, float64:
		return fmt.Sprint(value)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return "—"
	}
	return clipQueryField(string(raw))
}

func clipQueryField(text string) string {
	const limit = 800
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return strings.TrimSpace(string(runes[:limit-1])) + "…（字段内容为节选）"
}
