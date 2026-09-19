package main

import (
	"fmt"
	"time"
)

// The plan, as data.
//
// PLAN.md is the human copy; this is the same plan in a form something can
// reason about. They must agree, and plan_test.go asserts the step IDs appear
// in PLAN.md so a step cannot be quietly added here and never written down.
//
// The interesting field is Blocker. A checklist assumes step N+1 becomes
// available when step N is ticked; this plan does not. A Reddit account has to
// AGE for two weeks, and no amount of finishing the previous step makes that
// happen sooner. Encoding "why you cannot do this yet, and when you can" is the
// entire reason this is a program rather than a list.

type Status string

const (
	StatusDone     Status = "done"
	StatusReady    Status = "ready"
	StatusBlocked  Status = "blocked"
	StatusConflict Status = "conflict"
)

// Step is one thing to do. Verified and Blocker are optional; a step with
// neither is one a human simply does and reports.
type Step struct {
	ID      string
	Phase   string
	Title   string
	Minutes int
	Doc     string // the one file to open
	Why     string // one line, only where the reason is not obvious

	// Verified reports whether the world shows this done. known=false means the
	// check could not run (no network, no username), which is NOT the same as
	// "not done" and must never be reported as one.
	Verified func(World) (ok bool, note string, known bool)

	// Blocker returns why this cannot be started yet, or "" if it can.
	Blocker func(World) string
}

type Assessment struct {
	Step   Step
	Status Status
	Note   string
}

// World is everything observed, gathered once before any assessment so that
// evaluating the plan is pure and testable.
type World struct {
	Now   time.Time
	Start time.Time // when the plan started

	// Declared by a human in state/plan.json, for the steps nothing can check.
	Declared map[string]bool

	ItchUser   string
	RedditUser string

	// Negative means unknown. Zero means checked, and genuinely zero. Conflating
	// the two is how a report says "no traffic" when it means "no credentials".
	LiveGames      int
	RedditAgeDays  int
	RedditPosts    int
	RedditComments int

	Sessions7d  int
	Returning7d float64
	// ReturningKnown is false until session_start carries a returning flag.
	// Distinguished from Returning7d==0 on purpose: "nobody came back" and "we
	// never asked" look identical in a number and mean opposite things.
	ReturningKnown bool
	Replays7d      int
	GameViews7d    int
	RevenueUSD7d   float64

	// ItchViews/Plays come from a human reading the itch dashboard, because itch
	// publishes no API and our games are served from itch's servers, not ours.
	// See funnel.go for what this costs us.
	ItchViews7d int
	ItchPlays7d int

	OpenPRs    []Item
	OpenIssues []Item

	// Health is whether each agent has actually run, which is a different
	// question from whether it could.
	Health []Health

	// LiveSince is when the first game reached itch. A funnel stage cannot be
	// judged before there has been time for anyone to reach it, and a stage
	// declared broken on its first day is the mirror image of an unknown
	// reported as a zero.
	LiveSince time.Time

	// Notes records why a check could not run, so the report can say so.
	Notes []string
}

type Item struct {
	Number int
	Title  string
	URL    string
}

const totalGames = 6

// redditMinAgeDays is r/WebGames' rule, and typical of the gaming subreddits.
// Two weeks is not a guess: it is why PLAN.md creates the account on day one
// and posts on day fourteen.
const redditMinAgeDays = 14

// decisionAfterDays is the six-week mark from PLAN.md and docs/audience.md.
const decisionAfterDays = 42

