package durak

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/bvivg/axon/core/services/game/internal/engine"
)

type Mode string

const (
	ModeThrowIn  Mode = "throw-in"
	ModeTransfer Mode = "transfer"
)

type ThrowIn string

const (
	ThrowInAll       ThrowIn = "all"
	ThrowInNeighbors ThrowIn = "neighbors"
)

const (
	MinSeats = 2
	MaxSeats = 6
)

type Options struct {
	Mode           Mode    `json:"mode"`
	Cheats         bool    `json:"cheats"`
	Deck           int     `json:"deck"`
	ThrowIn        ThrowIn `json:"throw_in"`
	Draw           bool    `json:"draw"`
	FirstBoutLimit int     `json:"first_bout_limit"`
	Seats          int     `json:"seats"`
}

func DefaultOptions(seats int) Options {
	return Options{Mode: ModeThrowIn, Deck: DeckSize, ThrowIn: ThrowInAll, Draw: true, FirstBoutLimit: HandSize, Seats: seats}
}

func ParseOptions(raw []byte, seats int) (Options, error) {
	o := DefaultOptions(0)
	raw = bytes.TrimSpace(raw)
	if len(raw) > 0 && !bytes.Equal(raw, []byte("null")) {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&o); err != nil {
			return Options{}, fmt.Errorf("%w: %w", engine.ErrInvalidOptions, err)
		}
		if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			return Options{}, fmt.Errorf("%w: trailing data", engine.ErrInvalidOptions)
		}
	}
	if o.Seats == 0 {
		o.Seats = seats
	}
	if seats != 0 && o.Seats != seats {
		return Options{}, fmt.Errorf("%w: %d seats for %d players", engine.ErrInvalidOptions, o.Seats, seats)
	}
	if err := o.Validate(); err != nil {
		return Options{}, err
	}
	return o, nil
}

func (o Options) Validate() error {
	_, deck := deckSizes[o.Deck]
	switch {
	case o.Mode != ModeThrowIn && o.Mode != ModeTransfer:
		return fmt.Errorf("%w: mode %q", engine.ErrInvalidOptions, o.Mode)
	case !deck:
		return fmt.Errorf("%w: deck of %d", engine.ErrInvalidOptions, o.Deck)
	case o.ThrowIn != ThrowInAll && o.ThrowIn != ThrowInNeighbors:
		return fmt.Errorf("%w: throw-in %q", engine.ErrInvalidOptions, o.ThrowIn)
	case o.FirstBoutLimit != HandSize-1 && o.FirstBoutLimit != HandSize:
		return fmt.Errorf("%w: first bout limit %d", engine.ErrInvalidOptions, o.FirstBoutLimit)
	case o.Seats == 0:
		return nil
	case o.Seats < MinSeats || o.Seats > MaxSeats:
		return fmt.Errorf("%w: %d seats", engine.ErrInvalidOptions, o.Seats)
	case o.Seats*HandSize > o.Deck:
		return fmt.Errorf("%w: %d seats need more than %d cards", engine.ErrInvalidOptions, o.Seats, o.Deck)
	}
	return nil
}
