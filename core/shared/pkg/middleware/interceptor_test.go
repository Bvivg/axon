package middleware_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/bvivg/axon/core/shared/pkg/correlation"
	"github.com/bvivg/axon/core/shared/pkg/logger"
	"github.com/bvivg/axon/core/shared/pkg/middleware"
)

// procedure is a stand-in for a generated service method. The interceptors
// under test are exercised against a real Connect handler over a real HTTP
// server rather than a hand-rolled fake, so header propagation and error
// mapping are verified the way they behave in production.
const procedure = "/axon.test.v1.EchoService/Echo"

type echoFunc func(context.Context, *connect.Request[wrapperspb.StringValue]) (*connect.Response[wrapperspb.StringValue], error)

// echo is a handler that returns its request value unchanged.
func echo(_ context.Context, req *connect.Request[wrapperspb.StringValue]) (*connect.Response[wrapperspb.StringValue], error) {
	return connect.NewResponse(wrapperspb.String(req.Msg.GetValue())), nil
}

// newClient stands up an HTTP server hosting handler behind interceptors and
// returns a Connect client pointed at it.
func newClient(t *testing.T, handler echoFunc, interceptors ...connect.Interceptor) *connect.Client[wrapperspb.StringValue, wrapperspb.StringValue] {
	t.Helper()

	h := connect.NewUnaryHandler(procedure, handler, connect.WithInterceptors(interceptors...))

	mux := http.NewServeMux()
	mux.Handle(procedure, h)

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return connect.NewClient[wrapperspb.StringValue, wrapperspb.StringValue](srv.Client(), srv.URL+procedure)
}

func TestCorrelationInterceptorAdoptsInboundHeader(t *testing.T) {
	var seen string
	client := newClient(t, func(ctx context.Context, req *connect.Request[wrapperspb.StringValue]) (*connect.Response[wrapperspb.StringValue], error) {
		seen = correlation.FromContext(ctx)
		return echo(ctx, req)
	}, middleware.NewCorrelationInterceptor())

	req := connect.NewRequest(wrapperspb.String("hi"))
	req.Header().Set(correlation.Header, "corr-inbound")

	resp, err := client.CallUnary(context.Background(), req)
	if err != nil {
		t.Fatalf("CallUnary: %v", err)
	}

	if seen != "corr-inbound" {
		t.Errorf("handler saw correlation ID %q, want corr-inbound", seen)
	}
	if got := resp.Header().Get(correlation.Header); got != "corr-inbound" {
		t.Errorf("response header = %q, want corr-inbound", got)
	}
}

func TestCorrelationInterceptorGeneratesWhenAbsent(t *testing.T) {
	var seen string
	client := newClient(t, func(ctx context.Context, req *connect.Request[wrapperspb.StringValue]) (*connect.Response[wrapperspb.StringValue], error) {
		seen = correlation.FromContext(ctx)
		return echo(ctx, req)
	}, middleware.NewCorrelationInterceptor())

	resp, err := client.CallUnary(context.Background(), connect.NewRequest(wrapperspb.String("hi")))
	if err != nil {
		t.Fatalf("CallUnary: %v", err)
	}

	if seen == "" {
		t.Fatal("handler context carries no correlation ID")
	}
	if got := resp.Header().Get(correlation.Header); got != seen {
		t.Fatalf("response header = %q, want the handler's ID %q", got, seen)
	}
}

// On the client side the interceptor's job is the mirror image: take the ID off
// the context and put it on the wire, so the next service sees the same ID.
func TestCorrelationInterceptorPropagatesFromClientContext(t *testing.T) {
	var seen string
	h := connect.NewUnaryHandler(procedure,
		func(_ context.Context, req *connect.Request[wrapperspb.StringValue]) (*connect.Response[wrapperspb.StringValue], error) {
			seen = req.Header().Get(correlation.Header)
			return connect.NewResponse(wrapperspb.String(req.Msg.GetValue())), nil
		})

	mux := http.NewServeMux()
	mux.Handle(procedure, h)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := connect.NewClient[wrapperspb.StringValue, wrapperspb.StringValue](
		srv.Client(), srv.URL+procedure,
		connect.WithInterceptors(middleware.NewCorrelationInterceptor()),
	)

	ctx := correlation.WithID(context.Background(), "corr-outbound")
	if _, err := client.CallUnary(ctx, connect.NewRequest(wrapperspb.String("hi"))); err != nil {
		t.Fatalf("CallUnary: %v", err)
	}

	if seen != "corr-outbound" {
		t.Fatalf("downstream saw %q, want corr-outbound", seen)
	}
}

