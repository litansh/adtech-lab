package main

import (
	"math"
	"math/rand"
	"sort"
)

// ---------------------------------------------------------------------------
// The cold-path controller.
//
// Reads yesterday's events, decides which arm of an experiment is winning, and
// proposes new bucket ranges. Runs hourly, out of band, on logged data. The hot
// path never sees any of this -- it hashes and reads a range (ADR 0005, 0006).
//
// Deliberately statistical rather than an LLM. "Which of three floors earned
// more per session" is arithmetic; putting a language model in front of a
// division would add cost, latency and non-determinism to something that has
// one right answer. An LLM belongs one level up -- proposing which experiment
// to run next, and explaining a result in context.
// ---------------------------------------------------------------------------

// Observation is one assignment unit's outcome. Note the UNIT, not the request:
// a session that produced nine impressions is ONE observation whose revenue is
// the sum. Bootstrapping impressions when the experiment is session-assigned
// understates variance and manufactures winners, because impressions within a
// session are correlated. This is the single easiest way to declare a false
// positive, so the type makes it hard to get wrong.
type Observation struct {
	Unit     string
	Revenue  float64
	Requests int
	Paid     int  // requests that produced a PAID ad (house fill does not count)
	Replayed bool // the product guardrail: did the player choose to play again?
}

type ArmStats struct {
	ID           string
	Units        int
	Requests     int
	RevenuePer1k float64
	PaidFill     float64
	ReplayRate   float64
}

// Summarise reduces raw observations to the numbers a decision is made on.
func Summarise(id string, obs []Observation) ArmStats {
	s := ArmStats{ID: id, Units: len(obs)}
	var rev float64
	var paid, replays int
	for _, o := range obs {
		rev += o.Revenue
		s.Requests += o.Requests
		paid += o.Paid
		if o.Replayed {
			replays++
		}
	}
	if s.Requests > 0 {
		s.RevenuePer1k = rev / float64(s.Requests) * 1000
		s.PaidFill = float64(paid) / float64(s.Requests)
	}
	if s.Units > 0 {
		s.ReplayRate = float64(replays) / float64(s.Units)
	}
	return s
}

// Interval is a bootstrap confidence interval on the difference in revenue per
// 1000 between an arm and the control.
type Interval struct {
	Diff, Low, High float64
	Significant     bool // the interval excludes zero
}

// BootstrapDiff resamples UNITS with replacement. No distributional assumption,
// which matters because revenue per 1000 is heavy-tailed: most requests earn
// nothing and a few earn a lot. A t-test on this data reports significance that
// does not reproduce.
func BootstrapDiff(treatment, control []Observation, iters int, rng *rand.Rand) Interval {
	if len(treatment) == 0 || len(control) == 0 {
		return Interval{}
	}
	base := Summarise("t", treatment).RevenuePer1k - Summarise("c", control).RevenuePer1k
	diffs := make([]float64, 0, iters)
	for i := 0; i < iters; i++ {
		t := Summarise("t", resample(treatment, rng))
		c := Summarise("c", resample(control, rng))
		diffs = append(diffs, t.RevenuePer1k-c.RevenuePer1k)
	}
	sort.Float64s(diffs)
	lo := diffs[int(0.025*float64(len(diffs)))]
	hi := diffs[int(0.975*float64(len(diffs)))-0]
	if hi >= float64(len(diffs)) {
		hi = diffs[len(diffs)-1]
	}
	return Interval{
		Diff: base, Low: lo, High: hi,
		Significant: (lo > 0 && hi > 0) || (lo < 0 && hi < 0),
	}
}

func resample(obs []Observation, rng *rand.Rand) []Observation {
	out := make([]Observation, len(obs))
	for i := range out {
		out[i] = obs[rng.Intn(len(obs))]
	}
	return out
}

