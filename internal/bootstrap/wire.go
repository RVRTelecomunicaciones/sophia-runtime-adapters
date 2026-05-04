// Package bootstrap is the SOLE composition point for the Phase 1
// runtime (D7.9 / R3). It is the only place in the codebase that imports
// concrete adapters (internal/adapters/...) and infrastructure
// (internal/infrastructure/...). Domain and application layers depend on
// ports only.
//
// BuildRuntime(ctx, cfg) returns a *Runtime bundling the HTTP server,
// the pgx pool, and a shutdown function that tears them down in the
// right order.
package bootstrap

import (
	"context"
	"errors"
	"fmt"
	nethttp "net/http"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"

	inboundhttp "github.com/sophia-ecosystem/runtime-adapters/internal/adapters/inbound/http"
	"github.com/sophia-ecosystem/runtime-adapters/internal/adapters/outbound/filesystem"
	"github.com/sophia-ecosystem/runtime-adapters/internal/adapters/outbound/git"
	"github.com/sophia-ecosystem/runtime-adapters/internal/adapters/outbound/httpreq"
	"github.com/sophia-ecosystem/runtime-adapters/internal/adapters/outbound/pg"
	"github.com/sophia-ecosystem/runtime-adapters/internal/adapters/outbound/registration"
	"github.com/sophia-ecosystem/runtime-adapters/internal/adapters/outbound/shell"
	"github.com/sophia-ecosystem/runtime-adapters/internal/application/services"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/entities"
	domainservices "github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/services"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/execution/valueobjects"
	"github.com/sophia-ecosystem/runtime-adapters/internal/domain/shared"
	"github.com/sophia-ecosystem/runtime-adapters/internal/infrastructure/chaos"
	"github.com/sophia-ecosystem/runtime-adapters/internal/infrastructure/config"
	"github.com/sophia-ecosystem/runtime-adapters/internal/infrastructure/obs"
	"github.com/sophia-ecosystem/runtime-adapters/internal/infrastructure/obs/log"
	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/inbound"
	"github.com/sophia-ecosystem/runtime-adapters/internal/ports/outbound"
)

// errRuntimeModeLock is the sentinel returned when the Layer 3
// runtime-side mode lock fires per spec §9.3 + D2C4AB.16.
var errRuntimeModeLock = errors.New("CI must run with RUNTIME_TENANT=test")

// enforceRuntimeModeLock implements Layer 3 (runtime side) per spec
// §9.3 + D2C4AB.16. Aborts startup if CI=true && RUNTIME_TENANT !=
// "test". getenv is injectable for hermetic testing — production
// callers pass os.Getenv via BuildRuntime.
func enforceRuntimeModeLock(getenv func(string) string) error {
	if strings.ToLower(getenv("CI")) == "true" && getenv("RUNTIME_TENANT") != "test" {
		return fmt.Errorf("%w: got RUNTIME_TENANT=%q", errRuntimeModeLock, getenv("RUNTIME_TENANT"))
	}
	return nil
}

// Runtime is the live wiring returned by BuildRuntime. Consumers
// (cmd/runtime-adapters/main.go) call Server.ListenAndServe and later
// Shutdown to tear down.
type Runtime struct {
	Server   *nethttp.Server
	Pool     *pgxpool.Pool
	Shutdown func(ctx context.Context) error // idempotent; tears down HTTP → pool collector → OTel → pool in order
	// PoolCollector is the pgxpool stats observable (Phase 2C.4 / E).
	// Held here so Shutdown can unregister its OTel callback before
	// pool.Close().
	PoolCollector *obs.PgxPoolCollector
	// ExecSvc and QuerySvc are the inbound service handles wired at
	// bootstrap. Exposed for the in-process SDK peer (test/chaos/integration)
	// and any other consumer that needs direct service access without going
	// through the HTTP transport. Production callers use the HTTP server.
	ExecSvc  inbound.RuntimeService
	QuerySvc inbound.QueryService
}

