package matchmaking

import (
	"math"
	"sort"
	"time"

	"github.com/google/uuid"
)

const (
	MinWait       = 5 * time.Second
	MaxWait       = 30 * time.Second
	baseWait      = 10 * time.Second
	baseActive    = 20
	InitialWindow = 100
	FinalWindow   = 400
	StaleAfter    = 15 * time.Second
	AbandonAfter  = time.Minute
	Unlimited     = math.MaxInt32
)

type Entry struct {
	User     uuid.UUID
	Rating   int
	JoinedAt time.Time
}

type Pair struct {
	First  Entry
	Second Entry
}

func Wait(active int) time.Duration {
	if active < 0 {
		active = 0
	}
	wait := time.Duration(float64(baseWait) * math.Sqrt(float64(active)/baseActive))
	return min(max(wait, MinWait), MaxWait)
}

func Window(waited, wait time.Duration) int {
	if waited >= wait {
		return Unlimited
	}
	if waited <= 0 {
		return InitialWindow
	}
	return InitialWindow + int(float64(FinalWindow-InitialWindow)*float64(waited)/float64(wait))
}

func Match(entries []Entry, now time.Time, wait time.Duration) []Pair {
	queue := append([]Entry(nil), entries...)
	sort.SliceStable(queue, func(i, j int) bool { return queue[i].JoinedAt.Before(queue[j].JoinedAt) })
	taken := make([]bool, len(queue))
	var pairs []Pair
	for i, first := range queue {
		if taken[i] {
			continue
		}
		best := -1
		bestGap := 0
		for j := i + 1; j < len(queue); j++ {
			if taken[j] {
				continue
			}
			second := queue[j]
			gap := abs(first.Rating - second.Rating)
			allowed := max(Window(now.Sub(first.JoinedAt), wait), Window(now.Sub(second.JoinedAt), wait))
			if gap > allowed {
				continue
			}
			if best < 0 || gap < bestGap {
				best, bestGap = j, gap
			}
		}
		if best >= 0 {
			taken[i], taken[best] = true, true
			pairs = append(pairs, Pair{First: first, Second: queue[best]})
		}
	}
	return pairs
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
