package telemetry

import (
	"context"
	"errors"
	"net/http"
	"net/http/pprof"
	"os"
	"strconv"

	"connectrpc.com/connect"
	"connectrpc.com/otelconnect"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

type Shutdown func(context.Context) error

type Options struct {
	Service  string
	Instance string
	Endpoint string
	Ratio    float64
}

func FromEnv(service, instance string) Options {
	ratio := 1.0
	if raw := os.Getenv("OTEL_TRACES_SAMPLER_ARG"); raw != "" {
		if parsed, err := strconv.ParseFloat(raw, 64); err == nil && parsed >= 0 && parsed <= 1 {
			ratio = parsed
		}
	}
	return Options{Service: service, Instance: instance, Endpoint: os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"), Ratio: ratio}
}

func Setup(ctx context.Context, opts Options) (Shutdown, error) {
	otel.SetTextMapPropagator(Propagator())
	if opts.Endpoint == "" {
		return func(context.Context) error { return nil }, nil
	}
	if opts.Service == "" {
		return nil, errors.New("telemetry: service name is required")
	}
	exporter, err := otlptracegrpc.New(ctx)
	if err != nil {
		return nil, err
	}
	attrs := []resource.Option{resource.WithAttributes(semconv.ServiceName(opts.Service))}
	if opts.Instance != "" {
		attrs = append(attrs, resource.WithAttributes(semconv.ServiceInstanceID(opts.Instance)))
	}
	res, err := resource.New(ctx, attrs...)
	if err != nil {
		return nil, err
	}
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(opts.Ratio))),
	)
	otel.SetTracerProvider(provider)
	return provider.Shutdown, nil
}

func Propagator() propagation.TextMapPropagator {
	return propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})
}

func Tracer(name string) trace.Tracer { return otel.Tracer(name) }

func Interceptor() connect.Interceptor {
	interceptor, err := otelconnect.NewInterceptor(otelconnect.WithoutMetrics(), otelconnect.WithTrustRemote())
	if err != nil {
		panic(err)
	}
	return interceptor
}

func Handler(next http.Handler, operation string) http.Handler {
	return otelhttp.NewHandler(next, operation)
}

func Transport(base http.RoundTripper) http.RoundTripper {
	return otelhttp.NewTransport(base)
}

func TraceID(ctx context.Context) string {
	span := trace.SpanContextFromContext(ctx)
	if !span.IsValid() || !span.IsSampled() {
		return ""
	}
	return span.TraceID().String()
}

func Inject(ctx context.Context, carrier propagation.TextMapCarrier) {
	Propagator().Inject(ctx, carrier)
}

func Extract(ctx context.Context, carrier propagation.TextMapCarrier) context.Context {
	return Propagator().Extract(ctx, carrier)
}

func Traceparent(ctx context.Context) string {
	carrier := propagation.MapCarrier{}
	Inject(ctx, carrier)
	return carrier["traceparent"]
}

func WithTraceparent(ctx context.Context, traceparent string) context.Context {
	if traceparent == "" {
		return ctx
	}
	return Extract(ctx, propagation.MapCarrier{"traceparent": traceparent})
}

func Profiling(mux *http.ServeMux) {
	if os.Getenv("PPROF_ENABLED") != "true" {
		return
	}
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
}
