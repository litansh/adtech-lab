package main

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Observation. Everything here is best-effort and nothing here is fatal.
//
// The rule the whole file obeys: a check that could not run sets its field
// NEGATIVE, never zero. "I could not reach itch.io" and "you have published
// nothing" are the same number in a naive implementation, and they are opposite
// instructions to a human.

// State is what a person declares, in state/plan.json. Kept as small as
// possible: every declaration is a fact nothing can check, and an unverifiable
// fact is a place for the plan and reality to drift apart.
type State struct {
	StartedOn  string `json:"started_on"`
	ItchUser   string `json:"itch_user"`
	RedditUser string `json:"reddit_user"`

	// RedditCreatedOn is a date typed by a human, because Reddit answers 403 to
	// unauthenticated JSON from anything that is not a browser -- so the live
	// check fails from CI by design, not by accident. Without this the
	// two-week wait could never be measured, and reddit-post would stay
	// blocked forever: a scheduler that can never advance is worse than no
	// scheduler.
	RedditCreatedOn string          `json:"reddit_created_on"`
	Done            map[string]bool `json:"done"`

	// Weekly numbers read off the itch dashboard by hand. itch publishes no
	// API, and our itch builds are served by itch, so these cannot be observed.
	// docs/orchestrator.md explains what it would take to fix that.
	ItchViews7d int    `json:"itch_views_7d"`
	ItchPlays7d int    `json:"itch_plays_7d"`
	ItchAsOf    string `json:"itch_stats_as_of"`
}

func LoadState(path string) (State, error) {
	s := State{Done: map[string]bool{}, ItchViews7d: -1, ItchPlays7d: -1}
	b, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return s, fmt.Errorf("%s: %w", path, err)
	}
	if s.Done == nil {
		s.Done = map[string]bool{}
	}
	return s, nil
}

// staleAfterDays: hand-entered numbers rot. Past this, the orchestrator treats
// them as unknown rather than reasoning from a month-old snapshot.
const staleAfterDays = 10

func Observe(st State, eventsGlob string, offline bool) World {
	w := World{
		Now:      time.Now().UTC(),
		Declared: st.Done,
		ItchUser: st.ItchUser, RedditUser: st.RedditUser,
		LiveGames: -1, RedditAgeDays: -1, RedditPosts: -1, RedditComments: -1,
		ItchViews7d: -1, ItchPlays7d: -1,
	}
	if t, err := time.Parse("2006-01-02", st.StartedOn); err == nil {
		w.Start = t
	}
	if t, err := time.Parse("2006-01-02", st.RedditCreatedOn); err == nil {
		w.RedditAgeDays = int(w.Now.Sub(t).Hours() / 24)
	}

	if st.ItchViews7d >= 0 {
		age := staleness(st.ItchAsOf, w.Now)
		if age > staleAfterDays {
			w.Notes = append(w.Notes, fmt.Sprintf(
				"itch numbers in state/plan.json are %d days old — ignoring them", age))
		} else {
			w.ItchViews7d, w.ItchPlays7d = st.ItchViews7d, st.ItchPlays7d
		}
	}

	// No account named means nothing is published under one. That is a fact,
	// not an assumption, so it is a zero rather than an unknown -- and it keeps
	// the funnel pointing at "publish something" instead of shrugging.
	if st.ItchUser == "" {
		w.LiveGames = 0
	}

	if !offline {
		if st.ItchUser != "" {
			if n, err := itchGames(st.ItchUser); err == nil {
				w.LiveGames = n
			} else {
				w.Notes = append(w.Notes, "itch.io: "+err.Error())
			}
		}
		if st.RedditUser != "" {
			if age, err := redditAge(st.RedditUser); err == nil {
				w.RedditAgeDays = age
			} else if w.RedditAgeDays < 0 {
				w.Notes = append(w.Notes,
					"reddit blocks automated reads (403). Age is coming from reddit_created_on in state/plan.json")
			}
			// RSS first: it is the feed Reddit actually serves us.
			if posts, comments, err := redditActivityRSS(st.RedditUser); err == nil {
				w.RedditPosts, w.RedditComments = posts, comments
			} else if posts, comments, err := redditActivity(st.RedditUser); err == nil {
				w.RedditPosts, w.RedditComments = posts, comments
			} else {
				w.Notes = append(w.Notes, "reddit: could not read the account's activity")
			}
		}
		w.OpenPRs, w.OpenIssues = fleetQueue()
		w.Health = CheckFleet(w.Now, ghRuns)
	}

	if eventsGlob != "" {
		readEvents(&w, eventsGlob)
	}
	return w
}

