package operations

import (
	"context"
	"database/sql"
	"fmt"
	db "github.com/shilin414/cas/backend-go/internal/gen/db"
	"github.com/shilin414/cas/backend-go/internal/platform/capacityview"
	"sort"
	"time"
)

const activeStatesSQL = "('queued','running','waiting_input','waiting_external','cancelling')"

func (s *Service) Overview(ctx context.Context) (*Overview, error) {
	leave, err := s.enter(ctx, false)
	if err != nil {
		return nil, err
	}
	defer leave()
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	q := db.New(tx)
	now, err := q.CurrentDBTime(ctx)
	if err != nil {
		return nil, err
	}
	out := &Overview{SampledAt: now.UTC(), Providers: []Provider{}, Alerts: []Alert{}, Limitations: []string{
		"仅展示运维元数据，不包含任务输入、输出或身份凭据。",
		"运行中表示任务已被领取，可能仍在执行准入；并发判断以 Provider 有效占用为准。",
		"页面不推断各角色是否存活，请结合独立健康探针与监控。",
		"数据为有界时间内采样，不承诺精确队列位置或预计完成时间。",
	}}
	providers, err := readProviders(ctx, tx)
	if err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT provider, status, COUNT(*), MIN(queued_at) FROM runs WHERE status IN "+activeStatesSQL+" GROUP BY provider, status")
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var key, status string
		var count int64
		var oldest sql.NullTime
		if err = rows.Scan(&key, &status, &count, &oldest); err != nil {
			rows.Close()
			return nil, err
		}
		p := providers[key]
		if p == nil {
			p = &Provider{Key: key, Name: key, Status: "missing"}
			providers[key] = p
		}
		p.Counts.add(status, count)
		out.Totals.Counts.add(status, count)
		if status == "queued" && oldest.Valid {
			v := oldest.Time.UTC()
			p.OldestQueuedAt = &v
			p.OldestQueuedAgeSeconds = ageSeconds(now, v)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM schedule_occurrences WHERE status='pending'").Scan(&out.Totals.PendingOccurrences); err != nil {
		return nil, err
	}
	rows, err = tx.QueryContext(ctx, "SELECT status, COUNT(*) FROM delivery_executions WHERE status IN ('pending','sending') GROUP BY status")
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var status string
		var n int64
		if err := rows.Scan(&status, &n); err != nil {
			rows.Close()
			return nil, err
		}
		if status == "pending" {
			out.Totals.PendingDeliveries = n
		} else {
			out.Totals.SendingDeliveries = n
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	out.Totals.PendingOutbox, err = q.CountPendingOutbox(ctx)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(providers))
	for k := range providers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	// Every per-provider read is bounded by the shared request deadline. Unknown
	// legacy provider keys remain visible instead of disappearing from totals.
	for _, key := range keys {
		p := providers[key]
		depth, err := capacityview.Read(ctx, tx, key, now)
		if err != nil {
			return nil, err
		}
		p.ControlledInflight = depth.Controlled
		p.EffectiveInflight = depth.Effective
		p.UncontrolledInflight = depth.Uncontrolled
		out.Providers = append(out.Providers, *p)
		out.Alerts = append(out.Alerts, providerAlerts(*p)...)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}
func readProviders(ctx context.Context, tx *sql.Tx) (map[string]*Provider, error) {
	rows, err := tx.QueryContext(ctx, "SELECT provider_key, name, status, max_inflight FROM providers ORDER BY provider_key")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]*Provider{}
	for rows.Next() {
		p := &Provider{}
		if err := rows.Scan(&p.Key, &p.Name, &p.Status, &p.MaxInflight); err != nil {
			return nil, err
		}
		out[p.Key] = p
	}
	return out, rows.Err()
}
func (c *Counts) add(status string, n int64) {
	switch status {
	case "queued":
		c.Queued += n
	case "running":
		c.Running += n
	case "waiting_input":
		c.WaitingInput += n
	case "waiting_external":
		c.WaitingExternal += n
	case "cancelling":
		c.Cancelling += n
	}
}
func ageSeconds(now, at time.Time) int64 {
	if now.Before(at) {
		return 0
	}
	return int64(now.Sub(at) / time.Second)
}
func providerAlerts(p Provider) []Alert {
	out := []Alert{}
	add := func(code, severity, message string) {
		out = append(out, Alert{Code: code, Severity: severity, Provider: p.Key, Message: message})
	}
	if p.Status == "missing" || p.MaxInflight <= 0 {
		add("invalid_capacity_policy", "critical", "Provider 并发策略缺失或无效，新执行应保持停止准入。")
	}
	if p.UncontrolledInflight > 0 {
		add("uncontrolled_capacity", "critical", fmt.Sprintf("%d 个可能仍在上游执行的任务没有有效执行槽，请核对恢复状态，勿盲目重跑。", p.UncontrolledInflight))
	}
	if p.MaxInflight > 0 && p.EffectiveInflight > p.MaxInflight {
		add("capacity_above_policy", "warning", "有效占用高于当前上限，可能与额度下调或残留执行有关；新准入应等待占用下降。")
	}
	if p.Queued > 0 && p.Status != "active" {
		add("provider_paused_with_queue", "warning", "Provider 未启用且仍有排队任务。")
	}
	if p.Queued > 0 && p.OldestQueuedAgeSeconds >= 300 {
		add("queue_wait_high", "warning", "最早排队任务已等待至少 5 分钟，请检查容量、消费者及延期状态。")
	}
	return out
}
