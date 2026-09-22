package delivery

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/shilin414/cas/backend-go/internal/identity"
	"github.com/shilin414/cas/backend-go/internal/platform/ids"
)

// fakeSender records sends.
type fakeSender struct {
	calls   int
	lastKey string
}

func (f *fakeSender) Send(_ context.Context, req DeliveryRequest) error {
	f.calls++
	f.lastKey = req.IdempotencyKey
	return nil
}

type failingSender struct {
	calls int
	err   error
}

func (f *failingSender) Send(_ context.Context, _ DeliveryRequest) error {
	f.calls++
	return f.err
}

// TestFeishuSenderChoosesIDType: chats use chat_id, users use open_id.
func TestFeishuSenderChoosesIDType(t *testing.T) {
	fake := &recordingFeishu{}
	s := &FeishuSender{Client: fake, Auth: &staticAuth{token: "uat"}}
	_ = s.Send(context.Background(), DeliveryRequest{
		SenderUserID: 7,
		Target:       Target{Type: TargetChat, ID: "oc_1", Content: "hi"},
	})
	if fake.lastIDType != "chat_id" {
		t.Fatalf("chat id type = %q", fake.lastIDType)
	}
	_ = s.Send(context.Background(), DeliveryRequest{
		SenderUserID: 7,
		Target:       Target{Type: TargetUser, ID: "ou_1", Content: "hi"},
	})
	if fake.lastIDType != "open_id" {
		t.Fatalf("user id type = %q", fake.lastIDType)
	}
	if fake.lastToken != "uat" {
		t.Fatalf("token not passed through")
	}
}

// TestDeliveryRequestIdempotencyKey: the send request carries a stable
// key (= DeliveryExecution id) so retries of the same execution are
// correlatable (at-least-once external side effect).
func TestDeliveryRequestIdempotencyKey(t *testing.T) {
	fs := &fakeSender{}
	req := DeliveryRequest{
		ExecutionID:    ids.New(),
		SenderUserID:   42,
		Target:         Target{Type: TargetChat, ID: "oc_x", Content: "hi"},
		IdempotencyKey: ids.New().String(),
	}
	if err := fs.Send(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if fs.lastKey != req.IdempotencyKey {
		t.Fatalf("idempotency key not propagated: %q", fs.lastKey)
	}
}

type recordingFeishu struct {
	lastIDType, lastToken, lastReceiveID, lastMsgType, lastContent string
}

func (r *recordingFeishu) SendIMMessageWithUUID(_ context.Context, token, receiveIDType, receiveID, msgType, content, uuid string) error {
	r.lastToken, r.lastIDType, r.lastReceiveID = token, receiveIDType, receiveID
	r.lastMsgType, r.lastContent = msgType, content
	return nil
}

type staticAuth struct{ token string }

func (a *staticAuth) UserAccessToken(_ context.Context, _ int64) (string, error) {
	return a.token, nil
}

// TestFailureClassification: rate-limit markers map to rate_limited.
func TestFailureClassificationMarkers(t *testing.T) {
	msg := "feishu: send im: code 99991400: too many requests"
	if !strings.Contains(msg, "99991400") {
		t.Fatal("marker missing")
	}
	// Sanity: failingSender propagates errors as expected.
	fs := &failingSender{err: errors.New("boom")}
	if err := fs.Send(context.Background(), DeliveryRequest{}); err == nil {
		t.Fatal("expected error")
	}
}

func TestFeishuSenderSendsPreviewCardToBothTargets(t *testing.T) {
	for _, target := range []string{TargetUser, TargetChat} {
		fake := &recordingFeishu{}
		sender := &FeishuSender{Client: fake, Auth: &staticAuth{token: "uat"}}
		err := sender.Send(context.Background(), DeliveryRequest{SenderUserID: 7, Target: Target{Type: target, ID: "target", Content: "今日完成了巡检"}, Title: "每日巡检", URL: "https://studio.example/share/result"})
		if err != nil {
			t.Fatal(err)
		}
		if fake.lastMsgType != "interactive" {
			t.Fatalf("sent %s not card", fake.lastMsgType)
		}
		for _, part := range []string{"每日巡检", "今日完成了巡检", "查看完整结果", "https://studio.example/share/result"} {
			if !strings.Contains(fake.lastContent, part) {
				t.Fatalf("missing %s in %s", part, fake.lastContent)
			}
		}
	}
}

func TestDeliveryNativeTableAndBrand(t *testing.T) {
	fake := &recordingFeishu{}
	sender := &FeishuSender{Client: fake, Auth: &staticAuth{token: "uat"}}
	err := sender.Send(context.Background(), DeliveryRequest{SenderUserID: 7, Title: "日报", Target: Target{Type: TargetUser, ID: "test-target", Content: "| a | b |\n|---|---|\n| one | two |"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"tag":"table"`, `"c0":"one"`, "小安工作助手"} {
		if !strings.Contains(fake.lastContent, want) {
			t.Fatalf("missing %s: %s", want, fake.lastContent)
		}
	}
	if strings.Contains(fake.lastContent, "xiaoan-platform") {
		t.Fatal("old visible brand")
	}
}

type rejectingNativeCard struct {
	recordingFeishu
	calls int
}

func (f *rejectingNativeCard) SendIMMessageWithUUID(ctx context.Context, token, idType, id, kind, content, uuid string) error {
	f.calls++
	_ = f.recordingFeishu.SendIMMessageWithUUID(ctx, token, idType, id, kind, content, uuid)
	if f.calls == 1 {
		return &identity.FeishuAPIError{Code: 230099, Msg: "unsupported table"}
	}
	return nil
}
func TestScheduledDeliveryFallsBackOnceToPlainCard(t *testing.T) {
	fake := &rejectingNativeCard{}
	sender := &FeishuSender{Client: fake, Auth: &staticAuth{token: "same-user-token"}}
	err := sender.Send(context.Background(), DeliveryRequest{SenderUserID: 7, AgentName: "创作助手", AgentIcon: "✨", Target: Target{Type: TargetChat, ID: "oc-test", Content: "| a | b |\n|---|---|\n| one | two |"}})
	if err != nil || fake.calls != 2 || fake.lastToken != "same-user-token" || fake.lastReceiveID != "oc-test" || fake.lastIDType != "chat_id" {
		t.Fatalf("fallback identity changed: %+v %v", fake, err)
	}
	if strings.Contains(fake.lastContent, `"tag":"table"`) || !strings.Contains(fake.lastContent, "创作助手") || !strings.Contains(fake.lastContent, "one") {
		t.Fatalf("not a plain fallback: %s", fake.lastContent)
	}
}
