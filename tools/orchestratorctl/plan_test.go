package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func base() World {
	return World{
		Now:       time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		Start:     time.Date(2026, 8, 29, 0, 0, 0, 0, time.UTC),
		Declared:  map[string]bool{},
		LiveGames: -1, RedditAgeDays: -1, RedditPosts: -1, RedditComments: -1,
		ItchViews7d: -1, ItchPlays7d: -1,
	}
}

func find(as []Assessment, id string) Assessment {
	for _, a := range as {
		if a.Step.ID == id {
			return a
		}
	}
	panic("no step " + id)
}

// The rule the whole tool exists for: a step someone marked done, which the
// world says is not done, must be surfaced -- not quietly believed.
func TestDeclaredButNotTrueIsAConflict(t *testing.T) {
	w := base()
	w.ItchUser = "litansh"
	w.LiveGames = 0 // checked, and genuinely nothing there
	w.Declared["publish-sudoku"] = true

	got := find(Assess(w), "publish-sudoku")
	if got.Status != StatusConflict {
		t.Fatalf("want conflict, got %s (%s)", got.Status, got.Note)
	}
}

// ...but a check that could not RUN is not evidence of anything, so a
// declaration stands. Treating "I could not reach itch.io" as "you published
// nothing" would send someone to re-upload six games.
func TestUnreachableCheckDoesNotOverrideADeclaration(t *testing.T) {
	w := base()
	w.ItchUser = "litansh"
	w.LiveGames = -1 // network failed
	w.Declared["publish-sudoku"] = true

	got := find(Assess(w), "publish-sudoku")
	if got.Status != StatusDone {
		t.Fatalf("want done, got %s (%s)", got.Status, got.Note)
	}
}

// And the same unknown, WITHOUT a declaration, must not become "done".
func TestUnknownAndUndeclaredIsNotDone(t *testing.T) {
	w := base()
	w.ItchUser = "litansh"
	w.LiveGames = -1
	if got := find(Assess(w), "publish-sudoku"); got.Status == StatusDone {
		t.Fatal("an unverifiable, undeclared step must never report done")
	}
}

// The reason this is a program and not a checklist.
func TestRedditPostIsBlockedByAccountAgeAlone(t *testing.T) {
	w := base()
	w.ItchUser, w.RedditUser = "litansh", "someone"
	w.LiveGames, w.RedditAgeDays, w.RedditPosts, w.RedditComments = 6, 3, 0, 5

	got := find(Assess(w), "reddit-post")
	if got.Status != StatusBlocked {
		t.Fatalf("want blocked, got %s", got.Status)
	}
	if !strings.Contains(got.Note, "Unblocks in 11 days") {
		t.Fatalf("the note must say when it unblocks, got %q", got.Note)
	}

	w.RedditAgeDays = 14
	if got := find(Assess(w), "reddit-post"); got.Status != StatusReady {
		t.Fatalf("at 14 days it should be ready, got %s (%s)", got.Status, got.Note)
	}
}

// One next action, and it is the first available one in plan order.
func TestNextIsTheFirstReadyStep(t *testing.T) {
	w := base()
	n, ok := Next(Assess(w))
	if !ok {
		t.Fatal("something must always be available")
	}
	if n.Step.ID != "reddit-account" {
		t.Fatalf("on a fresh plan the first action is the reddit account (it ages); got %s", n.Step.ID)
	}
}

// A blocked step must never be offered as the next action.
func TestNextSkipsBlocked(t *testing.T) {
	w := base()
	w.RedditUser, w.RedditAgeDays, w.RedditComments, w.RedditPosts = "someone", 1, 0, 0
	w.ItchUser, w.LiveGames = "litansh", 0
	for _, a := range Assess(w) {
		if a.Status == StatusReady && a.Step.ID == "reddit-post" {
			t.Fatal("reddit-post is offered while the account is one day old")
		}
	}
}

// A pointer to a document that no longer exists is a plan that sends someone
// to a dead end, and nothing else in CI would catch it.
func TestEveryStepPointsAtAFileThatExists(t *testing.T) {
	for _, s := range plan() {
		if s.Doc == "" {
			continue
		}
		path := strings.FieldsFunc(s.Doc, func(r rune) bool {
			return r == ' ' || r == ','
		})[0]
		if !strings.Contains(path, "/") && !strings.HasSuffix(path, ".md") {
			continue
		}
		if _, err := os.Stat("../../" + path); err != nil {
			t.Errorf("step %q points at %s, which does not exist", s.ID, path)
		}
	}
}

