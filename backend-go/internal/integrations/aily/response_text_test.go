package aily

import (
	"context"
	"encoding/json"
	"github.com/shilin414/cas/backend-go/internal/catalog"
	"testing"
)

type responseTextAPI struct{ *floodingAPI }

func (responseTextAPI) GetChatResult(context.Context, string, string, string) (json.RawMessage, error) {
	return json.RawMessage(`{"status":"Completed","content":[{"type":"text","text":"检查权限"},{"type":"text","text":"查询数据"},{"type":"text","text":"最终结果"}]}`), nil
}
func TestStatusSeparatesProcessFromCanonicalAnswer(t *testing.T) {
	adapter := NewAgentAdapterWithAPI(responseTextAPI{&floodingAPI{}}, nil)
	result, err := adapter.Status(context.Background(), &catalog.ProviderAuthContext{Token: "test"}, "agent", "chat")
	if err != nil {
		t.Fatal(err)
	}
	if result.Output["text"] != "最终结果" || result.Output["process_text"] != "检查权限\n\n查询数据" {
		t.Fatalf("mixed result: %#v", result.Output)
	}
}
