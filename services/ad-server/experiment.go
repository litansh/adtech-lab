package main

import (
	"hash/fnv"
	"sort"
	"strconv"
)

// ---------------------------------------------------------------------------
// Experiments.
//
// The rule for this platform: no number that affects money is a constant.
// Floors, timeouts, how many buyers we call, how we shade, what we return to
// the publisher on a no-bid -- each is a variant, and traffic is split across
// variants continuously, forever. We are never running "the" configuration; we
// are always running several and measuring which one pays.
//
// Two things make that safe rather than reckless:
//
//   1. A HOLDOUT. A fixed slice of traffic always runs the control, even when
//      the controller is certain. Without it you cannot measure the controller
//      itself -- you only see the winner it chose, which is a number that
//      always looks good. The holdout is what tells you whether the whole
//      apparatus is earning its keep.
//
//   2. BUCKETS, not live weights. Assignment hashes into 10,000 fixed buckets
//      and variants own contiguous bucket ranges. When the controller shifts
//      weight, only the buckets at the boundary move. Assigning directly from
//      weights would reshuffle every user on every update, which destroys
//      session-level metrics and makes any result unreproducible.
//
// The hot path does NO learning. It hashes, reads a range, and serves. All
// inference -- which variant is winning, how much traffic it should get next
// -- happens in the cold path, on logged data, and arrives here as updated
// bucket ranges in the control plane. That is ADR 0005 applied to yield: the
// decision is agentic, the serve is arithmetic.
// ---------------------------------------------------------------------------

const bucketCount = 10000

// AssignUnit is what an experiment randomises over. Choosing this wrong is the
// classic way to get a confident, wrong answer.
type AssignUnit string

const (
	// UnitSession is the default. A player who sees a $2.00 floor on their
	// first game and a $0.50 floor on their second contaminates both arms, and
	// any session-level metric (replays, session length) becomes meaningless.
	UnitSession AssignUnit = "session"

	// UnitRequest is only correct for effects that cannot carry across
	// requests. It gives far more statistical power, which is exactly why it
	// is tempting to use where it does not belong.
	UnitRequest AssignUnit = "request"

	// UnitPlacement pins a whole placement to one arm. Used for changes whose
	// effect is on buyer behaviour rather than user behaviour: a buyer learns
	// a placement's floor over days, so splitting it per session teaches them
	// an average that exists nowhere.
	UnitPlacement AssignUnit = "placement"
)

// Variant is one arm. Params are deliberately strings: the control plane is
// JSON edited by a controller, and typed unions there cost more than they save.
type Variant struct {
	ID     string            `json:"id"`
	Params map[string]string `json:"params"`

	// BucketStart is inclusive, BucketEnd exclusive. Ranges are contiguous and
	// must cover [0, bucketCount) exactly; Validate enforces that.
	BucketStart int `json:"bucket_start"`
	BucketEnd   int `json:"bucket_end"`
}

func (v Variant) Float(key string, def float64) float64 {
	if s, ok := v.Params[key]; ok {
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return f
		}
	}
	return def
}

func (v Variant) Int(key string, def int) int {
	if s, ok := v.Params[key]; ok {
		if n, err := strconv.Atoi(s); err == nil {
			return n
		}
	}
	return def
}

// Experiment is one knob being tested. ControlID names the arm that is the
// current best-known configuration; the holdout is carved out of it.
type Experiment struct {
	Key       string     `json:"key"`
	Active    bool       `json:"active"`
	Unit      AssignUnit `json:"unit"`
	ControlID string     `json:"control_id"`
	Variants  []Variant  `json:"variants"`
}

// Assignment is what the hot path produced, and what the cold path needs in
// order to attribute revenue back to an arm. Every one of these is written to
// the event log; an unlogged assignment is an unmeasurable experiment.
type Assignment struct {
	Key       string `json:"key"`
	VariantID string `json:"variant_id"`
	Unit      string `json:"unit"`
	Holdout   bool   `json:"holdout,omitempty"`
}

// Experiments is the set in force, keyed by experiment key.
type Experiments struct {
	Set []Experiment `json:"experiments"`
}

// unitID picks the identifier this experiment randomises over. A missing
// session id falls back to the request id, which silently degrades a session
// experiment into a request one -- so callers must ensure sessions are set.
func unitID(u AssignUnit, r AdRequest, requestID string) string {
	switch u {
	case UnitRequest:
		return requestID
	case UnitPlacement:
		return r.PlacementID
	default:
		if r.SessionID != "" {
			return r.SessionID
		}
		return requestID
	}
}

