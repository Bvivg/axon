//go:build e2e

package e2e

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/encoding/protojson"

	authv1 "github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1/authv1connect"
	gamev1 "github.com/bvivg/axon/core/shared/gen/go/axon/game/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/game/v1/gamev1connect"
	tournamentv1 "github.com/bvivg/axon/core/shared/gen/go/axon/tournament/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/tournament/v1/tournamentv1connect"
	"github.com/bvivg/axon/core/shared/pkg/ws"
)

var crossWins = []uint32{0, 3, 1, 4, 2}

type player struct {
	id    string
	token string
}

func setting(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func gatewayURL() string { return setting("GATEWAY_URL", "http://gateway:8080") }

func client() *http.Client { return &http.Client{Timeout: 15 * time.Second} }

func retry[T any](t *testing.T, what string, call func() (T, error)) T {
	t.Helper()
	deadline := time.Now().Add(time.Minute)
	for {
		out, err := call()
		if err == nil {
			return out
		}
		code := connect.CodeOf(err)
		if time.Now().After(deadline) || (code != connect.CodeUnavailable && code != connect.CodeDeadlineExceeded && code != connect.CodeUnknown && code != connect.CodeInternal) {
			t.Fatalf("%s: %v", what, err)
		}
		time.Sleep(time.Second)
	}
}

func register(t *testing.T) player {
	t.Helper()
	auth := authv1connect.NewAuthServiceClient(client(), gatewayURL())
	registered := retry(t, "register", func() (*connect.Response[authv1.RegisterResponse], error) {
		return auth.Register(t.Context(), connect.NewRequest(&authv1.RegisterRequest{
			Email: "tournament-" + uuid.NewString()[:8] + "@axon.test", Password: "tournament-" + uuid.NewString(),
		}))
	})
	return player{id: registered.Msg.GetUser().GetId(), token: registered.Msg.GetTokens().GetAccessToken()}
}

func as[T any](who player, msg *T) *connect.Request[T] {
	req := connect.NewRequest(msg)
	req.Header().Set("Authorization", "Bearer "+who.token)
	return req
}

type socket struct {
	t    *testing.T
	conn *ws.Conn
}

func open(t *testing.T, who player, id string) (*socket, error) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	conn, err := ws.Dial(ctx, strings.Replace(gatewayURL(), "http", "ws", 1)+"/ws/game", ws.DialOptions{
		Subprotocols: []string{"axon.game.v1", "axon.bearer." + who.token},
	})
	if err != nil {
		return nil, fmt.Errorf("dial game socket: %w", err)
	}
	s := &socket{t: t, conn: conn}
	if err := s.send(&gamev1.SessionCommand{Action: &gamev1.SessionCommand_Subscribe{Subscribe: id}}); err != nil {
		_ = conn.CloseNow()
		return nil, err
	}
	return s, nil
}

func (s *socket) send(command *gamev1.SessionCommand) error {
	frame, err := protojson.Marshal(command)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(s.t.Context(), 15*time.Second)
	defer cancel()
	return s.conn.Write(ctx, frame)
}

func (s *socket) at(ply uint32) (*gamev1.Session, error) {
	ctx, cancel := context.WithTimeout(s.t.Context(), 45*time.Second)
	defer cancel()
	for {
		raw, err := s.conn.Read(ctx)
		if err != nil {
			return nil, fmt.Errorf("waiting for ply %d: %w", ply, err)
		}
		var event gamev1.SessionEvent
		if err := protojson.Unmarshal(raw, &event); err != nil {
			return nil, err
		}
		if event.GetError() != gamev1.SessionError_SESSION_ERROR_UNSPECIFIED {
			return nil, fmt.Errorf("game error at ply %d: %v", ply, event.GetError())
		}
		if session := event.GetSession(); session != nil && session.GetPly() == ply {
			return session, nil
		}
	}
}

