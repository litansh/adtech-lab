package main

import (
	"fmt"
	"sort"
)

// ---------------------------------------------------------------------------
// The Reliability agent: can we afford to stay up?
//
// It wins every conflict by design. There is NO HARD MONETARY CAP on this
// stack -- AWS provides none for usage-based services and CloudFront flat-rate
// plans are unavailable on the Free plan -- so protection is layered rather
// than guaranteed, and a fleet that can spend money needs one member whose job
// is to stop.
//
// It is also the only agent allowed to act without a proposal step. Everything
// else proposes; Reliability may pull the fuse. Waiting for a human to approve
// a spend cut is how a runaway bill becomes a large runaway bill.
//
// What it watches, and why each threshold is what it is:
//
//   SPEND against the ceiling      $100/month is the stated maximum, and the
//                                  credits expire 2027-02-08. A projection that
//                                  crosses either is worth waking someone for.
//   ALARM state                    an alarm in ALARM is the system already
//                                  saying something; an alarm with INSUFFICIENT
//                                  _DATA for days is an alarm that will never
//                                  fire, which is worse than not having it.
//   ERROR rate and p95             the two numbers that say "up" or "not up".
//
// The distinction that matters most here is between a PAGE and a NOTE. An agent
// that treats everything as urgent gets muted, and a muted reliability agent is
// the most dangerous object in the system.
// ---------------------------------------------------------------------------

type Severity string

const (
	// SevPage is worth interrupting someone. Money is leaving, or the site is
	// down.
	SevPage Severity = "PAGE"
	// SevNote is worth reading tomorrow.
	SevNote Severity = "NOTE"
	// SevOK is reported only in a summary, never on its own.
	SevOK Severity = "OK"
)

// Signals is everything the agent reads. A struct rather than direct AWS calls
// so the judgement is testable without a cloud account -- the part worth
// getting right is what counts as a problem, not how to fetch a metric.
type Signals struct {
	MonthToDateUSD float64
	DaysIntoMonth  int
	DaysInMonth    int
	CeilingUSD     float64
	CreditsLeftUSD float64
	DaysToExpiry   int

	Invocations  int
	Errors       int
	P95LatencyMS float64

	// Alarms is name -> state, as CloudWatch reports it.
	Alarms map[string]string
}

type Finding struct {
	Severity Severity `json:"severity"`
	Check    string   `json:"check"`
	Detail   string   `json:"detail"`
	Action   string   `json:"action"`
}

const (
	// ErrorRatePage is where errors stop being noise. Below this a handful of
	// failures is a bot hitting a bad URL; above it, something is broken.
	ErrorRatePage = 0.05
	// LatencyPageMS: the ad server's own budget is a fraction of this. At this
	// point buyers are timing out and the auction is degrading.
	LatencyPageMS = 400
	// ProjectedOverspendPage is how far past the ceiling a projection has to
	// land before it is worth an interruption. Small overshoots at this scale
	// are noise.
	ProjectedOverspendPage = 1.10
	// ErrorRateNote is the noise floor. Below it, errors are a bot hitting a
	// bad URL and reporting them trains the reader to skim. The first version
	// noted 2 errors in 5000 -- 0.04% -- which is the exact behaviour the
	// severity split exists to prevent.
	ErrorRateNote = 0.005
)

