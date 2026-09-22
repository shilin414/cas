package delivery

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/shilin414/cas/backend-go/internal/automation/schedule"
	"github.com/shilin414/cas/backend-go/internal/execution"
	db "github.com/shilin414/cas/backend-go/internal/gen/db"
	"github.com/shilin414/cas/backend-go/internal/platform/ids"
	"github.com/shilin414/cas/backend-go/internal/platform/redisx"
	"github.com/shilin414/cas/backend-go/internal/platform/rolemonitor"
	"github.com/shilin414/cas/backend-go/internal/platform/telemetry"
	"github.com/shilin414/cas/backend-go/internal/sharing"
)

const (
	group                    = "deliverers"
	leaseSeconds             = 60 * time.Second // stuck 'sending' rows are reclaimed after this
	recoveryOperationTimeout = 5 * time.Second
	reclaimEvery             = 20 * time.Second
	scanEvery                = time.Second
	deliverySendTimeout      = 45 * time.Second // includes claim, limiter, auth and remote IO; below lease
	// maxAttemptsV1: deliveries dead-end after this many attempts
	// (exponential backoff + jitter; 429-style errors cool down longer).
	maxAttemptsV1 = 5
)

// Worker consumes the delivery queue. At-least-once by design: the CAS on
// delivery_executions absorbs duplicates, the due-scan loop recovers any
// pending row whose stream message was lost (Redis restart, backlog loss).
type Worker struct {
	// ObserveLoop reports completed recovery IO, not timer liveness. Set before Run.
	ObserveLoop   func(string, error)
	PublicBaseURL string
	DB            *sql.DB
	RDB           *redisx.Client
	WorkerID      string
	Concurrency   int
	Sender        Sender
	Limiter       *execution.RateLimiter
	Log           *slog.Logger
	Metrics       *telemetry.Metrics
}

func NewWorker(d *sql.DB, rdb *redisx.Client, workerID string, sender Sender, limiter *execution.RateLimiter, log *slog.Logger, m *telemetry.Metrics) *Worker {
	if log == nil {
		log = slog.Default()
	}
	return &Worker{DB: d, RDB: rdb, WorkerID: workerID, Concurrency: 4,
		Sender: sender, Limiter: limiter, Log: log, Metrics: m}
}

// MonitoringLoopBudgets references the actual recovery periods and budget,
// so entrypoints cannot accidentally validate only the fast scan loop.
func (w *Worker) MonitoringLoopBudgets() map[string]rolemonitor.LoopBudget {
	return map[string]rolemonitor.LoopBudget{
		"delivery_scan":    {Interval: scanEvery, OperationBudget: recoveryOperationTimeout},
		"delivery_reclaim": {Interval: reclaimEvery, OperationBudget: recoveryOperationTimeout},
	}
}

func (w *Worker) stream() string { return w.RDB.Key("queue", ProviderKey) }

// isMissingGroup reports whether err means the stream or its consumer group
// is gone (Redis restart, key eviction, a FLUSHDB from a shared dev Redis).
// XREADGROUP then fails with NOGROUP on every call: without recovery the pool
// warns forever while dueScanLoop keeps re-adding messages nobody reads
// (2026-09-16 实测：11 小时 133k 条 warn + stream 里 6290 条孤儿消息)。
func isMissingGroup(err error) bool {
	return err != nil && strings.Contains(err.Error(), "NOGROUP")
}

func isBusyGroup(err error) bool {
	return err != nil && strings.Contains(err.Error(), "BUSYGROUP")
}

// ensureGroup creates the consumer group idempotently; BUSYGROUP is the
// healthy "already exists" case, not a failure.
func (w *Worker) ensureGroup(ctx context.Context) {
	if err := w.RDB.XGroupCreateMkStream(ctx, w.stream(), group, "0").Err(); err != nil && !isBusyGroup(err) {
		w.Log.Warn("delivery stream group create failed", "err", err, "stream", w.stream())
	}
}

// Run starts the consumer pool plus recovery loops until ctx is done.
func (w *Worker) Run(ctx context.Context) {
	w.ensureGroup(ctx)
	var wg sync.WaitGroup
	n := w.Concurrency
	if n <= 0 {
		n = 4
	}
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			w.loop(ctx, i)
		}(i)
	}
	wg.Add(1)
	go func() { defer wg.Done(); w.dueScanLoop(ctx) }()
	wg.Add(1)
	go func() { defer wg.Done(); w.reclaimLoop(ctx) }()
	wg.Wait()
}

