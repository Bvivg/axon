package durak

import (
	"encoding/binary"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
)

type Suit uint8

const (
	Spades Suit = iota
	Clubs
	Diamonds
	Hearts
)

type Rank uint8

const (
	Six Rank = iota + 6
	Seven
	Eight
	Nine
	Ten
	Jack
	Queen
	King
	Ace
)

const (
	rankNames = "6789TJQKA"
	suitNames = "SCDH"
	DeckSize  = 36
	HandSize  = 6
)

type Card struct {
	Rank Rank
	Suit Suit
}

func (c Card) String() string {
	return string([]byte{rankNames[c.Rank-Six], suitNames[c.Suit]})
}

func ParseCard(raw string) (Card, error) {
	if len(raw) != 2 {
		return Card{}, fmt.Errorf("%w: card %q", ErrBadMove, raw)
	}
	rank := strings.IndexByte(rankNames, raw[0])
	suit := strings.IndexByte(suitNames, raw[1])
	if rank < 0 || suit < 0 {
		return Card{}, fmt.Errorf("%w: card %q", ErrBadMove, raw)
	}
	return Card{Rank: Six + Rank(rank), Suit: Suit(suit)}, nil
}

func Deck() []Card {
	deck := make([]Card, 0, DeckSize)
	for suit := Spades; suit <= Hearts; suit++ {
		for rank := Six; rank <= Ace; rank++ {
			deck = append(deck, Card{Rank: rank, Suit: suit})
		}
	}
	return deck
}

func Shuffled(seed uint64) []Card {
	var key [32]byte
	binary.LittleEndian.PutUint64(key[:8], seed)
	copy(key[8:], "axon.durak.deck.v1")
	rng := rand.NewChaCha8(key)
	deck := Deck()
	for i := len(deck) - 1; i > 0; i-- {
		j := int(rng.Uint64() % uint64(i+1))
		deck[i], deck[j] = deck[j], deck[i]
	}
	return deck
}

func Beats(defense, attack Card, trump Suit) bool {
	if defense.Suit == attack.Suit {
		return defense.Rank > attack.Rank
	}
	return defense.Suit == trump
}

func sortHand(hand []Card, trump Suit) {
	weight := func(c Card) int {
		suit := int(c.Suit)
		if c.Suit == trump {
			suit = 4
		}
		return suit*16 + int(c.Rank)
	}
	slices.SortFunc(hand, func(a, b Card) int { return weight(a) - weight(b) })
}

func names(cards []Card) []string {
	out := make([]string, len(cards))
	for i, c := range cards {
		out[i] = c.String()
	}
	return out
}

func parseCards(raw []string) ([]Card, error) {
	out := make([]Card, 0, len(raw))
	for _, name := range raw {
		c, err := ParseCard(name)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}