// Reddit answers 403 to anything that is not a browser, so the live age check
// fails from CI every time. If that left reddit-post blocked, the schedule
// could never advance past week one -- so a declared creation date has to be
// enough on its own.
func TestDeclaredRedditDateUnblocksWithoutTheAPI(t *testing.T) {
	st := State{
		StartedOn: "2026-08-01", ItchUser: "", RedditUser: "someone",
		RedditCreatedOn: "2026-08-01", Done: map[string]bool{},
		ItchViews7d: -1, ItchPlays7d: -1,
	}
	w := Observe(st, "", true /* offline */)
	if w.RedditAgeDays < 14 {
		t.Fatalf("an account created 2026-08-01 is well over 14 days old, got %d", w.RedditAgeDays)
	}
	if got := find(Assess(w), "reddit-account"); got.Status != StatusDone {
		t.Fatalf("want done from the declared date, got %s (%s)", got.Status, got.Note)
	}
}

// The first live run of the Orchestrator workflow reported success while
// sending nothing: notify used a path relative to the repository root, agents
// run with the tool as the working directory, and the error was swallowed.
// The run was green. This is the sixth time in this repository that something
// reported success for work that did not happen.
func TestNotifyScriptIsFoundFromTheToolDirectory(t *testing.T) {
	// Tests run with the tool directory as the working directory -- exactly
	// what `go -C tools/orchestratorctl run .` produces in CI.
	got, err := notifyScript()
	if err != nil {
		t.Fatalf("could not find telegram.py from the tool directory: %v", err)
	}
	if _, err := os.Stat(got); err != nil {
		t.Fatalf("found %s, which does not exist", got)
	}
}

// The seven-day window is the whole point of the threshold in
// docs/audience.md. A visitor last seen two months ago is a returning human,
// not a returning-this-week one, and counting them would drift the number
// upward forever as the site aged.
func TestReturningCountsOnlyTheSevenDayBuckets(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC().UnixMilli()
	lines := []string{
		`{"event":"session_start","session_id":"a","returning":true,"returning_bucket":"d0"}`,
		`{"event":"session_start","session_id":"b","returning":true,"returning_bucket":"d1_7"}`,
		`{"event":"session_start","session_id":"c","returning":true,"returning_bucket":"d30_plus"}`,
		`{"event":"session_start","session_id":"d","returning":false,"returning_bucket":"new"}`,
		// Storage blocked: no returning field at all. Must land in neither the
		// numerator nor the denominator.
		`{"event":"session_start","session_id":"e","returning_bucket":"unknown"}`,
	}
	var body string
	for _, l := range lines {
		body += strings.Replace(l, `"session_start"`, `"session_start","ts":`+itoa(now), 1) + "\n"
	}
	path := filepath.Join(dir, "e.ndjson")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	w := Observe(State{Done: map[string]bool{}, ItchViews7d: -1, ItchPlays7d: -1},
		filepath.Join(dir, "*.ndjson"), true)

	if !w.ReturningKnown {
		t.Fatal("four sessions answered — this is measurable now")
	}
	if w.Sessions7d != 5 {
		t.Fatalf("want 5 sessions, got %d", w.Sessions7d)
	}
	// 2 of the 4 that answered. The blocked browser is excluded from both.
	if w.Returning7d != 0.5 {
		t.Fatalf("want 0.50 (2 of 4 that answered), got %.2f", w.Returning7d)
	}
}

