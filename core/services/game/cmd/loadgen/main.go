package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/bvivg/axon/core/services/game/internal/games/chess"
	"github.com/bvivg/axon/core/services/game/internal/games/quoridor"
	"github.com/bvivg/axon/core/services/game/internal/games/tictactoe"
	"github.com/bvivg/axon/core/services/game/internal/server"
	gamev1 "github.com/bvivg/axon/core/shared/gen/go/axon/game/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/game/v1/gamev1connect"
	"github.com/bvivg/axon/core/shared/pkg/ws"
)

type config struct {
	target      string
	users       string
	legends     string
	out         string
	matches     int
	concurrency int
	pace        time.Duration
	timeout     time.Duration
	seed        uint64
	redials     int
	players     int
}

type stats struct {
	mu        sync.Mutex
	latencies []time.Duration
	byGame    map[string]*gameStats
	errors    map[string]int
	moves     atomic.Int64
	sent      atomic.Int64
	done      atomic.Int64
	failed    atomic.Int64
	redials   atomic.Int64
}

type gameStats struct {
	Matches  int   `json:"matches"`
	Moves    int   `json:"moves"`
	Failed   int   `json:"failed"`
	Duration int64 `json:"total_duration_ms"`
}

type result struct {
	Session string `json:"session"`
	Game    string `json:"game"`
	Moves   int    `json:"moves"`
	Status  string `json:"status"`
	Error   string `json:"error,omitempty"`
}

func main() {
	var cfg config
	flag.StringVar(&cfg.target, "target", "http://game:8083", "game service base URL")
	flag.StringVar(&cfg.users, "users", "users.json", "JSON array of user ids")
	flag.StringVar(&cfg.legends, "legends", "", "chess games with uci moves (default: the bundled legends.json)")
	flag.StringVar(&cfg.out, "out", "results.json", "where to write per-match results")
	flag.IntVar(&cfg.matches, "matches", 50000, "matches to play")
	flag.IntVar(&cfg.concurrency, "concurrency", 1000, "matches in flight")
	flag.DurationVar(&cfg.pace, "pace", 260*time.Millisecond, "minimum gap between commands on one socket")
	flag.DurationVar(&cfg.timeout, "timeout", 3*time.Minute, "per-match timeout")
	flag.Uint64Var(&cfg.seed, "seed", 1, "random seed")
	flag.IntVar(&cfg.redials, "redials", 20, "socket reconnects allowed per player and match")
	flag.IntVar(&cfg.players, "players", 0, "generate this many user ids from the seed instead of reading -users")
	flag.Parse()
	if err := run(cfg); err != nil {
		slog.Error("loadgen failed", "error", err)
		os.Exit(1)
	}
}

