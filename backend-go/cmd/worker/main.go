// studio-worker — the execution plane. Consumes provider queues
// (Outbox → Redis Streams → CAS claim → lease → handler) and can be
// scaled per provider (--provider=feishu_aily).
//
// Batch 5: the concrete executor is NO LONGER bound here. The execution
// branch resolves its --provider through the worker dispatch registry and
// runs the returned plan; an unregistered provider fails the process
// before any consumer exists. feishu_delivery stays a separate role — it
// delivers scheduled results, it is not a Run runtime.
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
	"github.com/shilin414/cas/backend-go/internal/delivery"
	"github.com/shilin414/cas/backend-go/internal/execution"
	"github.com/shilin414/cas/backend-go/internal/platform/logging"
	"github.com/shilin414/cas/backend-go/internal/platform/rolemonitor"
	"github.com/shilin414/cas/backend-go/internal/platform/telemetry"
	"github.com/shilin414/cas/backend-go/internal/workerdispatch"
)

func main() {
	provider := flag.String("provider", "feishu_aily", "provider queue to consume (feishu_aily | feishu_delivery)")
	once := flag.Bool("migrate", false, "run migrations before starting")
	flag.Parse()

	cfg, err := app.LoadConfig()
	if err != nil {
		slog.Error("config", "err", err)
		os.Exit(1)
	}
	monitorConfig, err := rolemonitor.ParseEnv("WORKER", os.Getenv)
	if err != nil {
		slog.Error("worker monitor config", "err", err)
		os.Exit(1)
	}
	logger := logging.New(cfg.LogLevel)
	slog.SetDefault(logger)
	logger = logger.With(logging.KeyWorkerID, cfg.Runner.WorkerID)

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
	a.Log = logger

	shutdownTracer, err := telemetry.InitTracer(ctx, cfg.OTel)
	if err != nil {
		logger.Warn("tracer init failed", "err", err)
	} else {
		defer func() { _ = shutdownTracer(context.Background()) }()
	}

	runCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Execution provider plan (Batch 5 §24): resolved BEFORE any consumer
	// goroutine starts. An unregistered provider is a startup failure with
	// exit code 2 — it must never create a consumer group, XREADGROUP,
	// claim a Run or call a provider (§26).
	var plan *workerdispatch.Plan
	if *provider != delivery.ProviderKey {
		plan, err = a.WorkerDispatch.ResolveProvider(*provider)
		if err != nil {
			logger.Error("worker provider is not registered",
				"provider", *provider, "err", err)
			os.Exit(2)
		}
		logger.Info("worker provider registered",
			"provider", plan.Provider,
			"runtime_types", plan.RuntimeTypes,
			"concurrency", cfg.Runner.Concurrency)
	}

	// Monitoring is read-only and starts before any business consumer. Invalid or
	// occupied internal listeners fail startup rather than silently losing probes.
	var loops map[string]rolemonitor.LoopBudget
	var executionWorker *execution.Worker
	var deliveryWorker *delivery.Worker
	role := "worker"
	sampler := rolemonitor.SQLSampler{DB: a.DB, Provider: *provider}
	if plan == nil {
		role = "delivery_worker"
		deliveryWorker = delivery.NewWorker(a.DB, a.Redis, cfg.Runner.WorkerID,
			a.DeliverySender, a.DeliveryLimiter, logger, a.Metrics)
		deliveryWorker.PublicBaseURL = cfg.PublicBaseURL
		loops = deliveryWorker.MonitoringLoopBudgets()
		sampler.Delivery = true
	} else {
		executionWorker = &execution.Worker{
			Svc:             a.Runs,
			RDB:             a.Redis,
			Provider:        plan.Provider,
			WorkerID:        cfg.Runner.WorkerID,
			Group:           "workers",
			Handler:         plan.Handler,
			Concurrency:     cfg.Runner.Concurrency,
			Lease:           cfg.Runner.LeaseSeconds,
			Heartbeat:       cfg.Runner.HeartbeatInterval,
			ScanEvery:       cfg.Runner.ReaperInterval,
			Log:             logger,
			ProviderSlots:   plan.Slots,
			Gate:            app.NewExecutionGate(a.Catalog),
			PriorityWeights: cfg.Runner.PriorityWeights,
		}
		loops = executionWorker.MonitoringLoopBudgets()
		if plan.Slots != nil {
			sampler.IncludeCapacity = true
		}
	}
	if err := monitorConfig.ValidateLoopBudgets(loops); err != nil {
		logger.Error("worker monitor loop budget", "err", err)
		os.Exit(1)
	}
	redisProbe, err := rolemonitor.NewRedisProbe(monitorConfig, a.Redis.UniversalClient)
	if err != nil {
		logger.Error("worker Redis probe init", "err", err)
		os.Exit(1)
	}
	defer redisProbe.Close()
	monitor, err := rolemonitor.New(monitorConfig, role, *provider, loops, a.Metrics.Registry, rolemonitor.Dependencies{
		DB:    a.DB.PingContext,
		Redis: redisProbe.Ping,
		Sample: func(ctx context.Context) (rolemonitor.Snapshot, error) {
			value, err := sampler.Sample(ctx)
			if err != nil {
				return value, err
			}
			if err = ctx.Err(); err != nil {
				return rolemonitor.Snapshot{}, err
			}
			// Keep existing dashboard names compatible; never reset them on failure.
			// New studio_role_sample_* series are the freshness authority.
			a.Metrics.OutboxBacklog.Set(float64(value.PendingOutbox))
			if plan != nil {
				a.Metrics.QueueDepth.WithLabelValues(plan.Provider).Set(float64(value.Queued))
				degraded := 0.0
				if plan.Health != nil && plan.Health.LimiterDegraded() {
					degraded = 1
				}
				a.Metrics.ProviderLimiterDegraded.Set(degraded)
				if value.HasCapacity {
					a.Metrics.ProviderInflight.WithLabelValues(plan.Provider).Set(float64(value.Effective))
					a.Metrics.ProviderCapacityDepth.WithLabelValues(plan.Provider, telemetry.CapacityEffective).Set(float64(value.Effective))
					a.Metrics.ProviderCapacityDepth.WithLabelValues(plan.Provider, telemetry.CapacityControlled).Set(float64(value.Controlled))
					a.Metrics.ProviderCapacityDepth.WithLabelValues(plan.Provider, telemetry.CapacityUncontrolled).Set(float64(value.Uncontrolled))
				}
			}
			return value, nil
		},
	})
	if err != nil {
		logger.Error("worker monitor init", "err", err)
		os.Exit(1)
	}
	monitorErrors, err := monitor.Start(runCtx)
	if err != nil {
		logger.Error("worker monitor listener", "err", err)
		os.Exit(1)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := <-monitorErrors; err != nil {
			logger.Error("worker monitor stopped", "err", err)
			stop()
		}
	}()

	// Outbox relay: MySQL → Redis Streams (§22). Also routes delivery
	// outbox events to the feishu_delivery stream.
	relay := execution.NewRelay(a.Runs, a.Redis, 200)
	wg.Add(1)
	go func() {
		defer wg.Done()
		relay.Run(runCtx, cfg.Runner.RelayInterval)
	}()

	if *provider == delivery.ProviderKey {
		// Delivery consumer pool: scheduled-run results → Feishu IM.
		deliveryWorker.ObserveLoop = monitor.ObserveLoop
		wg.Add(1)
		go func() {
			defer wg.Done()
			logger.Info("delivery worker consuming", "queue", delivery.ProviderKey)
			deliveryWorker.Run(runCtx)
		}()
	} else {
		// Provider worker pool (Batch 5 §25): the plan owns the Handler and
		// the provider-wide slots — this binary never names a concrete
		// executor anymore.
		executionWorker.ObserveLoop = monitor.ObserveLoop
		wg.Add(1)
		go func() {
			defer wg.Done()
			logger.Info("worker consuming", "provider", plan.Provider, "concurrency", cfg.Runner.Concurrency)
			executionWorker.Run(runCtx)
		}()
	}

	<-runCtx.Done()
	logger.Info("worker shutting down (in-flight runs keep their leases; the reaper recovers orphans)")
	wg.Wait()
}
