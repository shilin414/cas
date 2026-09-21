package aimodel

import (
	"context"
	"database/sql"
	"errors"
	"time"

	db "github.com/shilin414/cas/backend-go/internal/gen/db"
)

func cleanupRetryDelay(attempts int32) time.Duration {
	if attempts < 0 {
		attempts = 0
	}
	if attempts > 7 {
		attempts = 7
	}
	delay := 30 * time.Second * time.Duration(1<<uint(attempts))
	if delay > time.Hour {
		delay = time.Hour
	}
	return delay
}
func (s *Service) cleanupClaimedObject(ctx context.Context, key string) error {
	row, err := s.Repo.q().GetAIObjectDeletion(ctx, key)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return dbError(err)
	}
	fail := func(cause error) error {
		_, e := s.Repo.q().RetryAIObjectDeletion(ctx, db.RetryAIObjectDeletionParams{StorageKey: key, CleanupAfter: s.now().Add(cleanupRetryDelay(row.CleanupAttempts))})
		return errors.Join(dbError(cause), dbError(e))
	}
	if s.opts.DeleteObject == nil {
		return fail(ErrUnavailable)
	}
	if err = s.opts.DeleteObject(ctx, key); err != nil {
		return fail(ErrUnavailable)
	}
	if err = s.Repo.q().DeleteAIObjectDeletion(ctx, key); err != nil {
		return fail(err)
	}
	return nil
}
func (s *Service) cleanupObjects(ctx context.Context, limit int) error {
	rows, err := s.Repo.q().ListAIObjectDeletions(ctx, db.ListAIObjectDeletionsParams{CleanupAfter: s.now(), Limit: int32(limit)})
	if err != nil {
		return dbError(err)
	}
	var failures []error
	for _, row := range rows {
		claimed := false
		err = s.Repo.transaction(ctx, func(q *db.Queries) error {
			current, e := q.LockAIObjectDeletion(ctx, row.StorageKey)
			if errors.Is(e, sql.ErrNoRows) {
				return nil
			}
			if e != nil {
				return e
			}
			if current.CleanupAfter.After(s.now()) {
				return nil
			}
			if e = affected(q.LeaseAIObjectDeletion(ctx, db.LeaseAIObjectDeletionParams{StorageKey: row.StorageKey, CleanupAfter: s.now().Add(5 * time.Minute)})); e != nil {
				return e
			}
			claimed = true
			return nil
		})
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if !claimed {
			continue
		}
		if err = s.cleanupClaimedObject(ctx, row.StorageKey); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
