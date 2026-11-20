package wstrace

import (
	"context"

	"go.opentelemetry.io/otel/trace"
	"google.golang.org/protobuf/encoding/protojson"

	gamev1 "github.com/bvivg/axon/core/shared/gen/go/axon/game/v1"
	"github.com/bvivg/axon/core/shared/pkg/telemetry"
)

var tracer = telemetry.Tracer("axon/gateway/ws")

func Game(ctx context.Context, payload []byte) ([]byte, func()) {
	command := &gamev1.SessionCommand{}
	if protojson.Unmarshal(payload, command) != nil {
		return payload, func() {}
	}
	name := "unknown"
	message := command.ProtoReflect()
	if field := message.WhichOneof(message.Descriptor().Oneofs().ByName("action")); field != nil {
		name = string(field.Name())
	}
	ctx, span := tracer.Start(telemetry.WithTraceparent(ctx, command.GetTraceparent()), "gateway.ws "+name, trace.WithSpanKind(trace.SpanKindServer))
	end := func() { span.End() }
	if !span.IsRecording() {
		return payload, end
	}
	traceparent := telemetry.Traceparent(ctx)
	if traceparent == "" || traceparent == command.GetTraceparent() {
		return payload, end
	}
	command.Traceparent = &traceparent
	out, err := protojson.Marshal(command)
	if err != nil {
		return payload, end
	}
	return out, end
}
