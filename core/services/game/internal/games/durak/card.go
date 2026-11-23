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
	Two Rank = iota + 2
	Three
	Four
	Five
	Six
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
	rankNames = "23456789TJQKA"
	suitNames = "SCDH"
	DeckSize  = 36
	HandSize  = 6
)

var deckSizes = map[int]Rank{24: Nine, 36: Six, 52: Two}

type Card struct {
	Rank Rank
	Suit Suit
}

func (c Card) String() string {
	if c.Rank < Two || c.Rank > Ace || c.Suit > Hearts {
		return ""
	}
	return string([]byte{rankNames[c.Rank-Two], suitNames[c.Suit]})
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
	return Card{Rank: Two + Rank(rank), Suit: Suit(suit)}, nil
}

func Deck() []Card {
	return DeckOf(DeckSize)
}

func DeckOf(size int) []Card {
	lowest, ok := deckSizes[size]
	if !ok {
		return nil
	}
	deck := make([]Card, 0, size)
	for suit := Spades; suit <= Hearts; suit++ {
		for rank := lowest; rank <= Ace; rank++ {
			deck = append(deck, Card{Rank: rank, Suit: suit})
		}
	}
	return deck
}

func Shuffled(seed uint64) []Card {
	return ShuffledDeck(seed, DeckSize)
}

func ShuffledDeck(seed uint64, size int) []Card {
	var key [32]byte
	binary.LittleEndian.PutUint64(key[:8], seed)
	copy(key[8:], "axon.durak.deck.v1")
	rng := rand.NewChaCha8(key)
	deck := DeckOf(size)
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

func byIdentity(a, b Card) int {
	return int(a.Suit)*16 + int(a.Rank) - int(b.Suit)*16 - int(b.Rank)
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
