package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// Benchmarking against what is already out there.
//
// The rest of productctl looks inward, at our own events. This looks outward,
// at a corpus of what the wider web is playing -- and ranks what we could build
// against it.
//
// It does NOT browse. The corpus is a file, refreshed on a schedule by the
// Horizon agent, and this tool only counts and ranks. That split is
// deliberate: Horizon's input is untrusted text from the open web, and an agent
// that both reads arbitrary text and can change the system is a prompt
// injection path with extra steps. Counting is injection-proof; interpreting is
// not.

type Landscape struct {
	AsOf    string `json:"as_of"`
	Sources []struct {
		Claim string `json:"claim"`
		URL   string `json:"url"`
	} `json:"sources"`
	Portfolio []struct {
		ID       string `json:"id"`
		Mechanic string `json:"mechanic"`
	} `json:"portfolio"`
	Candidates []Candidate `json:"candidates"`
}

type Candidate struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Kind     string `json:"kind"` // game | feature | platform
	Mechanic string `json:"mechanic"`
	Evidence string `json:"evidence"`

	BuildDays int `json:"build_days"`
	// ContentSetsPerYear is the number of hand-made puzzles it needs EVERY
	// year to stay alive. It is deliberately not folded into the score: a
	// recurring obligation and a one-off cost are different kinds of thing, and
	// averaging them is how a two-person team ends up owing 365 puzzles a year.
	ContentSetsPerYear int `json:"content_sets_per_year"`

	Retention  int `json:"retention"`  // 0-5, does it create a reason to come back
	Fit        int `json:"fit"`        // 0-5, works in our engine, on a phone, with no new assets
	Saturation int `json:"saturation"` // 0-5, how crowded the space already is

	// Shipped is the date it was built, if it has been. A benchmark that does
	// not know what exists keeps recommending finished work -- this one put
	// `one-away` at the top of its ranking two days after `one-away` shipped,
	// and would have gone on doing so forever.
	Shipped   string `json:"shipped,omitempty"`
	ShippedIn string `json:"shipped_in,omitempty"`

	// Constraint is a decision already taken about this candidate, in words.
	//
	// The agent proposed "streaks" when the daily already had one and Snake
	// deliberately did not -- game-room.js says in a comment that the site is
	// not built on loss aversion. The landscape had nowhere to record a
	// decision, so a candidate could be re-proposed against a choice that had
	// already been made and written down somewhere else.
	Constraint string `json:"constraint,omitempty"`
}

// Score is expected value per day of work.
//
// Retention is weighted hardest because docs/audience.md already decided that
// returning players are the only signal that matters here. Saturation is a
// discount rather than a veto -- Sudoku is saturated too, and ships anyway.
func (c Candidate) Score() float64 {
	if c.BuildDays <= 0 {
		return 0
	}
	value := float64(c.Retention) * float64(c.Fit) * float64(6-c.Saturation) / 5
	return value / float64(c.BuildDays)
}

// Obligation is what you still owe after it ships.
func (c Candidate) Obligation() string {
	if c.ContentSetsPerYear <= 0 {
		return ""
	}
	return fmt.Sprintf("%d hand-made puzzles a year, forever", c.ContentSetsPerYear)
}

// days, because "1 days" in a message on someone's phone reads as a machine
// that was not looked at.
func days(n int) string {
	if n == 1 {
		return "1 day"
	}
	return fmt.Sprintf("%d days", n)
}

func LoadLandscape(path string) (Landscape, error) {
	var l Landscape
	b, err := os.ReadFile(path)
	if err != nil {
		return l, err
	}
	if err := json.Unmarshal(b, &l); err != nil {
		return l, fmt.Errorf("%s: %w", path, err)
	}
	if len(l.Candidates) == 0 {
		return l, fmt.Errorf("%s: no candidates", path)
	}
	return l, nil
}

// Built returns what has already been shipped, so it can be shown rather than
// silently dropped: a candidate that vanishes from the table looks like an
// oversight, and one listed as done is a record.
func Built(l Landscape) []Candidate {
	var out []Candidate
	for _, c := range l.Candidates {
		if c.Shipped != "" {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Shipped > out[j].Shipped })
	return out
}

