package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/time/rate"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/bvivg/axon/core/services/game/internal/engine"
	"github.com/bvivg/axon/core/services/game/internal/games/checkers"
	"github.com/bvivg/axon/core/services/game/internal/games/chess"
	"github.com/bvivg/axon/core/services/game/internal/games/durak"
	"github.com/bvivg/axon/core/services/game/internal/games/quoridor"
	"github.com/bvivg/axon/core/services/game/internal/games/tictactoe"
	"github.com/bvivg/axon/core/services/game/internal/owner"
	"github.com/bvivg/axon/core/services/game/internal/session"
	gamev1 "github.com/bvivg/axon/core/shared/gen/go/axon/game/v1"
	"github.com/bvivg/axon/core/shared/pkg/telemetry"
	"github.com/bvivg/axon/core/shared/pkg/ws"
)

func (h *Handler) Socket(w http.ResponseWriter, r *http.Request) {
	user, claims, err := h.identity(r.Header)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	conn, err := ws.Accept(w, r, ws.AcceptOptions{
		Subprotocols: []string{Protocol},
		Options:      ws.Options{ReadLimit: 1024},
	})
	if err != nil {
		return
	}
	defer func() { _ = conn.CloseNow() }()
	ctx, cancel := context.WithDeadline(r.Context(), claims.ExpiresAt)
	defer cancel()
	invalidCommand := errors.New("invalid session command")

	var frame []byte
	read := func(readCtx context.Context) (*gamev1.SessionCommand, error) {
		var err error
		frame, err = conn.Read(readCtx)
		if err != nil {
			return nil, err
		}
		command := &gamev1.SessionCommand{}
		if protojson.Unmarshal(frame, command) != nil {
			return nil, invalidCommand
		}
		return command, nil
	}
	write := func(event *gamev1.SessionEvent) error {
		data, err := protojson.Marshal(event)
		if err != nil {
			return err
		}
		return conn.Write(ctx, data)
	}
	fail := func(code gamev1.SessionError) error {
		return write(&gamev1.SessionEvent{Update: &gamev1.SessionEvent_Error{Error: code}})
	}
	firstCtx, stopFirst := context.WithTimeout(ctx, 5*time.Second)
	first, err := read(firstCtx)
	stopFirst()
	if err != nil {
		if errors.Is(err, invalidCommand) {
			_ = fail(gamev1.SessionError_SESSION_ERROR_INVALID_COMMAND)
		}
		return
	}
	if game := first.GetSubscribeMatch(); game != "" {
		h.watchMatch(ctx, cancel, conn, user, engine.Key(game), write)
		return
	}
	id, err := sessionID(first.GetSubscribe())
	if err != nil {
		_ = fail(gamev1.SessionError_SESSION_ERROR_INVALID_COMMAND)
		return
	}
	moved := func() { _ = conn.Close(StatusMoved, "game moved") }
	changed, gone, unsubscribe := h.updates.subscribe(id)
	defer unsubscribe()
	if !h.owner.Serves(id) {
		if r.Header.Get(HopHeader) != "" {
			moved()
			return
		}
		h.relay(ctx, conn, id, r.Header, frame)
		return
	}
	push := func(item session.Session, err error) error {
		if errors.Is(err, owner.ErrNotOwner) {
			moved()
			return err
		}
		state, err := h.view(ctx, item, user, err)
		if err != nil {
			return err
		}
		state.Spectators = proto.Uint32(h.watchers.count(id))
		return write(&gamev1.SessionEvent{Update: &gamev1.SessionEvent_Session{Session: state}})
	}
	initial, err := h.owner.Get(ctx, id, user)
	if errors.Is(err, owner.ErrNotOwner) {
		moved()
		return
	}
	if err != nil {
		_ = fail(moveError(err))
		return
	}
	spectator := !initial.HasPlayer(user)
	if spectator {
		if !h.watchers.join(id) {
			_ = fail(gamev1.SessionError_SESSION_ERROR_UNAVAILABLE)
			return
		}
		h.metrics.Spectators(h.watchers.total())
		defer func() {
			h.watchers.leave(id)
			h.metrics.Spectators(h.watchers.total())
			h.updates.notify(id)
		}()
		h.updates.notify(id)
	}
	if push(initial, nil) != nil {
		return
	}
	commands := make(chan incoming)
	go func() {
		defer cancel()
		limiter, flood := rate.NewLimiter(4, 8), rate.NewLimiter(20, 40)
		for {
			command, err := read(ctx)
			if err != nil && !errors.Is(err, invalidCommand) {
				return
			}
			if !flood.Allow() {
				_ = conn.Close(ws.StatusPolicyViolation, "Too many commands")
				return
			}
			select {
			case commands <- incoming{command: command, limited: !limiter.Allow()}:
			case <-ctx.Done():
				return
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-gone:
			moved()
			return
		case <-changed:
			if push(h.owner.Get(ctx, id, user)) != nil {
				return
			}
		case next := <-commands:
			if next.limited {
				if fail(gamev1.SessionError_SESSION_ERROR_RATE_LIMITED) != nil {
					return
				}
				continue
			}
			if spectator {
				if fail(gamev1.SessionError_SESSION_ERROR_SPECTATOR) != nil {
					return
				}
				continue
			}
			command := next.command
			var item session.Session
			var err error
			var name string
			var run func(context.Context) (session.Session, error)
			switch action := command.GetAction().(type) {
			case *gamev1.SessionCommand_Move:
				move := action.Move
				if move == nil || move.Cell >= tictactoe.Size*tictactoe.Size {
					if fail(gamev1.SessionError_SESSION_ERROR_INVALID_COMMAND) != nil {
						return
					}
					continue
				}
				data, _ := json.Marshal(tictactoe.Cell{Row: int(move.Cell) / tictactoe.Size, Col: int(move.Cell) % tictactoe.Size})
				name, run = "move", func(ctx context.Context) (session.Session, error) {
					return h.owner.Move(ctx, id, user, int(move.Ply), tictactoe.Key, data, move.Revision)
				}
			case *gamev1.SessionCommand_ChessMove:
				move := action.ChessMove
				if move == nil || len(move.Uci) > 5 {
					if fail(gamev1.SessionError_SESSION_ERROR_INVALID_COMMAND) != nil {
						return
					}
					continue
				}
				data, _ := json.Marshal(chess.MoveData{UCI: move.Uci})
				name, run = "move", func(ctx context.Context) (session.Session, error) {
					return h.owner.Move(ctx, id, user, int(move.Ply), chess.Key, data, move.Revision)
				}
			case *gamev1.SessionCommand_QuoridorMove:
				move := action.QuoridorMove
				if move == nil || len(move.Move) > 3 {
					if fail(gamev1.SessionError_SESSION_ERROR_INVALID_COMMAND) != nil {
						return
					}
					continue
				}
				data, _ := json.Marshal(quoridor.MoveData{Move: move.Move})
				name, run = "move", func(ctx context.Context) (session.Session, error) {
					return h.owner.Move(ctx, id, user, int(move.Ply), quoridor.Key, data, move.Revision)
				}
			case *gamev1.SessionCommand_CheckersMove:
				move := action.CheckersMove
				if move == nil || len(move.Move) > checkers.MaxNotation {
					if fail(gamev1.SessionError_SESSION_ERROR_INVALID_COMMAND) != nil {
						return
					}
					continue
				}
				data, _ := json.Marshal(checkers.MoveData{Move: move.Move})
				name, run = "move", func(ctx context.Context) (session.Session, error) {
					return h.owner.Move(ctx, id, user, int(move.Ply), checkers.Key, data, move.Revision)
				}
			case *gamev1.SessionCommand_DurakMove:
				move := action.DurakMove
				if move == nil || len(move.Move) > durak.MaxNotation {
					if fail(gamev1.SessionError_SESSION_ERROR_INVALID_COMMAND) != nil {
						return
					}
					continue
				}
				data, _ := json.Marshal(durak.MoveData{Move: move.Move})
				name, run = "move", func(ctx context.Context) (session.Session, error) {
					return h.owner.Move(ctx, id, user, int(move.Ply), durak.Key, data, move.Revision)
				}
			case *gamev1.SessionCommand_Draw:
				drawAction, ok := drawActions[action.Draw.GetAction()]
				if !ok {
					if fail(gamev1.SessionError_SESSION_ERROR_INVALID_COMMAND) != nil {
						return
					}
					continue
				}
				name, run = "draw", func(ctx context.Context) (session.Session, error) {
					return h.owner.Draw(ctx, id, user, drawAction)
				}
			case *gamev1.SessionCommand_Undo:
				if action.Undo == nil {
					if fail(gamev1.SessionError_SESSION_ERROR_INVALID_COMMAND) != nil {
						return
					}
					continue
				}
				name, run = "undo", func(ctx context.Context) (session.Session, error) {
					return h.owner.Undo(ctx, id, user, action.Undo.Revision)
				}
			default:
				if fail(gamev1.SessionError_SESSION_ERROR_INVALID_COMMAND) != nil {
					return
				}
				continue
			}
			item, err = h.command(ctx, id, name, command.GetTraceparent(), run)
			if errors.Is(err, owner.ErrNotOwner) {
				moved()
				return
			}
			if err != nil {
				if fail(moveError(err)) != nil || push(h.owner.Get(ctx, id, user)) != nil {
					return
				}
				continue
			}
			if push(item, nil) != nil {
				return
			}
		}
	}
}

func (h *Handler) command(ctx context.Context, id uuid.UUID, name, traceparent string, run func(context.Context) (session.Session, error)) (session.Session, error) {
	ctx, span := tracer.Start(telemetry.WithTraceparent(ctx, traceparent), "game.command "+name,
		trace.WithSpanKind(trace.SpanKindServer), trace.WithAttributes(attribute.String("game.session_id", id.String())))
	defer span.End()
	started := time.Now()
	item, err := run(ctx)
	h.metrics.Command(ctx, name, time.Since(started), err)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	return item, err
}

var tracer = telemetry.Tracer("axon/game/server")

const StatusMoved ws.StatusCode = 4001

func (h *Handler) relay(ctx context.Context, client *ws.Conn, id uuid.UUID, incoming http.Header, first []byte) {
	routing, cancel := context.WithTimeout(ctx, routeTimeout)
	address, mine, err := h.ownerAddress(routing, id)
	cancel()
	if err != nil || mine {
		_ = client.Close(StatusMoved, "game moved")
		return
	}
	header := http.Header{}
	header.Set("Authorization", incoming.Get("Authorization"))
	header.Set(HopHeader, "1")
	if user := incoming.Get(TestUserHeader); user != "" && h.hooks {
		header.Set(TestUserHeader, user)
	}
	socketScheme := "ws"
	if h.scheme == "https" {
		socketScheme = "wss"
	}
	upstream, err := ws.Dial(ctx, socketScheme+"://"+address+"/ws", ws.DialOptions{
		HTTPClient:   h.sockets,
		Subprotocols: []string{Protocol},
		Header:       header,
		Options:      ws.Options{ReadLimit: ws.LargeMessageReadLimit},
	})
	if err != nil {
		_ = client.Close(StatusMoved, "game moved")
		return
	}
	defer func() { _ = upstream.CloseNow() }()
	if upstream.Write(ctx, first) != nil {
		_ = client.Close(StatusMoved, "game moved")
		return
	}
	join(ctx, client, upstream)
}

func join(ctx context.Context, client, upstream *ws.Conn) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{}, 2)
	go func() { pipe(ctx, client, upstream); done <- struct{}{} }()
	go func() { pipe(ctx, upstream, client); done <- struct{}{} }()
	<-done
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}
}

