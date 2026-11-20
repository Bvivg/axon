package lifecycle

import (
	"context"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

type drainer struct{ at atomic.Int64 }

func (d *drainer) Drain() { d.at.Store(time.Now().UnixNano()) }

func TestStopDrainsThenFinishesRequestsThenClosesStreams(t *testing.T) {
	health := &drainer{}
	g := New(50*time.Millisecond, time.Second, health, nil)
	var slept time.Duration
	g.sleep = func(d time.Duration) { slept = d }
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	inflight := make(chan struct{})
	finished := atomic.Bool{}
	streamClosed := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		close(inflight)
		time.Sleep(100 * time.Millisecond)
		finished.Store(true)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/stream", func(w http.ResponseWriter, r *http.Request) {
		conn, _, err := http.NewResponseController(w).Hijack()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		<-r.Context().Done()
		close(streamClosed)
	})
	srv := g.Serve(&http.Server{Handler: mux, ReadHeaderTimeout: time.Second})
	go func() { _ = srv.Serve(listener) }()
	base := "http://" + listener.Addr().String()
	stream, err := (&net.Dialer{}).DialContext(context.Background(), "tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stream.Close() }()
	if _, err := stream.Write([]byte("GET /stream HTTP/1.1\r\nHost: x\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	go func() {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, base+"/slow", nil)
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			_ = resp.Body.Close()
		}
	}()
	<-inflight
	if err := g.Stop(srv); err != nil {
		t.Fatal(err)
	}
	if health.at.Load() == 0 || slept != 50*time.Millisecond {
		t.Fatal("did not drain before shutdown", slept)
	}
	if !finished.Load() {
		t.Fatal("in-flight request was cut")
	}
	select {
	case <-streamClosed:
	case <-time.After(time.Second):
		t.Fatal("hijacked stream was not told to close")
	}
	if g.streams.Err() != context.Canceled {
		t.Fatal("streams context still open")
	}
}

func TestDrainFromEnv(t *testing.T) {
	for _, tt := range []struct {
		raw  string
		want time.Duration
	}{{"", 0}, {"5s", 5 * time.Second}, {"bad", 0}, {"-1s", 0}} {
		t.Setenv("SHUTDOWN_DRAIN", tt.raw)
		if got := DrainFromEnv(); got != tt.want {
			t.Errorf("%q: got %v, want %v", tt.raw, got, tt.want)
		}
	}
}