func TestRecoveryInterceptorMapsPanicToInternal(t *testing.T) {
	var buf bytes.Buffer
	log := logger.New(logger.Options{Service: "test", Output: &buf})

	client := newClient(t, func(context.Context, *connect.Request[wrapperspb.StringValue]) (*connect.Response[wrapperspb.StringValue], error) {
		panic("handler exploded")
	}, middleware.NewRecoveryInterceptor(log))

	_, err := client.CallUnary(context.Background(), connect.NewRequest(wrapperspb.String("hi")))
	if err == nil {
		t.Fatal("CallUnary succeeded, want an internal error")
	}
	if code := connect.CodeOf(err); code != connect.CodeInternal {
		t.Errorf("code = %v, want internal", code)
	}
	// The panic value is a server internal; it belongs in the log, not the wire.
	if strings.Contains(err.Error(), "handler exploded") {
		t.Errorf("error sent to the client leaked the panic value: %v", err)
	}
	if !strings.Contains(buf.String(), "handler exploded") {
		t.Errorf("panic value not logged: %s", buf.String())
	}
	if !strings.Contains(buf.String(), procedure) {
		t.Errorf("log line does not name the procedure: %s", buf.String())
	}
}

func TestLoggingInterceptorRecordsSuccess(t *testing.T) {
	var buf bytes.Buffer
	log := logger.New(logger.Options{Service: "test", Output: &buf})

	client := newClient(t, echo, middleware.NewCorrelationInterceptor(), middleware.NewLoggingInterceptor(log))

	req := connect.NewRequest(wrapperspb.String("hi"))
	req.Header().Set(correlation.Header, "corr-log")

	if _, err := client.CallUnary(context.Background(), req); err != nil {
		t.Fatalf("CallUnary: %v", err)
	}

	line := buf.String()
	for _, want := range []string{`"code":"ok"`, `"correlation_id":"corr-log"`, `"duration_ms"`, procedure} {
		if !strings.Contains(line, want) {
			t.Errorf("log line missing %s: %s", want, line)
		}
	}
}

// A client's mistake is not the server's fault, and must not inflate the error
// rate the way a genuine internal failure does.
func TestLoggingInterceptorSeparatesClientAndServerFaults(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		wantLevel string
	}{
		{
			name:      "invalid argument is the client's fault",
			err:       connect.NewError(connect.CodeInvalidArgument, errors.New("email is malformed")),
			wantLevel: `"level":"WARN"`,
		},
		{
			name:      "internal is the server's fault",
			err:       connect.NewError(connect.CodeInternal, errors.New("query failed")),
			wantLevel: `"level":"ERROR"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			log := logger.New(logger.Options{Service: "test", Output: &buf})

			client := newClient(t, func(context.Context, *connect.Request[wrapperspb.StringValue]) (*connect.Response[wrapperspb.StringValue], error) {
				return nil, tt.err
			}, middleware.NewLoggingInterceptor(log))

			if _, err := client.CallUnary(context.Background(), connect.NewRequest(wrapperspb.String("hi"))); err == nil {
				t.Fatal("CallUnary succeeded, want an error")
			}

			if !strings.Contains(buf.String(), tt.wantLevel) {
				t.Fatalf("log level is not %s: %s", tt.wantLevel, buf.String())
			}
		})
	}
}

func TestMetricsInterceptorRecordsRPCs(t *testing.T) {
	metrics := middleware.NewMetrics("auth")

	client := newClient(t, echo, metrics.Interceptor())
	if _, err := client.CallUnary(context.Background(), connect.NewRequest(wrapperspb.String("hi"))); err != nil {
		t.Fatalf("CallUnary: %v", err)
	}

	body := scrapeMetrics(t, metrics)

	for _, want := range []string{
		`rpc_requests_total{code="ok",procedure="` + procedure + `",service="auth"} 1`,
		`rpc_duration_seconds_count{code="ok",procedure="` + procedure + `",service="auth"} 1`,
		`rpc_in_flight{procedure="` + procedure + `",service="auth"} 0`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics output missing %q", want)
		}
	}
}

func TestMetricsInterceptorLabelsErrorCode(t *testing.T) {
	metrics := middleware.NewMetrics("auth")

	client := newClient(t, func(context.Context, *connect.Request[wrapperspb.StringValue]) (*connect.Response[wrapperspb.StringValue], error) {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("no token"))
	}, metrics.Interceptor())

	if _, err := client.CallUnary(context.Background(), connect.NewRequest(wrapperspb.String("hi"))); err == nil {
		t.Fatal("CallUnary succeeded, want an error")
	}

	body := scrapeMetrics(t, metrics)
	want := `rpc_requests_total{code="unauthenticated",procedure="` + procedure + `",service="auth"} 1`
	if !strings.Contains(body, want) {
		t.Errorf("metrics output missing %q, got:\n%s", want, body)
	}
}

// Services register their own domain collectors on the same registry.
func TestMetricsRegistryIsUsableByServices(t *testing.T) {
	metrics := middleware.NewMetrics("game")

	if metrics.Registry() == nil {
		t.Fatal("Registry() = nil")
	}
	if body := scrapeMetrics(t, metrics); !strings.Contains(body, "go_goroutines") {
		t.Error("runtime collectors are not registered")
	}
}

func scrapeMetrics(t *testing.T, m *middleware.Metrics) string {
	t.Helper()

	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("metrics handler status = %d, want 200", rec.Code)
	}
	return rec.Body.String()
}
