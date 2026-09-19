package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"time"
)

// Reading itch.io directly, instead of asking a human to read a dashboard.
//
// Two of the six funnel stages depended on numbers someone had to copy out of a
// browser by hand, and a number nobody is obliged to type in is a number that
// stops being typed in. `reach` was blind for exactly that reason.
//
// The API is READ-ONLY -- all six of its endpoints are GETs, and there is no
// way to modify a page through it even with an unscoped key. So this reads, and
// the listings are still edited by a person in a browser.
//
// itch does not expose a PLAY count. views_count and downloads_count are all
// there is, and for an HTML game downloads are near zero. So `reach` becomes
// first-party and `click` stays blind -- an honest half, rather than a number
// invented to fill a column.

type itchGame struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	Views     int    `json:"views_count"`
	Downloads int    `json:"downloads_count"`
	Published bool   `json:"published"`
	URL       string `json:"url"`
}

// Reading is one day's totals. Kept as a series because the API reports
// LIFETIME counts and the funnel asks about a week, and a lifetime total is
// indistinguishable from a good week until it stops moving.
type Reading struct {
	Date      string         `json:"date"`
	Views     int            `json:"views"`
	Downloads int            `json:"downloads"`
	PerGame   map[string]int `json:"per_game"`
}

type ItchHistory struct {
	Readings []Reading `json:"readings"`
}

func LoadItchHistory(path string) ItchHistory {
	var h ItchHistory
	b, err := os.ReadFile(path)
	if err != nil {
		return h
	}
	_ = json.Unmarshal(b, &h)
	return h
}

func SaveItchHistory(path string, h ItchHistory) error {
	// Bounded: two years of daily readings is plenty, and an unbounded file
	// committed on every run grows until someone notices it in a diff.
	if len(h.Readings) > 750 {
		h.Readings = h.Readings[len(h.Readings)-750:]
	}
	b, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// FetchItch reads the profile. The key is never logged, and never leaves here.
func FetchItch(key string) ([]itchGame, error) {
	req, _ := http.NewRequest("GET", "https://api.itch.io/profile/games", nil)
	req.Header.Set("Authorization", key)
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("itch api unreachable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		// Deliberately not echoing the body: an auth error can carry the key
		// back in a message, and this output goes into a public-ish CI log.
		return nil, fmt.Errorf("itch api returned %d", resp.StatusCode)
	}
	var r struct {
		Games []itchGame `json:"games"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, fmt.Errorf("itch api response unreadable")
	}
	return r.Games, nil
}

// Record folds today's totals into the history, replacing today's reading if
// the job runs twice.
func RecordReading(h ItchHistory, games []itchGame, day string) ItchHistory {
	r := Reading{Date: day, PerGame: map[string]int{}}
	for _, g := range games {
		if !g.Published {
			continue // a draft nobody can see is not reach
		}
		r.Views += g.Views
		r.Downloads += g.Downloads
		r.PerGame[g.Title] = g.Views
	}
	out := make([]Reading, 0, len(h.Readings)+1)
	for _, old := range h.Readings {
		if old.Date != day {
			out = append(out, old)
		}
	}
	out = append(out, r)
	sort.Slice(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return ItchHistory{Readings: out}
}

// Views7d turns lifetime totals into a weekly figure.
//
// Returns -1 when it genuinely cannot be worked out, which the funnel already
// treats as blindness rather than as zero.
func Views7d(h ItchHistory, now time.Time, liveSince time.Time) int {
	if len(h.Readings) == 0 {
		return -1
	}
	latest := h.Readings[len(h.Readings)-1]

	// A reading from a week ago gives a true delta.
	cutoff := now.AddDate(0, 0, -7).Format("2006-01-02")
	for i := len(h.Readings) - 1; i >= 0; i-- {
		if h.Readings[i].Date <= cutoff {
			d := latest.Views - h.Readings[i].Views
			if d < 0 {
				return -1 // counts went backwards; something is wrong, say nothing
			}
			return d
		}
	}

	// No week of history yet. If the pages themselves are younger than a week,
	// the lifetime total IS the last seven days -- true only while that holds,
	// which is why it is checked rather than assumed.
	if !liveSince.IsZero() && now.Sub(liveSince).Hours() < 7*24 {
		return latest.Views
	}
	return -1
}