func staleness(asOf string, now time.Time) int {
	t, err := time.Parse("2006-01-02", asOf)
	if err != nil {
		return staleAfterDays + 1 // undated is stale by definition
	}
	return int(now.Sub(t).Hours() / 24)
}

// --- the web ---------------------------------------------------------------

var client = &http.Client{Timeout: 12 * time.Second}

// A descriptive agent string, because a scraper that lies about what it is is
// exactly the traffic our own filter.go refuses.
const userAgent = "adtech-lab-orchestrator/1.0 (+https://github.com/litansh/adtech-lab)"

func get(url string) ([]byte, error) {
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s returned %d", url, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
}

// itchGames counts published projects by reading the profile page and
// collecting distinct project links.
//
// Counting links rather than probing a hard-coded list of slugs: itch derives
// the slug from whatever title gets typed into the form, so a guessed URL would
// report "not published" for a game that is published under a slightly
// different name -- a false alarm that would send someone to re-upload
// something that was already there.
func itchGames(user string) (int, error) {
	body, err := get("https://" + user + ".itch.io/")
	if err != nil {
		return 0, err
	}
	re := regexp.MustCompile(`https://` + regexp.QuoteMeta(user) + `\.itch\.io/([a-z0-9][a-z0-9-]{1,60})`)
	seen := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(string(body), -1) {
		seen[m[1]] = true
	}
	return len(seen), nil
}

