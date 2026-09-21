package directory

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/shilin414/cas/backend-go/internal/platform/ids"
)

type Scheduler struct {
	Repo          *Repo
	Sync          *Service
	Log           *slog.Logger
	Owner         string
	PollInterval  time.Duration
	LeaseDuration time.Duration
}

func NewScheduler(repo *Repo, syncer *Service, owner string, log *slog.Logger) *Scheduler {
	if owner == "" {
		owner = "enterprise-sync-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	owner = owner + ":" + ids.New().String()
	if log == nil {
		log = slog.Default()
	}
	return &Scheduler{Repo: repo, Sync: syncer, Log: log, Owner: owner, PollInterval: 15 * time.Second, LeaseDuration: 10 * time.Minute}
}
func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(s.PollInterval)
	defer ticker.Stop()
	for {
		if err := s.tick(ctx); err != nil && ctx.Err() == nil {
			s.Log.Error("enterprise sync tick", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (s *Scheduler) tick(ctx context.Context) error {
	if err := s.Repo.RecoverStaleRuns(ctx); err != nil {
		return err
	}
	if err := s.Repo.ReconcileOpenBatches(ctx); err != nil {
		return err
	}
	now := time.Now().UTC()
	due, err := s.Repo.ListDueTargetConfigs(ctx, now)
	if err != nil {
		return err
	}
	for _, cfg := range due {
		ok, leaseErr := s.Repo.AcquireTargetLease(ctx, cfg.TargetCode, s.Owner, s.LeaseDuration)
		if leaseErr != nil {
			return leaseErr
		}
		if !ok {
			continue
		}
		_, nextErr := s.Repo.EnqueueDueTargetRun(ctx, cfg.TargetCode, s.Owner)
		s.Repo.ReleaseTargetLease(context.Background(), cfg.TargetCode, s.Owner)
		if nextErr != nil {
			return nextErr
		}
	}
	candidate, err := s.Repo.PeekPendingRun(ctx)
	if err != nil || candidate == nil {
		return err
	}
	target := candidate.TargetCode
	if target == "" || target == "legacy_full" {
		target = TargetDirectory
	}
	ok, err := s.Repo.AcquireTargetLease(ctx, target, s.Owner, s.LeaseDuration)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	defer s.Repo.ReleaseTargetLease(context.Background(), target, s.Owner)
	run, err := s.Repo.ClaimPendingRunID(ctx, candidate.ID)
	if err != nil || run == nil {
		return err
	}
	if run.TargetCode == "" || run.TargetCode == "legacy_full" {
		run.TargetCode = target
	}
	syncCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go s.renew(syncCtx, cancel, target)
	runErr := s.Sync.RunWithLease(syncCtx, run, s.Owner)
	if reconcileErr := s.Repo.ReconcileBatch(ctx, run.BatchID); reconcileErr != nil {
		return reconcileErr
	}
	if runErr != nil {
		s.Log.Error("sync target failed", "run_id", run.ID, "target", target, "err", runErr)
		return nil
	}
	s.Log.Info("sync target completed", "run_id", run.ID, "target", target)
	return nil
}

func (s *Scheduler) renew(ctx context.Context, cancel context.CancelFunc, target string) {
	ticker := time.NewTicker(s.LeaseDuration / 3)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.Repo.RenewTargetLease(ctx, target, s.Owner, s.LeaseDuration); err != nil {
				s.Log.Error("sync target lease renew failed; cancelling sync", "target", target, "err", err)
				cancel()
				return
			}
		}
	}
}
func NextTargetRunAt(cfg TargetConfig, from time.Time) (time.Time, error) {
	return nextRunAt(cfg.ScheduleType, cfg.IntervalMinutes, cfg.DailyTime, cfg.Timezone, from)
}
func NextRunAt(cfg SyncConfig, from time.Time) (time.Time, error) {
	return nextRunAt(cfg.ScheduleType, cfg.IntervalMinutes, cfg.DailyTime, cfg.Timezone, from)
}
func nextRunAt(scheduleType string, intervalMinutes int, dailyTime, timezone string, from time.Time) (time.Time, error) {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return time.Time{}, err
	}
	local := from.In(loc)
	if scheduleType == "daily" {
		parts := strings.Split(dailyTime, ":")
		if len(parts) != 2 {
			return time.Time{}, fmt.Errorf("invalid daily_time")
		}
		h, _ := strconv.Atoi(parts[0])
		m, _ := strconv.Atoi(parts[1])
		next := time.Date(local.Year(), local.Month(), local.Day(), h, m, 0, 0, loc)
		if !next.After(local) {
			next = next.AddDate(0, 0, 1)
		}
		return next.UTC(), nil
	}
	if intervalMinutes < 15 {
		intervalMinutes = 360
	}
	return from.Add(time.Duration(intervalMinutes) * time.Minute).UTC(), nil
}
