package execution

import "testing"

func TestFinishEventPayloadKeepsCanonicalChannelsSeparate(t *testing.T) {
	in := &FinishInput{Status: StatusSucceeded, ProviderStatus: "Completed", Output: map[string]any{"text": "answer", "process_text": "progress", "private_other_field": "must not leak"}}
	got := finishEventPayload(in)
	if got["text"] != "answer" || got["process_text"] != "progress" {
		t.Fatalf("lost canonical channels: %#v", got)
	}
	if _, ok := got["private_other_field"]; ok {
		t.Fatal("unrelated output leaked")
	}
	in.Output = map[string]any{"text": "answer", "process_text": ""}
	got = finishEventPayload(in)
	if value, ok := got["process_text"]; !ok || value != "" {
		t.Fatalf("empty process must clear provisional text: %#v", got)
	}
	in.Output = nil
	in.ErrorCode = "upstream_failed"
	in.ErrorMessage = "unavailable"
	got = finishEventPayload(in)
	if got["error_code"] != "upstream_failed" || got["error_message"] != "unavailable" {
		t.Fatalf("lost error: %#v", got)
	}
	if _, ok := got["text"]; ok {
		t.Fatal("invented text without provider output")
	}
}
