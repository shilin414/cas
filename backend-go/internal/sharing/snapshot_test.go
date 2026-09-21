package sharing

import (
	"testing"
	"time"

	db "github.com/shilin414/cas/backend-go/internal/gen/db"
)

func TestResolveOnlySelectedAndImmutableResult(t *testing.T) {
	text := "本次完整结果"
	entries := []Entry{{ID: 2}, {Content: &text, Role: "assistant", CreatedAt: time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)}}
	got := Resolve(entries, []db.Message{{ID: 1, Content: "禁止泄漏"}, {ID: 2, Role: "user", Content: "选中问题"}, {ID: 3, Content: "后续任务禁止泄漏"}})
	if len(got) != 2 || got[0].Content != "选中问题" || got[1].Content != text {
		t.Fatalf("unexpected snapshot: %+v", got)
	}
	if len(Resolve([]Entry{{ID: 404}}, nil)) != 0 {
		t.Fatal("deleted messages must not appear")
	}
}
func TestShareURL(t *testing.T) {
	for _, base := range []string{"javascript:alert(1)", "//evil.test", "https://user:pass@host", "https://host?x=1", "https://host?", ""} {
		if _, err := URL(base, "token"); err == nil {
			t.Errorf("accepted invalid base %q", base)
		}
	}
	if got, err := URL("https://studio.example/app/", "abcd"); err != nil || got != "https://studio.example/app/share/abcd" {
		t.Fatalf("%s %v", got, err)
	}
}

func TestNeedsMessagesDistinguishesLegacyAndInlineSnapshots(t *testing.T) {
	content := "result"
	if NeedsMessages(nil) || NeedsMessages([]Entry{{Content: &content}}) {
		t.Fatal("inline result must not query unrelated history")
	}
	if !NeedsMessages([]Entry{{Content: &content}, {ID: 1}}) {
		t.Fatal("legacy message id must be resolved")
	}
}

func TestSnapshotTitleNeverLeaksReusedConversationForInlineResults(t *testing.T) {
	content := "current result"
	for _, title := range []string{"Pinned result", "", "   "} {
		got := SnapshotTitle([]Entry{{Content: &content, Title: title}}, "CONFIDENTIAL prior incident")
		want := title
		if title == "" || title == "   " {
			want = "自动化执行结果"
		}
		if got != want {
			t.Fatalf("title=%q got=%q want=%q", title, got, want)
		}
	}
	if got := SnapshotTitle([]Entry{{ID: 7}}, "Selected conversation"); got != "Selected conversation" {
		t.Fatalf("manual share title changed: %q", got)
	}
}
