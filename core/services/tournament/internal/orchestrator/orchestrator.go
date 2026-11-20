package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/bvivg/axon/core/services/tournament/internal/saga"
	"github.com/bvivg/axon/core/services/tournament/internal/store"
)

const (
	StepSeed    = "seed"
	StepAdvance = "advance"
	StepRecord  = "record"
	StepForfeit = "forfeit"
	StepCancel  = "cancel"

	ResultApplied   = "applied"
	ResultDuplicate = "duplicate"
	ResultSkipped   = "skipped"
	ResultInvalid   = "invalid"

	DefaultNoShow = 2 * time.Minute
	dueBatch      = 100
	maxSteps      = 4
)

var (
	ErrAdminOnly = errors.New("orchestrator: only an admin can cancel a tournament that has started")
	idNamespace  = uuid.MustParse("9e3b52c7-6d1a-4f08-b7e4-2c9a1f5d8e63")
)

func TournamentID(creator, request uuid.UUID) uuid.UUID {
	return uuid.NewSHA1(idNamespace, append(creator[:len(creator):len(creator)], request[:]...))
}

type Metrics struct {
	Active   prometheus.Gauge
	Rounds   prometheus.Counter
	Forfeits prometheus.Counter
	Steps    *prometheus.HistogramVec
	Events   *prometheus.CounterVec
}

func NewMetrics(registry prometheus.Registerer) *Metrics {
	m := &Metrics{
		Active: prometheus.NewGauge(prometheus.GaugeOpts{Name: "tournament_active", Help: "Tournaments that are seeding or playing a round."}),
		Rounds: prometheus.NewCounter(prometheus.CounterOpts{Name: "tournament_rounds_total", Help: "Tournament rounds started."}),
		Forfeits: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "tournament_forfeits_total", Help: "Forfeit commands sent to game for players who did not show up.",
		}),
		Steps: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "tournament_saga_step_seconds",
			Help:    "Time from the event that triggers a saga step to the commit of that step.",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
		}, []string{"step"}),
		Events: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "tournament_events_total", Help: "game.finished events seen by the tournament consumer, by result.",
		}, []string{"result"}),
	}
	registry.MustRegister(m.Active, m.Rounds, m.Forfeits, m.Steps, m.Events)
	return m
}

type Config struct {
	Store   *store.Postgres
	NoShow  time.Duration
	Now     func() time.Time
	Metrics *Metrics
	Log     *slog.Logger
}

type Orchestrator struct {
	store   *store.Postgres
	noShow  time.Duration
	now     func() time.Time
	metrics *Metrics
	log     *slog.Logger
}