func (w *Worker) q(ctx context.Context) db.Querier { return db.New(w.DB) }

func (w *Worker) loop(ctx context.Context, consumer int) {
	name := w.WorkerID + "-delivery-" + strconv.Itoa(consumer)
	for {
		if ctx.Err() != nil {
			return
		}
		res, err := w.RDB.XReadGroup(ctx, &goredis.XReadGroupArgs{
			Group:    group,
			Consumer: name,
			Streams:  []string{w.stream(), ">"},
			Count:    1,
			Block:    2 * time.Second,
		}).Result()
		if err == goredis.Nil {
			continue
		}
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if isMissingGroup(err) {
				// Recreate and catch up: a fresh group reads every entry that
				// was never delivered to it (including the ones dueScanLoop
				// re-added while the group was missing). CAS still dedupes.
				w.Log.Warn("delivery stream/consumer group missing, recreating",
					"stream", w.stream(), "err", err)
				w.ensureGroup(ctx)
			} else {
				w.Log.Warn("delivery xreadgroup failed", "err", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
			continue
		}
		for _, stream := range res {
			for _, msg := range stream.Messages {
				w.process(ctx, msg)
			}
		}
	}
}

// process claims and sends one delivery. Duplicate/stale messages lose the
// CAS and are ACKed; send results are always persisted before ACK.
func (w *Worker) process(ctx context.Context, msg goredis.XMessage) {
	raw, _ := msg.Values["run_id"].(string) // outbox relay maps aggregate_id → run_id
	id, err := ids.Parse(raw)
	if err != nil {
		w.ack(ctx, msg.ID)
		return
	}
	defer func() {
		if rec := recover(); rec != nil {
			w.Log.Error("delivery panic recovered", "delivery_id", raw, "panic", rec)
			w.ack(ctx, msg.ID)
		}
	}()

	sendCtx, cancelSend := context.WithTimeout(ctx, deliverySendTimeout)
	defer cancelSend()
	q := w.q(ctx)
	row, err := q.GetDeliveryExecutionByID(sendCtx, id.Bytes())
	if err != nil {
		if err == sql.ErrNoRows {
			w.ack(ctx, msg.ID)
		} else {
			w.Log.Warn("delivery lookup failed", "err", err)
		}
		return
	}
	if row.Status == schedule.DeliverySucceeded || row.Status == schedule.DeliveryFailed || row.Status == schedule.DeliverySkipped {
		w.ack(ctx, msg.ID)
		return
	}
	res, err := q.CASClaimDelivery(sendCtx, db.CASClaimDeliveryParams{ID: id.Bytes(), Attempt: row.Attempt})
	if err != nil {
		w.Log.Warn("delivery claim failed", "delivery_id", raw, "err", err)
		return // leave unacked; reclaimed later
	}
	n, err := res.RowsAffected()
	if err != nil {
		w.Log.Warn("delivery claim result unavailable", "err", err)
		return
	}
	if n == 0 {
		w.ack(ctx, msg.ID)
		return
	} // stale generation or not yet due

	row.Attempt++ // CASClaimDelivery incremented attempt in DB

	started := time.Now()
	sendErr := w.send(sendCtx, row)
	cancelSend()
	// Persist the outcome with a bounded independent context even on shutdown
	// or send timeout. attempt is the monotonic ownership generation.
	finishCtx, cancelFinish := execution.NewCleanupContext(ctx)
	defer cancelFinish()
	if sendErr != nil {
		w.handleFailure(finishCtx, row, sendErr)
	} else {
		result, err := q.CASFinishDelivery(finishCtx, db.CASFinishDeliveryParams{
			Status: schedule.DeliverySucceeded, Column5: schedule.DeliverySucceeded,
			ID: id.Bytes(), Attempt: row.Attempt,
		})
		if err != nil {
			w.Log.Error("delivery finish failed", "delivery_id", raw, "err", err)
		} else if n, err := result.RowsAffected(); err != nil {
			w.Log.Error("delivery finish result unavailable", "delivery_id", raw, "err", err)
		} else if n == 1 && w.Metrics != nil {
			w.Metrics.DeliveryDuration.WithLabelValues(ChannelFeishu, schedule.DeliverySucceeded).Observe(time.Since(started).Seconds())
			w.Metrics.DeliverySendsTotal.WithLabelValues(ChannelFeishu, "keyed").Inc()
		}
	}
	w.ack(ctx, msg.ID)
}

// send builds the message then delivers with owner UAT under the
// shared Feishu IM rate limit.
func (w *Worker) send(ctx context.Context, row db.DeliveryExecution) error {
	if w.Limiter != nil {
		if err := w.Limiter.Acquire(ctx); err != nil {
			return err
		}
	}
	occ, err := w.q(ctx).GetScheduleOccurrenceByID(ctx, row.OccurrenceID)
	if err != nil {
		return err
	}
	sch, err := w.q(ctx).GetScheduleByID(ctx, occ.ScheduleID)
	if err != nil {
		return err
	}
	run, err := w.q(ctx).GetRunByID(ctx, row.RunID)
	if err != nil {
		return err
	}
	// Validate configuration before minting a public capability.
	if _, err := sharing.URL(w.PublicBaseURL, "check"); err != nil {
		return err
	}
	if !run.UserID.Valid || uint64(run.UserID.Int64) != row.SenderUserID {
		return fmt.Errorf("delivery sender does not own result")
	}
	result, err := sharing.EnsureResult(ctx, w.q(ctx), run, sch.Name)
	if err != nil {
		return err
	}
	var entries []sharing.Entry
	if err := json.Unmarshal(result.Snapshot, &entries); err != nil || len(entries) != 1 || entries[0].Content == nil {
		return fmt.Errorf("invalid result snapshot")
	}
	shareURL, err := sharing.URL(w.PublicBaseURL, result.Token)
	if err != nil {
		return err
	}
	text := *entries[0].Content
	subtitle := "任务已完成 · 仅分享本次执行结果"
	if !entries[0].CreatedAt.IsZero() {
		location, tzErr := time.LoadLocation(sch.Timezone)
		if tzErr != nil {
			location = time.UTC
		}
		subtitle += " · " + entries[0].CreatedAt.In(location).Format("01-02 15:04 -07:00")
	}
	// Idempotency key = the DeliveryExecution id: stable across every
	// retry of this delivery, so adapters can dedupe natively where the
	// provider supports it and ops can correlate duplicate sends where it
	// does not (at-least-once external side effect).
	execID := ids.ID(row.ID)
	return w.Sender.Send(ctx, DeliveryRequest{
		ExecutionID: execID,
		AgentName:   entries[0].AgentName, AgentIcon: entries[0].AgentIcon, AgentAvatarKey: entries[0].AgentAvatarKey,
		Title: entries[0].Title, URL: shareURL, Subtitle: subtitle,
		SenderUserID:   int64(row.SenderUserID),
		Target:         Target{Type: row.TargetType, ID: row.TargetID, Content: text},
		IdempotencyKey: execID.String(),
	})
}

// handleFailure requeues with exponential backoff + jitter, or dead-ends
// the delivery after max attempts. 429-style rate errors cool down longer;
// all state lands in MySQL before the message is ACKed.
func (w *Worker) handleFailure(ctx context.Context, row db.DeliveryExecution, sendErr error) {
	attempt := int(row.Attempt)
	code := "send_failed"
	msg := sendErr.Error()
	if strings.Contains(msg, "99991400") || strings.Contains(strings.ToLower(msg), "rate limit") ||
		strings.Contains(strings.ToLower(msg), "too many") {
		code = "rate_limited"
	}
	q := w.q(ctx)
	maxAttempts := int(row.MaxAttempts)
	if maxAttempts <= 0 {
		maxAttempts = maxAttemptsV1
	}
	if attempt >= maxAttempts {
		result, err := q.CASFinishDelivery(ctx, db.CASFinishDeliveryParams{
			Status:       schedule.DeliveryFailed,
			ErrorCode:    code,
			ErrorMessage: sqlNullString(msg),
			// Column5 = the duplicated `status` placeholder guarding sent_at;
			// a failure must leave sent_at untouched.
			Column5: schedule.DeliveryFailed,
			ID:      row.ID, Attempt: row.Attempt,
		})
		if err != nil {
			w.Log.Error("delivery dead-end failed", "err", err)
			return
		}
		if n, err := result.RowsAffected(); err != nil || n != 1 {
			w.Log.Warn("delivery failure was not applied", "err", err, "attempt", row.Attempt)
			return
		}
		if w.Metrics != nil {
			w.Metrics.DeliveryFailuresTotal.WithLabelValues(ChannelFeishu, code).Inc()
			w.Metrics.DeliveryDuration.WithLabelValues(ChannelFeishu, schedule.DeliveryFailed).
				Observe(0)
		}
		w.Log.Warn("delivery failed permanently", "delivery_id", hexID(row.ID), "code", code)
		return
	}
	backoff := time.Duration(1<<attempt) * time.Second
	if code == "rate_limited" {
		backoff *= 2
	}
	backoff += time.Duration(rand.Int63n(int64(backoff/4) + 1))
	if backoff > 5*time.Minute {
		backoff = 5 * time.Minute
	}
	// The delay is applied by the DB clock inside RequeueDelivery: the
	// worker only contributes the duration, never an absolute instant.
	result, err := q.RequeueDelivery(ctx, db.RequeueDeliveryParams{
		BackoffMicros: backoff.Microseconds(),
		ErrorCode:     code,
		ErrorMessage:  sqlNullString(msg),
		ID:            row.ID, Attempt: row.Attempt,
	})
	if err != nil {
		w.Log.Error("delivery requeue failed", "err", err)
		return
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		w.Log.Warn("delivery retry was not applied", "err", err, "attempt", row.Attempt)
		return
	}
	if w.Metrics != nil {
		w.Metrics.DeliveryRetryTotal.Inc()
	}
	w.Log.Warn("delivery retry scheduled", "delivery_id", hexID(row.ID),
		"attempt", attempt, "code", code, "backoff", backoff.String())
}

// dueScanLoop re-enqueues pending due deliveries whose stream message was
// lost; generation fencing protects local state and upstream UUID deduplicates retries.
func (w *Worker) dueScanLoop(ctx context.Context) {
	ticker := time.NewTicker(scanEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.dueScanOnce(ctx)
		}
	}
}

