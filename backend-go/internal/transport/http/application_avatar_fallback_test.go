package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteApplicationAvatarFallbackReturnsSafeSVG(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeApplicationAvatarFallback(recorder, `<script>alert(1)</script>`)
	result := recorder.Result()
	if result.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", result.StatusCode)
	}
	if got := result.Header.Get("Content-Type"); !strings.HasPrefix(got, "image/svg+xml") {
		t.Fatalf("content-type=%q", got)
	}
	body := recorder.Body.String()
	if strings.Contains(body, "<script>") {
		t.Fatalf("unescaped svg body: %s", body)
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Fatalf("escaped icon missing: %s", body)
	}
}
