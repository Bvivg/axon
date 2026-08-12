package middleware

import (
	"context"
	"net/http"
	"time"

	"connectrpc.com/connect"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Metrics struct {
	registry *prometheus.Registry

	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
	inFlight *prometheus.GaugeVec
}

func NewMetrics(service string) *Metrics {
	registry := prometheus.NewRegistry()
	labels := prometheus.Labels{"service": service}

	m := &Metrics{
		registry: registry,
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name:        "rpc_requests_total",
			Help:        "Total RPCs handled, by procedure and outcome code.",
			ConstLabels: labels,
		}, []string{"procedure", "code"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "rpc_duration_seconds",
			Help: "RPC handling latency in seconds, by procedure and outcome code.",

			Buckets:     []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
			ConstLabels: labels,
		}, []string{"procedure", "code"}),
		inFlight: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name:        "rpc_in_flight",
			Help:        "RPCs currently being handled, by procedure.",
			ConstLabels: labels,
		}, []string{"procedure"}),
	}

	registry.MustRegister(
		m.requests,
		m.duration,
		m.inFlight,
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	return m
}

func (m *Metrics) Registry() *prometheus.Registry {
	return m.registry
}

func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

func (m *Metrics) Interceptor() connect.Interceptor {
	return &metricsInterceptor{metrics: m}
}

func (m *Metrics) observe(procedure string, start time.Time, err error) {
	code := codeLabel(err)
	m.requests.WithLabelValues(procedure, code).Inc()
	m.duration.WithLabelValues(procedure, code).Observe(time.Since(start).Seconds())
}

type metricsInterceptor struct {
	metrics *Metrics
}

var _ connect.Interceptor = (*metricsInterceptor)(nil)

func (i *metricsInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {

		if req.Spec().IsClient {
			return next(ctx, req)
		}

		procedure := req.Spec().Procedure
		start := time.Now()

		i.metrics.inFlight.WithLabelValues(procedure).Inc()
		defer i.metrics.inFlight.WithLabelValues(procedure).Dec()

		resp, err := next(ctx, req)

		i.metrics.observe(procedure, start, err)
		return resp, err
	}
}

func (i *metricsInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (i *metricsInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		procedure := conn.Spec().Procedure
		start := time.Now()

		i.metrics.inFlight.WithLabelValues(procedure).Inc()
		defer i.metrics.inFlight.WithLabelValues(procedure).Dec()

		err := next(ctx, conn)

		i.metrics.observe(procedure, start, err)
		return err
	}
}
