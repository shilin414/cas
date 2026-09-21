package runtime

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/shilin414/cas/backend-go/internal/aimodel"
	"github.com/shilin414/cas/backend-go/internal/platform/storage"
)

// ExecutionStore is intentionally narrow so lifecycle races are testable without
// a database and real production persistence remains in the model domain.
type ExecutionStore interface {
	LookupInvocation(context.Context, int64, string, aimodel.MessageInput) (*aimodel.Invocation, error)
	RuntimeConfig(context.Context, int64, string) (*aimodel.RuntimeConfig, error)
	GetSession(context.Context, int64, string) (*aimodel.SessionDetail, error)
	GetAttachment(context.Context, int64, string, string) (*aimodel.Attachment, error)
	BeginInvocation(context.Context, int64, string, aimodel.MessageInput) (*aimodel.Invocation, bool, error)
	StartInvocation(context.Context, string) (bool, error)
	GetInvocation(context.Context, int64, string) (*aimodel.Invocation, error)
	UpdateInvocationOutput(context.Context, string, string) error
	FinishInvocation(context.Context, string, aimodel.Completion) error
	RequestCancellation(context.Context, int64, string) (*aimodel.Invocation, error)
	RecoverStaleInvocations(context.Context, time.Time) (int, error)
	CleanupExpired(context.Context, int) (int, error)
}
type ExecutorOptions struct {
	AllowedPrivateHosts []string
	Log                 *slog.Logger
}

// Executor owns only bounded admin-test requests. It is deliberately not a
// durable business worker; uncertain shutdowns are visible, never auto-replayed.
type Executor struct {
	service   ExecutionStore
	storage   storage.Storage
	options   ExecutorOptions
	ctx       context.Context
	cancel    context.CancelFunc
	mu        sync.Mutex
	active    map[string]context.CancelFunc
	slots     chan struct{}
	wg        sync.WaitGroup
	startOnce sync.Once
	closeOnce sync.Once
	closed    bool
}