func pipe(ctx context.Context, from, to *ws.Conn) {
	for {
		payload, err := from.Read(ctx)
		if err != nil {
			code, reason := ws.CloseCodeFor(err)
			if status := ws.CloseStatus(err); status != -1 {
				code, reason = status, ""
			}
			_ = to.Close(code, ws.TruncateReason(reason))
			return
		}
		if err := to.Write(ctx, payload); err != nil {
			_ = from.CloseNow()
			return
		}
	}
}

type incoming struct {
	command *gamev1.SessionCommand
	limited bool
}

func moveError(err error) gamev1.SessionError {
	switch {
	case errors.Is(err, session.ErrNotFound), errors.Is(err, engine.ErrPlayerNotInGame):
		return gamev1.SessionError_SESSION_ERROR_NOT_FOUND
	case errors.Is(err, engine.ErrOutOfTurn):
		return gamev1.SessionError_SESSION_ERROR_NOT_YOUR_TURN
	case errors.Is(err, engine.ErrStaleMove):
		return gamev1.SessionError_SESSION_ERROR_STALE_MOVE
	case errors.Is(err, tictactoe.ErrCellTaken):
		return gamev1.SessionError_SESSION_ERROR_CELL_TAKEN
	case errors.Is(err, chess.ErrIllegalMove), errors.Is(err, chess.ErrBadMove), errors.Is(err, quoridor.ErrIllegalMove), errors.Is(err, quoridor.ErrBadMove),
		errors.Is(err, checkers.ErrIllegalMove), errors.Is(err, checkers.ErrBadMove),
		errors.Is(err, durak.ErrIllegalMove), errors.Is(err, durak.ErrBadMove):
		return gamev1.SessionError_SESSION_ERROR_ILLEGAL_MOVE
	case errors.Is(err, session.ErrNoDrawOffer):
		return gamev1.SessionError_SESSION_ERROR_NO_DRAW_OFFER
	case errors.Is(err, engine.ErrGameMismatch), errors.Is(err, engine.ErrMoveMalformed):
		return gamev1.SessionError_SESSION_ERROR_INVALID_COMMAND
	case errors.Is(err, session.ErrRated), errors.Is(err, session.ErrNoUndo):
		return gamev1.SessionError_SESSION_ERROR_UNDO_DISABLED
	case errors.Is(err, session.ErrNotPlaying), errors.Is(err, engine.ErrGameOver):
		return gamev1.SessionError_SESSION_ERROR_NOT_PLAYING
	default:
		return gamev1.SessionError_SESSION_ERROR_UNAVAILABLE
	}
}