func New(cfg Config) *Orchestrator {
	if cfg.NoShow <= 0 {
		cfg.NoShow = DefaultNoShow
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Metrics == nil {
		cfg.Metrics = NewMetrics(prometheus.NewRegistry())
	}
	clock := cfg.Now
	return &Orchestrator{store: cfg.Store, noShow: cfg.NoShow, now: func() time.Time { return clock().UTC().Truncate(time.Microsecond) },
		metrics: cfg.Metrics, log: cfg.Log.With("component", "orchestrator")}
}

func (o *Orchestrator) NoShow() time.Duration { return o.noShow }

func (o *Orchestrator) observe(step string, since time.Time) {
	o.metrics.Steps.WithLabelValues(step).Observe(max(0, o.now().Sub(since).Seconds()))
}

func (o *Orchestrator) Create(ctx context.Context, creator, request uuid.UUID, spec saga.Spec) (saga.Tournament, error) {
	t, err := saga.Create(TournamentID(creator, request), creator, spec, o.now())
	if err != nil {
		return saga.Tournament{}, err
	}
	stored, created, err := o.store.Create(ctx, t, request)
	if err != nil {
		return saga.Tournament{}, err
	}
	if created {
		o.log.InfoContext(ctx, "tournament created", "tournament_id", stored.ID, "format", stored.Spec.Format, "game", stored.Spec.Game, "capacity", stored.Spec.Capacity)
	}
	return stored, nil
}

func (o *Orchestrator) Get(ctx context.Context, id uuid.UUID) (saga.Tournament, error) {
	return o.store.Get(ctx, id)
}

func (o *Orchestrator) List(ctx context.Context, status saga.Status, limit int) ([]store.Summary, error) {
	return o.store.List(ctx, status, limit)
}

func (o *Orchestrator) Register(ctx context.Context, id, user uuid.UUID) (saga.Tournament, error) {
	current, err := o.store.Get(ctx, id)
	if err != nil {
		return saga.Tournament{}, err
	}
	rating, err := o.store.Rating(ctx, current.Spec.Game, user)
	if err != nil {
		return saga.Tournament{}, err
	}
	return o.store.Update(ctx, id, func(_ context.Context, _ *store.Tx, t saga.Tournament) (saga.Tournament, error) {
		next, _, err := t.Register(user, rating, o.now())
		return next, err
	})
}

func (o *Orchestrator) Withdraw(ctx context.Context, id, user uuid.UUID) (saga.Tournament, error) {
	return o.store.Update(ctx, id, func(_ context.Context, _ *store.Tx, t saga.Tournament) (saga.Tournament, error) {
		next, _, err := t.Withdraw(user)
		return next, err
	})
}

func (o *Orchestrator) Start(ctx context.Context, id uuid.UUID) (saga.Tournament, error) {
	started := false
	if _, err := o.store.Update(ctx, id, func(_ context.Context, _ *store.Tx, t saga.Tournament) (saga.Tournament, error) {
		next, changed, err := t.Start(o.now())
		started = changed
		return next, err
	}); err != nil {
		return saga.Tournament{}, err
	}
	if started {
		o.log.InfoContext(ctx, "tournament seeding", "tournament_id", id)
	}
	return o.Step(ctx, id)
}

func (o *Orchestrator) Cancel(ctx context.Context, id uuid.UUID, admin bool) (saga.Tournament, error) {
	began := o.now()
	var cancellation saga.Cancellation
	t, err := o.store.Update(ctx, id, func(ctx context.Context, tx *store.Tx, t saga.Tournament) (saga.Tournament, error) {
		if t.Started() && !admin && !t.Over() {
			return t, ErrAdminOnly
		}
		next, effect, err := t.Cancel(o.now())
		if err != nil {
			return t, err
		}
		cancellation, _ = effect.(saga.Cancellation)
		return next, o.publish(ctx, tx, next, effect)
	})
	if err != nil {
		return saga.Tournament{}, err
	}
	o.observe(StepCancel, began)
	o.log.InfoContext(ctx, "tournament cancelled", "tournament_id", id, "started", cancellation.Started,
		"aborted", len(cancellation.Sessions), "returned", len(cancellation.Returned))
	return t, nil
}

func (o *Orchestrator) publish(ctx context.Context, tx *store.Tx, t saga.Tournament, effects ...saga.Effect) error {
	for _, effect := range effects {
		event, ok := Announce(t.ID, effect)
		if !ok {
			return fmt.Errorf("orchestrator: no event for %T", effect)
		}
		if err := tx.Publish(ctx, event); err != nil {
			return err
		}
		if round, ok := effect.(saga.RoundStarted); ok {
			sessions := make([]uuid.UUID, 0, len(round.Matches))
			for _, m := range round.Matches {
				sessions = append(sessions, m.Session)
			}
			if err := tx.Schedule(ctx, t.ID, sessions, round.StartedAt.Add(o.noShow)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (o *Orchestrator) Step(ctx context.Context, id uuid.UUID) (saga.Tournament, error) {
	var t saga.Tournament
	for range maxSteps {
		var effects []saga.Effect
		step, trigger := "", time.Time{}
		next, err := o.store.Update(ctx, id, func(ctx context.Context, tx *store.Tx, current saga.Tournament) (saga.Tournament, error) {
			switch {
			case current.Status == saga.Seeding && current.StartedAt != nil:
				step, trigger = StepSeed, *current.StartedAt
			case current.Status == saga.Running && current.RoundComplete():
				step = StepAdvance
				if round, ok := current.CurrentRound(); ok && round.FinishedAt != nil {
					trigger = *round.FinishedAt
				}
			}
			advanced, produced, err := current.Advance(o.now())
			if err != nil {
				return current, err
			}
			effects = produced
			return advanced, o.publish(ctx, tx, advanced, produced...)
		})
		if err != nil {
			return saga.Tournament{}, fmt.Errorf("orchestrator: step %s: %w", id, err)
		}
		t = next
		if len(effects) == 0 {
			return t, nil
		}
		if !trigger.IsZero() {
			o.observe(step, trigger)
		}
		for _, effect := range effects {
			switch e := effect.(type) {
			case saga.RoundStarted:
				o.metrics.Rounds.Inc()
				o.log.InfoContext(ctx, "tournament round started", "tournament_id", id, "round", e.Round, "games", len(e.Matches))
			case saga.Completion:
				o.log.InfoContext(ctx, "tournament finished", "tournament_id", id, "winner_id", e.Winner, "rounds", t.Round)
			}
		}
	}
	return t, nil
}

type Result struct {
	EventID    uuid.UUID
	Tournament uuid.UUID
	Session    uuid.UUID
	Players    [2]uuid.UUID
	Winner     int
	Plies      int
	FinishedAt time.Time
}

func (o *Orchestrator) Record(ctx context.Context, r Result) (string, error) {
	result := ResultApplied
	_, err := o.store.Update(ctx, r.Tournament, func(ctx context.Context, tx *store.Tx, t saga.Tournament) (saga.Tournament, error) {
		claimed, err := tx.Claim(ctx, r.EventID, r.Session)
		if err != nil {
			return t, err
		}
		if !claimed {
			result = ResultDuplicate
			return t, nil
		}
		next, changed, err := t.Record(r.Session, saga.Outcome{Players: r.Players, Winner: r.Winner, Plies: r.Plies, At: r.FinishedAt})
		switch {
		case errors.Is(err, saga.ErrUnknownMatch), errors.Is(err, saga.ErrMismatch):
			result = ResultInvalid
			o.log.WarnContext(ctx, "game result does not match the tournament", "tournament_id", t.ID, "session_id", r.Session, "error", err)
			return t, nil
		case errors.Is(err, saga.ErrNotRunning):
			result = ResultSkipped
			return t, nil
		case err != nil:
			return t, err
		case !changed:
			result = ResultDuplicate
		}
		return next, nil
	})
	if errors.Is(err, store.ErrNotFound) {
		o.metrics.Events.WithLabelValues(ResultSkipped).Inc()
		return ResultSkipped, nil
	}
	if err != nil {
		return "", fmt.Errorf("orchestrator: record %s: %w", r.Session, err)
	}
	o.metrics.Events.WithLabelValues(result).Inc()
	if result == ResultApplied {
		o.observe(StepRecord, r.FinishedAt)
		o.log.InfoContext(ctx, "tournament game recorded", "tournament_id", r.Tournament, "session_id", r.Session, "winner_seat", r.Winner, "plies", r.Plies)
	}
	if _, err := o.Step(ctx, r.Tournament); err != nil {
		return result, err
	}
	return result, nil
}

func (o *Orchestrator) fire(ctx context.Context, deadline store.Deadline) error {
	issued := false
	_, err := o.store.Update(ctx, deadline.Tournament, func(ctx context.Context, tx *store.Tx, t saga.Tournament) (saga.Tournament, error) {
		live, ok, err := tx.Deadline(ctx, deadline.Session)
		if err != nil || !ok {
			return t, err
		}
		if effect, ok := t.Forfeit(deadline.Session, live.Due); ok {
			if err := o.publish(ctx, tx, t, effect); err != nil {
				return t, err
			}
			issued = true
		}
		return t, tx.Fire(ctx, deadline.Session, issued, o.now())
	})
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("orchestrator: deadline of %s: %w", deadline.Session, err)
	}
	if issued {
		o.metrics.Forfeits.Inc()
		o.observe(StepForfeit, deadline.Due)
		o.log.InfoContext(ctx, "no-show forfeit sent", "tournament_id", deadline.Tournament, "session_id", deadline.Session, "due_at", deadline.Due)
	}
	return nil
}

func (o *Orchestrator) Sweep(ctx context.Context) error {
	var failures []error
	due, err := o.store.Due(ctx, o.now(), dueBatch)
	if err != nil {
		return fmt.Errorf("orchestrator: due deadlines: %w", err)
	}
	for _, deadline := range due {
		if err := o.fire(ctx, deadline); err != nil {
			failures = append(failures, err)
		}
	}
	stalled, err := o.store.Stalled(ctx)
	if err != nil {
		return errors.Join(append(failures, fmt.Errorf("orchestrator: stalled tournaments: %w", err))...)
	}
	for _, id := range stalled {
		if _, err := o.Step(ctx, id); err != nil {
			failures = append(failures, err)
		}
	}
	if active, err := o.store.Active(ctx); err == nil {
		o.metrics.Active.Set(float64(active))
	} else {
		failures = append(failures, err)
	}
	return errors.Join(failures...)
}

func (o *Orchestrator) Run(ctx context.Context, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		if err := o.Sweep(ctx); err != nil && ctx.Err() == nil {
			o.log.WarnContext(ctx, "tournament sweep failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
