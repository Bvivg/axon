package durak

import (
	"fmt"
	"strings"
)

type Action uint8

const (
	Attack Action = iota + 1
	Defend
	Take
	Pass
)

const MaxNotation = len("defend 6S 6S")

type Move struct {
	Action  Action
	Card    Card
	Against Card
}

func (m Move) String() string {
	switch m.Action {
	case Attack:
		return "attack " + m.Card.String()
	case Defend:
		return "defend " + m.Against.String() + " " + m.Card.String()
	case Take:
		return "take"
	case Pass:
		return "pass"
	default:
		return ""
	}
}

func ParseMove(raw string) (Move, error) {
	if len(raw) > MaxNotation {
		return Move{}, fmt.Errorf("%w: notation is %d bytes", ErrBadMove, len(raw))
	}
	parts := strings.Split(raw, " ")
	switch {
	case len(parts) == 1 && parts[0] == "take":
		return Move{Action: Take}, nil
	case len(parts) == 1 && parts[0] == "pass":
		return Move{Action: Pass}, nil
	case len(parts) == 2 && parts[0] == "attack":
		card, err := ParseCard(parts[1])
		if err != nil {
			return Move{}, err
		}
		return Move{Action: Attack, Card: card}, nil
	case len(parts) == 3 && parts[0] == "defend":
		against, err := ParseCard(parts[1])
		if err != nil {
			return Move{}, err
		}
		card, err := ParseCard(parts[2])
		if err != nil {
			return Move{}, err
		}
		return Move{Action: Defend, Card: card, Against: against}, nil
	default:
		return Move{}, fmt.Errorf("%w: %q", ErrBadMove, raw)
	}
}
