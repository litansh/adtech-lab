package main

import (
	"math"
	"math/rand"
	"sort"
)

// ---------------------------------------------------------------------------
// The Product agent: is this still worth playing?
//
// It is the counterweight to every other agent. Without it, each of their
// metrics improves by degrading the product -- slowly, and nobody notices until
// the traffic is gone. Yield raises floors, Demand trims buyers, FinOps trims
// cost, and every one of those can be made to look good while the reason people
// visited quietly erodes.
//
// Its primary metric is the REPLAY RATE: of the people who finished a game, how
// many chose to play again. It is the cleanest engagement signal available
// without an identifier -- it needs no cross-session tracking, it is a decision
// the player actively made, and it is the thing that stops being true first
// when a site gets worse.
//
// Its most important single job is the placement rule from
// docs/ad-placement.md:
//
//     A placement ships permanently only if replay rate is NOT WORSE.
//     Not "not significantly worse".
//
// That asymmetry is deliberate and this file encodes it. Revenue is immediate
// and easy to measure; the damage a bad placement does is slow and shows up as
// traffic that never returns, which no experiment of reasonable length will
// detect. When the two signals are close, the product wins.
// ---------------------------------------------------------------------------

// GameStats is one game's engagement over a period.
type GameStats struct {
	Game      string
	Starts    int
	Ends      int
	Replays   int
	Sessions  map[string]bool
	Durations []float64 // seconds, per completed game
}

type GameReport struct {
	Game            string  `json:"game"`
	Sessions        int     `json:"sessions"`
	Starts          int     `json:"starts"`
	Completions     int     `json:"completions"`
	CompletionRate  float64 `json:"completion_rate"`
	ReplayRate      float64 `json:"replay_rate"`
	GamesPerSession float64 `json:"games_per_session"`
	MedianSeconds   float64 `json:"median_seconds"`
}

func Report(stats []GameStats) []GameReport {
	out := make([]GameReport, 0, len(stats))
	for _, s := range stats {
		r := GameReport{
			Game: s.Game, Starts: s.Starts, Completions: s.Ends,
			Sessions: len(s.Sessions),
		}
		if s.Starts > 0 {
			// A game people start and never finish is a game that is too hard,
			// too long, or not fun -- and the three look identical here, which
			// is why this metric prompts a question rather than an action.
			r.CompletionRate = float64(s.Ends) / float64(s.Starts)
		}
		if s.Ends > 0 {
			r.ReplayRate = float64(s.Replays) / float64(s.Ends)
		}
		if len(s.Sessions) > 0 {
			r.GamesPerSession = float64(s.Starts) / float64(len(s.Sessions))
		}
		r.MedianSeconds = median(s.Durations)
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ReplayRate > out[j].ReplayRate })
	return out
}

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	m := len(s) / 2
	if len(s)%2 == 1 {
		return s[m]
	}
	return (s[m-1] + s[m]) / 2
}

// --- the placement verdict ---

// ArmStats is one arm of a placement experiment.
type ArmStats struct {
	Arm      string
	Sessions int
	Ends     int
	Replays  int
	Revenue  float64
}

func (a ArmStats) ReplayRate() float64 {
	if a.Ends == 0 {
		return 0
	}
	return float64(a.Replays) / float64(a.Ends)
}

func (a ArmStats) RevenuePerSession() float64 {
	if a.Sessions == 0 {
		return 0
	}
	return a.Revenue / float64(a.Sessions)
}

// PlacementVerdict is the decision docs/ad-placement.md describes.
type PlacementVerdict struct {
	On, Off      ArmStats
	ReplayDelta  float64 // relative, on vs off
	RevenueDelta float64 // relative
	ReplayCILow  float64 // bootstrap, on the relative difference
	ReplayCIHigh float64
	Decision     string
	Why          string
}