// Before the flag shipped this reported blindness. It must still do so when no
// session carries the field, rather than reporting a confident zero.
func TestNoFlagAtAllIsStillBlindness(t *testing.T) {
	dir := t.TempDir()
	now := itoa(time.Now().UTC().UnixMilli())
	body := `{"event":"session_start","session_id":"a","ts":` + now + "}\n"
	if err := os.WriteFile(filepath.Join(dir, "e.ndjson"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	w := Observe(State{Done: map[string]bool{}, ItchViews7d: -1, ItchPlays7d: -1},
		filepath.Join(dir, "*.ndjson"), true)
	if w.ReturningKnown {
		t.Fatal("no session reported the flag, so it is not known")
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// The Orchestrator is a briefing, not an alerter, and a briefing that arrives
// only when something changed is one nobody can rely on: its absence is
// unreadable. The reader cannot tell "nothing changed" from "the agent died".
func TestTheBriefingArrivesEvenOnAnUnchangedDay(t *testing.T) {
	dir := t.TempDir()
	last := filepath.Join(dir, "last.json")
	digest := "DO NOW\nCreate an itch.io account\n"
	now := time.Date(2026, 9, 1, 6, 0, 0, 0, time.UTC)

	recordSent(last, digest, now)

	if shouldSend(last, digest, now.Add(2*time.Hour)) {
		t.Fatal("a manual re-run two hours later must not repeat the same digest")
	}
	if !shouldSend(last, digest, now.Add(24*time.Hour)) {
		t.Fatal("the next morning it must arrive, unchanged or not")
	}
	// GitHub's scheduler drifts. Yesterday's run at 06:31, today's at 05:58, is
	// 23h27m apart -- an elapsed-hours rule skips the morning entirely, and a
	// skipped morning is indistinguishable from a dead agent.
	late := time.Date(2026, 9, 1, 6, 31, 0, 0, time.UTC)
	early := time.Date(2026, 9, 2, 5, 58, 0, 0, time.UTC)
	recordSent(last, digest, late)
	if !shouldSend(last, digest, early) {
		t.Fatal("a slightly early run the next day must still send")
	}
	if !shouldSend(last, digest+"something moved", now.Add(2*time.Hour)) {
		t.Fatal("a changed digest must go out immediately")
	}
}

// The orchestrator told Litan to apply to an affiliate programme the day the
// listings went up. PLAN.md says week 3, after the Reddit post, and for a
// reason: the application is judged on what you can show, and a rejection
// costs more than the wait.
//
// The gate encoded the plan's ORDERING badly by asking the weaker question --
// "is anything live" rather than "is anything worth pointing at". Encoding the
// reason instead means the tool and the plan cannot drift apart.
func TestAffiliateWaitsForTheRedditPostNotJustForListings(t *testing.T) {
	w := base()
	w.ItchUser, w.LiveGames = "litansh", 6
	w.RedditUser = "someone"
	w.RedditAgeDays, w.RedditComments, w.RedditPosts = 20, 3, 0

	got := find(Assess(w), "affiliate-apply")
	if got.Status != StatusBlocked {
		t.Fatalf("six listings and no post: want blocked, got %s", got.Status)
	}
	if !strings.Contains(got.Note, "r/WebGames") {
		t.Fatalf("the note must say what is missing, got %q", got.Note)
	}

	w.RedditPosts = 1
	if got := find(Assess(w), "affiliate-apply"); got.Status != StatusReady {
		t.Fatalf("with the post out it is ready, got %s (%s)", got.Status, got.Note)
	}
}

// A briefing that says "nothing to do" and nothing else is the least useful
// thing it can say, and that is exactly what shipped: the "Next up" line was
// found by searching the note for the word "unblocks", and capitalising that
// word one commit later deleted the line silently.
//
// A message assembled by grepping its own English is one wording change away
// from being wrong.
func TestNothingToDoStillSaysWhatIsNext(t *testing.T) {
	w := base()
	w.ItchUser, w.LiveGames = "litansh", 6
	w.RedditUser, w.RedditAgeDays, w.RedditComments, w.RedditPosts = "someone", 3, 4, 0
	w.Declared["phone-test"] = true

	as := Assess(w)
	if _, ok := Next(as); ok {
		t.Skip("something is available; this test is about the idle state")
	}

	msg := renderDigest(w, as, "too early to judge reach", "nothing is wrong yet")
	if !strings.Contains(msg, "Next up:") {
		t.Fatalf("an idle briefing must say what is coming:\n%s", msg)
	}
	if !strings.Contains(msg, "r/WebGames") {
		t.Fatalf("it must name the thing that is waiting:\n%s", msg)
	}
	// The specific regression: the line must not depend on the casing of a
	// word in the prose.
	if strings.Contains(msg, "Next up: \n") {
		t.Fatal("the note was dropped")
	}
}