// BuildRuntime composes the full Phase 1 runtime from cfg. Returns the
// HTTP server (not yet started), the pgx pool, and a shutdown function.
// On error, already-opened resources are released before return.
func BuildRuntime(ctx context.Context, cfg config.Config) (*Runtime, error) {
	// 0. Layer 3 runtime-side mode lock (Phase 2C.4 / A+B B3).
	//    Aborts startup if CI=true && RUNTIME_TENANT != "test".
	//    Pre-empts any other initialization (logger, OTel, pool) so
	//    a misconfigured CI run never opens connections to prod
	//    Postgres or pages a real Slack channel.
	if err := enforceRuntimeModeLock(os.Getenv); err != nil {
		return nil, err
	}

	// 1. Root logger. Constructed BEFORE OTel so obs setup (and any later
	//    step) has a concrete logger available if it needs to emit. A
	//    logger build failure aborts startup (fail fast — invalid
	//    RUNTIME_LOG_* env, R10 strict-config stance).
	logCfg, err := log.LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("log config: %w", err)
	}
	rootLogger, err := log.New(logCfg)
	if err != nil {
		return nil, fmt.Errorf("log.New: %w", err)
	}

	// 2. OTel (adapters use otel.Tracer/Meter; must be initialized before
	//    any adapter construction).
	otelShutdown, err := obs.SetupOTel(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("setup otel: %w", err)
	}

	// 3. Pool + migrations.
	pool, err := pgxpool.New(ctx, cfg.PostgresDSN)
	if err != nil {
		_ = otelShutdown(ctx)
		return nil, fmt.Errorf("pgxpool.New: %w", err)
	}
	if err := pg.Migrate(ctx, pool); err != nil {
		// poolCollector is not yet declared at this point — pre-step-3.5.
		pool.Close()
		_ = otelShutdown(ctx)
		return nil, fmt.Errorf("apply migrations: %w", err)
	}

	// 3.5 — pgx pool stats collector (Phase 2C.4 / E).
	// Wired AFTER migrations succeed so the first observable callback
	// sees a stable pool. The collector is held on Runtime so the
	// shutdown closure can unregister its callback BEFORE pool.Close()
	// — keeping the SDK from retaining a callback over a torn pool.
	poolCollector, err := obs.NewPgxPoolCollector(
		otel.Meter("runtime-adapters"),
		func() obs.PoolStatSnapshot { return obs.SnapshotFromPgx(pool.Stat()) },
	)
	if err != nil {
		_ = poolCollector.Close()
		pool.Close()
		_ = otelShutdown(ctx)
		return nil, fmt.Errorf("pgx pool collector: %w", err)
	}

	// 4. Outbound repos (T48 / T49).
	// NewReceiptRepositoryPG and NewIdempotencyStorePG only take a pool.
	// receiptRepo is declared as the interface so the chaos wrap (step 6.5)
	// can reassign it to a ChaosReceiptRepository without a type mismatch.
	var receiptRepo outbound.ReceiptRepository
	receiptRepo, err = pg.NewReceiptRepositoryPG(pool)
	if err != nil {
		_ = poolCollector.Close()
		pool.Close()
		_ = otelShutdown(ctx)
		return nil, fmt.Errorf("receipt repository: %w", err)
	}
	idempStore, err := pg.NewIdempotencyStorePG(pool)
	if err != nil {
		_ = poolCollector.Close()
		pool.Close()
		_ = otelShutdown(ctx)
		return nil, fmt.Errorf("idempotency store: %w", err)
	}

	// 5. Domain registry + normalizer.
	caps, err := valueobjects.NewPhase1Capabilities()
	if err != nil {
		_ = poolCollector.Close()
		pool.Close()
		_ = otelShutdown(ctx)
		return nil, fmt.Errorf("phase 1 capabilities: %w", err)
	}
	registry, err := valueobjects.NewCapabilityRegistry(caps...)
	if err != nil {
		_ = poolCollector.Close()
		pool.Close()
		_ = otelShutdown(ctx)
		return nil, fmt.Errorf("capability registry: %w", err)
	}
	normalizer := domainservices.NewResultNormalizer(cfg.InlineStreamLimit)

	// Metrics Registry (§6.3). Bound to the process-wide MeterProvider
	// installed by SetupOTel; safe with the no-op provider if OTel is
	// disabled. ExecuteService uses this to emit execution.active /
	// concurrency.rejects / execution.total / execution.duration /
	// partial.signal from a single choke-point.
	metricsRegistry, err := obs.NewRegistry(otel.Meter("runtime-adapters"))
	if err != nil {
		_ = poolCollector.Close()
		pool.Close()
		_ = otelShutdown(ctx)
		return nil, fmt.Errorf("metrics registry: %w", err)
	}

	// 6. Concrete adapters + register normalizers.
	clk := shared.RealClock{}
	adapters, err := registration.RegisterAllPhase1(normalizer, adapterConfig(cfg), clk)
	if err != nil {
		_ = poolCollector.Close()
		pool.Close()
		_ = otelShutdown(ctx)
		return nil, fmt.Errorf("register adapters: %w", err)
	}
	if err := registration.VerifyCoversPhase1Catalog(normalizer); err != nil {
		_ = poolCollector.Close()
		pool.Close()
		_ = otelShutdown(ctx)
		return nil, fmt.Errorf("verify catalog: %w", err)
	}

	// 6.5. Chaos integration (Phase 2C.3 B1 Task 1.10).
	// chaos.LoadConfig is fail-closed (R17): disabled by default, rejects
	// production, requires a profile path when enabled, validates against
	// the Phase 1 catalog. MaybeWrapAdaptersWithChaos returns inputs
	// unchanged when chaos is disabled — zero behavior change to the
	// non-chaos production path.
	chaosCat := chaos.NewCatalogFromCapabilities(caps)
	chaosCfg, err := chaos.LoadConfig(cfg.Chaos, cfg.Env, chaosCat)
	if err != nil {
		_ = poolCollector.Close()
		pool.Close()
		_ = otelShutdown(ctx)
		return nil, fmt.Errorf("chaos config: %w", err)
	}
	adapters, wrappedReceiptRepo, err := chaos.MaybeWrapAdaptersWithChaos(adapters, receiptRepo, chaosCfg, clk)
	if err != nil {
		_ = poolCollector.Close()
		pool.Close()
		_ = otelShutdown(ctx)
		return nil, fmt.Errorf("chaos wrap: %w", err)
	}
	receiptRepo = wrappedReceiptRepo

	// 7. Concurrency limiter + provenance baseline.
	limiter := services.NewConcurrencyLimiter(cfg.MaxConcurrentExecutions)
	prov, err := entities.NewProvenance(
		entities.ProvenanceSource(cfg.ProvenanceSource),
		cfg.RuntimeVersion,
		cfg.Hostname,
		cfg.RuntimeVersion,
		"",
	)
	if err != nil {
		_ = poolCollector.Close()
		pool.Close()
		_ = otelShutdown(ctx)
		return nil, fmt.Errorf("provenance baseline: %w", err)
	}

	// 8. ExecuteService + QueryService.
	// Adapter map keyed by AdapterID.String() for ExecuteService consumption.
	adaptersByString := make(map[string]outbound.Adapter, len(adapters))
	for aid, a := range adapters {
		adaptersByString[aid.String()] = a
	}
	idGen := entities.ULIDGen{}
	execSvc, err := services.NewExecuteService(services.ExecuteServiceConfig{
		Adapters:    adaptersByString,
		Registry:    registry,
		Metrics:     metricsRegistry,
		Normalizer:  normalizer,
		Receipts:    receiptRepo,
		Idempotency: idempStore,
		Limiter:     limiter,
		Clock:       clk,
		IDGen:       idGen,
		MaxTimeout:  cfg.MaxTimeoutBudget,
		IdempWindow: cfg.IdempotencyWindow,
		Provenance:  prov,
	})
	if err != nil {
		_ = poolCollector.Close()
		pool.Close()
		_ = otelShutdown(ctx)
		return nil, fmt.Errorf("execute service: %w", err)
	}
	querySvc, err := services.NewQueryService(services.QueryServiceConfig{
		Registry:       registry,
		Receipts:       receiptRepo,
		RuntimeVersion: cfg.RuntimeVersion,
	})
	if err != nil {
		_ = poolCollector.Close()
		pool.Close()
		_ = otelShutdown(ctx)
		return nil, fmt.Errorf("query service: %w", err)
	}

	// 9. HTTP router (inbound). rootLogger is threaded into the chain so
	//    LoggerMiddleware binds a request-scoped logger into ctx (§5.5).
	router := inboundhttp.NewRouter(execSvc, querySvc, rootLogger)
	server := &nethttp.Server{
		Addr:    cfg.HTTPAddr,
		Handler: router,
		// ReadHeaderTimeout caps the time spent reading the request line +
		// headers — closes Slowloris-style stalled-header attacks (G112).
		// 10s is generous for any well-behaved client given runtime-adapters
		// is typically reached over the same VPC / cluster as its callers.
		ReadHeaderTimeout: 10 * time.Second,
	}

	shutdown := func(ctx context.Context) error {
		// Shutdown order (deliberate):
		//  1. HTTP server first — stops accepting new requests and
		//     drains in-flight handlers; any handler still holding a
		//     pool connection gets to finish cleanly.
		//  2. Pool collector — unregister the OTel observable callback
		//     so the SDK doesn't try to read pool.Stat() after the
		//     pool closes (Phase 2C.4 / E).
		//  3. OTel — flushes any spans/metrics generated during the
		//     handler drain before closing exporters.
		//  4. Pool last — by now no handler needs it; closing earlier
		//     would abort in-flight receipts mid-persist (A4.3).
		// Errors are collected but the sequence is always completed.
		var firstErr error
		if err := server.Shutdown(ctx); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("http shutdown: %w", err)
		}
		if err := poolCollector.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("pool collector shutdown: %w", err)
		}
		if err := otelShutdown(ctx); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("otel shutdown: %w", err)
		}
		pool.Close()
		return firstErr
	}

	return &Runtime{
		Server:        server,
		Pool:          pool,
		Shutdown:      shutdown,
		PoolCollector: poolCollector,
		ExecSvc:       execSvc,
		QuerySvc:      querySvc,
	}, nil
}

// adapterConfig translates the top-level Config into the adapter-specific
// sub-configs expected by registration.RegisterAllPhase1.
func adapterConfig(cfg config.Config) registration.Config {
	return registration.Config{
		Shell: shell.Config{
			AllowedCommandsPath: cfg.AllowedCommandsPath,
			AllowedWorkingDirs:  cfg.AllowedWorkingDirs,
			InlineStreamLimit:   cfg.InlineStreamLimit,
		},
		Git: git.Config{
			AllowedWorkingDirs: cfg.AllowedWorkingDirs,
			SSHAgent:           true, // enabled by default in Phase 1; callers may disable later via config
			InlineStreamLimit:  cfg.InlineStreamLimit,
		},
		Filesystem: filesystem.Config{
			AllowedFilesystemRoots: cfg.AllowedFilesystemRoots,
			InlineStreamLimit:      cfg.InlineStreamLimit,
		},
		HTTP: httpreq.Config{
			AllowPrivateNetworks: cfg.HTTPAllowPrivateNetworks,
			HostAllowlist:        cfg.HTTPHostAllowlist,
			InlineStreamLimit:    cfg.InlineStreamLimit,
		},
	}
}
