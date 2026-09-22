package delivery

import (
	"context"
	"testing"

	"github.com/shilin414/cas/backend-go/internal/platform/ids"
)

type keyedFeishu struct {
	recordingFeishu
	key        string
	plainCalls int
}

func (f *keyedFeishu) SendIMMessage(ctx context.Context, token, kind, target, typ, content string) error {
	f.plainCalls++
	return nil
}
func (f *keyedFeishu) SendIMMessageWithUUID(ctx context.Context, token, kind, target, typ, content, key string) error {
	f.key = key
	return nil
}
func TestRealDeliveryAdapterPassesStableUUID(t *testing.T) {
	client := &keyedFeishu{}
	sender := &FeishuSender{Client: client, Auth: &staticAuth{token: "uat"}}
	key := ids.New().String()
	for i := 0; i < 2; i++ {
		if err := sender.Send(context.Background(), DeliveryRequest{SenderUserID: 7, Target: Target{Type: TargetChat, ID: "chat"}, IdempotencyKey: key}); err != nil {
			t.Fatal(err)
		}
	}
	if client.key != key || client.plainCalls != 0 {
		t.Fatalf("upstream UUID=%q plain sends=%d", client.key, client.plainCalls)
	}
}