func run(cfg config) error {
	users, err := players(cfg)
	if err != nil {
		return err
	}
	if len(users) < 2 {
		return errors.New("need at least two users")
	}
	games, err := registry()
	if err != nil {
		return err
	}
	rng := rand.New(rand.NewPCG(cfg.seed, cfg.seed^0x5eed)) //nolint:gosec
	ttt, err := tictactoeScripts(games, rng, 500)
	if err != nil {
		return err
	}
	qr, err := quoridorScripts(games, rng, 500)
	if err != nil {
		return err
	}
	ch, err := chessScripts(games, cfg.legends)
	if err != nil {
		return err
	}
	pools := [][]script{ttt, ch, qr}
	slog.Info("scripts ready", "tictactoe", len(ttt), "chess", len(ch), "quoridor", len(qr),
		"avg_plies", fmt.Sprintf("%.1f/%.1f/%.1f", avg(ttt), avg(ch), avg(qr)))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	transport := &http.Transport{MaxIdleConns: 4096, MaxIdleConnsPerHost: 4096, IdleConnTimeout: 90 * time.Second}
	client := gamev1connect.NewGameServiceClient(&http.Client{Transport: transport, Timeout: 30 * time.Second}, cfg.target)
	st := &stats{byGame: map[string]*gameStats{}, errors: map[string]int{}}
	results := make([]result, cfg.matches)
	jobs := make(chan int)
	var wg sync.WaitGroup
	started := time.Now()
	for range cfg.concurrency {
		wg.Go(func() {
			for i := range jobs {
				a, b := users[(2*i)%len(users)], users[(2*i+1)%len(users)]
				pool := pools[i%len(pools)]
				s := pool[(i/len(pools))%len(pool)]
				begin := time.Now()
				res := play(ctx, cfg, client, st, a, b, s)
				results[i] = res
				st.record(res, time.Since(begin))
			}
		})
	}
	reporter := time.NewTicker(10 * time.Second)
	go func() {
		for range reporter.C {
			elapsed := time.Since(started).Seconds()
			slog.Info("progress", "done", st.done.Load(), "failed", st.failed.Load(), "moves", st.moves.Load(),
				"moves_per_sec", int(float64(st.moves.Load())/elapsed), "matches_per_sec", int(float64(st.done.Load())/elapsed))
		}
	}()
	for i := range cfg.matches {
		select {
		case jobs <- i:
		case <-ctx.Done():
		}
		if ctx.Err() != nil {
			break
		}
	}
	close(jobs)
	wg.Wait()
	reporter.Stop()
	elapsed := time.Since(started)
	return st.report(cfg, elapsed, results)
}

func players(cfg config) ([]uuid.UUID, error) {
	if cfg.players > 0 {
		users := make([]uuid.UUID, cfg.players)
		for i := range users {
			users[i] = uuid.NewSHA1(uuid.NameSpaceOID, fmt.Appendf(nil, "axon-loadgen/%d/%d", cfg.seed, i))
		}
		return users, nil
	}
	raw, err := os.ReadFile(cfg.users)
	if err != nil {
		return nil, err
	}
	var users []uuid.UUID
	if err := json.Unmarshal(raw, &users); err != nil {
		return nil, err
	}
	return users, nil
}

func avg(scripts []script) float64 {
	total := 0
	for _, s := range scripts {
		total += len(s.moves)
	}
	return float64(total) / float64(len(scripts))
}

func (st *stats) record(res result, took time.Duration) {
	st.mu.Lock()
	defer st.mu.Unlock()
	g := st.byGame[res.Game]
	if g == nil {
		g = &gameStats{}
		st.byGame[res.Game] = g
	}
	g.Matches++
	g.Moves += res.Moves
	g.Duration += took.Milliseconds()
	if res.Error != "" {
		g.Failed++
		st.failed.Add(1)
		st.errors[res.Error]++
	}
	st.done.Add(1)
}

func (st *stats) latency(d time.Duration) {
	st.mu.Lock()
	st.latencies = append(st.latencies, d)
	st.mu.Unlock()
}

func (st *stats) fail(code string) {
	st.mu.Lock()
	st.errors[code]++
	st.mu.Unlock()
}

func (st *stats) report(cfg config, elapsed time.Duration, results []result) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	slices.Sort(st.latencies)
	q := func(p float64) int64 {
		if len(st.latencies) == 0 {
			return 0
		}
		return st.latencies[min(len(st.latencies)-1, int(p*float64(len(st.latencies))))].Milliseconds()
	}
	summary := map[string]any{
		"matches": st.done.Load(), "failed": st.failed.Load(), "elapsed_s": int(elapsed.Seconds()),
		"moves": st.moves.Load(), "commands_sent": st.sent.Load(), "redials": st.redials.Load(),
		"moves_per_sec": int(float64(st.moves.Load()) / elapsed.Seconds()), "matches_per_sec": float64(st.done.Load()) / elapsed.Seconds(),
		"latency_ms": map[string]int64{"p50": q(0.5), "p95": q(0.95), "p99": q(0.99), "max": q(1)},
		"by_game":    st.byGame, "errors": st.errors, "concurrency": cfg.concurrency,
	}
	out, _ := json.MarshalIndent(summary, "", " ")
	fmt.Println(string(out))
	raw, err := json.Marshal(results)
	if err != nil {
		return err
	}
	return os.WriteFile(cfg.out, raw, 0o600)
}

