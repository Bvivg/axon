package metrics

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/bvivg/axon/core/shared/pkg/middleware"
)

type Game struct {
	commands       *prometheus.CounterVec
	commandLatency *prometheus.HistogramVec
	moves          *prometheus.CounterVec
	appendLatency  prometheus.Histogram
	owned          prometheus.Gauge
	claims         prometheus.Counter
	claimLatency   prometheus.Histogram
	revocations    *prometheus.CounterVec
	renewals       *prometheus.CounterVec
	active         *prometheus.GaugeVec
	resident       prometheus.Gauge
	projected      prometheus.Counter
	batchSize      prometheus.Histogram
	applyLatency   prometheus.Histogram
	lagRecords     *prometheus.GaugeVec
	lagSeconds     prometheus.Gauge
	spectators     prometheus.Gauge
}

const (
	RevokedByRebalance = "rebalance"
	RevokedLeaseLost   = "lease_lost"
	RevokedLeaseExpiry = "lease_expired"
)

var latency = []float64{.001, .0025, .005, .01, .025, .05, .1, .25, .5, 1, 2.5}

func New(registry prometheus.Registerer) *Game {
	m := &Game{
		commands: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "game_commands_total", Help: "Socket commands handled by the owner, by command and outcome."}, []string{"command", "outcome"}),
		commandLatency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "game_command_duration_seconds", Help: "Time from reading a socket command to its journal acknowledgement.", Buckets: latency,
		}, []string{"command"}),
		moves:         prometheus.NewCounterVec(prometheus.CounterOpts{Name: "game_moves_total", Help: "Moves recorded in the journal, by game."}, []string{"game"}),
		appendLatency: prometheus.NewHistogram(prometheus.HistogramOpts{Name: "game_journal_append_duration_seconds", Help: "Journal append latency.", Buckets: latency}),
		owned:         prometheus.NewGauge(prometheus.GaugeOpts{Name: "game_partitions_owned", Help: "Journal partitions this instance serves."}),
		claims:        prometheus.NewCounter(prometheus.CounterOpts{Name: "game_partition_claims_total", Help: "Partitions claimed by this instance."}),
		claimLatency: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name: "game_partition_claim_duration_seconds", Help: "Time to claim a partition, including lease wait and catch-up.", Buckets: []float64{.01, .05, .1, .25, .5, 1, 2.5, 5, 10, 30},
		}),
		revocations: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "game_partition_revocations_total", Help: "Partitions given up, by reason."}, []string{"reason"}),
		renewals:    prometheus.NewCounterVec(prometheus.CounterOpts{Name: "game_lease_renewals_total", Help: "Lease renewal rounds, by outcome."}, []string{"outcome"}),
		active:      prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "game_active_sessions", Help: "In-progress games held in memory by this owner, by game."}, []string{"game"}),
		resident:    prometheus.NewGauge(prometheus.GaugeOpts{Name: "game_resident_sessions", Help: "Sessions held in memory by this owner, including finished ones not yet evicted."}),
		projected:   prometheus.NewCounter(prometheus.CounterOpts{Name: "game_projector_records_total", Help: "Journal records projected into the read model."}),
		batchSize: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name: "game_projector_batch_size", Help: "Records per projector transaction.", Buckets: []float64{1, 5, 10, 50, 100, 500, 1000, 2500, 5000},
		}),
		applyLatency: prometheus.NewHistogram(prometheus.HistogramOpts{Name: "game_projector_apply_duration_seconds", Help: "Projector transaction latency.", Buckets: latency}),
		lagRecords:   prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "game_projector_lag_records", Help: "Journal records not yet projected, by partition."}, []string{"partition"}),
		lagSeconds:   prometheus.NewGauge(prometheus.GaugeOpts{Name: "game_projector_lag_seconds", Help: "Age of the newest projected record when the projector is behind, zero when caught up."}),
		spectators:   prometheus.NewGauge(prometheus.GaugeOpts{Name: "game_spectators", Help: "Spectator sockets watching games served by this owner."}),
	}
	registry.MustRegister(m.commands, m.commandLatency, m.moves, m.appendLatency, m.owned, m.claims, m.claimLatency,
		m.revocations, m.renewals, m.active, m.resident, m.projected, m.batchSize, m.applyLatency, m.lagRecords, m.lagSeconds, m.spectators)
	return m
}

func (m *Game) Command(ctx context.Context, command string, took time.Duration, err error) {
	if m == nil {
		return
	}
	outcome := "ok"
	if err != nil {
		outcome = "error"
	}
	m.commands.WithLabelValues(command, outcome).Inc()
	middleware.Observe(ctx, m.commandLatency.WithLabelValues(command), took.Seconds())
}

func (m *Game) Moved(game string) {
	if m != nil {
		m.moves.WithLabelValues(game).Inc()
	}
}

func (m *Game) Appended(ctx context.Context, took time.Duration) {
	if m != nil {
		middleware.Observe(ctx, m.appendLatency, took.Seconds())
	}
}

func (m *Game) Owned(partitions int) {
	if m != nil {
		m.owned.Set(float64(partitions))
	}
}

func (m *Game) Claimed(took time.Duration) {
	if m != nil {
		m.claims.Inc()
		m.claimLatency.Observe(took.Seconds())
	}
}

func (m *Game) Revoked(reason string) {
	if m != nil {
		m.revocations.WithLabelValues(reason).Inc()
	}
}

func (m *Game) Renewed(err error) {
	if m == nil {
		return
	}
	outcome := "ok"
	if err != nil {
		outcome = "error"
	}
	m.renewals.WithLabelValues(outcome).Inc()
}

func (m *Game) Active(byGame map[string]int) {
	if m == nil {
		return
	}
	m.active.Reset()
	for game, count := range byGame {
		m.active.WithLabelValues(game).Set(float64(count))
	}
}

func (m *Game) Resident(sessions int) {
	if m != nil {
		m.resident.Set(float64(sessions))
	}
}

func (m *Game) Projected(ctx context.Context, records int, took time.Duration) {
	if m == nil {
		return
	}
	m.projected.Add(float64(records))
	m.batchSize.Observe(float64(records))
	middleware.Observe(ctx, m.applyLatency, took.Seconds())
}

func (m *Game) Lag(partition string, records int64) {
	if m != nil {
		m.lagRecords.WithLabelValues(partition).Set(float64(max(records, 0)))
	}
}

func (m *Game) LagAge(age time.Duration) {
	if m != nil {
		m.lagSeconds.Set(max(age.Seconds(), 0))
	}
}

func (m *Game) Spectators(watching int) {
	if m != nil {
		m.spectators.Set(float64(watching))
	}
}
