//go:build e2e

package e2e

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/bvivg/axon/core/services/rating/internal/ledger"
	authv1 "github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1/authv1connect"
	eventsv1 "github.com/bvivg/axon/core/shared/gen/go/axon/events/v1"
	gamev1 "github.com/bvivg/axon/core/shared/gen/go/axon/game/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/game/v1/gamev1connect"
	ratingv1 "github.com/bvivg/axon/core/shared/gen/go/axon/rating/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/rating/v1/ratingv1connect"
	"github.com/bvivg/axon/core/shared/pkg/outbox"
	"github.com/bvivg/axon/core/shared/pkg/ws"
)

const (
	quickGame      = "tictactoe"
	gameProtocol   = "axon.game.v1"
	bearerProtocol = "axon.bearer."
	deviceHeader   = "X-Axon-Device"
	finishedType   = "axon.events.v1.GameFinished"
	changedType    = "axon.events.v1.RatingChanged"
)

const roundEvery = 7 * time.Second

var (
	xWins = []uint32{0, 3, 1, 4, 2}
	oWins = []uint32{0, 3, 1, 4, 8, 5}
	drawn = []uint32{0, 4, 8, 2, 6, 3, 5, 7, 1}
)

func waitFor(t *testing.T, timeout time.Duration, what string, check func() bool) time.Duration {
	t.Helper()
	started := time.Now()
	deadline := started.Add(timeout)
	for !check() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for", what)
		}
		time.Sleep(200 * time.Millisecond)
	}
	return time.Since(started)
}

type gamer struct {
	id    uuid.UUID
	token string
}

type stand struct {
	gateway string
	auth    authv1connect.AuthServiceClient
	game    gamev1connect.GameServiceClient
	rating  ratingv1connect.RatingServiceClient
	pool    *pgxpool.Pool
	brokers []string
}

