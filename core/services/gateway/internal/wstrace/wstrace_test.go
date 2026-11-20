package wstrace_test

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/bvivg/axon/core/services/gateway/internal/wstrace"
	gamev1 "github.com/bvivg/axon/core/shared/gen/go/axon/game/v1"
	"github.com/bvivg/axon/core/shared/pkg/telemetry"
)

func TestGameCommandContinuesTheClientTrace(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)))
	otel.SetTextMapPropagator(telemetry.Propagator())
	client := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	frame, _ := protojson.Marshal(&gamev1.SessionCommand{
		Action:      &gamev1.SessionCommand_ChessMove{ChessMove: &gamev1.ChessMove{Ply: 0, Uci: "e2e4"}},
		Traceparent: &client,
	})
	out, done := wstrace.Game(context.Background(), frame)
	done()
	forwarded := &gamev1.SessionCommand{}
	if err := protojson.Unmarshal(out, forwarded); err != nil {
		t.Fatal(err)
	}
	if forwarded.GetChessMove().GetUci() != "e2e4" {
		t.Fatal("command changed", forwarded)
	}
	spans := recorder.Ended()
	if len(spans) != 1 || spans[0].Name() != "gateway.ws chess_move" {
		t.Fatal("spans", spans)
	}
	gateway := spans[0].SpanContext()
	if gateway.TraceID().String() != "4bf92f3577b34da6a3ce929d0e0e4736" || spans[0].Parent().SpanID().String() != "00f067aa0ba902b7" {
		t.Fatal("gateway span is not a child of the client span")
	}
	upstream := trace.SpanContextFromContext(telemetry.WithTraceparent(context.Background(), forwarded.GetTraceparent()))
	if upstream.SpanID() != gateway.SpanID() || upstream.TraceID() != gateway.TraceID() {
		t.Fatal("forwarded frame does not carry the gateway span", forwarded.GetTraceparent())
	}
}

func TestNonCommandFramesPassThrough(t *testing.T) {
	out, done := wstrace.Game(context.Background(), []byte("not json"))
	done()
	if string(out) != "not json" {
		t.Fatal(string(out))
	}
}