// JudgePlacement applies the asymmetric rule.
//
// The burden is on the PLACEMENT: it ships only if we can say replay rate is
// not worse. An inconclusive result is not a pass -- with revenue improving and
// engagement unmeasurable, shipping is a bet that the invisible cost is zero,
// and that bet is exactly how sites decay.
func JudgePlacement(on, off ArmStats, rng *rand.Rand) PlacementVerdict {
	v := PlacementVerdict{On: on, Off: off}

	if off.ReplayRate() > 0 {
		v.ReplayDelta = (on.ReplayRate() - off.ReplayRate()) / off.ReplayRate()
	}
	if off.RevenuePerSession() > 0 {
		v.RevenueDelta = (on.RevenuePerSession() - off.RevenuePerSession()) /
			off.RevenuePerSession()
	}

	const minEnds = 400
	if on.Ends < minEnds || off.Ends < minEnds {
		v.Decision = "KEEP MEASURING"
		v.Why = "too few completed games to judge replay rate"
		return v
	}

	// Bootstrap the relative difference in replay rate. Replay is a proportion,
	// so resampling completions is the right unit -- and a normal approximation
	// on a proportion this small misbehaves.
	v.ReplayCILow, v.ReplayCIHigh = bootstrapReplayDelta(on, off, 4000, rng)

	switch {
	case v.ReplayCIHigh < 0:
		v.Decision = "REMOVE"
		v.Why = "replay rate is worse and the interval excludes zero"
	case v.ReplayCILow > 0:
		v.Decision = "SHIP"
		v.Why = "replay rate is no worse -- it is better"
	case v.ReplayDelta >= 0:
		v.Decision = "SHIP"
		v.Why = "replay rate is not worse; the interval straddles zero but the point estimate is non-negative"
	default:
		// The case the asymmetry exists for.
		v.Decision = "DO NOT SHIP"
		v.Why = "replay rate is lower and the interval straddles zero. " +
			"Inconclusive is not a pass: the burden is on the placement"
	}
	return v
}

func bootstrapReplayDelta(on, off ArmStats, iters int, rng *rand.Rand) (lo, hi float64) {
	if rng == nil || on.Ends == 0 || off.Ends == 0 {
		return 0, 0
	}
	diffs := make([]float64, 0, iters)
	for i := 0; i < iters; i++ {
		a := resampleRate(on.Replays, on.Ends, rng)
		b := resampleRate(off.Replays, off.Ends, rng)
		if b > 0 {
			diffs = append(diffs, (a-b)/b)
		}
	}
	if len(diffs) == 0 {
		return 0, 0
	}
	sort.Float64s(diffs)
	return diffs[int(0.025*float64(len(diffs)))], diffs[int(0.975*float64(len(diffs)))-1]
}

// resampleRate draws a proportion by simulating the same number of completions.
func resampleRate(successes, trials int, rng *rand.Rand) float64 {
	p := float64(successes) / float64(trials)
	n := 0
	for i := 0; i < trials; i++ {
		if rng.Float64() < p {
			n++
		}
	}
	return float64(n) / float64(trials)
}

// Flags are the product observations worth acting on, ranked.
func Flags(reports []GameReport) []string {
	var out []string
	for _, r := range reports {
		if r.Starts < 100 {
			continue
		}
		if r.ReplayRate < 0.20 {
			out = append(out, r.Game+
				": replay rate "+pct(r.ReplayRate)+
				" -- players finish once and leave. The game works; it is not compelling")
		}
		if r.CompletionRate < 0.5 {
			out = append(out, r.Game+
				": only "+pct(r.CompletionRate)+
				" of started games are finished -- too hard, too long, or not fun, and "+
				"this metric cannot tell which")
		}
		if r.GamesPerSession < 1.3 && r.Sessions > 50 {
			out = append(out, r.Game+
				": "+trim(r.GamesPerSession)+" games per session -- visitors are not exploring")
		}
	}
	return out
}

func pct(f float64) string { return trim(f*100) + "%" }
func trim(f float64) string {
	s := math.Round(f*10) / 10
	return trimFloat(s)
}
