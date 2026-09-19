package main

import "fmt"

// The funnel, and the one rule for reading it.
//
// Distribution -> reach -> click -> play -> return -> revenue. Each stage can
// only be as healthy as the one before it, so the orchestrator reports the
// FIRST broken stage and recommends only that. Improving stage four while
// stage two is broken is work that cannot show up in the numbers, and it is the
// most common way a small product burns a month.
//
// The thresholds below are opinions, and they are written here rather than
// argued about later. Two of them come from docs/audience.md, which was written
// before there was any data to be disappointed by.

// minReachDays: how long a listing must have existed before its reach can be
// called bad.
//
// The first reading after publishing was one view across six games, hours old,
// and the funnel called reach broken. That is true and useless: nothing had had
// time to happen. A week is the shortest span in which "nobody is finding this"
// is a statement about the pages rather than about the clock.
const minReachDays = 7

const (
	// minWeeklyViews: below this, nothing downstream has enough volume to mean
	// anything. Fifty is not a business, it is the floor for a signal.
	minWeeklyViews = 50

	// minPlayRate: on an itch browse page the cover image is essentially the
	// entire pitch, so a low view->play rate is a cover image problem far more
	// often than a game problem.
	minPlayRate = 0.25

	// minReplayRate and minReturnRate are from docs/audience.md.
	minReplayRate = 0.35
	minReturnRate = 0.15
)

type Stage struct {
	Name string
	Have string
	OK   bool
	// Known is false when the signal could not be read at all.
	Known bool
	// TooEarly is true when the signal was read and there has not yet been
	// enough TIME for it to mean anything. Distinct from !Known and from !OK:
	// "I could not look", "nobody came" and "nobody has had the chance yet"
	// are three different findings and only one of them is a problem.
	TooEarly bool
	Reading  string // what a failure at this stage actually means
	Action   string // the single thing to do about it
}

// Diagnose returns every stage plus the verdict: the first stage that is
// broken, or the first stage we are blind at. Being blind early is reported as
// blindness, not as health -- a funnel that silently skips the stage it cannot
// see will confidently blame the stage it can.
func Diagnose(w World) (stages []Stage, verdict string, action string) {
	pct := func(n, d int) string {
		if d <= 0 {
			return "—"
		}
		return fmt.Sprintf("%.0f%% (%d/%d)", 100*float64(n)/float64(d), n, d)
	}

	stages = []Stage{
		{
			Name: "distribution", Known: w.LiveGames >= 0,
			Have:    distHave(w),
			OK:      w.LiveGames >= 1,
			Reading: "there is nowhere for anyone to find this",
			Action:  "publish Sudoku — docs/GO-LIVE.md step 1.2",
		},
		{
			Name: "reach", Known: w.ItchViews7d >= 0,
			TooEarly: tooEarly(w, minReachDays),
			Have:     countOrUnknown(w.ItchViews7d, "views/wk") + liveFor(w),
			OK:       w.ItchViews7d >= minWeeklyViews,
			Reading:  "the pages exist and nobody is finding them",
			Action: "this is a tags-and-a-Reddit-post problem, not a build-more-games problem. " +
				"docs/distribution-playbook.md",
		},
		{
			Name: "click", Known: w.ItchViews7d > 0 && w.ItchPlays7d >= 0,
			Have:    pct(w.ItchPlays7d, w.ItchViews7d),
			OK:      w.ItchViews7d > 0 && float64(w.ItchPlays7d)/float64(w.ItchViews7d) >= minPlayRate,
			Reading: "people see the listing and do not open it",
			Action: "the cover image is almost the whole pitch on a browse page. " +
				"Replace it with a short loop of actual play before changing anything in the game",
		},
		{
			Name: "play", Known: w.GameViews7d > 0,
			Have:    pct(w.Replays7d, w.GameViews7d) + " replay",
			OK:      w.GameViews7d > 0 && float64(w.Replays7d)/float64(w.GameViews7d) >= minReplayRate,
			Reading: "they open it and do not play a second round",
			Action: "the first thirty seconds are the problem — difficulty ramp or first-move clarity, " +
				"not content volume",
		},
		{
			Name: "return", Known: w.ReturningKnown,
			Have:    returnHave(w),
			OK:      w.ReturningKnown && w.Returning7d >= minReturnRate,
			Reading: "they play once and never come back — the single worst signal in docs/audience.md",
			Action:  returnAction(w),
		},
		{
			Name: "revenue", Known: w.Sessions7d > 0,
			Have:    fmt.Sprintf("$%.4f over %d sessions", w.RevenueUSD7d, w.Sessions7d),
			OK:      w.RevenueUSD7d > 0,
			Reading: "traffic that earns nothing",
			Action: "this is an ad stack question, not a product one — check buyer fill in the " +
				"nightly report before touching the games",
		},
	}

	for _, s := range stages {
		if !s.Known {
			return stages, "blind at " + s.Name, s.Action
		}
		if s.TooEarly && !s.OK {
			// Stop here rather than reading past it. Every later stage depends
			// on this one having had a chance, so judging them now would be
			// measuring the clock in four more places.
			return stages, "too early to judge " + s.Name + " — " + liveForBare(w),
				"nothing is wrong yet. The first real test is the r/WebGames post, " +
					"which unblocks once the account is fourteen days old"
		}
		if !s.OK {
			return stages, s.Name + " is the earliest broken stage: " + s.Reading, s.Action
		}
	}
	return stages, "every stage healthy", "keep going — nothing here needs attention"
}

// returnAction says something different depending on whether the stage failed
// or was never measurable, because "nobody returns" and "we cannot see returns"
// call for completely different work.
func distHave(w World) string {
	if w.LiveGames < 0 {
		return "unknown — could not reach itch.io"
	}
	return fmt.Sprintf("%d/%d games on itch", w.LiveGames, totalGames)
}

func returnAction(w World) string {
	if !w.ReturningKnown {
		return "adlab.js keeps session identity in sessionStorage only, so a visitor on Tuesday " +
			"and the same visitor on Thursday are two strangers. The 15% threshold in " +
			"docs/audience.md cannot be evaluated until session_start records a returning FLAG " +
			"(not an identity — a boolean saying this browser has played before)"
	}
	return "the daily puzzle is the only thing here designed to be a reason to come back. " +
		"Put it in front of them at the end of a game before building anything new"
}

func returnHave(w World) string {
	if !w.ReturningKnown {
		return "not measurable"
	}
	return fmt.Sprintf("%.0f%% returning", 100*w.Returning7d)
}

func countOrUnknown(n int, unit string) string {
	if n < 0 {
		return "unknown"
	}
	return fmt.Sprintf("%d %s", n, unit)
}

// tooEarly reports whether the site has been reachable for long enough that a
// bad number means something.
func tooEarly(w World, days int) bool {
	if w.LiveSince.IsZero() {
		return false // nothing known about when it went up; judge it normally
	}
	return w.Now.Sub(w.LiveSince).Hours() < float64(days*24)
}

func liveDays(w World) int {
	if w.LiveSince.IsZero() {
		return -1
	}
	return int(w.Now.Sub(w.LiveSince).Hours() / 24)
}

func liveFor(w World) string {
	if d := liveDays(w); d >= 0 && d < minReachDays {
		return fmt.Sprintf(", live %s", plural(d, "day"))
	}
	return ""
}

func liveForBare(w World) string {
	d := liveDays(w)
	if d < 0 {
		return "the pages are new"
	}
	return fmt.Sprintf("the pages have been live for %s", plural(d, "day"))
}

func plural(n int, unit string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", unit)
	}
	return fmt.Sprintf("%d %ss", n, unit)
}