func NewExecutor(service ExecutionStore, store storage.Storage, options ExecutorOptions) *Executor {
	ctx, cancel := context.WithCancel(context.Background())
	if options.Log == nil {
		options.Log = slog.Default()
	}
	return &Executor{service: service, storage: store, options: options, ctx: ctx, cancel: cancel, active: make(map[string]context.CancelFunc), slots: make(chan struct{}, 8)}
}
func (e *Executor) Start() {
	e.startOnce.Do(func() {
		e.mu.Lock()
		if e.closed {
			e.mu.Unlock()
			return
		}
		e.wg.Add(1)
		e.mu.Unlock()
		go func() {
			defer e.wg.Done()
			e.maintain()
			ticker := time.NewTicker(time.Minute)
			defer ticker.Stop()
			for {
				select {
				case <-e.ctx.Done():
					return
				case <-ticker.C:
					e.maintain()
				}
			}
		}()
	})
}
func (e *Executor) maintain() {
	ctx, cancel := context.WithTimeout(e.ctx, 20*time.Second)
	defer cancel()
	if _, err := e.service.RecoverStaleInvocations(ctx, time.Now().Add(-10*time.Minute)); err != nil && !errors.Is(err, context.Canceled) {
		e.options.Log.Warn("AI test stale invocation maintenance failed")
	}
	if _, err := e.service.CleanupExpired(ctx, 50); err != nil && !errors.Is(err, context.Canceled) {
		e.options.Log.Warn("AI test expiration cleanup failed")
	}
}
func (e *Executor) Close() {
	e.closeOnce.Do(func() { e.mu.Lock(); e.closed = true; e.cancel(); e.mu.Unlock(); e.wg.Wait() })
}
func (e *Executor) Submit(ctx context.Context, owner int64, sessionID string, input aimodel.MessageInput) (*aimodel.Invocation, error) {
	// Close and admission share the mutex so Wait never races an untracked Add.
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil, aimodel.ErrUnavailable
	}
	e.wg.Add(1)
	e.mu.Unlock()
	handedOff := false
	// Submit owns its counter until all deferred watcher cleanup has completed.
	defer e.wg.Done()
	prepCtx, prepCancel := context.WithTimeout(ctx, 30*time.Second)
	stopShutdown := context.AfterFunc(e.ctx, prepCancel)
	defer stopShutdown()
	defer prepCancel()
	existing, err := e.service.LookupInvocation(prepCtx, owner, sessionID, input)
	if err != nil || existing != nil {
		return existing, err
	}
	select {
	case e.slots <- struct{}{}:
	default:
		return nil, aimodel.ErrLimit
	}
	acceptedID := ""
	defer func() {
		if !handedOff {
			<-e.slots
			if acceptedID != "" {
				e.mu.Lock()
				delete(e.active, acceptedID)
				e.mu.Unlock()
			}
		}
	}()
	config, err := e.service.RuntimeConfig(prepCtx, owner, sessionID)
	if err != nil {
		return nil, err
	}
	input.Config = config
	invocation, created, err := e.service.BeginInvocation(prepCtx, owner, sessionID, input)
	if err != nil || !created {
		return invocation, err
	}
	acceptedID = invocation.ID
	e.mu.Lock()
	e.active[invocation.ID] = prepCancel
	e.mu.Unlock()
	stopPrepWatch := e.watchCancellation(prepCtx, owner, invocation.ID, prepCancel)
	defer stopPrepWatch()
	// The admission transaction has appended this turn and excluded competitors.
	detail, err := e.service.GetSession(prepCtx, owner, sessionID)
	var request Request
	if err == nil {
		request, err = e.request(prepCtx, config, detail.Messages, input)
	}
	if err != nil {
		completion := aimodel.Completion{Status: "failed", ErrorCode: "request_preparation"}
		var modelError *Error
		if errors.As(err, &modelError) {
			completion.ErrorCode = modelError.Code
			completion.ErrorMessage = modelError.Message
		}
		if prepCtx.Err() != nil {
			completion.Status = "cancelled"
			completion.CancellationConfirmed = true
		}
		if finishErr := e.finish(invocation.ID, completion); finishErr != nil {
			return nil, aimodel.ErrUnavailable
		}
		return e.service.GetInvocation(ctx, owner, invocation.ID)
	}
	// The remote lifetime is independent of the completed HTTP request, but still
	// attached to executor shutdown. Preprocessing is included in Close's wait.
	callctx, cancel := context.WithTimeout(e.ctx, time.Duration(config.Connection.TimeoutSeconds)*time.Second)
	e.mu.Lock()
	if e.closed || prepCtx.Err() != nil {
		e.mu.Unlock()
		cancel()
		if err := e.finish(invocation.ID, aimodel.Completion{Status: "cancelled", CancellationConfirmed: true}); err != nil {
			return nil, aimodel.ErrUnavailable
		}
		return e.service.GetInvocation(ctx, owner, invocation.ID)
	}
	e.active[invocation.ID] = cancel
	// The background call has a distinct lifetime from Submit's deferred cleanup.
	e.wg.Add(1)
	e.mu.Unlock()
	handedOff = true
	go e.run(callctx, cancel, owner, invocation, config, request)
	return invocation, nil
}