// Guardrails are the conditions under which an arm loses regardless of revenue.
// The replay guardrail is the one with teeth: a floor change that raises
// revenue 4% while cutting replay rate 10% has borrowed tomorrow's sessions to
// pay for today, and CLAUDE.md requires that trade to be refused.
type Guardrails struct {
	MinPaidFill      float64 // absolute floor on paid fill
	MaxReplayDropRel float64 // e.g. 0.05 -- arm may not be 5% relatively worse than control
	MinUnits         int     // below this there is no result, only a number
}

func DefaultGuardrails() Guardrails {
	return Guardrails{MinPaidFill: 0.40, MaxReplayDropRel: 0.05, MinUnits: 1000}
}

// Breaches returns the reasons an arm is disqualified, empty if it is sound.
func (g Guardrails) Breaches(arm, control ArmStats) []string {
	var out []string
	if arm.Units < g.MinUnits {
		out = append(out, "insufficient_volume")
	}
	if arm.PaidFill < g.MinPaidFill {
		out = append(out, "paid_fill_below_floor")
	}
	if control.ReplayRate > 0 {
		if drop := (control.ReplayRate - arm.ReplayRate) / control.ReplayRate; drop > g.MaxReplayDropRel {
			out = append(out, "replay_rate_regression")
		}
	}
	return out
}

// Allocation limits. These exist so that one anomalous hour cannot swing the
// whole platform, and so that a losing arm can always recover.
const (
	MinArmShare   = 0.05 // no arm is ever starved to zero
	MaxMovePerRun = 0.10 // at most 10% of buckets move in one run
	BucketCount   = 10000
)

type Proposal struct {
	ArmID              string
	FromShare, ToShare float64
	FromStart, FromEnd int
	ToStart, ToEnd     int
	Reasons            []string
}

// Allocate performs one controller step: Thompson-style weighting by each arm's
// sampled mean, clamped hard.
//
// Thompson sampling rather than a fixed split because it earns while it learns;
// a fixed split knowingly runs the losing arm at full weight for the whole test.
// The holdout is what keeps that honest -- without a reserved control slice you
// can only observe the winner the controller chose, which is a number that
// always looks good.
func Allocate(stats []ArmStats, controlID string, holdoutShare float64,
	breached map[string][]string, current map[string]float64, rng *rand.Rand) []Proposal {

	// --- 1. Sample a plausible mean per arm (Thompson). Variance shrinks with
	// volume, so a low-volume arm keeps exploring rather than being ranked on
	// noise. A breached arm scores zero: revenue must never buy back a
	// guardrail.
	score := map[string]float64{}
	var scoreSum float64
	for _, s := range stats {
		if len(breached[s.ID]) > 0 {
			continue
		}
		se := 0.0
		if s.Units > 1 {
			se = s.RevenuePer1k / math.Sqrt(float64(s.Units))
		}
		v := s.RevenuePer1k + rng.NormFloat64()*se
		if v < 0 {
			v = 0
		}
		score[s.ID] = v
		scoreSum += v
	}

	// --- 2. Raw desired share, unconstrained.
	raw := map[string]float64{}
	for _, s := range stats {
		if scoreSum > 0 {
			raw[s.ID] = score[s.ID] / scoreSum
		}
	}
	if scoreSum <= 0 {
		// Nothing measurable, or everything breached: prefer the control.
		raw[controlID] = 1
	}

	// --- 3. Project the desired shares onto the feasible box.
	//
	// Each arm has a lower and upper bound:
	//   lower  its floor, and no more than MaxMovePerRun below where it is now
	//   upper  no more than MaxMovePerRun above where it is now
	//
	// Both constraints have to hold in the FINAL numbers. An earlier version
	// clamped movement and then water-filled the remainder, which quietly
	// undid the clamp -- the control moved 20% -> 46% in one step against a
	// stated 10% limit. Enforcing a constraint before a later step can
	// redistribute past it is not enforcing it.
	lo := map[string]float64{}
	hi := map[string]float64{}
	for _, s := range stats {
		f := MinArmShare
		if s.ID == controlID && holdoutShare > f {
			f = holdoutShare
		}
		l, h := f, 1.0
		if len(breached[s.ID]) > 0 {
			// Breached arms drop to the minimum IMMEDIATELY, exempt from the
			// move limit. The limit exists so a noisy hour cannot swing the
			// platform; a guardrail breach is not noise, and draining a
			// paid-fill collapse over four hours costs money every one of them.
			// ADR 0006 says revert immediately, so it reverts immediately.
			lo[s.ID], hi[s.ID] = MinArmShare, MinArmShare
			continue
		}
		if cur, known := current[s.ID]; known {
			if cur-MaxMovePerRun > l {
				l = cur - MaxMovePerRun
			}
			if cur+MaxMovePerRun < h {
				h = cur + MaxMovePerRun
			}
			if h < l {
				h = l // a floor outranks the move limit
			}
		}
		lo[s.ID], hi[s.ID] = l, h
	}

	target := waterFill(stats, raw, lo, hi, controlID)

	// --- 4. Lay the shares out as contiguous bucket ranges, in a stable order
	// so that re-running the controller does not permute arms across the
	// bucket space.
	ids := make([]string, 0, len(stats))
	for _, s := range stats {
		ids = append(ids, s.ID)
	}
	sort.Strings(ids)

	var out []Proposal
	cursor := 0
	for i, id := range ids {
		end := cursor + int(math.Round(target[id]*BucketCount))
		if i == len(ids)-1 {
			end = BucketCount // absorb rounding in the last arm
		}
		if end > BucketCount {
			end = BucketCount
		}
		if end < cursor {
			end = cursor
		}
		out = append(out, Proposal{
			ArmID: id, FromShare: current[id], ToShare: target[id],
			ToStart: cursor, ToEnd: end, Reasons: breached[id],
		})
		cursor = end
	}
	return out
}

