package httpclient

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestSharedClientReusesIdleConnection(t *testing.T) {
	var connections atomic.Int64
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	server.Start()
	defer server.Close()
	client := New(Default(), 3*time.Second)
	defer client.CloseIdleConnections()
	for i := 0; i < 3; i++ {
		response, err := client.Get(server.URL)
		if err != nil {
			t.Fatal(err)
		}
		_, err = io.Copy(io.Discard, response.Body)
		closeErr := response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
	}
	if got := connections.Load(); got != 1 {
		t.Fatalf("three drained requests opened %d connections; want one shared connection", got)
	}
}

func TestCancelledStreamReleasesConnectionSlot(t *testing.T) {
	streamStopped := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stream" {
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			close(streamStopped)
			return
		}
		_, _ = io.WriteString(w, "ok")
	}))
	defer server.Close()
	cfg := Default()
	cfg.MaxConnsPerHost = 1
	client := New(cfg, 0)
	defer client.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	cancel()
	if _, err := response.Body.Read(make([]byte, 1)); err == nil {
		t.Fatal("cancelled stream body still readable")
	}
	select {
	case <-streamStopped:
	case <-time.After(time.Second):
		t.Fatal("request cancellation did not release the streaming connection")
	}
	// Even before closing the cancelled body's wrapper, a new request must
	// acquire the sole per-host slot rather than wait on the abandoned stream.
	nextCtx, nextCancel := context.WithTimeout(context.Background(), time.Second)
	defer nextCancel()
	next, err := http.NewRequestWithContext(nextCtx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	nextResponse, err := client.Do(next)
	if err != nil {
		t.Fatalf("cancelled stream retained the only connection slot: %v", err)
	}
	defer nextResponse.Body.Close()
	if _, err := io.Copy(io.Discard, nextResponse.Body); err != nil {
		t.Fatal(err)
	}
}