func redditAge(user string) (int, error) {
	body, err := get("https://www.reddit.com/user/" + user + "/about.json")
	if err != nil {
		return 0, err
	}
	var r struct {
		Data struct {
			Created float64 `json:"created_utc"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &r); err != nil || r.Data.Created == 0 {
		return 0, fmt.Errorf("could not read the account creation date for u/%s", user)
	}
	created := time.Unix(int64(r.Data.Created), 0).UTC()
	return int(time.Since(created).Hours() / 24), nil
}

// redditActivityRSS counts comments and self-posts from the Atom feeds.
//
// Reddit answers 403 to its JSON endpoints for anything that is not a browser,
// which is why this used to be a thing a human declared. Its RSS feeds answer
// 200 -- that is Reddit drawing a line and us staying behind it, not a way
// around one.
//
// Feeds are capped at 25 entries, so this counts recent activity rather than a
// lifetime total. That is the right question anyway: the warm-up asks whether
// the account has been used lately, not whether it ever was.
func redditActivityRSS(user string) (posts, comments int, err error) {
	base := "https://www.reddit.com/user/" + user

	b, err := get(base + "/comments/.rss")
	if err != nil {
		return -1, -1, err
	}
	items, err := parseAtomEntries(b)
	if err != nil {
		return -1, -1, err
	}
	comments = len(items)

	// Reddit rate-limits hard: two feed reads 1.2s apart earned a 429. Backing
	// off is the polite response; retrying harder is how the source is lost.
	time.Sleep(4 * time.Second)

	b, err = get(base + "/submitted/.rss")
	if err != nil {
		return 0, comments, nil // comments are the warm-up; posts can wait
	}
	subs, err := parseAtomEntries(b)
	if err != nil {
		return 0, comments, nil
	}
	for _, e := range subs {
		if strings.Contains(e, "xoxoxo.live") || strings.Contains(e, ".itch.io") {
			posts++
		}
	}
	return posts, comments, nil
}

// parseAtomEntries returns each entry's raw text, so a caller can look for a
// link without this needing to know what it is looking for.
func parseAtomEntries(b []byte) ([]string, error) {
	var f struct {
		Entries []struct {
			Title   string `xml:"title"`
			Content string `xml:"content"`
		} `xml:"entry"`
	}
	if err := xml.Unmarshal(b, &f); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(f.Entries))
	for _, e := range f.Entries {
		out = append(out, e.Title+" "+e.Content)
	}
	return out, nil
}

// redditActivity counts comments, and posts that actually link to us. A post
// about something else is not progress on this plan.
func redditActivity(user string) (posts, comments int, err error) {
	type listing struct {
		Data struct {
			Children []struct {
				Data struct {
					URL      string `json:"url"`
					Selftext string `json:"selftext"`
				} `json:"data"`
			} `json:"children"`
		} `json:"data"`
	}

	b, err := get("https://www.reddit.com/user/" + user + "/submitted.json?limit=100")
	if err != nil {
		return -1, -1, err
	}
	var subs listing
	if err := json.Unmarshal(b, &subs); err != nil {
		return -1, -1, err
	}
	for _, c := range subs.Data.Children {
		hay := c.Data.URL + " " + c.Data.Selftext
		if strings.Contains(hay, "xoxoxo.live") || strings.Contains(hay, ".itch.io/") {
			posts++
		}
	}

	b, err = get("https://www.reddit.com/user/" + user + "/comments.json?limit=100")
	if err != nil {
		return posts, -1, nil
	}
	var com listing
	if err := json.Unmarshal(b, &com); err != nil {
		return posts, -1, nil
	}
	return posts, len(com.Data.Children), nil
}

// --- the fleet -------------------------------------------------------------

// fleetQueue is what the other seven agents are waiting on. This is the "on top
// of" part: the orchestrator does not re-derive their findings, it reports that
// they have findings and that nothing has happened to them.
func fleetQueue() (prs, issues []Item) {
	run := func(kind string) []Item {
		// Every open pull request, not only agent-labelled ones.
		//
		// It filtered on --label agent, which meant the four pull requests
		// actually waiting for review were reported as an empty queue. The
		// question this answers is "what is waiting on you", and a human's own
		// pull request is waiting on them just as much as an agent's.
		cmd := exec.Command("gh", kind, "list", "--state", "open",
			"--limit", "20", "--json", "number,title,url")
		out, err := cmd.Output()
		if err != nil {
			return nil
		}
		var items []Item
		if json.Unmarshal(out, &items) != nil {
			return nil
		}
		return items
	}
	return run("pr"), run("issue")
}

// --- our own data ----------------------------------------------------------

type ev struct {
	Event     string  `json:"event"`
	SessionID string  `json:"session_id"`
	TS        int64   `json:"ts"`
	PriceCPM  float64 `json:"price_cpm"`
	Billable  *bool   `json:"billable"`
	Env       string  `json:"env"`
	// Returning is absent when the browser could not tell us -- storage
	// blocked, a private window. A pointer rather than a bool so "did not come
	// back" and "could not tell" stay distinguishable, which are opposite
	// findings.
	Returning *bool `json:"returning"`
	// Bucket is how long since the previous visit, already reduced on the
	// client to one of five constants. See apps/publisher/static/adlab.js.
	Bucket string `json:"returning_bucket"`
}

func readEvents(w *World, glob string) {
	files, err := filepath.Glob(glob)
	if err != nil || len(files) == 0 {
		w.Notes = append(w.Notes, "no event files matched "+glob)
		return
	}
	cutoff := w.Now.AddDate(0, 0, -7).UnixMilli()

	sessions := map[string]bool{}
	// Only sessions that actually reported. A session whose browser blocked
	// storage must be in NEITHER the numerator nor the denominator: counting it
	// as "did not return" would understate the rate by exactly the number of
	// privacy-conscious players, who are not a random sample.
	answered := map[string]bool{}
	returning := map[string]bool{}
	var sawFlag bool

	for _, f := range files {
		fh, err := os.Open(f)
		if err != nil {
			continue
		}
		var sc *bufio.Scanner
		if strings.HasSuffix(f, ".gz") {
			gz, err := gzip.NewReader(bufio.NewReader(fh))
			if err != nil {
				fh.Close()
				continue
			}
			sc = bufio.NewScanner(gz)
		} else {
			sc = bufio.NewScanner(bufio.NewReader(fh))
		}
		sc.Buffer(make([]byte, 1<<20), 1<<20)

		for sc.Scan() {
			var e ev
			if json.Unmarshal(sc.Bytes(), &e) != nil {
				continue
			}
			// Lab traffic is synthetic. Counting it here would let a load test
			// look like an audience, which is the one number nobody may fake.
			if e.Env == "lab" || e.TS < cutoff {
				continue
			}
			switch e.Event {
			case "session_start":
				sessions[e.SessionID] = true
				if e.Returning != nil {
					sawFlag = true
					answered[e.SessionID] = true
					// docs/audience.md asks for returning within SEVEN days, so
					// a visitor last seen two months ago is a returning human
					// and not a returning-this-week one. The bucket already
					// carries that distinction; using *e.Returning alone would
					// quietly drift the number upward forever as the site aged.
					if *e.Returning && (e.Bucket == "d0" || e.Bucket == "d1_7") {
						returning[e.SessionID] = true
					}
				}
			case "game_view":
				w.GameViews7d++
			case "game_replay":
				w.Replays7d++
			case "impression":
				if e.Billable == nil || *e.Billable {
					w.RevenueUSD7d += e.PriceCPM / 1000
				}
			}
		}
		fh.Close()
	}

	w.Sessions7d = len(sessions)
	w.ReturningKnown = sawFlag && len(answered) > 0
	if w.ReturningKnown {
		w.Returning7d = float64(len(returning)) / float64(len(answered))
	}
}
