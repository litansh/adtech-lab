package main

import (
	"sort"
	"strings"
)

// What the fetched text is allowed to do.
//
// It is COUNTED against a fixed dictionary and nothing else. Titles from the
// open web are untrusted input, and the only safe operation on untrusted input
// is one that cannot be steered by its contents. Counting keyword matches
// cannot be steered; summarising, ranking by "interestingness" or asking a
// model what a title means all can.
//
// So Horizon produces numbers and quoted titles. A human reads the titles. That
// is also why Horizon is capped at Recommend permanently in agent-fleet.md.

// mechanics maps our vocabulary to the words the outside world actually uses.
// Explicit rather than clever: a dictionary that grows on its own makes two
// scans incomparable, which is the whole value of scanning repeatedly.
var mechanics = map[string][]string{
	"categorisation": {"connections", "grouping", "categor", "sort the"},
	"word":           {"wordle", "word game", "anagram", "crossword", "spelling", "letter"},
	"deduction":      {"sudoku", "logic puzzle", "nonogram", "minesweeper", "picross", "deduction"},
	"score-attack":   {"2048", "endless", "high score", "arcade", "score attack"},
	"recall":         {"memory game", "matching game", "concentration"},
	"adversarial":    {"chess", "checkers", "connect four", "tic-tac-toe", "othello", "reversi"},
	"multiplayer":    {"multiplayer", ".io game", "io game", "co-op", "versus"},
	"physics":        {"physics", "platformer", "ragdoll"},
	"idle":           {"idle game", "incremental", "clicker"},
}

// meta are product patterns rather than game mechanics: the things that turn a
// set of games into a habit. Counted separately because "add a streak" and
// "build a word game" are not comparable pieces of work.
var meta = map[string][]string{
	"daily":       {"daily", "every day", "one a day"},
	"streak":      {"streak"},
	"archive":     {"archive", "past puzzles"},
	"leaderboard": {"leaderboard", "high scores", "ranking"},
	"share":       {"share your", "shareable", "share result"},
}

type Count struct {
	Name    string `json:"name"`
	Hits    int    `json:"hits"`
	Points  int    `json:"points"`
	Ship    bool   `json:"we_ship_it"`
	Example string `json:"example"`
	Link    string `json:"link"`
}

// Tally counts matches. Points are summed as well as hits, because ten
// forgettable posts and one that reached the front page are different evidence
// and a hit count alone flattens them into the same number.
func Tally(items []Item, dict map[string][]string, ship map[string]bool) []Count {
	type acc struct {
		hits, points  int
		example, link string
		best          int
	}
	got := map[string]*acc{}

	for _, it := range items {
		low := strings.ToLower(it.Title)
		for name, words := range dict {
			matched := false
			for _, w := range words {
				if strings.Contains(low, w) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
			a := got[name]
			if a == nil {
				a = &acc{}
				got[name] = a
			}
			a.hits++
			a.points += it.Points
			// Keep the highest-scored example, so the quoted title is the one
			// most worth a human's attention.
			if it.Points >= a.best || a.example == "" {
				a.best, a.example, a.link = it.Points, it.Title, it.URL
			}
		}
	}

	out := make([]Count, 0, len(got))
	for name, a := range got {
		out = append(out, Count{name, a.hits, a.points, ship[name], a.example, a.link})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Points != out[j].Points {
			return out[i].Points > out[j].Points
		}
		if out[i].Hits != out[j].Hits {
			return out[i].Hits > out[j].Hits
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// noticeThreshold: a mechanic has to clear this much external attention before
// it is worth raising. One enthusiastic post is not a trend, and an agent that
// files an issue every week gets muted -- which is the failure mode that makes
// every later finding worthless.
const noticeThreshold = 300

// Gaps are mechanics the outside world is paying attention to that we do not
// ship at all. This is the finding worth waking someone for.
func Gaps(counts []Count) []Count {
	var out []Count
	for _, c := range counts {
		if !c.Ship && c.Points >= noticeThreshold {
			out = append(out, c)
		}
	}
	return out
}

// Saturation is the one candidate score with an objective proxy, so it is the
// only one Horizon proposes. Retention and fit are judgements about our
// product and stay with a human -- an agent inventing them would be making
// product decisions with no one reviewing them.
func Saturation(c Count) int {
	switch {
	case c.Hits >= 40:
		return 5
	case c.Hits >= 20:
		return 4
	case c.Hits >= 8:
		return 3
	case c.Hits >= 3:
		return 2
	default:
		return 1
	}
}
