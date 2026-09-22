package http

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestArtifactRedirectNeverSubstitutesAnotherGalleryImage(t *testing.T) {
	for _, tt := range []struct {
		requested string
		status    int
	}{{"forest_sunset.jpg", 302}, {"snow_mountain_lake.jpg", 404}, {"../forest_sunset.jpg", 400}, {"", 302}} {
		r := httptest.NewRequest("GET", "/open", nil)
		q := r.URL.Query()
		q.Set("filename", tt.requested)
		r.URL.RawQuery = q.Encode()
		w := httptest.NewRecorder()
		redirectArtifactFile(w, r, "https://cdn.example/artifacts/gallery/forest_sunset.jpg?signature=secret")
		if w.Code != tt.status {
			t.Errorf("file=%q status=%d want=%d", tt.requested, w.Code, tt.status)
		}
		if tt.status != http.StatusFound && w.Header().Get("Location") != "" {
			t.Fatal("mismatched file was redirected")
		}
	}
}
