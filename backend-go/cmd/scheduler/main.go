// studio-scheduler — the fourth deployment role. Scans due schedules and
// converts them into occurrences + runs through the existing execution
// engine. It never calls providers and never sends messages.
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/shilin414/cas/backend-go/internal/app"
	"github.com/shilin414/cas/backend-go/internal/platform/logging"
	"github.com/shilin414/cas/backend-go/internal/platform/rolemonitor"
	"github.com/shilin414/cas/backend-go/internal/platform/telemetry"
)

func main() {
	once := flag.Bool("migrate", false, "run migrations before starting")
	flag.Parse()

	cfg, err := app.LoadConfig()
	if err != nil {
		slog.Error("config", "err", err)
		os.Exit(1)
	}
	monitorConfig, err := rolemonitor.ParseEnv("SCHEDULER", os.Getenv)
	if err != nil {
		slog.Error("scheduler monitor config", "err", err)
		os.Exit(1)
	}
	logger := logging.New(cfg.LogLevel)
	slog.SetDefault(logger)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if *once {
		if err := app.MigrateUp(ctx, cfg.Database.DSN(), "db/migrations"); err != nil {
			logger.Error("migrate failed", "err", err)
			os.Exit(1)
		}
	}

	a, err := app.Build(ctx, cfg)
	if err != nil {
		logger.Error("build app", "err", err)
		os.Exit(1)
	}
	defer a.Close()

	shutdownTracer, err := telemetry.InitTracer(ctx, cfg.OTel)
	if err != nil {
		logger.Warn("tracer init failed", "err", err)
	} else {
		defer func() { _ = shutdownTracer(context.Background()) }()
	}

	runCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	loops := a.Scheduler.MonitoringLoopBudgets()
	if err := monitorConfig.ValidateLoopBudgets(loops); err != nil {
		logger.Error("scheduler monitor loop budget", "err", err)
		os.Exit(1)
	}
	redisProbe, err := rolemonitor.NewRedisProbe(monitorConfig, a.Redis.UniversalClient)
	if err != nil {
		logger.Error("scheduler Redis probe init", "err", err)
		os.Exit(1)
	}
	defer redisProbe.Close()
	sampler := rolemonitor.SQLSampler{DB: a.DB}
	monitor, err := rolemonitor.New(monitorConfig, "scheduler", "all", loops, a.Metrics.Registry, rolemonitor.Dependencies{
		DB:    a.DB.PingContext,
		Redis: redisProbe.Ping,
		Sample: func(ctx context.Context) (rolemonitor.Snapshot, error) {
			value, err := sampler.Sample(ctx)
			if err == nil {
				err = ctx.Err()
			}
			if err == nil {
				a.Metrics.OutboxBacklog.Set(float64(value.PendingOutbox))
			}
			return value, err
		},
	})
	if err != nil {
		logger.Error("scheduler monitor init", "err", err)
		os.Exit(1)
	}
	a.Scheduler.ObserveLoop = monitor.ObserveLoop
	monitorErrors, err := monitor.Start(runCtx)
	if err != nil {
		logger.Error("scheduler monitor listener", "err", err)
		os.Exit(1)
	}
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		if err := <-monitorErrors; err != nil {
			logger.Error("scheduler monitor stopped", "err", err)
			stop()
		}
	}()
	go func() { defer wg.Done(); a.Scheduler.Run(runCtx) }()
	go func() { defer wg.Done(); a.DirectoryScheduler.Run(runCtx) }()
	wg.Wait()
	logger.Info("scheduler stopped")
}
