// Package capacityview reads a coherent, metadata-only capacity observation.
package capacityview

import (
	"context"
	"errors"
	db "github.com/shilin414/cas/backend-go/internal/gen/db"
	"time"
)

type Depth struct{ Effective, Controlled, Uncontrolled int64 }

// Both the row version and expiry clock must be coherent. A repeatable-read
// transaction alone does not freeze CURRENT_TIMESTAMP between statements.
const query = `SELECT COUNT(*) AS effective,
 COALESCE(SUM(has_slot),0) AS controlled,
 COALESCE(SUM(CASE WHEN has_slot=0 THEN external_work ELSE 0 END),0) AS uncontrolled
 FROM (
   SELECT run_id, MAX(controlled) AS has_slot, MAX(external_work) AS external_work
   FROM (
     SELECT s.run_id, 1 AS controlled, 0 AS external_work
     FROM provider_execution_slots s
     WHERE s.provider=? AND s.expires_at>?
     UNION ALL
     SELECT r.id AS run_id, 0 AS controlled, 1 AS external_work
     FROM runs r STRAIGHT_JOIN provider_submissions ps
       ON ps.run_id=r.id AND ps.provider=r.provider
     WHERE r.provider=?
       AND r.status IN ('queued','running','waiting_input','waiting_external','cancelling')
       AND ps.state IN ('sending','unknown','accepted')
   ) candidates
   GROUP BY run_id
 ) capacity_runs`

// Read pins the expiry predicate to the caller's DATABASE sampling clock. It
// deduplicates attempts/submissions by run, matching the admission bound. It
// must never receive a host-clock fallback or fabricate zeros on read failure.
func Read(ctx context.Context, conn db.DBTX, provider string, at time.Time) (Depth, error) {
	if conn == nil || at.IsZero() {
		return Depth{}, errors.New("capacity observation requires a database and its sampling time")
	}
	var out Depth
	err := conn.QueryRowContext(ctx, query, provider, at, provider).Scan(&out.Effective, &out.Controlled, &out.Uncontrolled)
	return out, err
}
