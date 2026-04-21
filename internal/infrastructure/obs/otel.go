// Package obs provides the Phase 1 observability wiring — OpenTelemetry
// trace + metric providers and the Registry of named instruments used by
// application + adapter layers.
//
// Activation is opt-in via config (OTEL_ENABLED=true). When disabled,
// SetupOTel installs the no-op providers from the OTel SDK and returns
// a no-op shutdown function — code that calls otel.Tracer()/otel.Meter()
// receives valid, silent instruments. This keeps instrumentation
// side-effect-free during local development and tests.
package obs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"

	"github.com/sophia-ecosystem/runtime-adapters/internal/infrastructure/config"
)

// ShutdownFn flushes pending spans/metrics and closes the exporters.
// Callers MUST defer it to avoid losing telemetry on process exit.
// Safe to call with a short timeout context; returns nil when already
// shut down.
type ShutdownFn func(ctx context.Context) error

// SetupOTel configures the global OTel providers from the supplied
// config. When cfg.OTelEnabled is false, installs no-op providers and
// returns a no-op shutdown. Always returns a non-nil ShutdownFn so
// callers can unconditionally defer it.
//
// On error the global providers are NOT modified.
func SetupOTel(ctx context.Context, cfg config.Config) (ShutdownFn, error) {
	if !cfg.OTelEnabled {
		// No-op path — do not touch global providers; the default
		// noop providers are already in place.
		return func(context.Context) error { return nil }, nil
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(cfg.OTelServiceName),
			semconv.ServiceVersionKey.String(cfg.RuntimeVersion),
			semconv.HostNameKey.String(cfg.Hostname),
		),
	)
	if err != nil {
		return noopShutdown, fmt.Errorf("otel resource: %w", err)
	}

	// Trace pipeline.
	tExp, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(cfg.OTelExporterEndpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return noopShutdown, fmt.Errorf("otel trace exporter: %w", err)
	}
	sampler, err := parseSampler(cfg.OTelTracesSampler, cfg.OTelTracesSamplerArg)
	if err != nil {
		_ = tExp.Shutdown(ctx)
		return noopShutdown, fmt.Errorf("otel sampler: %w", err)
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
		sdktrace.WithBatcher(tExp, sdktrace.WithMaxExportBatchSize(512)),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// Metric pipeline.
	mExp, err := otlpmetricgrpc.New(ctx,
		otlpmetricgrpc.WithEndpoint(cfg.OTelExporterEndpoint),
		otlpmetricgrpc.WithInsecure(),
	)
	if err != nil {
		_ = tp.Shutdown(ctx)
		_ = tExp.Shutdown(ctx)
		return noopShutdown, fmt.Errorf("otel metric exporter: %w", err)
	}
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(mExp,
			sdkmetric.WithInterval(30*time.Second),
		)),
	)
	otel.SetMeterProvider(mp)

	shutdown := func(ctx context.Context) error {
		var firstErr error
		if err := tp.Shutdown(ctx); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("tracer shutdown: %w", err)
		}
		if err := mp.Shutdown(ctx); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("meter shutdown: %w", err)
		}
		return firstErr
	}
	return shutdown, nil
}

func noopShutdown(context.Context) error { return nil }

// parseSampler translates OTEL_TRACES_SAMPLER + OTEL_TRACES_SAMPLER_ARG
// to a Sampler. Supports:
//
//	always_on
//	always_off
//	traceidratio (arg = ratio 0..1)
//	parentbased_always_on
//	parentbased_always_off
//	parentbased_traceidratio (default; arg = ratio 0..1)
func parseSampler(name string, arg float64) (sdktrace.Sampler, error) {
	switch name {
	case "always_on":
		return sdktrace.AlwaysSample(), nil
	case "always_off":
		return sdktrace.NeverSample(), nil
	case "traceidratio":
		return sdktrace.TraceIDRatioBased(arg), nil
	case "parentbased_always_on":
		return sdktrace.ParentBased(sdktrace.AlwaysSample()), nil
	case "parentbased_always_off":
		return sdktrace.ParentBased(sdktrace.NeverSample()), nil
	case "parentbased_traceidratio", "":
		return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(arg)), nil
	default:
		return nil, errors.New("unknown sampler: " + name)
	}
}
