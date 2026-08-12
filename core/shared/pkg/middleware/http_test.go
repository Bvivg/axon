package middleware_test

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bvivg/axon/core/shared/pkg/correlation"
	"github.com/bvivg/axon/core/shared/pkg/logger"
	"github.com/bvivg/axon/core/shared/pkg/middleware"
)

func TestCorrelationGeneratesID(t *testing.T) {
	var seen string
	h := middleware.Correlation(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = correlation.FromContext(r.Context())
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if seen == "" {
		t.Fatal("handler context carries no correlation ID")
	}
	if got := rec.Header().Get(correlation.Header); got != seen {
		t.Fatalf("response header = %q, want the context ID %q", got, seen)
	}
}

func TestCorrelationReusesInboundID(t *testing.T) {
	var seen string
	h := middleware.Correlation(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = correlation.FromContext(r.Context())
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(correlation.Header, "inbound-id")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if seen != "inbound-id" {
		t.Fatalf("context ID = %q, want the inbound ID", seen)
	}
	if got := rec.Header().Get(correlation.Header); got != "inbound-id" {
		t.Fatalf("response header = %q, want the inbound ID", got)
	}
}

func TestRecoveryReturns500AndLogsStack(t *testing.T) {
	var buf bytes.Buffer
	log := logger.New(logger.Options{Service: "test", Output: &buf})

	h := middleware.Recovery(log)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/rooms", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}

	if strings.Contains(rec.Body.String(), "boom") {
		t.Errorf("response body leaked the panic value: %s", rec.Body.String())
	}
	if !strings.Contains(buf.String(), "boom") {
		t.Errorf("log line does not contain the panic value: %s", buf.String())
	}
	if !strings.Contains(buf.String(), "stack") {
		t.Errorf("log line has no stack trace: %s", buf.String())
	}
}

func TestRecoveryPropagatesErrAbortHandler(t *testing.T) {
	h := middleware.Recovery(logger.Discard())(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))

	defer func() {
		err, ok := recover().(error)
		if !ok || !errors.Is(err, http.ErrAbortHandler) {
			t.Fatalf("recovered %v, want ErrAbortHandler to propagate", err)
		}
	}()

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}

func TestRecoveryPassesThroughNormalRequests(t *testing.T) {
	h := middleware.Recovery(logger.Discard())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
}

func TestRequestLoggerRecordsOutcome(t *testing.T) {
	var buf bytes.Buffer
	log := logger.New(logger.Options{Service: "test", Output: &buf})

	h := middleware.RequestLogger(log)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/rooms/42", nil))

	line := buf.String()
	for _, want := range []string{`"status":418`, `"method":"GET"`, `"path":"/rooms/42"`, `"duration_ms"`} {
		if !strings.Contains(line, want) {
			t.Errorf("log line missing %s: %s", want, line)
		}
	}
}

func TestRequestLoggerDefaultsTo200(t *testing.T) {
	var buf bytes.Buffer
	log := logger.New(logger.Options{Service: "test", Output: &buf})

	h := middleware.RequestLogger(log)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if !strings.Contains(buf.String(), `"status":200`) {
		t.Fatalf("log line does not report status 200: %s", buf.String())
	}
}

func TestRequestLoggerLogsServerErrorsAtErrorLevel(t *testing.T) {
	var buf bytes.Buffer
	log := logger.New(logger.Options{Service: "test", Output: &buf})

	h := middleware.RequestLogger(log)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if !strings.Contains(buf.String(), `"level":"ERROR"`) {
		t.Fatalf("5xx not logged at error level: %s", buf.String())
	}
}

func TestChainOrderAndCorrelationInLogs(t *testing.T) {
	var buf bytes.Buffer
	log := logger.New(logger.Options{Service: "test", Level: slog.LevelInfo, Output: &buf})

	var order []string
	mark := func(name string) middleware.Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, name)
				next.ServeHTTP(w, r)
			})
		}
	}

	h := middleware.Chain(
		mark("first"),
		middleware.Correlation,
		middleware.RequestLogger(log),
		mark("second"),
	)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(correlation.Header, "corr-99")
	h.ServeHTTP(httptest.NewRecorder(), req)

	if len(order) != 2 || order[0] != "first" || order[1] != "second" {
		t.Fatalf("middleware ran in order %v, want [first second]", order)
	}
	if !strings.Contains(buf.String(), `"correlation_id":"corr-99"`) {
		t.Fatalf("request log line has no correlation ID: %s", buf.String())
	}
}