func plan() []Step {
	return []Step{
		{
			ID: "reddit-account", Phase: "Today", Title: "Create a Reddit account", Minutes: 2,
			Doc: "PLAN.md step 1",
			Why: "it does nothing but age, and the ageing is the point",
			Verified: func(w World) (bool, string, bool) {
				if w.RedditUser == "" {
					return false, "no reddit_user in state/plan.json", false
				}
				if w.RedditAgeDays < 0 {
					return false, "put the creation date in reddit_created_on — reddit blocks automated reads", false
				}
				return true, fmt.Sprintf("u/%s, %d days old", w.RedditUser, w.RedditAgeDays), true
			},
		},
		{
			ID: "itch-account", Phase: "Today", Title: "Create an itch.io account", Minutes: 2,
			Doc: "PLAN.md step 2",
			Verified: func(w World) (bool, string, bool) {
				if w.ItchUser == "" {
					return false, "no itch_user in state/plan.json", false
				}
				if w.LiveGames < 0 {
					return false, "could not reach itch.io", false
				}
				return true, w.ItchUser + ".itch.io responds", true
			},
		},
		{
			ID: "publish-sudoku", Phase: "Today", Title: "Publish Sudoku on itch", Minutes: 10,
			Doc: "docs/GO-LIVE.md step 1.2 — text in dist/itch/LISTINGS.md §1, file dist/itch/sudoku.zip",
			Verified: func(w World) (bool, string, bool) {
				if w.LiveGames < 0 {
					return false, "could not reach itch.io", false
				}
				return w.LiveGames >= 1, fmt.Sprintf("%d/%d games live", w.LiveGames, totalGames), true
			},
			Blocker: func(w World) string {
				if w.ItchUser == "" {
					return "no itch account yet"
				}
				return ""
			},
		},
		{
			ID: "phone-test", Phase: "Today", Title: "Play the itch page once on your phone", Minutes: 1,
			Doc: "PLAN.md step 4",
			Why: "an itch embed is a different environment from our own site",
			Blocker: func(w World) string {
				if w.LiveGames <= 0 {
					return "nothing published yet"
				}
				return ""
			},
		},
		{
			ID: "publish-rest", Phase: "Today", Title: "Publish the other five games", Minutes: 35,
			Doc: "docs/GO-LIVE.md step 1.4 — the per-game table",
			Verified: func(w World) (bool, string, bool) {
				if w.LiveGames < 0 {
					return false, "could not reach itch.io", false
				}
				return w.LiveGames >= totalGames, fmt.Sprintf("%d/%d games live", w.LiveGames, totalGames), true
			},
			Blocker: func(w World) string {
				if w.LiveGames <= 0 {
					return "publish Sudoku first — the first one teaches the form"
				}
				return ""
			},
		},
		{
			ID: "reddit-warmup", Phase: "This week", Title: "Leave two or three real comments on Reddit", Minutes: 10,
			Doc: "PLAN.md step 6",
			Why: "an account with no history posting its own link is what the rules exist to stop",
			Verified: func(w World) (bool, string, bool) {
				if w.RedditComments < 0 {
					return false, "could not read reddit history", false
				}
				return w.RedditComments >= 2, plural(w.RedditComments, "comment"), true
			},
			Blocker: func(w World) string {
				if w.RedditUser == "" {
					return "no reddit account yet"
				}
				return ""
			},
		},
		{
			ID: "reddit-post", Phase: "Week 2", Title: "Post to r/WebGames", Minutes: 15,
			Doc: "docs/distribution-playbook.md, step 3 — title and body are written",
			Verified: func(w World) (bool, string, bool) {
				if w.RedditPosts < 0 {
					return false, "could not read reddit history", false
				}
				return w.RedditPosts >= 1, plural(w.RedditPosts, "post") + " linking to us", true
			},
			Blocker: func(w World) string {
				if w.LiveGames <= 0 {
					return "nothing to link to yet"
				}
				if w.RedditAgeDays < 0 {
					return "cannot check the account age, and posting too early burns the account"
				}
				if w.RedditAgeDays < redditMinAgeDays {
					d := redditMinAgeDays - w.RedditAgeDays
					return fmt.Sprintf("the account is %d days old; r/WebGames wants %d. %s.",
						w.RedditAgeDays, redditMinAgeDays, unblocksIn(d))
				}
				return ""
			},
		},
		{
			ID: "reddit-replies", Phase: "Week 2", Title: "Reply to every comment, then again tomorrow", Minutes: 15,
			Doc: "PLAN.md step 9",
			Why: "this is the difference between a post that does well and one that vanishes",
			Blocker: func(w World) string {
				if w.RedditPosts <= 0 {
					return "nothing posted yet"
				}
				return ""
			},
		},
		{
			ID: "affiliate-apply", Phase: "Week 3", Title: "Apply to an affiliate programme", Minutes: 30,
			Doc: "docs/affiliate-guide.md",
			Why: "an application pointing at six live listings and a Reddit thread is a much stronger one",
			Blocker: func(w World) string {
				if w.LiveGames <= 0 {
					return "nothing live to point the application at"
				}
				// PLAN.md puts this AFTER the Reddit post, and the gate here
				// used to be "is there anything live", which six listings
				// satisfied the moment they went up. That is the weaker
				// question. The plan's reasoning is whether there is anything
				// WORTH pointing at: an affiliate programme reviews the site,
				// and an application showing six pages with one view between
				// them invites a rejection that costs more than the wait --
				// several programmes make you reapply cold.
				//
				// So the real blocker is the post, which is itself waiting on
				// the account to age. Encoding the plan's reason rather than
				// its ordering means the two cannot drift apart.
				if w.RedditPosts < 0 {
					return "cannot tell whether the Reddit post has gone out yet"
				}
				if w.RedditPosts == 0 {
					return "post to r/WebGames first — an application pointing at a live thread " +
						"is a much stronger one, and a rejection costs more than the wait"
				}
				return ""
			},
		},
		{
			ID: "week6-decision", Phase: "Week 6", Title: "Read the numbers and make the call", Minutes: 20,
			Doc: "docs/audience.md — the thresholds, written before either of us had a stake in the answer",
			Blocker: func(w World) string {
				if w.Start.IsZero() {
					return "no started_on in state/plan.json, so there is no six-week mark"
				}
				d := decisionAfterDays - int(w.Now.Sub(w.Start).Hours()/24)
				if d > 0 {
					return fmt.Sprintf("six weeks is not up. %s.", unblocksIn(d))
				}
				if w.Sessions7d <= 0 {
					return "no sessions in the last 7 days — there is nothing to decide on yet"
				}
				return ""
			},
		},
	}
}

