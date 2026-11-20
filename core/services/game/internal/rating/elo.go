package rating

import "math"

const (
	Initial         = 1200
	provisionalK    = 40
	establishedK    = 24
	provisionalGame = 20
)

type Score float64

const (
	Loss Score = 0
	Draw Score = 0.5
	Win  Score = 1
)

type Player struct {
	Rating int
	Games  int
}

func Expected(rating, opponent int) float64 {
	return 1 / (1 + math.Pow(10, float64(opponent-rating)/400))
}

func factor(games int) float64 {
	if games < provisionalGame {
		return provisionalK
	}
	return establishedK
}

func Next(player, opponent Player, score Score) int {
	next := int(math.Round(float64(player.Rating) + factor(player.Games)*(float64(score)-Expected(player.Rating, opponent.Rating))))
	return max(next, 1)
}
