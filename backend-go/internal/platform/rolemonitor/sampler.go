package rolemonitor

import (
	"context"
	"database/sql"
	"fmt"
	db "github.com/shilin414/cas/backend-go/internal/gen/db"
	"github.com/shilin414/cas/backend-go/internal/platform/capacityview"
)

// Snapshot is replaced only after every read succeeds. Capacity uses a single
// SQL statement and one DB sampling instant; queue/outbox are adjacent reads.
type Snapshot struct {
	Queued, PendingOutbox               int64
	OldestWaitSeconds                   float64
	HasCapacity                         bool
	Controlled, Uncontrolled, Effective int64
}
type SQLSampler struct {
	DB              *sql.DB
	Provider        string // empty: all execution providers (scheduler)
	Delivery        bool
	IncludeCapacity bool
}

// These queries restrict the indexed active status prefix: they never aggregate
// completed history or payloads. Counts remain O(active backlog), not O(1).
// FORCE INDEX prevents the optimizer choosing a full historical scan. No locks,
// schema changes, generated queries or business operations are involved.
const queueSQL = `SELECT COUNT(*), COALESCE(GREATEST(0, TIMESTAMPDIFF(MICROSECOND, MIN(queued_at), CURRENT_TIMESTAMP(3)) / 1000000.0), 0)
 FROM runs FORCE INDEX (idx_runs_claim) WHERE status = 'queued'`
const deliverySQL = `SELECT COUNT(*), COALESCE(GREATEST(0, TIMESTAMPDIFF(MICROSECOND, MIN(created_at), CURRENT_TIMESTAMP(3)) / 1000000.0), 0)
 FROM delivery_executions FORCE INDEX (idx_deliveries_pending) WHERE status = 'pending'`
const outboxSQL = `SELECT COUNT(*) FROM outbox_events FORCE INDEX (idx_outbox_dispatch) WHERE status = 'pending'`

func (s SQLSampler) Sample(ctx context.Context) (Snapshot, error) {
	var v Snapshot
	if s.DB == nil {
		return v, fmt.Errorf("monitor database unavailable")
	}
	query := queueSQL
	var args []any
	if s.Delivery {
		query = deliverySQL
	} else if s.Provider != "" {
		query += " AND provider = ?"
		args = append(args, s.Provider)
	}
	if err := s.DB.QueryRowContext(ctx, query, args...).Scan(&v.Queued, &v.OldestWaitSeconds); err != nil {
		return Snapshot{}, err
	}
	if err := s.DB.QueryRowContext(ctx, outboxSQL).Scan(&v.PendingOutbox); err != nil {
		return Snapshot{}, err
	}
	if s.IncludeCapacity {
		at, err := db.New(s.DB).CurrentDBTime(ctx)
		if err != nil {
			return Snapshot{}, err
		}
		depth, err := capacityview.Read(ctx, s.DB, s.Provider, at)
		if err != nil {
			return Snapshot{}, err
		}
		v.Effective, v.Controlled, v.Uncontrolled = depth.Effective, depth.Controlled, depth.Uncontrolled
		v.HasCapacity = true
	}

	return v, nil
}
