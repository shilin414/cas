package operations

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	db "github.com/shilin414/cas/backend-go/internal/gen/db"
	"strings"
	"time"
)

const operationTimeout = 5 * time.Second

// Service uses a separate small admin-query budget instead of permitting an
// arbitrary number of dashboards to consume the execution database pool.
type Service struct {
	DB     *sql.DB
	reads  chan struct{}
	writes chan struct{}
}

func NewService(d *sql.DB) *Service {
	return &Service{DB: d, reads: make(chan struct{}, 4), writes: make(chan struct{}, 1)}
}
func (s *Service) enter(ctx context.Context, write bool) (func(), error) {
	if s == nil || s.DB == nil {
		return nil, ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	budget := s.reads
	if write {
		budget = s.writes
	}
	select {
	case budget <- struct{}{}:
		return func() { <-budget }, nil
	default:
		return nil, ErrBusy
	}
}
func (s *Service) UpdateCapacity(ctx context.Context, actor int64, provider string, in CapacityInput) (*CapacityResult, error) {
	if actor <= 0 || !providerPattern.MatchString(provider) {
		return nil, fmt.Errorf("%w: invalid actor or provider", ErrInvalid)
	}
	if err := in.Validate(); err != nil {
		return nil, err
	}
	leave, err := s.enter(ctx, true)
	if err != nil {
		return nil, err
	}
	defer leave()
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	// Same lock ordering as ProviderSlots: admission lock -> catalog row.
	if err := db.New(s.DB).EnsureProviderAdmissionLock(ctx, provider); err != nil {
		return nil, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	q := db.New(tx)
	locked, err := q.LockProviderAdmission(ctx, provider)
	if err != nil {
		return nil, err
	}
	if n, err := locked.RowsAffected(); err != nil {
		return nil, err
	} else if n != 1 {
		return nil, ErrUnavailable
	}
	current, err := q.GetProviderCapacityForUpdate(ctx, provider)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if int64(current) != in.ExpectedMaxInflight {
		return nil, ErrConflict
	}
	result, err := tx.ExecContext(ctx, `UPDATE providers SET max_inflight=? WHERE provider_key=?`, in.MaxInflight, provider)
	if err != nil {
		return nil, err
	}
	if n, err := result.RowsAffected(); err != nil {
		return nil, err
	} else if n != 1 && !(n == 0 && int64(current) == in.MaxInflight) {
		return nil, ErrConflict
	}
	detail, err := json.Marshal(map[string]any{"before": map[string]any{"max_inflight": current}, "after": map[string]any{"max_inflight": in.MaxInflight}, "reason": strings.TrimSpace(in.Reason), "effective_for": "new_admissions"})
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_logs(user_id,action,resource,resource_id,detail) VALUES(?,?,?,?,?)`, actor, "provider.capacity.update", "provider", provider, detail); err != nil {
		return nil, err
	}
	now, err := q.CurrentDBTime(ctx)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &CapacityResult{Provider: provider, PreviousMaxInflight: int64(current), MaxInflight: in.MaxInflight, EffectiveFor: "new_admissions", UpdatedAt: now.UTC()}, nil
}