// bucketOf is deterministic and stable across processes and restarts. The
// experiment key is mixed in so that a session does not land in the same
// relative position in every experiment -- otherwise arms correlate across
// experiments and interaction effects become invisible.
func bucketOf(key, unit string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	_, _ = h.Write([]byte{0x1f})
	_, _ = h.Write([]byte(unit))
	return int(h.Sum32() % bucketCount)
}

// Assign returns the variant this request falls into, plus the record to log.
// An inactive, unknown or malformed experiment yields the control variant --
// never an error and never a panic. A broken experiment must degrade to
// "serve normally", because the alternative is that a bad control-plane edit
// stops the ad server.
func (e Experiments) Assign(key string, r AdRequest, requestID string) (Variant, Assignment) {
	for _, x := range e.Set {
		if x.Key != key {
			continue
		}
		ctrl, ok := x.control()
		if !ok {
			return Variant{ID: "control"}, Assignment{Key: key, VariantID: "control", Unit: "none"}
		}
		if !x.Active || len(x.Variants) == 0 {
			return ctrl, Assignment{Key: key, VariantID: ctrl.ID, Unit: "none"}
		}
		uid := unitID(x.Unit, r, requestID)
		b := bucketOf(key, uid)
		for _, v := range x.Variants {
			if b >= v.BucketStart && b < v.BucketEnd {
				return v, Assignment{
					Key: key, VariantID: v.ID, Unit: uid,
					Holdout: v.ID == x.ControlID,
				}
			}
		}
		// Ranges did not cover this bucket. Serve the control and let
		// Validate surface it, rather than dropping the request.
		return ctrl, Assignment{Key: key, VariantID: ctrl.ID, Unit: uid}
	}
	return Variant{ID: "control"}, Assignment{Key: key, VariantID: "control", Unit: "none"}
}

func (x Experiment) control() (Variant, bool) {
	for _, v := range x.Variants {
		if v.ID == x.ControlID {
			return v, true
		}
	}
	if len(x.Variants) > 0 {
		return x.Variants[0], true
	}
	return Variant{}, false
}

// Validate is run at load time, not per request. It refuses configurations that
// would produce a silently biased split -- the failure mode that looks like a
// result. Returns the problems found, empty if the set is sound.
func (e Experiments) Validate() []string {
	var problems []string
	seen := map[string]bool{}
	for _, x := range e.Set {
		if seen[x.Key] {
			problems = append(problems, "duplicate experiment key: "+x.Key)
		}
		seen[x.Key] = true

		if len(x.Variants) == 0 {
			problems = append(problems, x.Key+": no variants")
			continue
		}
		if _, ok := x.control(); !ok {
			problems = append(problems, x.Key+": control_id matches no variant")
		}
		if _, named := x.namedControl(); !named {
			problems = append(problems, x.Key+": control_id "+strconv.Quote(x.ControlID)+" not found, falling back to first variant")
		}

		vs := append([]Variant(nil), x.Variants...)
		sort.Slice(vs, func(i, j int) bool { return vs[i].BucketStart < vs[j].BucketStart })
		cursor := 0
		ids := map[string]bool{}
		for _, v := range vs {
			if ids[v.ID] {
				problems = append(problems, x.Key+": duplicate variant id "+v.ID)
			}
			ids[v.ID] = true
			if v.BucketEnd <= v.BucketStart {
				problems = append(problems, x.Key+"/"+v.ID+": empty or inverted bucket range")
				continue
			}
			if v.BucketStart != cursor {
				problems = append(problems, x.Key+"/"+v.ID+": bucket range starts at "+
					strconv.Itoa(v.BucketStart)+", expected "+strconv.Itoa(cursor)+
					" (gap or overlap)")
			}
			cursor = v.BucketEnd
		}
		if cursor != bucketCount {
			problems = append(problems, x.Key+": buckets cover "+strconv.Itoa(cursor)+
				" of "+strconv.Itoa(bucketCount))
		}

		// The holdout guard. Without a reserved slice of control traffic the
		// controller cannot be evaluated, only trusted.
		if ctrl, ok := x.namedControl(); ok {
			if share := ctrl.BucketEnd - ctrl.BucketStart; share < bucketCount/20 {
				problems = append(problems, x.Key+": control holdout is "+
					strconv.Itoa(share*100/bucketCount)+"%, minimum is 5%")
			}
		}
	}
	return problems
}

func (x Experiment) namedControl() (Variant, bool) {
	for _, v := range x.Variants {
		if v.ID == x.ControlID {
			return v, true
		}
	}
	return Variant{}, false
}

// Experiment keys in force. Named constants rather than string literals at the
// call site, so that a typo is a compile error instead of a permanently
// unassigned experiment that silently serves control to everyone.
const (
	ExpFloorPrice  = "floor_price"
	ExpBuyerTmax   = "buyer_tmax"
	ExpBuyerFanout = "buyer_fanout"
)