type player struct {
	id      uuid.UUID
	conn    *ws.Conn
	last    time.Time
	header  http.Header
	redials int
}

type update struct {
	seat  int
	event *gamev1.SessionEvent
	err   error
}

func identity(user uuid.UUID) http.Header {
	header := http.Header{}
	header.Set(server.TestUserHeader, user.String())
	return header
}

func call[Req, Res any](ctx context.Context, user uuid.UUID, msg *Req, do func(context.Context, *connect.Request[Req]) (*connect.Response[Res], error)) (*Res, error) {
	var lastErr error
	for attempt := range 20 {
		req := connect.NewRequest(msg)
		req.Header().Set(server.TestUserHeader, user.String())
		res, err := do(ctx, req)
		if err == nil {
			return res.Msg, nil
		}
		lastErr = err
		if connect.CodeOf(err) != connect.CodeUnavailable || ctx.Err() != nil {
			return nil, err
		}
		time.Sleep(time.Duration(attempt+1) * 200 * time.Millisecond)
	}
	return nil, lastErr
}

func play(ctx context.Context, cfg config, client gamev1connect.GameServiceClient, st *stats, a, b uuid.UUID, s script) result {
	res := result{Game: string(s.game)}
	ctx, cancel := context.WithTimeout(ctx, cfg.timeout)
	defer cancel()
	created, err := call(ctx, a, &gamev1.CreateSessionRequest{Game: string(s.game), RequestId: uuid.NewString()}, client.CreateSession)
	if err != nil {
		res.Error = "create: " + connect.CodeOf(err).String()
		return res
	}
	res.Session = created.Session.Id
	if _, err := call(ctx, b, &gamev1.JoinSessionRequest{SessionId: res.Session}, client.JoinSession); err != nil {
		res.Error = "join: " + connect.CodeOf(err).String()
		return res
	}
	players := [2]*player{{id: a, header: identity(a)}, {id: b, header: identity(b)}}
	updates := make(chan update, 64)
	wsURL := "ws" + cfg.target[len("http"):] + "/ws"
	var readers sync.WaitGroup
	dial := func(seat int) error {
		conn, err := ws.Dial(ctx, wsURL, ws.DialOptions{Subprotocols: []string{server.Protocol}, Header: players[seat].header, Options: ws.Options{ReadLimit: ws.LargeMessageReadLimit}})
		if err != nil {
			return err
		}
		subscribe, _ := protojson.Marshal(&gamev1.SessionCommand{Action: &gamev1.SessionCommand_Subscribe{Subscribe: res.Session}})
		if err := conn.Write(ctx, subscribe); err != nil {
			_ = conn.CloseNow()
			return err
		}
		players[seat].conn = conn
		readers.Go(func() {
			for {
				frame, err := conn.Read(ctx)
				if err != nil {
					select {
					case updates <- update{seat: seat, err: err}:
					case <-ctx.Done():
					}
					return
				}
				event := &gamev1.SessionEvent{}
				if protojson.Unmarshal(frame, event) != nil {
					continue
				}
				select {
				case updates <- update{seat: seat, event: event}:
				case <-ctx.Done():
					return
				}
			}
		})
		return nil
	}
	defer func() {
		cancel()
		for _, p := range players {
			if p.conn != nil {
				_ = p.conn.Close(ws.StatusNormalClosure, "")
			}
		}
		readers.Wait()
	}()
	for seat := range players {
		if err := dial(seat); err != nil {
			res.Error = "connect"
			return res
		}
	}
	redial := func(seat int) error {
		p := players[seat]
		_ = p.conn.CloseNow()
		for attempt := 1; ; attempt++ {
			p.redials++
			st.redials.Add(1)
			if err := sleepCtx(ctx, min(time.Duration(attempt)*100*time.Millisecond, time.Second)); err != nil {
				return err
			}
			err := dial(seat)
			if err == nil || p.redials >= cfg.redials {
				return err
			}
		}
	}
	sentPly, sentAt := -1, time.Time{}
	var resend <-chan time.Time
	var latest *gamev1.Session
	send := func() {
		if latest == nil {
			return
		}
		ply := int(latest.Ply)
		turn := latest.GetTurnUserId()
		seat := 0
		if turn == b.String() {
			seat = 1
		}
		p := players[seat]
		if wait := cfg.pace - time.Since(p.last); wait > 0 {
			resend = time.After(wait)
			return
		}
		command := &gamev1.SessionCommand{}
		revision := proto.Uint32(latest.GetRevision())
		switch s.game {
		case tictactoe.Key:
			cell, _ := strconv.Atoi(s.moves[ply])
			command.Action = &gamev1.SessionCommand_Move{Move: &gamev1.TicTacToeMove{Ply: uint32(ply), Cell: uint32(cell), Revision: revision}}
		case chess.Key:
			command.Action = &gamev1.SessionCommand_ChessMove{ChessMove: &gamev1.ChessMove{Ply: uint32(ply), Uci: s.moves[ply], Revision: revision}}
		case quoridor.Key:
			command.Action = &gamev1.SessionCommand_QuoridorMove{QuoridorMove: &gamev1.QuoridorMove{Ply: uint32(ply), Move: s.moves[ply], Revision: revision}}
		}
		frame, _ := protojson.Marshal(command)
		if sentPly != ply {
			sentAt = time.Now()
		}
		sentPly = ply
		p.last = time.Now()
		st.sent.Add(1)
		_ = p.conn.Write(ctx, frame)
	}
	for {
		select {
		case <-ctx.Done():
			res.Error = "timeout"
			if latest != nil {
				res.Moves = int(latest.Ply)
			}
			return res
		case <-resend:
			resend = nil
			send()
		case u := <-updates:
			if u.err != nil {
				if ctx.Err() != nil {
					continue
				}
				if players[u.seat].redials >= cfg.redials || redial(u.seat) != nil {
					res.Error = "socket closed: " + strconv.Itoa(int(ws.CloseStatus(u.err)))
					if latest != nil {
						res.Moves = int(latest.Ply)
					}
					return res
				}
				sentPly = -1
				continue
			}
			if code := u.event.GetError(); code != gamev1.SessionError_SESSION_ERROR_UNSPECIFIED {
				st.fail(code.String())
				if code == gamev1.SessionError_SESSION_ERROR_RATE_LIMITED {
					resend = time.After(cfg.pace)
				}
				continue
			}
			session := u.event.GetSession()
			if session == nil || latest != nil && session.GetRevision() < latest.GetRevision() {
				continue
			}
			latest = session
			ply := int(session.Ply)
			if sentPly >= 0 && ply > sentPly {
				st.latency(time.Since(sentAt))
				st.moves.Add(int64(ply - sentPly))
				sentPly = -1
			}
			switch session.Status {
			case gamev1.SessionStatus_SESSION_STATUS_WON, gamev1.SessionStatus_SESSION_STATUS_DRAW:
				res.Moves, res.Status = ply, session.Status.String()
				return res
			}
			if ply >= len(s.moves) {
				loser := a
				if s.loser == 1 {
					loser = b
				}
				left, err := call(ctx, loser, &gamev1.LeaveSessionRequest{SessionId: res.Session}, client.LeaveSession)
				res.Moves = ply
				if err != nil {
					res.Error = "leave: " + connect.CodeOf(err).String()
					return res
				}
				res.Status = left.GetSession().GetStatus().String()
				return res
			}
			if sentPly != ply && resend == nil {
				send()
			}
		}
	}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