// Capitalised, because it follows a full stop in every caller. A note that
// reads "wants 14. unblocks in 11 days" is a note assembled by a program.
func unblocksIn(days int) string {
	if days <= 1 {
		return "Unblocks tomorrow"
	}
	return fmt.Sprintf("Unblocks in %d days", days)
}

// Assess turns observation into status. The rule that matters is the last one:
// a step a human declared done, which the world says is NOT done, is a
// CONFLICT rather than a quiet "done". Six separate bugs in this repository
// were something reporting success for work that did not happen, and a plan
// tracker is an easy seventh.
func Assess(w World) []Assessment {
	out := make([]Assessment, 0, len(plan()))
	for _, s := range plan() {
		declared := w.Declared[s.ID]
		progress := ""

		if s.Verified != nil {
			ok, note, known := s.Verified(w)
			switch {
			case known && ok:
				out = append(out, Assessment{s, StatusDone, note})
				continue
			case known && !ok && declared:
				out = append(out, Assessment{s, StatusConflict,
					"you marked this done, but " + note})
				continue
			case !known && declared:
				out = append(out, Assessment{s, StatusDone, "declared — " + note})
				continue
			case !known:
				// Unverifiable and undeclared: fall through and treat it as
				// outstanding. Assuming it is done because we cannot see it is
				// the failure mode this whole tool exists to avoid.
			case known && !ok:
				// Checked, genuinely not done — but the check knows HOW FAR
				// along it is, and throwing that away turns "one of the two
				// comments is written" into an undifferentiated to-do.
				progress = note
			}
		} else if declared {
			out = append(out, Assessment{s, StatusDone, "declared"})
			continue
		}

		if s.Blocker != nil {
			if why := s.Blocker(w); why != "" {
				out = append(out, Assessment{s, StatusBlocked, why})
				continue
			}
		}
		out = append(out, Assessment{s, StatusReady, progress})
	}
	return out
}

// Next is the single thing to do now. One, not a list: a plan that offers five
// choices is a plan that gets none of them done.
func Next(as []Assessment) (Assessment, bool) {
	for _, a := range as {
		if a.Status == StatusReady {
			return a, true
		}
	}
	return Assessment{}, false
}