// Stop waits for the watcher to exit; no store access survives a lifecycle close.
func (e *Executor) watchCancellation(ctx context.Context, owner int64, id string, cancel context.CancelFunc) func() {
	done, exited := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(exited)
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				if ctx.Err() != nil {
					return
				}
				checkCtx, checkCancel := context.WithTimeout(ctx, 2*time.Second)
				current, err := e.service.GetInvocation(checkCtx, owner, id)
				checkCancel()
				if err == nil && current.CancelRequested {
					cancel()
					return
				}
			}
		}
	}()
	return func() { close(done); <-exited }
}
func (e *Executor) request(ctx context.Context, cfg *aimodel.RuntimeConfig, messages []aimodel.Message, input aimodel.MessageInput) (Request, error) {
	request := Request{Model: cfg.Model.ModelID, SystemPrompt: input.SystemPrompt, Temperature: input.Parameters.Temperature, MaxOutputTokens: input.Parameters.MaxOutputTokens, Stream: cfg.Model.Capabilities.Streaming}
	if request.Temperature == nil {
		request.Temperature = cfg.Model.DefaultParameters.Temperature
	}
	if request.MaxOutputTokens == nil {
		request.MaxOutputTokens = cfg.Model.DefaultParameters.MaxOutputTokens
	}
	total := len(request.SystemPrompt)
	for _, m := range messages {
		if m.Role != "user" && m.Role != "assistant" {
			continue
		}
		message := Message{Role: m.Role, Text: m.Text}
		total += len(m.Text)
		for _, a := range m.Attachments {
			allowed := a.Kind == "image" && cfg.Model.Capabilities.Image || a.Kind == "video" && cfg.Model.Capabilities.Video || a.Kind == "pdf" && cfg.Model.Capabilities.PDF
			if !allowed {
				return request, fail("unsupported_input", "所选模型未启用此附件能力，请更换模型或移除附件")
			}
			if e.storage == nil {
				return request, aimodel.ErrUnavailable
			}
			file, _, err := e.storage.Open(ctx, a.StorageKey)
			if err != nil {
				return request, fail("attachment_unavailable", "附件已不可用，请重新上传")
			}
			data, readErr := io.ReadAll(io.LimitReader(file, aimodel.MaxAttachmentBytes+1))
			_ = file.Close()
			if readErr != nil || len(data) > aimodel.MaxAttachmentBytes {
				return request, fail("attachment_unavailable", "无法完整读取附件")
			}
			total += len(data)
			if total > MaxInputBytes {
				return request, fail("input_limit", "对话历史与附件总大小超过 24 MiB，请新建测试对话")
			}
			message.Files = append(message.Files, File{MIME: a.MIMEType, Data: data})
		}
		request.Messages = append(request.Messages, message)
	}
	return request, validateInput(cfg.Connection.Adapter, request)
}
func (e *Executor) run(ctx context.Context, cancel context.CancelFunc, owner int64, inv *aimodel.Invocation, cfg *aimodel.RuntimeConfig, request Request) {
	defer e.wg.Done()
	defer cancel()
	defer func() { <-e.slots; e.mu.Lock(); delete(e.active, inv.ID); e.mu.Unlock() }()
	started := time.Now()
	claimed, err := e.service.StartInvocation(ctx, inv.ID)
	if err != nil || !claimed {
		if err != nil {
			_ = e.finish(inv.ID, aimodel.Completion{Status: "failed", ErrorCode: "execution_start", ErrorMessage: "无法启动测试调用"})
		}
		return
	}
	stopWatch := e.watchCancellation(ctx, owner, inv.ID, cancel)
	defer stopWatch()
	lastUpdate := time.Time{}
	result, err := Generate(ctx, Config{Adapter: cfg.Connection.Adapter, BaseURL: cfg.Connection.BaseURL, Credential: cfg.Credential, AllowedPrivateHosts: e.options.AllowedPrivateHosts, Timeout: time.Duration(cfg.Connection.TimeoutSeconds) * time.Second}, request, func(text string) error {
		if time.Since(lastUpdate) < 200*time.Millisecond {
			return nil
		}
		lastUpdate = time.Now()
		if err := e.service.UpdateInvocationOutput(ctx, inv.ID, text); err != nil {
			return &Error{Code: "state_write_failed", Message: "无法保存测试输出，调用已中断", Uncertain: true}
		}
		return nil
	})
	completion := aimodel.Completion{Status: "succeeded", OutputText: result.Text, InputTokens: result.InputTokens, OutputTokens: result.OutputTokens, DurationMs: time.Since(started).Milliseconds()}
	if err != nil {
		completion.Status = "failed"
		completion.ErrorCode = ErrorCode(err)
		completion.ErrorMessage = err.Error()
		if IsUncertain(err) || ctx.Err() != nil {
			completion.Status = "indeterminate"
			completion.ErrorCode = "upstream_unconfirmed"
			completion.ErrorMessage = "请求已中断，上游是否停止或产生用量尚未确认"
		}
	}
	if err := e.finish(inv.ID, completion); err != nil {
		e.options.Log.Error("AI test result could not be persisted", "invocation_id", inv.ID)
	}
}
func (e *Executor) finish(id string, completion aimodel.Completion) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return e.service.FinishInvocation(ctx, id, completion)
}
func (e *Executor) Cancel(ctx context.Context, owner int64, id string) (*aimodel.Invocation, error) {
	invocation, err := e.service.RequestCancellation(ctx, owner, id)
	if err != nil {
		return nil, err
	}
	e.mu.Lock()
	cancel := e.active[id]
	e.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return invocation, nil
}
