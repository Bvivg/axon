package glicko

import "math"

const (
	scale       = 173.7178
	center      = 1500.0
	convergence = 0.000001
	maxSteps    = 100
)

type Score float64

const (
	Loss Score = 0
	Draw Score = 0.5
	Win  Score = 1
)

type Rating struct {
	Rating     float64
	Deviation  float64
	Volatility float64
}

type Result struct {
	Opponent Rating
	Score    Score
}

type System struct {
	Tau          float64
	MaxDeviation float64
}

var (
	Default  = System{Tau: 0.5, MaxDeviation: 350}
	Newcomer = Rating{Rating: 1200, Deviation: 350, Volatility: 0.06}
)

func g(phi float64) float64 {
	return 1 / math.Sqrt(1+3*phi*phi/(math.Pi*math.Pi))
}

func expected(mu, muj, phij float64) float64 {
	return 1 / (1 + math.Exp(-g(phij)*(mu-muj)))
}

func (s System) Rate(player Rating, results []Result) Rating {
	mu := (player.Rating - center) / scale
	phi := player.Deviation / scale
	sigma := player.Volatility
	if len(results) == 0 {
		return Rating{Rating: player.Rating, Deviation: s.cap(scale * math.Sqrt(phi*phi+sigma*sigma)), Volatility: sigma}
	}
	var inverseV, improvement float64
	for _, result := range results {
		muj := (result.Opponent.Rating - center) / scale
		phij := result.Opponent.Deviation / scale
		gj := g(phij)
		e := expected(mu, muj, phij)
		inverseV += gj * gj * e * (1 - e)
		improvement += gj * (float64(result.Score) - e)
	}
	v := 1 / inverseV
	delta := v * improvement
	sigma = s.volatility(phi, sigma, v, delta)
	phiStar := math.Sqrt(phi*phi + sigma*sigma)
	phi = 1 / math.Sqrt(1/(phiStar*phiStar)+1/v)
	mu += phi * phi * improvement
	return Rating{Rating: scale*mu + center, Deviation: s.cap(scale * phi), Volatility: sigma}
}

func (s System) volatility(phi, sigma, v, delta float64) float64 {
	a := math.Log(sigma * sigma)
	tau2 := s.Tau * s.Tau
	f := func(x float64) float64 {
		ex := math.Exp(x)
		d := phi*phi + v + ex
		return ex*(delta*delta-phi*phi-v-ex)/(2*d*d) - (x-a)/tau2
	}
	lower := a
	var upper float64
	if delta*delta > phi*phi+v {
		upper = math.Log(delta*delta - phi*phi - v)
	} else {
		k := 1.0
		for f(a-k*s.Tau) < 0 && k < maxSteps {
			k++
		}
		upper = a - k*s.Tau
	}
	fLower, fUpper := f(lower), f(upper)
	for step := 0; math.Abs(upper-lower) > convergence && step < maxSteps; step++ {
		next := lower + (lower-upper)*fLower/(fUpper-fLower)
		fNext := f(next)
		if fNext*fUpper <= 0 {
			lower, fLower = upper, fUpper
		} else {
			fLower /= 2
		}
		upper, fUpper = next, fNext
	}
	return math.Exp(lower / 2)
}

func (s System) cap(deviation float64) float64 {
	if s.MaxDeviation > 0 {
		return math.Min(deviation, s.MaxDeviation)
	}
	return deviation
}

func (s System) Game(a, b Rating, score Score) (Rating, Rating) {
	return s.Rate(a, []Result{{Opponent: b, Score: score}}), s.Rate(b, []Result{{Opponent: a, Score: 1 - score}})
}