func waitSession(t *testing.T, games gamev1connect.GameServiceClient, who player, id string) (*gamev1.Session, error) {
	deadline := time.Now().Add(2 * time.Minute)
	for {
		res, err := games.GetSession(t.Context(), as(who, &gamev1.GetSessionRequest{SessionId: id}))
		if err == nil {
			return res.Msg.GetSession(), nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("session %s never appeared: %w", id, err)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func play(t *testing.T, games gamev1connect.GameServiceClient, match *tournamentv1.Match, players map[string]player, moves int) error {
	seats := match.GetPlayerIds()
	first, second := players[seats[0]], players[seats[1]]
	session, err := waitSession(t, games, first, match.GetSessionId())
	if err != nil {
		return err
	}
	if session.GetTournamentId() == "" || !session.GetRated() || session.GetStatus() != gamev1.SessionStatus_SESSION_STATUS_PLAYING {
		return fmt.Errorf("tournament session %s = %v", match.GetSessionId(), session)
	}
	present := []player{first, second}
	if moves < len(crossWins) {
		present = present[:1]
	}
	sockets := map[string]*socket{}
	for _, who := range present {
		s, err := open(t, who, match.GetSessionId())
		if err != nil {
			return err
		}
		defer func() { _ = s.conn.CloseNow() }()
		sockets[who.id] = s
	}
	for ply := range moves {
		var mover *socket
		for _, s := range sockets {
			state, err := s.at(uint32(ply))
			if err != nil {
				return err
			}
			if turn := state.GetTurnUserId(); turn != "" {
				mover = sockets[turn]
			}
		}
		if mover == nil {
			return fmt.Errorf("session %s: nobody here is to move at ply %d", match.GetSessionId(), ply)
		}
		if err := mover.send(&gamev1.SessionCommand{Action: &gamev1.SessionCommand_Move{Move: &gamev1.TicTacToeMove{Ply: uint32(ply), Cell: crossWins[ply]}}}); err != nil {
			return err
		}
	}
	if moves < len(crossWins) {
		waiting, err := sockets[first.id].at(uint32(moves))
		if err != nil {
			return err
		}
		if waiting.GetTurnUserId() != second.id || waiting.GetStatus() != gamev1.SessionStatus_SESSION_STATUS_PLAYING {
			return fmt.Errorf("session %s after the opening move = %v", match.GetSessionId(), waiting)
		}
		return nil
	}
	final, err := sockets[first.id].at(uint32(len(crossWins)))
	if err != nil {
		return err
	}
	if final.GetStatus() != gamev1.SessionStatus_SESSION_STATUS_WON || final.GetWinnerUserId() != first.id {
		return fmt.Errorf("session %s ended as %v", match.GetSessionId(), final)
	}
	return nil
}

func metric(t *testing.T, names ...string) map[string]string {
	t.Helper()
	out := map[string]string{}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, setting("TOURNAMENT_METRICS_URL", "http://tournament:9096/metrics"), nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := client().Do(req)
	if err != nil {
		return out
	}
	defer func() { _ = res.Body.Close() }()
	scanner := bufio.NewScanner(res.Body)
	for scanner.Scan() {
		line := scanner.Text()
		for _, name := range names {
			if value, ok := strings.CutPrefix(line, name+" "); ok {
				out[name] = value
			}
		}
	}
	return out
}

func TestEightPlayerBracketWithANoShowThroughTheGateway(t *testing.T) {
	ctx := t.Context()
	noShow, err := time.ParseDuration(setting("TOURNAMENT_E2E_NO_SHOW", "30s"))
	if err != nil {
		t.Fatal(err)
	}
	tournaments := tournamentv1connect.NewTournamentServiceClient(client(), gatewayURL())
	games := gamev1connect.NewGameServiceClient(client(), gatewayURL())
	players := map[string]player{}
	var order []player
	for range 8 {
		p := register(t)
		players[p.id] = p
		order = append(order, p)
	}
	creator := order[0]

	request := uuid.NewString()
	create := func() (*connect.Response[tournamentv1.CreateTournamentResponse], error) {
		return tournaments.CreateTournament(ctx, as(creator, &tournamentv1.CreateTournamentRequest{
			RequestId: request, Name: "E2E bracket", Game: "tictactoe",
			Format: tournamentv1.TournamentFormat_TOURNAMENT_FORMAT_SINGLE_ELIMINATION, Capacity: 8,
		}))
	}
	created := retry(t, "create", create)
	id := created.Msg.GetTournament().GetId()
	if again := retry(t, "create again", create); again.Msg.GetTournament().GetId() != id {
		t.Fatalf("a repeated create made a second tournament: %s, %s", id, again.Msg.GetTournament().GetId())
	}
	if _, err := tournaments.CreateTournament(ctx, connect.NewRequest(&tournamentv1.CreateTournamentRequest{RequestId: uuid.NewString(), Name: "anonymous", Game: "tictactoe", Format: tournamentv1.TournamentFormat_TOURNAMENT_FORMAT_SINGLE_ELIMINATION, Capacity: 4})); connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("anonymous create = %v, want Unauthenticated", err)
	}

	for _, p := range order {
		retry(t, "register for the tournament", func() (*connect.Response[tournamentv1.RegisterForTournamentResponse], error) {
			return tournaments.RegisterForTournament(ctx, as(p, &tournamentv1.RegisterForTournamentRequest{TournamentId: id}))
		})
	}
	outsider := register(t)
	if _, err := tournaments.RegisterForTournament(ctx, as(outsider, &tournamentv1.RegisterForTournamentRequest{TournamentId: id})); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("a ninth registration = %v, want FailedPrecondition", err)
	}
	if _, err := tournaments.StartTournament(ctx, as(order[1], &tournamentv1.StartTournamentRequest{TournamentId: id})); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("start by a player = %v, want PermissionDenied", err)
	}

	startedAt := time.Now()
	retry(t, "start", func() (*connect.Response[tournamentv1.StartTournamentResponse], error) {
		return tournaments.StartTournament(ctx, as(creator, &tournamentv1.StartTournamentRequest{TournamentId: id}))
	})

	get := func() *tournamentv1.Tournament {
		return retry(t, "get", func() (*connect.Response[tournamentv1.GetTournamentResponse], error) {
			return tournaments.GetTournament(ctx, as(creator, &tournamentv1.GetTournamentRequest{TournamentId: id}))
		}).Msg.GetTournament()
	}
	waitFor := func(what string, limit time.Duration, check func(*tournamentv1.Tournament) bool) *tournamentv1.Tournament {
		deadline := time.Now().Add(limit)
		for {
			current := get()
			if check(current) {
				return current
			}
			if current.GetStatus() == tournamentv1.TournamentStatus_TOURNAMENT_STATUS_CANCELLED || time.Now().After(deadline) {
				t.Fatalf("waiting for %s: %v", what, current)
			}
			time.Sleep(250 * time.Millisecond)
		}
	}

	seeded := waitFor("the first round", 2*time.Minute, func(c *tournamentv1.Tournament) bool { return c.GetCurrentRound() == 1 })
	seeds := map[string]uint32{}
	bySeed := map[uint32]string{}
	for _, r := range seeded.GetRegistrations() {
		seeds[r.GetUserId()] = r.GetSeed()
		bySeed[r.GetSeed()] = r.GetUserId()
	}
	if seeded.GetTotalRounds() != 3 || len(seeds) != 8 {
		t.Fatalf("seeded = %v", seeded)
	}
	absent := bySeed[8]

	var forfeited *tournamentv1.Match
	var roundStarts []time.Time
	for round := uint32(1); round <= 3; round++ {
		current := waitFor(fmt.Sprintf("round %d", round), 2*time.Minute, func(c *tournamentv1.Tournament) bool { return c.GetCurrentRound() == round })
		var matches []*tournamentv1.Match
		for _, r := range current.GetRounds() {
			if r.GetNumber() == round {
				matches = r.GetMatches()
				roundStarts = append(roundStarts, r.GetStartedAt().AsTime())
			}
		}
		if len(matches) != 8>>round {
			t.Fatalf("round %d has %d matches", round, len(matches))
		}
		var wg sync.WaitGroup
		errs := make(chan error, len(matches))
		for _, m := range matches {
			if seeds[m.GetPlayerIds()[0]] > seeds[m.GetPlayerIds()[1]] {
				t.Fatalf("the better seed is not on the first seat: %v", m)
			}
			moves := len(crossWins)
			if m.GetPlayerIds()[1] == absent {
				moves = 1
			}
			wg.Go(func() {
				if err := play(t, games, m, players, moves); err != nil {
					errs <- err
				}
			})
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Fatal(err)
		}
		done := waitFor(fmt.Sprintf("round %d to finish", round), noShow+2*time.Minute, func(c *tournamentv1.Tournament) bool {
			for _, r := range c.GetRounds() {
				if r.GetNumber() == round {
					return r.GetStatus() == tournamentv1.RoundStatus_ROUND_STATUS_FINISHED
				}
			}
			return false
		})
		for _, r := range done.GetRounds() {
			if r.GetNumber() != round {
				continue
			}
			var summary []string
			for _, m := range r.GetMatches() {
				if m.GetPlayerIds()[1] == absent {
					forfeited = m
				}
				summary = append(summary, fmt.Sprintf("%d-%d won by %d forfeit=%v", seeds[m.GetPlayerIds()[0]], seeds[m.GetPlayerIds()[1]], seeds[m.GetWinnerId()], m.GetForfeit()))
			}
			t.Logf("round %d finished at +%s: %s", round, time.Since(startedAt).Round(time.Millisecond), strings.Join(summary, ", "))
		}
	}

	finished := waitFor("the tournament to finish", 2*time.Minute, func(c *tournamentv1.Tournament) bool {
		return c.GetStatus() == tournamentv1.TournamentStatus_TOURNAMENT_STATUS_FINISHED
	})
	elapsed := time.Since(startedAt)
	if finished.GetWinnerId() != bySeed[1] {
		t.Fatalf("winner seed %d, want 1", seeds[finished.GetWinnerId()])
	}
	wantRanks := map[uint32]uint32{1: 1, 2: 2, 3: 3, 4: 3, 5: 5, 6: 5, 7: 5, 8: 5}
	standings := finished.GetStandings()
	if len(standings) != 8 {
		t.Fatalf("standings = %v", standings)
	}
	for i, s := range standings {
		seed := seeds[s.GetUserId()]
		if seed != uint32(i+1) || s.GetRank() != wantRanks[seed] {
			t.Fatalf("standing %d is seed %d at rank %d", i+1, seed, s.GetRank())
		}
	}
	if top := standings[0]; top.GetWins() != 3 || top.GetLosses() != 0 || top.GetHalfPoints() != 6 || top.GetEliminated() {
		t.Fatalf("champion row = %v", top)
	}
	if last := standings[7]; last.GetLosses() != 1 || last.GetWins() != 0 || !last.GetEliminated() {
		t.Fatalf("no-show row = %v", last)
	}
	if forfeited == nil || !forfeited.GetForfeit() || forfeited.GetWinnerId() != bySeed[1] || forfeited.GetResult() != tournamentv1.MatchResult_MATCH_RESULT_WIN {
		t.Fatalf("no-show match = %v", forfeited)
	}
	forfeitLag := forfeited.GetFinishedAt().AsTime().Sub(roundStarts[0].Add(noShow))
	if forfeitLag < 0 {
		t.Fatalf("the no-show lost before the deadline: %s", forfeitLag)
	}
	resigned, err := waitSession(t, games, players[bySeed[1]], forfeited.GetSessionId())
	if err != nil {
		t.Fatal(err)
	}
	if resigned.GetStatus() != gamev1.SessionStatus_SESSION_STATUS_WON || !resigned.GetResigned() || resigned.GetWinnerUserId() != bySeed[1] || resigned.GetTournamentId() != id {
		t.Fatalf("forfeited session = %v", resigned)
	}
	matches := 0
	for _, r := range finished.GetRounds() {
		matches += len(r.GetMatches())
	}
	if matches != 7 || len(finished.GetRounds()) != 3 {
		t.Fatalf("%d matches over %d rounds", matches, len(finished.GetRounds()))
	}
	if _, err := tournaments.CancelTournament(ctx, as(creator, &tournamentv1.CancelTournamentRequest{TournamentId: id})); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("cancel after the finish = %v, want FailedPrecondition", err)
	}
	listed := retry(t, "list", func() (*connect.Response[tournamentv1.ListTournamentsResponse], error) {
		status := tournamentv1.TournamentStatus_TOURNAMENT_STATUS_FINISHED
		return tournaments.ListTournaments(ctx, as(outsider, &tournamentv1.ListTournamentsRequest{Status: &status, Limit: 50}))
	})
	found := false
	for _, item := range listed.Msg.GetTournaments() {
		found = found || item.GetId() == id
	}
	if !found {
		t.Fatal("the finished tournament is not listed")
	}

	values := metric(t, "tournament_forfeits_total", "tournament_rounds_total", "tournament_active",
		`tournament_saga_step_seconds_sum{step="record"}`, `tournament_saga_step_seconds_count{step="record"}`,
		`tournament_saga_step_seconds_sum{step="advance"}`, `tournament_saga_step_seconds_count{step="advance"}`,
		`tournament_saga_step_seconds_sum{step="seed"}`, `tournament_saga_step_seconds_count{step="seed"}`,
		`tournament_saga_step_seconds_sum{step="forfeit"}`, `tournament_saga_step_seconds_count{step="forfeit"}`)
	t.Logf("8-player bracket: start to finished %s (round 1 waits for the %s no-show deadline), forfeit %s after the deadline, rounds started at +%s/+%s/+%s",
		elapsed.Round(time.Millisecond), noShow, forfeitLag.Round(time.Millisecond),
		roundStarts[0].Sub(startedAt).Round(time.Millisecond), roundStarts[1].Sub(startedAt).Round(time.Millisecond), roundStarts[2].Sub(startedAt).Round(time.Millisecond))
	t.Logf("metrics: %v", values)
}
