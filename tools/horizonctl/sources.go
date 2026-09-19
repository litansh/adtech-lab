package main

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Where Horizon looks, and why only here.
//
// Every source below is machine-readable, public, and served willingly: a JSON
// API or an RSS feed. Two obvious sources are deliberately absent.
//
// itch.io's browse and tag pages sit behind a Cloudflare challenge. They can be
// read by a browser and answer 403 to this. Getting around that is bot
// detection evasion, which is the exact behaviour our own filter.go exists to
// refuse -- and an ad tech project that scrapes past a bot wall to do research
// has lost the argument it is trying to make. So itch contributes only its
// public RSS feed.
//
// Reddit's JSON endpoints answer 403 to anything that is not a browser. Its RSS
// feeds answer 200. That is Reddit drawing the line, not us stepping over one.

type Item struct {
	Title  string `json:"title"`
	URL    string `json:"url"`
	Points int    `json:"points"`
	Source string `json:"source"`
	When   string `json:"when"`
}

const userAgent = "adtech-lab-horizon/1.0 (+https://github.com/litansh/adtech-lab)"

var client = &http.Client{Timeout: 20 * time.Second}

func fetch(u string) ([]byte, error) {
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s returned %d", short(u), resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
}

func short(u string) string {
	if p, err := url.Parse(u); err == nil {
		return p.Host
	}
	return u
}

// --- Hacker News, via the Algolia API ---------------------------------------

// hnQueries are the searches run each scan. Kept explicit rather than derived,
// because a query list that changes on its own makes two scans incomparable.
var hnQueries = []string{
	"puzzle game", "daily puzzle", "browser game", "word game",
	"wordle", "connections game", "html5 game", "game jam",
}

// lookbackMonths bounds every search to recent attention.
//
// Without it the loudest result is "The New York Times buys Wordle" -- from
// 2022. An unbounded search measures all-time fame and reports it as a trend,
// which is the most confidently wrong thing a research agent can do. Eighteen
// months is long enough to survive a quiet quarter and short enough that a
// four-year-old acquisition cannot dominate the table.
const lookbackMonths = 18

func hackerNews(minPoints int, now time.Time) ([]Item, []error) {
	var out []Item
	var errs []error
	since := now.AddDate(0, -lookbackMonths, 0).Unix()
	for _, q := range hnQueries {
		u := fmt.Sprintf(
			"https://hn.algolia.com/api/v1/search?query=%s&tags=story&numericFilters=points%%3E%d,created_at_i%%3E%d",
			url.QueryEscape(q), minPoints, since)
		polite(u)
		b, err := fetch(u)
		if err != nil {
			errs = append(errs, fmt.Errorf("hn %q: %w", q, err))
			continue
		}
		var r struct {
			Hits []struct {
				Title     string `json:"title"`
				URL       string `json:"url"`
				Points    int    `json:"points"`
				CreatedAt string `json:"created_at"`
				ObjectID  string `json:"objectID"`
			} `json:"hits"`
		}
		if json.Unmarshal(b, &r) != nil {
			errs = append(errs, fmt.Errorf("hn %q: unreadable response", q))
			continue
		}
		for _, h := range r.Hits {
			link := h.URL
			if link == "" {
				link = "https://news.ycombinator.com/item?id=" + h.ObjectID
			}
			out = append(out, Item{h.Title, link, h.Points, "hn", h.CreatedAt})
		}
	}
	return out, errs
}

// --- RSS and Atom -----------------------------------------------------------

// One struct for both: RSS puts entries in <item>, Atom in <entry>, and no
// scan should fail because a site changed feed dialects.
type feed struct {
	Items []struct {
		Title   string `xml:"title"`
		Link    string `xml:"link"`
		Updated string `xml:"updated"`
		PubDate string `xml:"pubDate"`
	} `xml:"channel>item"`
	Entries []struct {
		Title string `xml:"title"`
		Link  struct {
			Href string `xml:"href,attr"`
		} `xml:"link"`
		Updated string `xml:"updated"`
	} `xml:"entry"`
}

func parseFeed(b []byte, source string) ([]Item, error) {
	var f feed
	if err := xml.Unmarshal(b, &f); err != nil {
		return nil, err
	}
	var out []Item
	for _, it := range f.Items {
		when := it.Updated
		if when == "" {
			when = it.PubDate
		}
		out = append(out, Item{clean(it.Title), it.Link, 0, source, when})
	}
	for _, e := range f.Entries {
		out = append(out, Item{clean(e.Title), e.Link.Href, 0, source, e.Updated})
	}
	return out, nil
}

func clean(s string) string { return strings.TrimSpace(strings.ReplaceAll(s, "\n", " ")) }

var feeds = []struct{ name, url string }{
	{"reddit/WebGames", "https://www.reddit.com/r/WebGames/top/.rss?t=month"},
	{"reddit/playmygame", "https://www.reddit.com/r/playmygame/top/.rss?t=month"},
	{"itch/newest", "https://itch.io/games/newest.xml"},
}

func rssSources() ([]Item, []error) {
	var out []Item
	var errs []error
	for _, f := range feeds {
		polite(f.url)
		b, err := fetch(f.url)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", f.name, err))
			continue
		}
		items, err := parseFeed(b, f.name)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", f.name, err))
			continue
		}
		out = append(out, items...)
	}
	return out, errs
}

func lobsters() ([]Item, []error) {
	b, err := fetch("https://lobste.rs/t/games.json")
	if err != nil {
		return nil, []error{fmt.Errorf("lobsters: %w", err)}
	}
	var r []struct {
		Title     string `json:"title"`
		URL       string `json:"url"`
		Score     int    `json:"score"`
		CreatedAt string `json:"created_at"`
	}
	if json.Unmarshal(b, &r) != nil {
		return nil, []error{fmt.Errorf("lobsters: unreadable response")}
	}
	var out []Item
	for _, s := range r {
		out = append(out, Item{s.Title, s.URL, s.Score, "lobsters", s.CreatedAt})
	}
	return out, nil
}

// Gather reads every source. Failures are collected, never fatal: a scan that
// dies because one feed is down is a scan that reports nothing on exactly the
// week something changed.
// polite spaces requests out. Reddit answered 429 to back-to-back feed reads,
// and a research agent that hammers a free feed is one robots.txt change away
// from having no sources at all.
func polite(u string) {
	// Reddit answered 429 to two feed reads 1.2s apart. Its limits are
	// stricter than everyone else's, so it gets a longer gap rather than a
	// retry loop -- backing off is the polite response to a rate limit, and
	// retrying harder is the one that loses the source.
	if strings.Contains(u, "reddit.com") {
		time.Sleep(4 * time.Second)
		return
	}
	time.Sleep(800 * time.Millisecond)
}

func Gather(minPoints int, now time.Time) ([]Item, []error) {
	items, errs := hackerNews(minPoints, now)
	r, e := rssSources()
	items, errs = append(items, r...), append(errs, e...)
	l, e := lobsters()
	return append(items, l...), append(errs, e...)
}
