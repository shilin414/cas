package execution

import (
	"context"
	"database/sql"
	"fmt"

	db "github.com/shilin414/cas/backend-go/internal/gen/db"
)

// CheckUserAdmissionInTx shares the user budget between interactive and
// scheduled work. Callers must use BeginUserAdmissionTx: the user lock
// serializes inserts, and READ COMMITTED makes the count current even after
// schedule/snapshot reads without locking other users' empty index gaps.
func CheckUserAdmissionInTx(ctx context.Context, tx *sql.Tx, userID int64, maxOutstanding int) error {
	if maxOutstanding <= 0 || userID == 0 {
		return nil
	}
	q := db.New(tx)
	if _, err := q.LockUserRow(ctx, uint64(userID)); err != nil {
		return fmt.Errorf("user admission lock: %w", err)
	}
	live, err := q.CountOutstandingRunsByUser(ctx, sql.NullInt64{Int64: userID, Valid: true})
	if err != nil {
		return fmt.Errorf("user admission current read: %w", err)
	}
	if live >= int64(maxOutstanding) {
		return ErrUserOutstandingExceeded
	}
	return nil
}

// BeginUserAdmissionTx gives each admission count a fresh statement snapshot.
// User-row locking, not run-range locks, serializes competing submissions.
func BeginUserAdmissionTx(ctx context.Context, d *sql.DB) (*sql.Tx, error) {
	return d.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
}