// Assess turns signals into findings, ordered most urgent first.
func Assess(s Signals) []Finding {
	var out []Finding

	// --- spend ---
	if s.DaysIntoMonth > 0 && s.DaysInMonth > 0 && s.CeilingUSD > 0 {
		projected := s.MonthToDateUSD / float64(s.DaysIntoMonth) * float64(s.DaysInMonth)
		ratio := projected / s.CeilingUSD
		switch {
		case ratio >= ProjectedOverspendPage:
			out = append(out, Finding{SevPage, "spend",
				fmt.Sprintf("$%.2f so far, projecting $%.2f against a $%.2f ceiling",
					s.MonthToDateUSD, projected, s.CeilingUSD),
				"Investigate before it lands. There is no hard cap; the fuse is the only stop."})
		case ratio >= 0.8:
			out = append(out, Finding{SevNote, "spend",
				fmt.Sprintf("projecting $%.2f of a $%.2f ceiling (%.0f%%)",
					projected, s.CeilingUSD, ratio*100),
				"Worth a look, not worth waking up for."})
		default:
			out = append(out, Finding{SevOK, "spend",
				fmt.Sprintf("$%.2f so far, projecting $%.2f of $%.2f",
					s.MonthToDateUSD, projected, s.CeilingUSD), ""})
		}
	}

	// --- credits ---
	if s.CreditsLeftUSD > 0 && s.DaysToExpiry > 0 && s.DaysIntoMonth > 0 {
		daily := s.MonthToDateUSD / float64(s.DaysIntoMonth)
		if daily > 0 {
			daysOfCredit := s.CreditsLeftUSD / daily
			if daysOfCredit < float64(s.DaysToExpiry) {
				// Running out before the expiry date means the date stops being
				// the deadline -- which changes the plan, not just the budget.
				out = append(out, Finding{SevNote, "credits",
					fmt.Sprintf("$%.2f left, ~%.0f days at the current rate, but %d days until expiry",
						s.CreditsLeftUSD, daysOfCredit, s.DaysToExpiry),
					"Credits run out before they expire. The spending decision arrives sooner than the date suggests."})
			}
		}
	}

	// --- errors ---
	if s.Invocations > 100 {
		rate := float64(s.Errors) / float64(s.Invocations)
		if rate >= ErrorRatePage {
			out = append(out, Finding{SevPage, "errors",
				fmt.Sprintf("%.1f%% of %d invocations failed", rate*100, s.Invocations),
				"The ad server is failing requests. Games still work; monetisation does not."})
		} else if rate >= ErrorRateNote {
			out = append(out, Finding{SevNote, "errors",
				fmt.Sprintf("%d errors in %d invocations (%.2f%%)", s.Errors, s.Invocations, rate*100), ""})
		}
	}

	// --- latency ---
	if s.P95LatencyMS >= LatencyPageMS {
		out = append(out, Finding{SevPage, "latency",
			fmt.Sprintf("p95 is %.0fms", s.P95LatencyMS),
			"Buyers are timing out and the auction is degrading. Check tmax and buyer health."})
	}

	// --- alarms ---
	var firing, blind []string
	for name, state := range s.Alarms {
		switch state {
		case "ALARM":
			firing = append(firing, name)
		case "INSUFFICIENT_DATA":
			blind = append(blind, name)
		}
	}
	sort.Strings(firing)
	sort.Strings(blind)

	// "I read eight alarms and all are fine" and "I could not read any alarms"
	// look identical if you only report problems -- and for a reliability
	// agent that is the dangerous confusion, because silence is exactly what a
	// broken agent produces.
	if len(s.Alarms) == 0 {
		out = append(out, Finding{SevNote, "alarms",
			"no alarms could be read",
			"This is not 'nothing is wrong'. It is 'I cannot see', which is worse."})
	} else if len(firing) == 0 && len(blind) == 0 {
		out = append(out, Finding{SevOK, "alarms",
			fmt.Sprintf("%d alarms, all OK", len(s.Alarms)), ""})
	}

	if len(firing) > 0 {
		out = append(out, Finding{SevPage, "alarms",
			fmt.Sprintf("%d in ALARM: %v", len(firing), firing),
			"The system is already saying something. Read it before doing anything else."})
	}
	if len(blind) > 0 {
		// An alarm that has never had data will never fire. It is not
		// protection, it is the appearance of protection -- which is worse,
		// because it stops anyone looking.
		out = append(out, Finding{SevNote, "alarms",
			fmt.Sprintf("%d with no data: %v", len(blind), blind),
			"These cannot fire. An alarm with no data is the appearance of protection, not protection."})
	}

	rank := map[Severity]int{SevPage: 0, SevNote: 1, SevOK: 2}
	sort.SliceStable(out, func(i, j int) bool { return rank[out[i].Severity] < rank[out[j].Severity] })
	return out
}

// Worst is what a notifier acts on.
func Worst(fs []Finding) Severity {
	for _, f := range fs {
		if f.Severity == SevPage {
			return SevPage
		}
	}
	for _, f := range fs {
		if f.Severity == SevNote {
			return SevNote
		}
	}
	return SevOK
}