// Ranked returns candidates best-first, excluding anything already built.
func Ranked(l Landscape) []Candidate {
	out := make([]Candidate, 0, len(l.Candidates))
	for _, c := range l.Candidates {
		if c.Shipped == "" {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score() != out[j].Score() {
			return out[i].Score() > out[j].Score()
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// MissingMechanics is the gap analysis, and usually the most interesting line
// in the report. Six games that are all deduction and score-attack is a narrow
// portfolio wearing six hats, and no amount of adding a seventh score-attack
// game widens it.
func MissingMechanics(l Landscape) []string {
	have := map[string]bool{}
	for _, p := range l.Portfolio {
		have[p.Mechanic] = true
	}
	seen, missing := map[string]bool{}, []string{}
	for _, c := range l.Candidates {
		if c.Mechanic == "" || c.Mechanic == "meta" || have[c.Mechanic] || seen[c.Mechanic] {
			continue
		}
		seen[c.Mechanic] = true
		missing = append(missing, c.Mechanic)
	}
	sort.Strings(missing)
	return missing
}

// buildableThreshold: below this, a candidate is not worth the day it costs.
// It exists so the report REJECTS things out loud. A benchmark that only ever
// says yes is a wish list.
const buildableThreshold = 1.0

func Benchmark(l Landscape) string {
	var b strings.Builder
	ranked := Ranked(l)

	fmt.Fprintf(&b, "product benchmark — landscape as of %s, %d candidates open\n\n", l.AsOf, len(ranked))

	if built := Built(l); len(built) > 0 {
		fmt.Fprintf(&b, "BUILT   ")
		names := make([]string, 0, len(built))
		for _, c := range built {
			names = append(names, fmt.Sprintf("%s (%s)", c.ID, c.ShippedIn))
		}
		fmt.Fprintf(&b, "%s\n\n", strings.Join(names, ", "))
	}

	if m := MissingMechanics(l); len(m) > 0 {
		fmt.Fprintf(&b, "GAP     we ship no %s game at all\n", strings.Join(m, " and no "))
		fmt.Fprintf(&b, "        our six are deduction, score-attack, recall and adversarial — a narrow\n")
		fmt.Fprintf(&b, "        portfolio wearing six hats\n\n")
	}

	fmt.Fprintf(&b, "%-4s %-24s %6s %5s %s\n", "", "candidate", "score", "days", "why")
	for _, c := range ranked {
		mark := "  "
		if c.Score() < buildableThreshold {
			mark = "no"
		}
		fmt.Fprintf(&b, "%-4s %-24s %6.2f %5d %s\n", mark, c.ID, c.Score(), c.BuildDays, c.Evidence)
		if o := c.Obligation(); o != "" {
			fmt.Fprintf(&b, "     %-24s %s  ← owes %s\n", "", "", o)
		}
		if c.Constraint != "" {
			fmt.Fprintf(&b, "     %-24s %s  ← %s\n", "", "", c.Constraint)
		}
	}

	// Two recommendations, because value-per-day has a known bias: a one-day
	// tweak will always outrank a three-day platform change, and a roadmap of
	// nothing but one-day tweaks never becomes a better product. So the metric
	// picks the next day of work, and the same metric restricted to things that
	// change what the site IS picks the direction.
	best := ranked[0]
	fmt.Fprintf(&b, "\nDO NEXT %s (%s)\n        %s\n", best.Name, days(best.BuildDays), best.Evidence)

	for _, c := range ranked {
		if (c.Kind == "game" || c.Kind == "platform") && c.Score() >= buildableThreshold {
			fmt.Fprintf(&b, "\nDIRECT. %s (%s)\n        %s\n", c.Name, days(c.BuildDays), c.Evidence)
			if o := c.Obligation(); o != "" {
				fmt.Fprintf(&b, "        note: owes %s\n", o)
			}
			break
		}
	}

	// And the most interesting rejection, which is usually more instructive
	// than the winner.
	for _, c := range ranked {
		if c.Score() < buildableThreshold {
			fmt.Fprintf(&b, "\nSKIP    %s\n        %s\n", c.Name, c.Evidence)
			break
		}
	}

	fmt.Fprintf(&b, "\nSOURCES\n")
	for _, s := range l.Sources {
		fmt.Fprintf(&b, "        %s\n        %s\n", s.Claim, s.URL)
	}
	return b.String()
}
