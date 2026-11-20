package rating

import "testing"

func TestNext(t *testing.T) {
	tests := []struct {
		name     string
		player   Player
		opponent Player
		score    Score
		want     int
	}{
		{"equal newcomers, win", Player{1200, 0}, Player{1200, 0}, Win, 1220},
		{"equal newcomers, loss", Player{1200, 0}, Player{1200, 0}, Loss, 1180},
		{"equal newcomers, draw", Player{1200, 0}, Player{1200, 0}, Draw, 1200},
		{"established player uses smaller factor", Player{1200, 20}, Player{1200, 30}, Win, 1212},
		{"underdog win gains more", Player{1000, 30}, Player{1400, 30}, Win, 1022},
		{"favourite win gains little", Player{1400, 30}, Player{1000, 30}, Win, 1402},
		{"favourite draw loses points", Player{1400, 30}, Player{1000, 30}, Draw, 1390},
		{"rating never drops below one", Player{1, 30}, Player{3000, 30}, Loss, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Next(tt.player, tt.opponent, tt.score); got != tt.want {
				t.Fatalf("Next() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestExpectedIsSymmetric(t *testing.T) {
	for _, pair := range [][2]int{{1200, 1200}, {1000, 1400}, {1800, 900}} {
		if sum := Expected(pair[0], pair[1]) + Expected(pair[1], pair[0]); sum < 0.999999 || sum > 1.000001 {
			t.Fatalf("Expected(%d,%d) + reverse = %v", pair[0], pair[1], sum)
		}
	}
}