func (h *Handler) watchMatch(ctx context.Context, cancel context.CancelFunc, conn *ws.Conn, user uuid.UUID, game engine.Key, write func(*gamev1.SessionEvent) error) {
	if _, err := h.store.Games().Definition(game); err != nil {
		_ = write(&gamev1.SessionEvent{Update: &gamev1.SessionEvent_Error{Error: gamev1.SessionError_SESSION_ERROR_INVALID_COMMAND}})
		return
	}
	search, ok := h.searches.join(matchKey{user: user, game: game})
	if !ok {
		_ = write(&gamev1.SessionEvent{Update: &gamev1.SessionEvent_Error{Error: gamev1.SessionError_SESSION_ERROR_RATE_LIMITED}})
		_ = conn.Close(ws.StatusPolicyViolation, "Too many match searches")
		return
	}
	defer search.leave()
	go func() {
		defer cancel()
		for {
			if _, err := conn.Read(ctx); err != nil {
				return
			}
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case <-search.signal:
			ticket, err := search.latest()
			if err != nil || write(&gamev1.SessionEvent{Update: &gamev1.SessionEvent_Match{Match: ticket}}) != nil {
				return
			}
		}
	}
}

var drawActions = map[gamev1.DrawAction]session.DrawAction{
	gamev1.DrawAction_DRAW_ACTION_OFFER:   session.OfferDraw,
	gamev1.DrawAction_DRAW_ACTION_ACCEPT:  session.AcceptDraw,
	gamev1.DrawAction_DRAW_ACTION_DECLINE: session.DeclineDraw,
}
