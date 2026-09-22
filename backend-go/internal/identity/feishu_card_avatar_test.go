package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shilin414/cas/backend-go/internal/platform/storage"
)

func TestCardAvatarUploadUsesPinnedStorageAndSameUserToken(t *testing.T) {
	ctx := context.Background()
	store, err := storage.NewLocalFS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var pngData bytes.Buffer
	if err := png.Encode(&pngData, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	key := "application-avatars/12/original.png"
	if _, err = store.Put(ctx, key, bytes.NewReader(pngData.Bytes()), "image/png"); err != nil {
		t.Fatal(err)
	}
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/open-apis/im/v1/images" || r.Header.Get("Authorization") != "Bearer user-token" {
			t.Error("wrong route or sender identity")
		}
		if err := r.ParseMultipartForm(3 << 20); err != nil {
			t.Fatal(err)
		}
		defer r.MultipartForm.RemoveAll()
		if r.FormValue("image_type") != "message" {
			t.Error("image must be usable in message cards")
		}
		file, _, err := r.FormFile("image")
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		data, _ := io.ReadAll(file)
		if !bytes.Equal(data, pngData.Bytes()) {
			t.Error("avatar bytes changed")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"image_key": "img_avatar"}})
	}))
	defer upstream.Close()
	client := NewFeishuClient(upstream.URL, "app", "secret", upstream.Client())
	got, err := client.UploadStoredCardAvatar(ctx, "user-token", store, key)
	if err != nil || got != "img_avatar" || calls != 1 {
		t.Fatalf("%q %v calls=%d", got, err, calls)
	}
	for _, invalid := range []string{"https://internal/secret", "../secret", "application-avatars/../secret", "other-private/file.png"} {
		if _, err := client.UploadStoredCardAvatar(ctx, "user-token", store, invalid); err == nil {
			t.Fatalf("accepted unsafe key %q", invalid)
		}
	}
	if calls != 1 {
		t.Fatal("unsafe reference reached external service")
	}
}

func TestCardAvatarUploadRejectsUnconfirmedResponse(t *testing.T) {
	ctx := context.Background()
	store, _ := storage.NewLocalFS(t.TempDir())
	var data bytes.Buffer
	_ = png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 1, 1)))
	key := "application-avatars/1/a.png"
	_, _ = store.Put(ctx, key, bytes.NewReader(data.Bytes()), "image/png")
	for _, body := range []string{`{}`, `{"code":0,"data":{}}`, `{"code":99991672,"msg":"permission denied"}`} {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
		client := NewFeishuClient(upstream.URL, "app", "secret", upstream.Client())
		if _, err := client.UploadStoredCardAvatar(ctx, "user-token", store, key); err == nil {
			t.Errorf("accepted %s", body)
		}
		upstream.Close()
	}
}
