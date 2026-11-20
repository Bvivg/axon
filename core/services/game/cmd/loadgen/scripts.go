package main

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"

	"github.com/bvivg/axon/core/services/game/internal/engine"
	"github.com/bvivg/axon/core/services/game/internal/games/chess"
	"github.com/bvivg/axon/core/services/game/internal/games/quoridor"
	"github.com/bvivg/axon/core/services/game/internal/games/tictactoe"
)

type script struct {
	game  engine.Key
	moves []string
	loser int
}

var seats = []engine.PlayerID{"first", "second"}

func registry() (*engine.Registry, error) {
	games := engine.NewRegistry()
	for _, def := range []engine.Definition{tictactoe.Definition(), chess.Definition(), quoridor.Definition()} {
		if err := games.Register(def); err != nil {
			return nil, err
		}
	}
	return games, nil
}

func simulate(games *engine.Registry, key engine.Key, limit int, pick func(state engine.State, legal []engine.Move) engine.Move, label func(engine.Move) (string, error)) (script, bool, error) {
	game, err := games.Get(key)
	if err != nil {
		return script{}, false, err
	}
	state := game.Init(seats)
	out := script{game: key, loser: -1}
	for !state.Over() {
		if len(out.moves) >= limit {
			return script{}, false, nil
		}
		legal := game.ValidMoves(state, seats[state.Turn])
		if len(legal) == 0 {
			return script{}, false, nil
		}
		move := pick(state, legal)
		name, err := label(move)
		if err != nil {
			return script{}, false, err
		}
		if state, err = game.ApplyMove(state, move); err != nil {
			return script{}, false, err
		}
		out.moves = append(out.moves, name)
	}
	return out, true, nil
}

func tictactoeScripts(games *engine.Registry, rng *rand.Rand, n int) ([]script, error) {
	label := func(move engine.Move) (string, error) {
		var cell tictactoe.Cell
		if err := json.Unmarshal(move.Data, &cell); err != nil {
			return "", err
		}
		return fmt.Sprint(cell.Row*tictactoe.Size + cell.Col), nil
	}
	pick := func(_ engine.State, legal []engine.Move) engine.Move { return legal[rng.IntN(len(legal))] }
	return collect(n, func() (script, bool, error) { return simulate(games, tictactoe.Key, 9, pick, label) })
}

func quoridorScripts(games *engine.Registry, rng *rand.Rand, n int) ([]script, error) {
	name := func(move engine.Move) (string, error) {
		var data quoridor.MoveData
		err := json.Unmarshal(move.Data, &data)
		return data.Move, err
	}
	pick := func(state engine.State, legal []engine.Move) engine.Move {
		var walls, best []engine.Move
		bestGap := 1 << 30
		goal := quoridor.GoalRow(state.Turn)
		for _, move := range legal {
			raw, _ := name(move)
			parsed, err := quoridor.ParseMove(raw)
			if err != nil {
				continue
			}
			if parsed.Wall != nil {
				walls = append(walls, move)
				continue
			}
			gap := parsed.To.Row() - goal
			if gap < 0 {
				gap = -gap
			}
			switch {
			case gap < bestGap:
				bestGap, best = gap, []engine.Move{move}
			case gap == bestGap:
				best = append(best, move)
			}
		}
		if len(walls) > 0 && (len(best) == 0 || rng.IntN(8) == 0) {
			return walls[rng.IntN(len(walls))]
		}
		return best[rng.IntN(len(best))]
	}
	return collect(n, func() (script, bool, error) { return simulate(games, quoridor.Key, 160, pick, name) })
}

func collect(n int, build func() (script, bool, error)) ([]script, error) {
	var out []script
	for attempts := 0; len(out) < n && attempts < n*50; attempts++ {
		s, ok, err := build()
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no playable scripts")
	}
	return out, nil
}

type legend struct {
	Name   string   `json:"name"`
	Result string   `json:"result"`
	UCI    []string `json:"uci"`
}

func chessScripts(games *engine.Registry, path string) ([]script, error) {
	raw, err := os.ReadFile(path) //nolint:gosec
	if err != nil {
		return nil, err
	}
	var legends []legend
	if err := json.Unmarshal(raw, &legends); err != nil {
		return nil, err
	}
	game, err := games.Get(chess.Key)
	if err != nil {
		return nil, err
	}
	var out []script
	for _, item := range legends {
		state := game.Init(seats)
		played := 0
		for _, uci := range item.UCI {
			if state.Over() {
				break
			}
			next, err := game.ApplyMove(state, chess.NewMove(seats[state.Turn], state.Ply, uci))
			if err != nil {
				break
			}
			state, played = next, played+1
		}
		if played == 0 {
			continue
		}
		s := script{game: chess.Key, moves: item.UCI[:played], loser: -1}
		if !state.Over() {
			s.loser = 1
			if item.Result == "0-1" {
				s.loser = 0
			}
		}
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no playable chess games in %s", path)
	}
	return out, nil
}
