package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/shilin414/cas/backend-go/internal/identity"
	"github.com/shilin414/cas/backend-go/internal/integrations/aily"
	"github.com/shilin414/cas/backend-go/internal/integrations/aily/bridge"
	"github.com/shilin414/cas/backend-go/internal/platform/config"
	"github.com/shilin414/cas/backend-go/internal/platform/crypto"
	"github.com/shilin414/cas/backend-go/internal/platform/database"
	"github.com/shilin414/cas/backend-go/internal/platform/httpclient"
	"github.com/shilin414/cas/backend-go/internal/platform/redisx"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func main() {
	agentID := flag.String("agent", "", "Aily agent_id")
	userID := flag.String("user", "", "local user_id with cached UAT")
	prompt := flag.String("prompt", "", "test prompt")
	timeout := flag.Duration("timeout", 90*time.Second, "stream timeout")
	flag.Parse()
	if *agentID == "" || *userID == "" || *prompt == "" {
		flag.Usage()
		os.Exit(2)
	}

	cfg, err := config.Load()
	must(err)
	dbh, err := database.Open(context.Background(), cfg.Database)
	must(err)
	defer dbh.Close()
	rdb, err := redisx.Open(context.Background(), cfg.Redis)
	must(err)
	defer rdb.Close()

	feishu := identity.NewFeishuClient(
		cfg.Feishu.BaseURL,
		cfg.Feishu.AppID,
		cfg.Feishu.AppSecret,
		httpclient.New(httpclient.Default(), 20*time.Second),
	)
	gcm, err := crypto.NewAESGCM(cfg.Auth.TokenEncryptionKey)
	must(err)
	oauth := &identity.ExchangeOrchestrator{DB: dbh}
	auth := &aily.AuthResolver{
		DB:     dbh,
		Redis:  rdb,
		Feishu: bridge.NewTokenAPI(feishu),
		AppID:  cfg.Feishu.AppID,
		OnRotate: func(ctx context.Context, identityID int64, enc string, expires *time.Time) error {
			return oauth.RotateRefresh(ctx, identityID, enc, expires)
		},
	}
	auth.GCM = gcm

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	token, err := auth.UserAccessToken(ctx, 30001)
	must(err)
	if token == "" {
		panic("no usable cached UAT for user")
	}

	body, _ := json.Marshal(map[string]any{
		"user_message": map[string]any{
			"content": []map[string]any{{"type": "text", "text": *prompt}},
		},
		"stream": true,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://open.feishu.cn/open-apis/aily/v1/agents/"+*agentID+"/chats",
		bytes.NewReader(body))
	must(err)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	must(err)
	defer resp.Body.Close()
	fmt.Fprintf(os.Stderr, "HTTP %s content-type=%s\n", resp.Status, resp.Header.Get("Content-Type"))
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		panic("non-200 response: " + string(raw))
	}

	reader := bufio.NewReader(resp.Body)
	for {
		line, err := reader.ReadString('\n')
		if line != "" {
			fmt.Print(line)
		}
		if err != nil {
			if err == io.EOF {
				fmt.Fprintln(os.Stderr, "SSE EOF")
				return
			}
			panic(err)
		}
	}
}
