package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// Turning a finding into work.
//
// The benchmark has been running daily in three workflows, printing a ranked
// answer, sending it to a phone -- and stopping there. Nothing opened an issue,
// nothing reached the backlog, and the recommendation evaporated every night.
//
// An agent whose findings never become tracked work is a very well-tested
// opinion generator. The whole argument of this fleet is that a proposal
// reaches a human as something they can accept or decline, and a Telegram
// message that scrolls away is neither.
//
// It raises an ISSUE, not a pull request. The split is the authority rule in
// CLAUDE.md: a PARAMETER change is a diff a human merges, an OBSERVATION is a
// discussion. "Build near-miss feedback" is neither a parameter nor a diff this
// agent could write -- it is product work, which is architecture, and
// architecture is where agents stop.

// Proposal is what the agent currently recommends, remembered so that the same
// recommendation is not raised every night.
type Proposal struct {
	Next      string `json:"next"`      // candidate id: the best next day of work
	Direction string `json:"direction"` // candidate id: the strategic pick
	RaisedOn  string `json:"raised_on"`
}

func LoadProposal(path string) Proposal {
	var p Proposal
	b, err := os.ReadFile(path)
	if err != nil {
		return p
	}
	_ = json.Unmarshal(b, &p)
	return p
}

func SaveProposal(path string, p Proposal) error {
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// Recommend returns the two picks the report makes: the best next day of work,
// and the best thing that changes what the site IS. Both, because value per day
// has a known bias toward one-day tweaks and a roadmap of tweaks never becomes
// a better product.
func Recommend(l Landscape) (next, direction Candidate, ok bool) {
	ranked := Ranked(l)
	if len(ranked) == 0 {
		return next, direction, false
	}
	next = ranked[0]
	for _, c := range ranked {
		if (c.Kind == "game" || c.Kind == "platform") && c.Score() >= buildableThreshold {
			direction = c
			break
		}
	}
	return next, direction, true
}

// Changed reports whether this is worth raising. Unchanged recommendations are
// silent: an agent that files the same issue every night gets muted, and then
// the night it finds something new is the night nobody reads it.
func Changed(prev Proposal, next, direction Candidate) bool {
	return prev.Next != next.ID || prev.Direction != direction.ID
}

// IssueBody is what a human reads before deciding. It leads with the evidence
// rather than the score, because the score is this tool's opinion and the
// evidence is not.
func IssueBody(l Landscape, next, direction Candidate) string {
	b := fmt.Sprintf("The Product agent's ranking changed. Nothing here is applied; "+
		"this is a proposal.\n\n## Do next — %s\n\n**%s**\n\n> %s\n\n%s to build.\n",
		next.ID, next.Name, next.Evidence, days(next.BuildDays))
	if o := next.Obligation(); o != "" {
		b += fmt.Sprintf("\n**It also owes %s.** That is a recurring cost, not a build cost.\n", o)
	}

	if direction.ID != "" && direction.ID != next.ID {
		b += fmt.Sprintf("\n## Direction — %s\n\n**%s**\n\n> %s\n\n%s to build.\n",
			direction.ID, direction.Name, direction.Evidence, days(direction.BuildDays))
		if o := direction.Obligation(); o != "" {
			b += fmt.Sprintf("\n**It also owes %s.**\n", o)
		}
		b += "\nTwo picks, because value per day of work always favours a one-day tweak, " +
			"and a roadmap made only of tweaks never becomes a better product.\n"
	}

	if m := MissingMechanics(l); len(m) > 0 {
		b += fmt.Sprintf("\n## Gap\n\nWe ship no %s game at all.\n", join(m))
	}

	b += "\n## The full ranking\n\n```\n" + Benchmark(l) + "```\n"
	b += "\n---\n\nRaised by `tools/productctl` from `product/landscape.json`, which the " +
		"Horizon agent refreshes weekly. **This agent has never run in shadow mode**, so " +
		"treat the ranking as a suggestion from something with no track record.\n"
	return b
}

func join(xs []string) string {
	switch len(xs) {
	case 0:
		return ""
	case 1:
		return xs[0]
	}
	out := ""
	for i, x := range xs {
		switch {
		case i == 0:
			out = x
		case i == len(xs)-1:
			out += " and no " + x
		default:
			out += ", no " + x
		}
	}
	return out
}