func newStand(t *testing.T) *stand {
	t.Helper()
	gateway := strings.TrimSuffix(env(t, "RATING_E2E_GATEWAY_URL"), "/")
	s := &stand{
		gateway: gateway,
		auth:    authv1connect.NewAuthServiceClient(http.DefaultClient, gateway),
		game:    gamev1connect.NewGameServiceClient(http.DefaultClient, gateway),
		rating:  ratingv1connect.NewRatingServiceClient(http.DefaultClient, env(t, "RATING_E2E_API_URL")),
		brokers: strings.Split(env(t, "RATING_E2E_KAFKA_BROKERS"), ","),
	}
	pool, err := pgxpool.New(t.Context(), env(t, "RATING_E2E_POSTGRES_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	s.pool = pool
	return s
}

func (s *stand) register(t *testing.T, name string) gamer {
	t.Helper()
	req := connect.NewRequest(&authv1.RegisterRequest{
		Email: "rating-e2e-" + uuid.NewString() + "@axon.test", Password: "correct-horse-battery-staple", DisplayName: &name,
	})
	req.Header().Set(deviceHeader, uuid.NewString())
	res, err := s.auth.Register(t.Context(), req)
	if err != nil {
		t.Fatalf("register %s: %v", name, err)
	}
	return gamer{id: uuid.MustParse(res.Msg.GetUser().GetId()), token: res.Msg.GetTokens().GetAccessToken()}
}

func authorized[T any](g gamer, msg *T) *connect.Request[T] {
	req := connect.NewRequest(msg)
	req.Header().Set("Authorization", "Bearer "+g.token)
	return req
}

func (s *stand) dial(ctx context.Context, g gamer) (*ws.Conn, error) {
	return ws.Dial(ctx, "ws"+strings.TrimPrefix(s.gateway, "http")+"/ws/game", ws.DialOptions{
		Subprotocols: []string{gameProtocol, bearerProtocol + g.token},
		Options:      ws.Options{ReadLimit: ws.LargeMessageReadLimit},
	})
}

func send(ctx context.Context, conn *ws.Conn, command *gamev1.SessionCommand) error {
	data, err := protojson.Marshal(command)
	if err != nil {
		return err
	}
	return conn.Write(ctx, data)
}

func receive(ctx context.Context, conn *ws.Conn) (*gamev1.SessionEvent, error) {
	data, err := conn.Read(ctx)
	if err != nil {
		return nil, err
	}
	event := &gamev1.SessionEvent{}
	return event, protojson.Unmarshal(data, event)
}

func (s *stand) quickMatch(ctx context.Context, g gamer) (uuid.UUID, error) {
	for {
		current, err := s.game.GetMatch(ctx, authorized(g, &gamev1.GetMatchRequest{Game: quickGame}))
		if err != nil {
			return uuid.Nil, fmt.Errorf("get match: %w", err)
		}
		if current.Msg.GetTicket().GetStatus() != gamev1.MatchStatus_MATCH_STATUS_MATCHED {
			break
		}
		select {
		case <-ctx.Done():
			return uuid.Nil, ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	request := uuid.NewString()
	for {
		_, err := s.game.FindMatch(ctx, authorized(g, &gamev1.FindMatchRequest{Game: quickGame, RequestId: request}))
		if err == nil {
			break
		}
		var failure *connect.Error
		if !errors.As(err, &failure) || failure.Code() != connect.CodeResourceExhausted {
			return uuid.Nil, fmt.Errorf("find match: %w", err)
		}
		wait, _ := strconv.Atoi(failure.Meta().Get("Retry-After"))
		select {
		case <-ctx.Done():
			return uuid.Nil, ctx.Err()
		case <-time.After(time.Duration(max(wait, 1)) * time.Second):
		}
	}
	conn, err := s.dial(ctx, g)
	if err != nil {
		return uuid.Nil, fmt.Errorf("match socket: %w", err)
	}
	defer func() { _ = conn.CloseNow() }()
	if err := send(ctx, conn, &gamev1.SessionCommand{Action: &gamev1.SessionCommand_SubscribeMatch{SubscribeMatch: quickGame}}); err != nil {
		return uuid.Nil, err
	}
	for {
		event, err := receive(ctx, conn)
		if err != nil {
			return uuid.Nil, fmt.Errorf("match socket: %w", err)
		}
		ticket := event.GetMatch()
		switch ticket.GetStatus() {
		case gamev1.MatchStatus_MATCH_STATUS_MATCHED:
			return uuid.Parse(ticket.GetSessionId())
		case gamev1.MatchStatus_MATCH_STATUS_IDLE:
			return uuid.Nil, errors.New("the match ticket was dropped")
		}
	}
}

func script(session uuid.UUID) []uint32 {
	return [][]uint32{xWins, oWins, drawn}[int(session[15])%3]
}

type played struct {
	final    *gamev1.Session
	ended    time.Time
	rated    *gamev1.Session
	ratedAt  time.Time
	finished bool
}

func over(status gamev1.SessionStatus) bool {
	return status == gamev1.SessionStatus_SESSION_STATUS_WON || status == gamev1.SessionStatus_SESSION_STATUS_DRAW
}

func (s *stand) play(ctx context.Context, g gamer, session uuid.UUID, cells []uint32, ratings time.Duration) (played, error) {
	var result played
	conn, err := s.dial(ctx, g)
	if err != nil {
		return result, fmt.Errorf("session socket: %w", err)
	}
	defer func() { _ = conn.CloseNow() }()
	if err := send(ctx, conn, &gamev1.SessionCommand{Action: &gamev1.SessionCommand_Subscribe{Subscribe: session.String()}}); err != nil {
		return result, err
	}
	read := ctx
	sent := -1
	var last *gamev1.SessionCommand
	for {
		event, err := receive(read, conn)
		if err != nil {
			if result.finished && read.Err() != nil {
				return result, nil
			}
			return result, fmt.Errorf("session %s socket: %w", session, err)
		}
		switch code := event.GetError(); code {
		case gamev1.SessionError_SESSION_ERROR_UNSPECIFIED:
		case gamev1.SessionError_SESSION_ERROR_STALE_MOVE, gamev1.SessionError_SESSION_ERROR_NOT_YOUR_TURN:
			sent = -1
			continue
		case gamev1.SessionError_SESSION_ERROR_RATE_LIMITED:
			time.Sleep(300 * time.Millisecond)
			if last != nil {
				if err := send(ctx, conn, last); err != nil {
					return result, err
				}
			}
			continue
		default:
			return result, fmt.Errorf("session %s: %v", session, code)
		}
		state := event.GetSession()
		if state == nil {
			continue
		}
		if over(state.GetStatus()) {
			if !result.finished {
				result.final, result.ended, result.finished = state, time.Now(), true
			}
			if len(state.GetRatingChanges()) == 2 {
				result.rated, result.ratedAt = state, time.Now()
				return result, nil
			}
			if ratings <= 0 {
				return result, nil
			}
			if read == ctx {
				var cancel context.CancelFunc
				read, cancel = context.WithTimeout(ctx, ratings)
				defer cancel()
			}
			continue
		}
		if state.GetStatus() != gamev1.SessionStatus_SESSION_STATUS_PLAYING || state.GetTurnUserId() != g.id.String() || int(state.GetPly()) <= sent {
			continue
		}
		ply := int(state.GetPly())
		if ply >= len(cells) {
			return result, fmt.Errorf("session %s: the script has no move for ply %d", session, ply)
		}
		last = &gamev1.SessionCommand{Action: &gamev1.SessionCommand_Move{Move: &gamev1.TicTacToeMove{Ply: state.GetPly(), Cell: cells[ply], Revision: state.Revision}}}
		if err := send(ctx, conn, last); err != nil {
			return result, err
		}
		sent = ply
	}
}

func (s *stand) round(ctx context.Context, players []gamer, ratings time.Duration) (map[uuid.UUID][]played, error) {
	var mu sync.Mutex
	var wg sync.WaitGroup
	results := map[uuid.UUID][]played{}
	var failures []error
	for _, player := range players {
		wg.Go(func() {
			session, err := s.quickMatch(ctx, player)
			if err == nil {
				var result played
				result, err = s.play(ctx, player, session, script(session), ratings)
				mu.Lock()
				results[session] = append(results[session], result)
				mu.Unlock()
			}
			if err != nil {
				mu.Lock()
				failures = append(failures, fmt.Errorf("player %s: %w", player.id, err))
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	return results, errors.Join(failures...)
}

func findRecord(ctx context.Context, brokers []string, topic string, match func(*kgo.Record) bool) (*kgo.Record, error) {
	client, err := kgo.NewClient(kgo.SeedBrokers(brokers...), kgo.ConsumeTopics(topic), kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()))
	if err != nil {
		return nil, err
	}
	defer client.Close()
	for {
		fetches := client.PollFetches(ctx)
		if ctx.Err() != nil {
			return nil, fmt.Errorf("no matching record on %s: %w", topic, ctx.Err())
		}
		var found *kgo.Record
		fetches.EachRecord(func(record *kgo.Record) {
			if found == nil && match(record) {
				found = record
			}
		})
		if found != nil {
			return found, nil
		}
	}
}

func scanTopic(ctx context.Context, brokers []string, topic string, each func(*kgo.Record)) error {
	client, err := kgo.NewClient(kgo.SeedBrokers(brokers...))
	if err != nil {
		return err
	}
	defer client.Close()
	ends, err := kadm.NewClient(client).ListEndOffsets(ctx, topic)
	if err != nil {
		return err
	}
	remaining := map[int32]int64{}
	ends.Each(func(end kadm.ListedOffset) {
		if end.Err == nil && end.Offset > 0 {
			remaining[end.Partition] = end.Offset
		}
	})
	if len(remaining) == 0 {
		return nil
	}
	reader, err := kgo.NewClient(kgo.SeedBrokers(brokers...), kgo.ConsumeTopics(topic), kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()))
	if err != nil {
		return err
	}
	defer reader.Close()
	for len(remaining) > 0 {
		fetches := reader.PollFetches(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		fetches.EachRecord(func(record *kgo.Record) {
			each(record)
			if end, ok := remaining[record.Partition]; ok && record.Offset+1 >= end {
				delete(remaining, record.Partition)
			}
		})
	}
	return nil
}

func headerOf(record *kgo.Record, key string) string {
	for _, h := range record.Headers {
		if h.Key == key {
			return string(h.Value)
		}
	}
	return ""
}

func describeRecord(record *kgo.Record) string {
	headers := make([]string, 0, len(record.Headers))
	for _, h := range record.Headers {
		headers = append(headers, h.Key+"="+string(h.Value))
	}
	return fmt.Sprintf("topic=%s partition=%d offset=%d key=%q headers=[%s] value=%d bytes", record.Topic, record.Partition, record.Offset, record.Key, strings.Join(headers, " "), len(record.Value))
}

type outboxRow struct {
	id      uuid.UUID
	topic   string
	key     string
	kind    string
	payload []byte
	event   *eventsv1.GameFinished
}

func (s *stand) finishedRow(ctx context.Context, session uuid.UUID) (outboxRow, bool, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, aggregatetype, aggregateid, type, payload FROM game.outbox WHERE aggregatetype = $1`, ledger.FinishedTopic)
	if err != nil {
		return outboxRow{}, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var row outboxRow
		if err := rows.Scan(&row.id, &row.topic, &row.key, &row.kind, &row.payload); err != nil {
			return outboxRow{}, false, err
		}
		row.event = &eventsv1.GameFinished{}
		if err := proto.Unmarshal(row.payload, row.event); err != nil {
			return outboxRow{}, false, err
		}
		if row.event.GetSessionId() == session.String() {
			return row, true, nil
		}
	}
	return outboxRow{}, false, rows.Err()
}

func TestRatedQuickMatchTravelsTheOutboxChain(t *testing.T) {
	s := newStand(t)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	a, b := s.register(t, "Chain Alpha"), s.register(t, "Chain Bravo")

	var sessions [2]uuid.UUID
	var results [2]played
	var errs [2]error
	var wg sync.WaitGroup
	for seat, player := range []gamer{a, b} {
		wg.Go(func() {
			sessions[seat], errs[seat] = s.quickMatch(ctx, player)
			if errs[seat] == nil {
				results[seat], errs[seat] = s.play(ctx, player, sessions[seat], xWins, 90*time.Second)
			}
		})
	}
	wg.Wait()
	if err := errors.Join(errs[:]...); err != nil {
		t.Fatal(err)
	}
	if sessions[0] != sessions[1] {
		t.Fatalf("quick match paired the players into %s and %s", sessions[0], sessions[1])
	}
	session := sessions[0]
	final := results[0].final
	if final.GetStatus() != gamev1.SessionStatus_SESSION_STATUS_WON || !final.GetRated() {
		t.Fatalf("final session = %v", final)
	}
	winner, loser := uuid.MustParse(final.GetWinnerUserId()), a.id
	if winner == a.id {
		loser = b.id
	}
	ended := results[0].ended
	if results[1].ended.Before(ended) {
		ended = results[1].ended
	}
	t.Logf("session %s: %s won as %s in %d plies, players in seat order %v", session, winner, map[bool]string{true: "X", false: "O"}[final.GetPlayerIds()[0] == winner.String()], final.GetPly(), final.GetPlayerIds())

	var row outboxRow
	outboxAfter := waitFor(t, 30*time.Second, "the game.finished outbox row", func() bool {
		found, ok, err := s.finishedRow(ctx, session)
		if err != nil {
			t.Fatal(err)
		}
		row = found
		return ok
	})
	if row.id.Version() != 5 || row.key != quickGame || row.kind != finishedType || !row.event.GetRated() ||
		row.event.GetEventId() != row.id.String() || !slices.Equal(row.event.GetPlayers(), final.GetPlayerIds()) ||
		row.event.GetOutcome() != eventsv1.GameFinished_OUTCOME_WIN || final.GetPlayerIds()[row.event.GetWinnerSeat()] != winner.String() {
		t.Fatalf("game.outbox row = id %s (v%d) aggregatetype %s aggregateid %s type %s event %v", row.id, row.id.Version(), row.topic, row.key, row.kind, row.event)
	}
	t.Logf("game.outbox: id=%s (uuid v%d) aggregatetype=%s aggregateid=%s type=%s rated=%t winner_seat=%d players=%v (+%s after the last move)",
		row.id, row.id.Version(), row.topic, row.key, row.kind, row.event.GetRated(), row.event.GetWinnerSeat(), row.event.GetPlayers(), outboxAfter.Round(time.Millisecond))

	finishedRecord, err := findRecord(ctx, s.brokers, ledger.FinishedTopic, func(record *kgo.Record) bool {
		return headerOf(record, outbox.IDHeader) == row.id.String()
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(finishedRecord.Key) != quickGame || headerOf(finishedRecord, outbox.TypeHeader) != finishedType || !slices.Equal(finishedRecord.Value, row.payload) {
		t.Fatalf("game.finished.v1 record = %s", describeRecord(finishedRecord))
	}
	t.Logf("kafka: %s, value equals the outbox payload", describeRecord(finishedRecord))

	var processedSession uuid.UUID
	waitFor(t, 60*time.Second, "rating.processed_events", func() bool {
		err := s.pool.QueryRow(ctx, `SELECT session_id FROM rating.processed_events WHERE event_id = $1`, row.id).Scan(&processedSession)
		return err == nil
	})
	if processedSession != session {
		t.Fatalf("processed_events maps %s to %s", row.id, processedSession)
	}
	type live struct {
		rating, deviation, volatility float64
		games, wins, losses           int
	}
	ratingOf := func(user uuid.UUID) live {
		var r live
		if err := s.pool.QueryRow(ctx, `SELECT rating, deviation, volatility, games, wins, losses FROM rating.ratings WHERE game = $1 AND user_id = $2`, quickGame, user).
			Scan(&r.rating, &r.deviation, &r.volatility, &r.games, &r.wins, &r.losses); err != nil {
			t.Fatal(err)
		}
		return r
	}
	won, lost := ratingOf(winner), ratingOf(loser)
	if math.Round(won.rating) != 1362 || math.Round(lost.rating) != 1038 || won.wins != 1 || lost.losses != 1 || won.games != 1 || lost.games != 1 {
		t.Fatalf("rating.ratings winner %+v loser %+v", won, lost)
	}
	var changes int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM rating.changes WHERE session_id = $1`, session).Scan(&changes); err != nil || changes != 2 {
		t.Fatalf("rating.changes for the session = %d, %v", changes, err)
	}
	t.Logf("rating.processed_events: %s -> %s; rating.ratings: winner %.2f (RD %.2f, σ %.6f), loser %.2f (RD %.2f, σ %.6f); rating.changes: %d rows",
		row.id, processedSession, won.rating, won.deviation, won.volatility, lost.rating, lost.deviation, lost.volatility, changes)

	changedID := ledger.ChangedEventID(row.id)
	changedRecord, err := findRecord(ctx, s.brokers, ledger.ChangedTopic, func(record *kgo.Record) bool {
		return headerOf(record, outbox.IDHeader) == changedID.String()
	})
	if err != nil {
		t.Fatal(err)
	}
	changed := &eventsv1.RatingChanged{}
	if err := proto.Unmarshal(changedRecord.Value, changed); err != nil || string(changedRecord.Key) != quickGame ||
		headerOf(changedRecord, outbox.TypeHeader) != changedType || changed.GetSessionId() != session.String() || changed.GetSourceEventId() != row.id.String() {
		t.Fatalf("rating.changed.v1 record %s = %v (%v)", describeRecord(changedRecord), changed, err)
	}
	announced := map[string]string{}
	for _, change := range changed.GetChanges() {
		announced[change.GetUserId()] = fmt.Sprintf("%d->%d", change.GetBefore(), change.GetAfter())
	}
	if announced[winner.String()] != "1200->1362" || announced[loser.String()] != "1200->1038" {
		t.Fatalf("rating.changed.v1 changes = %v", announced)
	}
	t.Logf("kafka: %s, changes winner %s loser %s", describeRecord(changedRecord), announced[winner.String()], announced[loser.String()])

	local := map[uuid.UUID]int{}
	caughtUp := waitFor(t, 60*time.Second, "game.ratings to follow rating.changed", func() bool {
		rows, err := s.pool.Query(ctx, `SELECT user_id, rating FROM game.ratings WHERE game = $1 AND user_id = ANY($2)`, quickGame, []uuid.UUID{winner, loser})
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		for rows.Next() {
			var user uuid.UUID
			var value int
			if err := rows.Scan(&user, &value); err != nil {
				t.Fatal(err)
			}
			local[user] = value
		}
		return local[winner] == 1362 && local[loser] == 1038
	})
	var localChanges int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM game.rating_changes WHERE session_id = $1`, session).Scan(&localChanges); err != nil || localChanges != 2 {
		t.Fatalf("game.rating_changes for the session = %d, %v", localChanges, err)
	}
	t.Logf("game.ratings: winner %d loser %d, game.rating_changes: %d rows (polled %s, %s after the last move)",
		local[winner], local[loser], localChanges, caughtUp.Round(time.Millisecond), time.Since(ended).Round(time.Millisecond))

	for seat, result := range results {
		if result.rated == nil {
			t.Fatalf("player %d never saw the rating delta on the open game socket", seat)
		}
		deltas := map[string]int{}
		for _, change := range result.rated.GetRatingChanges() {
			deltas[change.GetUserId()] = int(change.GetAfter()) - int(change.GetBefore())
		}
		if deltas[winner.String()] != 162 || deltas[loser.String()] != -162 {
			t.Fatalf("socket rating changes = %v", result.rated.GetRatingChanges())
		}
		t.Logf("socket: player %d received the rating push %s after the final board", seat, result.ratedAt.Sub(result.ended).Round(time.Millisecond))
	}

	got, err := s.game.GetRatings(ctx, authorized(a, &gamev1.GetRatingsRequest{Game: quickGame, UserIds: []string{winner.String(), loser.String()}}))
	if err != nil {
		t.Fatal(err)
	}
	through := map[string]*gamev1.Rating{}
	for _, item := range got.Msg.GetRatings() {
		through[item.GetUserId()] = item
	}
	if through[winner.String()].GetRating() != 1362 || through[loser.String()].GetRating() != 1038 || through[winner.String()].GetWins() != 1 || through[loser.String()].GetLosses() != 1 {
		t.Fatalf("GetRatings through the gateway = %v", got.Msg.GetRatings())
	}
	t.Logf("gateway GetRatings: winner %d (%d-%d), loser %d (%d-%d)", through[winner.String()].GetRating(), through[winner.String()].GetWins(), through[winner.String()].GetLosses(),
		through[loser.String()].GetRating(), through[loser.String()].GetWins(), through[loser.String()].GetLosses())

	var board []*ratingv1.LeaderboardEntry
	waitFor(t, 30*time.Second, "the tic-tac-toe leaderboard", func() bool {
		res, err := s.rating.GetLeaderboard(ctx, connect.NewRequest(&ratingv1.GetLeaderboardRequest{Game: quickGame, Limit: 100}))
		if err != nil {
			t.Fatal(err)
		}
		board = res.Msg.GetEntries()
		seen := 0
		for _, entry := range board {
			if entry.GetUserId() == winner.String() || entry.GetUserId() == loser.String() {
				seen++
			}
		}
		return seen == 2
	})
	ranks := map[string]*ratingv1.LeaderboardEntry{}
	for _, entry := range board {
		ranks[entry.GetUserId()] = entry
	}
	top, bottom := ranks[winner.String()], ranks[loser.String()]
	if top.GetRank() >= bottom.GetRank() || top.GetWins() != 1 || bottom.GetLosses() != 1 || top.GetRating() != 1362 || bottom.GetRating() != 1038 {
		t.Fatalf("leaderboard = %v", board)
	}
	t.Logf("rating GetLeaderboard: winner rank %d rating %d, loser rank %d rating %d, %d entries", top.GetRank(), top.GetRating(), bottom.GetRank(), bottom.GetRating(), len(board))
}

func TestRatedGamesSurviveAnOutboxOutage(t *testing.T) {
	want, err := strconv.Atoi(os.Getenv("RATING_E2E_OUTAGE_GAMES"))
	if err != nil || want <= 0 {
		t.Skip("RATING_E2E_OUTAGE_GAMES is unset")
	}
	s := newStand(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Minute)
	defer cancel()
	players := make([]gamer, 10)
	ids := make([]uuid.UUID, len(players))
	for i := range players {
		players[i] = s.register(t, fmt.Sprintf("Outage %02d", i))
		ids[i] = players[i].id
	}

	var sessions []uuid.UUID
	outcomes := map[gamev1.SessionStatus]int{}
	offline := 0
	started := time.Now()
	for round := 1; len(sessions) < want; round++ {
		if round > 1 {
			select {
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			case <-time.After(time.Until(started.Add(time.Duration(round-1) * roundEvery))):
			}
		}
		results, err := s.round(ctx, players, 0)
		if err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
		for session, seats := range results {
			if len(seats) != 2 || seats[0].final == nil || !seats[0].final.GetRated() {
				t.Fatalf("round %d session %s: %d players reported, final %v", round, session, len(seats), seats[0].final)
			}
			sessions = append(sessions, session)
			outcomes[seats[0].final.GetStatus()]++
		}
		var streaming bool
		if err := s.pool.QueryRow(ctx, `SELECT COALESCE(bool_or(active), false) FROM pg_replication_slots WHERE slot_name = 'axon_outbox'`).Scan(&streaming); err != nil {
			t.Fatal(err)
		}
		if !streaming {
			offline += len(results)
		}
		t.Logf("%s round %d: %d games, %d in total, debezium slot active=%t", time.Now().Format(time.TimeOnly), round, len(results), len(sessions), streaming)
	}
	lastMove := time.Now()
	t.Logf("%d rated games in %s: %d won, %d drawn, %d finished while the debezium slot was inactive", len(sessions), lastMove.Sub(started).Round(time.Second),
		outcomes[gamev1.SessionStatus_SESSION_STATUS_WON], outcomes[gamev1.SessionStatus_SESSION_STATUS_DRAW], offline)

	var finished int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM game.sessions WHERE rated AND opponent_id IS NOT NULL AND state->>'status' <> 'in_progress'
		AND creator_id = ANY($1) AND opponent_id = ANY($1)`, ids).Scan(&finished); err != nil || finished != len(sessions) {
		t.Fatalf("game.sessions holds %d rated finished games of these players, played %d (%v)", finished, len(sessions), err)
	}

	count := func(query string) int {
		var n int
		if err := s.pool.QueryRow(ctx, query, sessions).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	settled := waitFor(t, 5*time.Minute, "rating to process every finished game", func() bool {
		return count(`SELECT count(*) FROM rating.processed_events WHERE session_id = ANY($1)`) == len(sessions)
	})
	t.Logf("rating.processed_events caught up %s after the last game", settled.Round(time.Second))
	waitFor(t, 2*time.Minute, "game to follow every rating change", func() bool {
		return count(`SELECT count(*) FROM game.rating_changes WHERE session_id = ANY($1)`) == 2*len(sessions)
	})

	if n := count(`SELECT count(*) FROM (SELECT session_id FROM rating.processed_events WHERE session_id = ANY($1) GROUP BY session_id HAVING count(*) <> 1) d`); n != 0 {
		t.Fatalf("%d sessions are in processed_events more than once", n)
	}
	if n := count(`SELECT count(DISTINCT session_id) FROM rating.processed_events WHERE session_id = ANY($1)`); n != len(sessions) {
		t.Fatalf("processed_events holds %d of %d sessions", n, len(sessions))
	}
	if n := count(`SELECT count(*) FROM (SELECT session_id FROM rating.changes WHERE session_id = ANY($1) GROUP BY session_id HAVING count(*) <> 2) d`); n != 0 {
		t.Fatalf("%d sessions do not have exactly two rating.changes rows", n)
	}
	if n := count(`SELECT count(*) FROM rating.changes WHERE session_id = ANY($1)`); n != 2*len(sessions) {
		t.Fatalf("rating.changes holds %d rows for %d games", n, len(sessions))
	}
	if n := count(`SELECT count(*) FROM (SELECT session_id, user_id FROM game.rating_changes WHERE session_id = ANY($1) GROUP BY 1, 2 HAVING count(*) > 1) d`); n != 0 {
		t.Fatalf("%d duplicate game.rating_changes rows", n)
	}

	var mismatched []string
	rows, err := s.pool.Query(ctx, `SELECT r.user_id, g.rating, round(r.rating)::int, g.games, r.games, g.wins, r.wins, g.losses, r.losses, g.draws, r.draws
		FROM rating.ratings r LEFT JOIN game.ratings g ON g.user_id = r.user_id AND g.game = r.game
		WHERE r.game = $1 AND r.user_id = ANY($2)`, quickGame, ids)
	if err != nil {
		t.Fatal(err)
	}
	compared := 0
	for rows.Next() {
		var user uuid.UUID
		var local, games, wins, losses, draws *int
		var remote, rGames, rWins, rLosses, rDraws int
		if err := rows.Scan(&user, &local, &remote, &games, &rGames, &wins, &rWins, &losses, &rLosses, &draws, &rDraws); err != nil {
			t.Fatal(err)
		}
		compared++
		if local == nil || *local != remote || *games != rGames || *wins != rWins || *losses != rLosses || *draws != rDraws {
			mismatched = append(mismatched, fmt.Sprintf("%s game=%v rating=%d", user, local, remote))
		}
	}
	rows.Close()
	if compared != len(players) || len(mismatched) > 0 {
		t.Fatalf("game.ratings and rating.ratings differ for %d of %d players: %v", len(mismatched), compared, mismatched)
	}

	events := map[string]bool{}
	for _, session := range sessions {
		row, ok, err := s.finishedRow(ctx, session)
		if err != nil || !ok {
			t.Fatalf("no game.outbox row for %s: %v", session, err)
		}
		events[row.id.String()] = true
	}
	delivered := map[string]int{}
	scanCtx, stopScan := context.WithTimeout(ctx, time.Minute)
	defer stopScan()
	if err := scanTopic(scanCtx, s.brokers, ledger.FinishedTopic, func(record *kgo.Record) {
		if id := headerOf(record, outbox.IDHeader); events[id] {
			delivered[id]++
		}
	}); err != nil {
		t.Fatal(err)
	}
	copies := 0
	for _, n := range delivered {
		copies += n
	}
	if len(delivered) != len(sessions) {
		t.Fatalf("game.finished.v1 carries %d of %d events", len(delivered), len(sessions))
	}
	t.Logf("%d games: processed_events %d, rating.changes %d, game.rating_changes %d, players compared %d; game.finished.v1 holds %d records for %d events (%d redelivered)",
		len(sessions), count(`SELECT count(*) FROM rating.processed_events WHERE session_id = ANY($1)`), count(`SELECT count(*) FROM rating.changes WHERE session_id = ANY($1)`),
		count(`SELECT count(*) FROM game.rating_changes WHERE session_id = ANY($1)`), compared, copies, len(delivered), copies-len(delivered))
}