// waterFill gives every arm its lower bound, then distributes what remains in
// proportion to the raw weights, clipping anything that hits its upper bound
// and re-distributing the overflow. Converges in at most one pass per arm.
func waterFill(stats []ArmStats, raw, lo, hi map[string]float64, controlID string) map[string]float64 {
	target := map[string]float64{}
	frozen := map[string]bool{}
	var loSum float64
	for _, s := range stats {
		target[s.ID] = lo[s.ID]
		loSum += lo[s.ID]
		if hi[s.ID] <= lo[s.ID] {
			frozen[s.ID] = true
		}
	}

	room := 1 - loSum
	if room <= 0 {
		// The lower bounds alone fill the space. Nothing to distribute; the
		// bounds themselves are the answer.
		return target
	}

	for pass := 0; pass < len(stats)+1 && room > 1e-12; pass++ {
		var weight float64
		for _, s := range stats {
			if !frozen[s.ID] {
				weight += raw[s.ID]
			}
		}
		if weight <= 0 {
			// No signal among the movable arms: the remainder goes to the
			// control if it can take it, otherwise spread evenly.
			if !frozen[controlID] {
				give := room
				if headroom := hi[controlID] - target[controlID]; give > headroom {
					give = headroom
				}
				target[controlID] += give
				room -= give
			}
			if room > 1e-12 {
				var movable []string
				for _, s := range stats {
					if !frozen[s.ID] {
						movable = append(movable, s.ID)
					}
				}
				if len(movable) == 0 {
					break
				}
				each := room / float64(len(movable))
				for _, id := range movable {
					give := each
					if headroom := hi[id] - target[id]; give > headroom {
						give = headroom
					}
					target[id] += give
					room -= give
				}
			}
			break
		}

		overflow := 0.0
		for _, s := range stats {
			if frozen[s.ID] {
				continue
			}
			give := room * raw[s.ID] / weight
			if headroom := hi[s.ID] - target[s.ID]; give >= headroom {
				give = headroom
				frozen[s.ID] = true
			}
			target[s.ID] += give
			overflow += give
		}
		room -= overflow
		if overflow <= 1e-12 {
			break
		}
	}
	return target
}

func normalise(m map[string]float64) {
	var sum float64
	for _, v := range m {
		sum += v
	}
	if sum == 0 {
		return
	}
	for k := range m {
		m[k] /= sum
	}
}
