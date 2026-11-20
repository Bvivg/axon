package lifecycle

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"
)

type Drainer interface {
	Drain()
}

type Graceful struct {
	Drain   time.Duration
	Timeout time.Duration
	Health  Drainer
	Log     *slog.Logger
	streams context.Context
	close   context.CancelFunc
	sleep   func(time.Duration)
}

func New(drain, timeout time.Duration, health Drainer, log *slog.Logger) *Graceful {
	streams, closeStreams := context.WithCancel(context.Background())
	return &Graceful{Drain: drain, Timeout: timeout, Health: health, Log: log, streams: streams, close: closeStreams, sleep: time.Sleep}
}

func DrainFromEnv() time.Duration {
	raw := os.Getenv("SHUTDOWN_DRAIN")
	if raw == "" {
		return 0
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d < 0 {
		return 0
	}
	return d
}

func (g *Graceful) Serve(srv *http.Server) *http.Server {
	srv.BaseContext = func(net.Listener) context.Context { return g.streams }
	return srv
}

func (g *Graceful) Stop(servers ...*http.Server) error {
	if g.Health != nil {
		g.Health.Drain()
	}
	if g.Drain > 0 {
		if g.Log != nil {
			g.Log.Info("draining before shutdown", "drain", g.Drain)
		}
		g.sleep(g.Drain)
	}
	ctx, cancel := context.WithTimeout(context.Background(), g.Timeout)
	defer cancel()
	var errs []error
	for _, srv := range servers {
		if err := srv.Shutdown(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	g.close()
	return errors.Join(errs...)
}
