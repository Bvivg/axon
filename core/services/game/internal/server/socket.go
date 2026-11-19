package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"golang.org/x/time/rate"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/bvivg/axon/core/services/game/internal/engine"
	"github.com/bvivg/axon/core/services/game/internal/games/chess"
	"github.com/bvivg/axon/core/services/game/internal/games/tictactoe"
	"github.com/bvivg/axon/core/services/game/internal/session"
	gamev1 "github.com/bvivg/axon/core/shared/gen/go/axon/game/v1"
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

	read := func(readCtx context.Context) (*gamev1.SessionCommand, error) {
		frame, err := conn.Read(readCtx)
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
	push := func(item session.Session, err error) error {
		state, err := h.view(ctx, item, err)
		if err != nil {
			return err
		}
		return write(&gamev1.SessionEvent{Update: &gamev1.SessionEvent_Session{Session: state}})
	}
	changed, unsubscribe := h.updates.subscribe(id)
	defer unsubscribe()
	initial, err := h.store.Get(ctx, id, user)
	if err != nil {
		_ = fail(moveError(err))
		return
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
		case <-changed:
			if push(h.store.Get(ctx, id, user)) != nil {
				return
			}
		case next := <-commands:
			if next.limited {
				if fail(gamev1.SessionError_SESSION_ERROR_RATE_LIMITED) != nil {
					return
				}
				continue
			}
			command := next.command
			var item session.Session
			var err error
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
				item, err = h.store.Move(ctx, id, user, int(move.Ply), tictactoe.Key, data, move.Revision)
			case *gamev1.SessionCommand_ChessMove:
				move := action.ChessMove
				if move == nil || len(move.Uci) > 5 {
					if fail(gamev1.SessionError_SESSION_ERROR_INVALID_COMMAND) != nil {
						return
					}
					continue
				}
				data, _ := json.Marshal(chess.MoveData{UCI: move.Uci})
				item, err = h.store.Move(ctx, id, user, int(move.Ply), chess.Key, data, move.Revision)
			case *gamev1.SessionCommand_Draw:
				drawAction, ok := drawActions[action.Draw.GetAction()]
				if !ok {
					if fail(gamev1.SessionError_SESSION_ERROR_INVALID_COMMAND) != nil {
						return
					}
					continue
				}
				item, err = h.store.Draw(ctx, id, user, drawAction)
			case *gamev1.SessionCommand_Undo:
				if action.Undo == nil {
					if fail(gamev1.SessionError_SESSION_ERROR_INVALID_COMMAND) != nil {
						return
					}
					continue
				}
				item, err = h.store.Undo(ctx, id, user, action.Undo.Revision)
			default:
				if fail(gamev1.SessionError_SESSION_ERROR_INVALID_COMMAND) != nil {
					return
				}
				continue
			}
			if err != nil {
				if fail(moveError(err)) != nil || push(h.store.Get(ctx, id, user)) != nil {
					return
				}
				continue
			}
			h.updates.notify(id)
			if push(item, nil) != nil {
				return
			}
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
	case errors.Is(err, chess.ErrIllegalMove), errors.Is(err, chess.ErrBadMove):
		return gamev1.SessionError_SESSION_ERROR_ILLEGAL_MOVE
	case errors.Is(err, session.ErrNoDrawOffer):
		return gamev1.SessionError_SESSION_ERROR_NO_DRAW_OFFER
	case errors.Is(err, engine.ErrGameMismatch), errors.Is(err, engine.ErrMoveMalformed):
		return gamev1.SessionError_SESSION_ERROR_INVALID_COMMAND
	case errors.Is(err, session.ErrRated):
		return gamev1.SessionError_SESSION_ERROR_UNDO_DISABLED
	case errors.Is(err, session.ErrNotPlaying), errors.Is(err, engine.ErrGameOver):
		return gamev1.SessionError_SESSION_ERROR_NOT_PLAYING
	default:
		return gamev1.SessionError_SESSION_ERROR_UNAVAILABLE
	}
}

func (h *Handler) watchMatch(ctx context.Context, cancel context.CancelFunc, conn *ws.Conn, user uuid.UUID, game engine.Key, write func(*gamev1.SessionEvent) error) {
	changed, unsubscribe := h.matches.subscribe(user)
	defer unsubscribe()
	go func() {
		defer cancel()
		for {
			if _, err := conn.Read(ctx); err != nil {
				return
			}
		}
	}()
	push := func() error {
		found, err := h.store.Ticket(ctx, user, game)
		ticket, err := ticketView(string(game), found, err)
		if err != nil {
			return err
		}
		return write(&gamev1.SessionEvent{Update: &gamev1.SessionEvent_Match{Match: ticket}})
	}
	if h.store.Touch(ctx, user, game) != nil || push() != nil {
		return
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-changed:
			if push() != nil {
				return
			}
		case <-ticker.C:
			if h.store.Touch(ctx, user, game) != nil || push() != nil {
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
