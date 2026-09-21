package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/shilin414/cas/backend-go/internal/aimodel"
)

type memoryExecutionStore struct {
	mu         sync.Mutex
	cfg        *aimodel.RuntimeConfig
	inv        aimodel.Invocation
	result     aimodel.Completion
	done       chan struct{}
	begun      bool
	finished   bool
	failConfig bool
}

func (s *memoryExecutionStore) LookupInvocation(_ context.Context, _ int64, _ string, input aimodel.MessageInput) (*aimodel.Invocation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.begun {
		copy := s.inv
		return &copy, nil
	}
	return nil, nil
}
func (s *memoryExecutionStore) RuntimeConfig(context.Context, int64, string) (*aimodel.RuntimeConfig, error) {
	if s.failConfig {
		return nil, aimodel.ErrUnavailable
	}
	return s.cfg, nil
}
func (s *memoryExecutionStore) GetSession(context.Context, int64, string) (*aimodel.SessionDetail, error) {
	return &aimodel.SessionDetail{Messages: []aimodel.Message{{Role: "user", Text: "hi"}}}, nil
}
func (s *memoryExecutionStore) GetAttachment(context.Context, int64, string, string) (*aimodel.Attachment, error) {
	return nil, aimodel.ErrNotFound
}
func (s *memoryExecutionStore) BeginInvocation(_ context.Context, _ int64, _ string, input aimodel.MessageInput) (*aimodel.Invocation, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(input.AttachmentIDs) > 0 {
		return nil, false, aimodel.ErrNotFound
	}
	fresh := !s.begun
	s.begun = true
	if fresh {
		s.inv = aimodel.Invocation{ID: "i", Status: "queued"}
	}
	copy := s.inv
	return &copy, fresh, nil
}
func (s *memoryExecutionStore) StartInvocation(context.Context, string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inv.CancelRequested {
		s.inv.Status = "cancelled"
		return false, nil
	}
	s.inv.Status = "running"
	return true, nil
}
func (s *memoryExecutionStore) GetInvocation(context.Context, int64, string) (*aimodel.Invocation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	copy := s.inv
	return &copy, nil
}
func (s *memoryExecutionStore) UpdateInvocationOutput(_ context.Context, _ string, text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inv.OutputText = text
	return nil
}
func (s *memoryExecutionStore) FinishInvocation(_ context.Context, _ string, result aimodel.Completion) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.result = result
	s.inv.Status = result.Status
	s.inv.OutputText = result.OutputText
	if !s.finished {
		s.finished = true
		close(s.done)
	}
	return nil
}
func (s *memoryExecutionStore) RequestCancellation(context.Context, int64, string) (*aimodel.Invocation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inv.CancelRequested = true
	copy := s.inv
	return &copy, nil
}
func (s *memoryExecutionStore) RecoverStaleInvocations(context.Context, time.Time) (int, error) {
	return 0, nil
}
func (s *memoryExecutionStore) CleanupExpired(context.Context, int) (int, error) { return 0, nil }
func executionStore(url string) *memoryExecutionStore {
	return &memoryExecutionStore{done: make(chan struct{}), cfg: &aimodel.RuntimeConfig{Model: &aimodel.Model{ModelInput: aimodel.ModelInput{ModelID: "m", Capabilities: aimodel.Capabilities{}}}, Connection: &aimodel.Connection{Adapter: "openai_chat", BaseURL: url, TimeoutSeconds: 5}, Credential: "key"}}
}
func TestExecutorRunsAndCloses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"works"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	store := executionStore(server.URL)
	e := NewExecutor(store, nil, ExecutorOptions{AllowedPrivateHosts: []string{"127.0.0.1"}})
	e.Start()
	defer e.Close()
	got, err := e.Submit(context.Background(), 1, "s", aimodel.MessageInput{Text: "hi"})
	if err != nil || got.ID != "i" {
		t.Fatalf("%+v %v", got, err)
	}
	select {
	case <-store.done:
	case <-time.After(3 * time.Second):
		t.Fatal("not completed")
	}
	store.mu.Lock()
	result := store.result
	store.mu.Unlock()
	if result.Status != "succeeded" || result.OutputText != "works" {
		t.Fatalf("%+v", result)
	}
	e.Close()
	if _, err = e.Submit(context.Background(), 1, "s", aimodel.MessageInput{Text: "hi"}); !errors.Is(err, aimodel.ErrUnavailable) {
		t.Fatal("closed executor accepted work")
	}
}
func TestExecutorCancellationIsHonest(t *testing.T) {
	entered := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer server.Close()
	store := executionStore(server.URL)
	e := NewExecutor(store, nil, ExecutorOptions{AllowedPrivateHosts: []string{"127.0.0.1"}})
	defer e.Close()
	_, err := e.Submit(context.Background(), 1, "s", aimodel.MessageInput{Text: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("didn't submit")
	}
	if _, err = e.Cancel(context.Background(), 1, "i"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-store.done:
	case <-time.After(3 * time.Second):
		t.Fatal("didn't settle")
	}
	store.mu.Lock()
	result := store.result
	store.mu.Unlock()
	if result.Status != "indeterminate" || result.CancellationConfirmed {
		t.Fatalf("must not claim upstream cancel confirmed: %+v", result)
	}
}
func TestExecutorRejectsInvalidBeforeRecording(t *testing.T) {
	store := executionStore("https://example.com")
	e := NewExecutor(store, nil, ExecutorOptions{})
	defer e.Close()
	_, err := e.Submit(context.Background(), 1, "s", aimodel.MessageInput{Text: "x", AttachmentIDs: []string{"other-owner-file"}})
	if !errors.Is(err, aimodel.ErrNotFound) || store.begun {
		t.Fatal("invalid attachment recorded")
	}
	store.failConfig = true
	if _, err = e.Submit(context.Background(), 1, "s", aimodel.MessageInput{Text: "x"}); !errors.Is(err, aimodel.ErrUnavailable) {
		t.Fatal("disabled config passed")
	}
}
func TestRetryReturnsOriginalAfterConnectionDisabled(t *testing.T) {
	store := executionStore("https://example.com")
	store.begun = true
	store.inv = aimodel.Invocation{ID: "original", Status: "succeeded"}
	store.failConfig = true
	e := NewExecutor(store, nil, ExecutorOptions{})
	defer e.Close()
	got, err := e.Submit(context.Background(), 1, "s", aimodel.MessageInput{Text: "hi"})
	if err != nil || got.ID != "original" {
		t.Fatalf("retry consulted disabled config: %+v %v", got, err)
	}
}

type preprocessingBarrier struct {
	*memoryExecutionStore
	entered chan struct{}
	release chan struct{}
}

func (s *preprocessingBarrier) RuntimeConfig(ctx context.Context, _ int64, _ string) (*aimodel.RuntimeConfig, error) {
	s.entered <- struct{}{}
	select {
	case <-ctx.Done():
	case <-s.release:
	}
	return nil, aimodel.ErrUnavailable
}
func TestPreprocessingSharesBoundedExecutionSlots(t *testing.T) {
	store := &preprocessingBarrier{memoryExecutionStore: executionStore("https://example.com"), entered: make(chan struct{}, 16), release: make(chan struct{})}
	e := NewExecutor(store, nil, ExecutorOptions{})
	defer e.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var done sync.WaitGroup
	for n := 0; n < cap(e.slots); n++ {
		done.Add(1)
		go func() { defer done.Done(); _, _ = e.Submit(ctx, 1, "s", aimodel.MessageInput{Text: "hi"}) }()
	}
	for n := 0; n < cap(e.slots); n++ {
		select {
		case <-store.entered:
		case <-ctx.Done():
			t.Fatal("preprocessing did not start")
		}
	}
	_, err := e.Submit(ctx, 1, "s", aimodel.MessageInput{Text: "hi"})
	if !errors.Is(err, aimodel.ErrLimit) {
		t.Errorf("unbounded preprocessing: %v", err)
	}
	close(store.release)
	done.Wait()
}

type admissionHistoryStore struct {
	*memoryExecutionStore
	admitted  bool
	earlyRead bool
}

func (s *admissionHistoryStore) BeginInvocation(ctx context.Context, owner int64, session string, input aimodel.MessageInput) (*aimodel.Invocation, bool, error) {
	s.admitted = true
	return s.memoryExecutionStore.BeginInvocation(ctx, owner, session, input)
}
func (s *admissionHistoryStore) GetSession(context.Context, int64, string) (*aimodel.SessionDetail, error) {
	if !s.admitted {
		s.earlyRead = true
	}
	return &aimodel.SessionDetail{Messages: []aimodel.Message{{Role: "user", Text: "previous"}, {Role: "assistant", Text: "latest committed reply"}, {Role: "user", Text: "next"}}}, nil
}
func TestHistoryReadAfterAdmissionIncludesCommittedReply(t *testing.T) {
	var count int
	var middle string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		count = len(body.Messages)
		if count > 1 {
			middle = body.Messages[1].Content
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	store := &admissionHistoryStore{memoryExecutionStore: executionStore(server.URL)}
	e := NewExecutor(store, nil, ExecutorOptions{AllowedPrivateHosts: []string{"127.0.0.1"}})
	defer e.Close()
	if _, err := e.Submit(context.Background(), 1, "s", aimodel.MessageInput{Text: "next"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-store.done:
	case <-time.After(3 * time.Second):
		t.Fatal("did not finish")
	}
	if store.earlyRead || count != 3 || middle != "latest committed reply" {
		t.Fatalf("history inconsistent: early=%v count=%d reply=%q", store.earlyRead, count, middle)
	}
}

type preparingStore struct {
	*memoryExecutionStore
	entered   chan struct{}
	cancelled chan struct{}
}

func (s *preparingStore) GetSession(ctx context.Context, _ int64, _ string) (*aimodel.SessionDetail, error) {
	close(s.entered)
	<-ctx.Done()
	close(s.cancelled)
	return nil, ctx.Err()
}
func TestCloseWaitsForAcceptedPreprocessingAndSettlement(t *testing.T) {
	store := &preparingStore{memoryExecutionStore: executionStore("https://example.com"), entered: make(chan struct{}), cancelled: make(chan struct{})}
	e := NewExecutor(store, nil, ExecutorOptions{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	submitted := make(chan struct{})
	go func() { defer close(submitted); _, _ = e.Submit(ctx, 1, "s", aimodel.MessageInput{Text: "hi"}) }()
	<-store.entered
	closed := make(chan struct{})
	go func() { e.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		cancel()
		t.Fatal("Close did not cancel preprocessing")
	}
	select {
	case <-store.cancelled:
	default:
		cancel()
		<-submitted
		t.Fatal("Close returned before cancelling preprocessing")
	}
	select {
	case <-store.done:
	default:
		t.Fatal("Close returned before settling accepted invocation")
	}
	if len(e.slots) != 0 {
		t.Fatal("preprocessing slot leaked")
	}
	store.mu.Lock()
	result := store.result
	store.mu.Unlock()
	if result.Status != "cancelled" || !result.CancellationConfirmed {
		t.Fatalf("pre-submit shutdown should be confirmed locally: %+v", result)
	}
}
func TestCancellationInterruptsPreprocessing(t *testing.T) {
	store := &preparingStore{memoryExecutionStore: executionStore("https://example.com"), entered: make(chan struct{}), cancelled: make(chan struct{})}
	e := NewExecutor(store, nil, ExecutorOptions{})
	defer e.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() { _, _ = e.Submit(ctx, 1, "s", aimodel.MessageInput{Text: "hi"}) }()
	<-store.entered
	if _, err := e.Cancel(context.Background(), 1, "i"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-store.cancelled:
	case <-time.After(time.Second):
		cancel()
		t.Fatal("cancel did not interrupt preparation")
	}
	select {
	case <-store.done:
	case <-time.After(time.Second):
		t.Fatal("cancel did not settle")
	}
}

type handoffWatcherStore struct {
	*memoryExecutionStore
	watcherEntered chan struct{}
	releaseWatcher chan struct{}
	once           sync.Once
}

func (s *handoffWatcherStore) GetSession(context.Context, int64, string) (*aimodel.SessionDetail, error) {
	<-s.watcherEntered
	return &aimodel.SessionDetail{Messages: []aimodel.Message{{Role: "user", Text: "hi"}}}, nil
}
func (s *handoffWatcherStore) GetInvocation(ctx context.Context, owner int64, id string) (*aimodel.Invocation, error) {
	first := false
	s.once.Do(func() { first = true; close(s.watcherEntered) })
	if first {
		<-s.releaseWatcher
	} // Simulate query cleanup that outlives cancellation.
	return s.memoryExecutionStore.GetInvocation(ctx, owner, id)
}
func TestCloseAlsoWaitsForPreprocessingWatcherAfterHandoff(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	store := &handoffWatcherStore{memoryExecutionStore: executionStore(server.URL), watcherEntered: make(chan struct{}), releaseWatcher: make(chan struct{})}
	e := NewExecutor(store, nil, ExecutorOptions{AllowedPrivateHosts: []string{"127.0.0.1"}})
	submitted := make(chan struct{})
	go func() {
		defer close(submitted)
		_, _ = e.Submit(context.Background(), 1, "s", aimodel.MessageInput{Text: "hi"})
	}()
	select {
	case <-store.done:
	case <-time.After(3 * time.Second):
		close(store.releaseWatcher)
		e.Close()
		t.Fatal("remote call did not finish")
	}
	closed := make(chan struct{})
	go func() { e.Close(); close(closed) }()
	select {
	case <-closed:
		close(store.releaseWatcher)
		<-submitted
		t.Fatal("Close returned while preprocessing watcher was still unwinding")
	case <-time.After(40 * time.Millisecond):
	}
	close(store.releaseWatcher)
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Close did not finish after watcher release")
	}
	<-submitted
}