// reclaimLoop returns rows stuck in 'sending' (crashed worker) to pending,
// or dead-ends those that exhausted their attempts.
func (w *Worker) reclaimLoop(ctx context.Context) {
	ticker := time.NewTicker(reclaimEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.reclaimOnce(ctx)
		}
	}
}

// Recovery observations are bounded independently of sends. Empty successful
// scans count as progress; any read/write/enqueue error fails the pass.
func (w *Worker) dueScanOnce(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, recoveryOperationTimeout)
	defer cancel()
	var result error
	defer func() {
		if w.ObserveLoop != nil {
			w.ObserveLoop("delivery_scan", result)
		}
	}()
	rows, err := w.q(ctx).ListDueDeliveries(ctx, 50)
	if err != nil {
		result = err
		return
	}
	for _, row := range rows {
		result = errors.Join(result, w.enqueue(ctx, row.ID))
	}
	result = errors.Join(result, ctx.Err())
}
func (w *Worker) reclaimOnce(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, recoveryOperationTimeout)
	defer cancel()
	q := w.q(ctx)
	// The lease duration (not a local timestamp) preserves DB clock authority.
	_, first := q.ReclaimStuckDeliveries(ctx, leaseSeconds.Microseconds())
	if first != nil {
		w.Log.Warn("delivery reclaim failed", "err", first)
	}
	_, second := q.FailStuckDeliveries(ctx, db.FailStuckDeliveriesParams{
		ErrorMessage: sqlNullString("worker crashed before delivery completed"),
		LeaseMicros:  leaseSeconds.Microseconds(),
	})
	if second != nil {
		w.Log.Warn("delivery stuck fail failed", "err", second)
	}
	if w.ObserveLoop != nil {
		w.ObserveLoop("delivery_reclaim", errors.Join(first, second, ctx.Err()))
	}
}

func (w *Worker) enqueue(ctx context.Context, id []byte) error {
	if err := w.RDB.XAdd(ctx, &goredis.XAddArgs{
		Stream: w.stream(),
		MaxLen: 100000,
		Approx: true,
		Values: map[string]any{
			"run_id": ids.ID(id).Hex(),
			"event":  "delivery.dispatch",
		},
	}).Err(); err != nil {
		w.Log.Warn("delivery enqueue failed", "err", err)
		return err
	}
	return nil
}

func (w *Worker) ack(ctx context.Context, msgID string) {
	if err := w.RDB.XAck(ctx, w.stream(), group, msgID).Err(); err != nil {
		w.Log.Warn("delivery ack failed", "msg", msgID, "err", err)
	}
}

func hexID(b []byte) string { return ids.ID(b).Hex() }

func sqlNullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
