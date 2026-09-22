package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/shilin414/cas/backend-go/internal/identity"
	"github.com/shilin414/cas/backend-go/internal/integrations/aily"
	"github.com/shilin414/cas/backend-go/internal/integrations/aily/bridge"
	"github.com/shilin414/cas/backend-go/internal/platform/config"
	"github.com/shilin414/cas/backend-go/internal/platform/crypto"
	"github.com/shilin414/cas/backend-go/internal/platform/database"
	"github.com/shilin414/cas/backend-go/internal/platform/httpclient"
	"github.com/shilin414/cas/backend-go/internal/platform/redisx"
	"os"
	"time"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}
func main() {
	agent := flag.String("agent", "", "agent ID")
	chat := flag.String("chat", "", "existing chat ID")
	user := flag.Int64("user", 0, "run owner")
	out := flag.String("out", "", "local result capture")
	flag.Parse()
	if *agent == "" || *chat == "" || *user == 0 || *out == "" {
		panic("all arguments required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	cfg, err := config.Load()
	must(err)
	db, err := database.Open(ctx, cfg.Database)
	must(err)
	defer db.Close()
	rdb, err := redisx.Open(ctx, cfg.Redis)
	must(err)
	defer rdb.Close()
	gcm, err := crypto.NewAESGCM(cfg.Auth.TokenEncryptionKey)
	must(err)
	feishu := identity.NewFeishuClient(cfg.Feishu.BaseURL, cfg.Feishu.AppID, cfg.Feishu.AppSecret, httpclient.New(httpclient.Default(), 20*time.Second))
	oauth := &identity.ExchangeOrchestrator{DB: db}
	auth := &aily.AuthResolver{DB: db, Redis: rdb, GCM: gcm, Feishu: bridge.NewTokenAPI(feishu), AppID: cfg.Feishu.AppID, OnRotate: func(ctx context.Context, id int64, enc string, expires *time.Time) error {
		return oauth.RotateRefresh(ctx, id, enc, expires)
	}}
	token, err := auth.UserAccessToken(ctx, *user)
	must(err)
	client := aily.NewClient(cfg.Aily.BaseURL, httpclient.New(httpclient.Default(), 25*time.Second))
	raw, err := client.GetChatResult(ctx, *agent, token, *chat)
	must(err)
	must(os.WriteFile(*out, raw, 0600))
	var result map[string]any
	must(json.Unmarshal(raw, &result))
	for key, value := range result {
		if key != "content" {
			fmt.Printf("%s: %v\n", key, value)
		}
	}
	final, process := (aily.Mapper{}).SplitResponseText(result)
	normalized, err := json.Marshal(map[string]any{"text": final, "process_text": process})
	must(err)
	must(os.WriteFile(*out+".normalized.json", normalized, 0600))
	fmt.Printf("normalized: final=%d runes, process=%d runes\n", len([]rune(final)), len([]rune(process)))
	items, _ := result["content"].([]any)
	for i, v := range items {
		item, _ := v.(map[string]any)
		fmt.Printf("item %d metadata:", i)
		for k, v := range item {
			if k != "text" {
				fmt.Printf(" %s=%v", k, v)
			}
		}
		fmt.Println()
		if text, ok := item["text"].(string); ok {
			r := []rune(text)
			if len(r) > 100 {
				r = r[:100]
			}
			fmt.Printf(" text-prefix=%s\n", string(r))
		}
	}
}
