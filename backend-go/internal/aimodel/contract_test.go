package aimodel

import (
	"encoding/json"
	genapi "github.com/shilin414/cas/backend-go/internal/gen/api"
	"testing"
	"time"
)

func TestOwnedSessionAndAttachmentJSONMatchOpenAPI(t *testing.T) {
	spec, err := genapi.GetSwagger()
	must(t, err)
	now := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
	attachment := Attachment{ID: testRequest, AttachmentInput: AttachmentInput{Name: "photo.png", MIMEType: "image/png", SizeBytes: 10, Kind: "image", StorageKey: "private/key"}, CreatedAt: now}
	session := Session{ID: testSession, OwnerID: 99, ModelID: testModel, Title: "test", CreatedAt: now, ExpiresAt: now.Add(time.Hour), ActiveInvocationID: testRequest}
	cases := []struct {
		name  string
		value any
	}{{"AITestAttachment", attachment}, {"AITestSession", session}, {"AITestSessionDetail", SessionDetail{Session: session, Messages: []Message{{ID: testRequest, Role: "user", Text: "hello", Attachments: []Attachment{attachment}, CreatedAt: now}}}}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, e := json.Marshal(tc.value)
			must(t, e)
			var value any
			must(t, json.Unmarshal(raw, &value))
			if e = spec.Components.Schemas[tc.name].Value.VisitJSON(value); e != nil {
				t.Fatal(e)
			}
		})
	}
	raw, err := json.Marshal(session)
	must(t, err)
	var object map[string]any
	must(t, json.Unmarshal(raw, &object))
	if object["active_invocation_id"] != testRequest {
		t.Fatal("active invocation missing")
	}
	if _, exists := object["owner_id"]; exists {
		t.Fatal("internal owner serialized")
	}
	session.ActiveInvocationID = ""
	raw, err = json.Marshal(session)
	must(t, err)
	object = map[string]any{}
	must(t, json.Unmarshal(raw, &object))
	if _, exists := object["active_invocation_id"]; exists {
		t.Fatal("empty active invocation was not omitted")
	}
}
