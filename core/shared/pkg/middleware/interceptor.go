package middleware

import (
	"context"
	"log/slog"
	"runtime/debug"
	"time"

	"connectrpc.com/connect"

	"github.com/bvivg/axon/core/shared/pkg/correlation"
)

// CorrelationInterceptor moves the correlation ID between the context and the
// wire in both directions: on a handler it adopts the inbound header, and on an
// outgoing call it writes the context's ID into the request header so the next
// service downstream sees the same ID.
type CorrelationInterceptor struct{}

// NewCorrelationInterceptor returns an interceptor that propagates correlation IDs.
func NewCorrelationInterceptor() *CorrelationInterceptor {
	return &CorrelationInterceptor{}
}

var _ connect.Interceptor = (*CorrelationInterceptor)(nil)

func (i *CorrelationInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		if req.Spec().IsClient {
			ctx, id := correlation.Ensure(ctx)
			req.Header().Set(correlation.Header, id)
			return next(ctx, req)
		}

		ctx, id := correlation.Ensure(correlation.WithID(ctx, req.Header().Get(correlation.Header)))

		resp, err := next(ctx, req)
		if resp != nil {
			resp.Header().Set(correlation.Header, id)
		}
		return resp, err
	}
}

func (i *CorrelationInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return func(ctx context.Context, spec connect.Spec) connect.StreamingClientConn {
		ctx, id := correlation.Ensure(ctx)
		conn := next(ctx, spec)
		conn.RequestHeader().Set(correlation.Header, id)
		return conn
	}
}

func (i *CorrelationInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		ctx, id := correlation.Ensure(correlation.WithID(ctx, conn.RequestHeader().Get(correlation.Header)))
		conn.ResponseHeader().Set(correlation.Header, id)
		return next(ctx, conn)
	}
}

// RecoveryInterceptor converts a panic in a handler into an internal error.
// The client is told only that something broke; the stack goes to the log.
type RecoveryInterceptor struct {
	logger *slog.Logger
}

// NewRecoveryInterceptor returns an interceptor that recovers handler panics.
func NewRecoveryInterceptor(log *slog.Logger) *RecoveryInterceptor {
	return &RecoveryInterceptor{logger: log}
}

var _ connect.Interceptor = (*RecoveryInterceptor)(nil)

// errInternal is what a recovered panic looks like on the wire.
func errInternal() error {
	return connect.NewError(connect.CodeInternal, errUnavailable{})
}

// errUnavailable carries a deliberately opaque message.
type errUnavailable struct{}

func (errUnavailable) Error() string { return "internal error" }

func (i *RecoveryInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (resp connect.AnyResponse, err error) {
		if req.Spec().IsClient {
			return next(ctx, req)
		}

		defer func() {
			if v := recover(); v != nil {
				i.log(ctx, req.Spec().Procedure, v)
				resp, err = nil, errInternal()
			}
		}()

		return next(ctx, req)
	}
}

func (i *RecoveryInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (i *RecoveryInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) (err error) {
		defer func() {
			if v := recover(); v != nil {
				i.log(ctx, conn.Spec().Procedure, v)
				err = errInternal()
			}
		}()

		return next(ctx, conn)
	}
}

func (i *RecoveryInterceptor) log(ctx context.Context, procedure string, v any) {
	i.logger.ErrorContext(ctx, "panic recovered in rpc handler",
		"panic", v,
		"procedure", procedure,
		"stack", string(debug.Stack()),
	)
}

// LoggingInterceptor logs one line per RPC on completion.
type LoggingInterceptor struct {
	logger *slog.Logger
}

// NewLoggingInterceptor returns an interceptor that logs completed RPCs.
func NewLoggingInterceptor(log *slog.Logger) *LoggingInterceptor {
	return &LoggingInterceptor{logger: log}
}

var _ connect.Interceptor = (*LoggingInterceptor)(nil)

func (i *LoggingInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		start := time.Now()

		resp, err := next(ctx, req)

		i.log(ctx, req.Spec(), start, err)
		return resp, err
	}
}

func (i *LoggingInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (i *LoggingInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		start := time.Now()

		err := next(ctx, conn)

		i.log(ctx, conn.Spec(), start, err)
		return err
	}
}

func (i *LoggingInterceptor) log(ctx context.Context, spec connect.Spec, start time.Time, err error) {
	attrs := []any{
		"procedure", spec.Procedure,
		"code", codeLabel(err),
		"duration_ms", time.Since(start).Milliseconds(),
	}

	// A failed RPC is not automatically a server problem: an invalid argument
	// or an unauthenticated call is the client's, and logging those at error
	// level makes the error rate meaningless.
	level := slog.LevelInfo
	if err != nil {
		attrs = append(attrs, "error", err.Error())
		if isServerFault(err) {
			level = slog.LevelError
		} else {
			level = slog.LevelWarn
		}
	}

	i.logger.Log(ctx, level, "rpc", attrs...)
}

// codeLabel renders the outcome of an RPC as a metric- and log-friendly label.
func codeLabel(err error) string {
	if err == nil {
		return "ok"
	}
	return connect.CodeOf(err).String()
}

func isServerFault(err error) bool {
	switch connect.CodeOf(err) {
	case connect.CodeInternal, connect.CodeUnknown, connect.CodeDataLoss, connect.CodeUnimplemented:
		return true
	default:
		return false
	}
}
