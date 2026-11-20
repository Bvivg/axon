package telemetry_test

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/bvivg/axon/core/shared/pkg/telemetry"
)

func TestTraceparentRoundTrip(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)))
	otel.SetTextMapPropagator(telemetry.Propagator())
	ctx, span := telemetry.Tracer("test").Start(context.Background(), "parent")
	header := telemetry.Traceparent(ctx)
	span.End()
	child := trace.SpanContextFromContext(telemetry.WithTraceparent(context.Background(), header))
	if child.TraceID() != span.SpanContext().TraceID() || child.SpanID() != span.SpanContext().SpanID() || !child.IsRemote() {
		t.Fatal("traceparent did not survive the round trip", header)
	}
	if telemetry.TraceID(ctx) != span.SpanContext().TraceID().String() {
		t.Fatal("trace id helper")
	}
	if telemetry.WithTraceparent(ctx, "") != ctx {
		t.Fatal("empty traceparent must keep the context")
	}
}

func TestSetupWithoutEndpointIsNoop(t *testing.T) {
	shutdown, err := telemetry.Setup(context.Background(), telemetry.Options{Service: "svc"})
	if err != nil {
		t.Fatal(err)
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}
