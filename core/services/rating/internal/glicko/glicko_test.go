package glicko_test

import (
	"math"
	"testing"

	"github.com/bvivg/axon/core/services/rating/internal/glicko"
)

func near(got, want, tolerance float64) bool { return math.Abs(got-want) <= tolerance }

func TestRate(t *testing.T) {
	glickman := glicko.Rating{Rating: 1500, Deviation: 200, Volatility: 0.06}
	newcomer := glicko.Rating{Rating: 1500, Deviation: 350, Volatility: 0.06}
	tests := []struct {
		name    string
		system  glicko.System
		player  glicko.Rating
		results []glicko.Result
		want    glicko.Rating
	}{
		{
			name:   "Glickman's worked example",
			system: glicko.System{Tau: 0.5},
			player: glickman,
			results: []glicko.Result{
				{Opponent: glicko.Rating{Rating: 1400, Deviation: 30, Volatility: 0.06}, Score: glicko.Win},
				{Opponent: glicko.Rating{Rating: 1550, Deviation: 100, Volatility: 0.06}, Score: glicko.Loss},
				{Opponent: glicko.Rating{Rating: 1700, Deviation: 300, Volatility: 0.06}, Score: glicko.Loss},
			},
			want: glicko.Rating{Rating: 1464.06, Deviation: 151.52, Volatility: 0.05999},
		},
		{
			name:   "an idle period only widens the deviation",
			system: glicko.System{Tau: 0.5},
			player: glickman,
			want:   glicko.Rating{Rating: 1500, Deviation: 200.27, Volatility: 0.06},
		},
		{
			name:    "two newcomers, the winner",
			system:  glicko.Default,
			player:  newcomer,
			results: []glicko.Result{{Opponent: newcomer, Score: glicko.Win}},
			want:    glicko.Rating{Rating: 1662.31, Deviation: 290.32, Volatility: 0.06},
		},
		{
			name:    "two newcomers, the loser",
			system:  glicko.Default,
			player:  newcomer,
			results: []glicko.Result{{Opponent: newcomer, Score: glicko.Loss}},
			want:    glicko.Rating{Rating: 1337.69, Deviation: 290.32, Volatility: 0.06},
		},
		{
			name:    "a draw between equals keeps the rating and narrows the deviation",
			system:  glicko.Default,
			player:  newcomer,
			results: []glicko.Result{{Opponent: newcomer, Score: glicko.Draw}},
			want:    glicko.Rating{Rating: 1500, Deviation: 290.32, Volatility: 0.06},
		},
		{
			name:   "the deviation never grows past the cap",
			system: glicko.Default,
			player: glicko.Rating{Rating: 1500, Deviation: 349.99, Volatility: 0.06},
			want:   glicko.Rating{Rating: 1500, Deviation: 350, Volatility: 0.06},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.system.Rate(tt.player, tt.results)
			if !near(got.Rating, tt.want.Rating, 0.01) || !near(got.Deviation, tt.want.Deviation, 0.01) || !near(got.Volatility, tt.want.Volatility, 0.00001) {
				t.Fatalf("Rate() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestGameIsTranslationInvariant(t *testing.T) {
	low := glicko.Rating{Rating: 1200, Deviation: 350, Volatility: 0.06}
	high := glicko.Rating{Rating: 1500, Deviation: 350, Volatility: 0.06}
	lowWinner, lowLoser := glicko.Default.Game(low, low, glicko.Win)
	highWinner, highLoser := glicko.Default.Game(high, high, glicko.Win)
	if !near(lowWinner.Rating-low.Rating, highWinner.Rating-high.Rating, 1e-9) || !near(lowLoser.Rating-low.Rating, highLoser.Rating-high.Rating, 1e-9) {
		t.Fatalf("shifting both players changed the result: %+v %+v vs %+v %+v", lowWinner, lowLoser, highWinner, highLoser)
	}
	if !near(lowWinner.Rating-low.Rating, low.Rating-lowLoser.Rating, 1e-9) {
		t.Fatalf("equal players moved asymmetrically: %+v %+v", lowWinner, lowLoser)
	}
}

func TestGameIsDeterministic(t *testing.T) {
	a := glicko.Rating{Rating: 1432.5, Deviation: 87.25, Volatility: 0.0601}
	b := glicko.Rating{Rating: 1610.75, Deviation: 140.5, Volatility: 0.0598}
	firstA, firstB := glicko.Default.Game(a, b, glicko.Draw)
	for range 100 {
		againA, againB := glicko.Default.Game(a, b, glicko.Draw)
		if againA != firstA || againB != firstB {
			t.Fatal("the same inputs produced different ratings")
		}
	}
}
