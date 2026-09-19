// horizonctl -- the Horizon agent. What is the rest of the world playing?
//
//	horizonctl scan --landscape ../../product/landscape.json --out ../../product/signals.json
//
// The only agent whose input is the open web. It reads public JSON APIs and RSS
// feeds, counts what it finds against a fixed dictionary, and writes numbers.
//
// It never interprets. Titles from the open web are untrusted input, and the
// only safe operation on untrusted input is one its contents cannot steer --
// counting can't be steered, summarising can. Horizon is therefore capped at
// Recommend permanently: it proposes, a human merges.
//
// Its output feeds `productctl benchmark`, which does the ranking. Gathering
// and judging are separate programs on purpose.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Signals struct {
	ScannedAt  string   `json:"scanned_at"`
	Items      int      `json:"items_read"`
	MinPoints  int      `json:"min_points"`
	Mechanics  []Count  `json:"mechanics"`
	Meta       []Count  `json:"meta_patterns"`
	Gaps       []Count  `json:"gaps"`
	Unreadable []string `json:"sources_unreadable"`
}

func main() {
	if len(os.Args) < 2 || os.Args[1] != "scan" {
		fmt.Fprintln(os.Stderr, "horizonctl scan [--landscape FILE] [--out FILE] [--min-points N] [--fixture FILE]")
		os.Exit(2)
	}
	fs := flag.NewFlagSet("scan", flag.ExitOnError)
	landscape := fs.String("landscape", "product/landscape.json", "what we already ship")
	out := fs.String("out", "", "write signals here")
	minPoints := fs.Int("min-points", 30, "ignore stories below this score")
	fixture := fs.String("fixture", "", "read items from a file instead of the web")
	_ = fs.Parse(os.Args[2:])

	ship, err := shipped(*landscape)
	if err != nil {
		fmt.Fprintln(os.Stderr, "horizonctl:", err)
		os.Exit(1)
	}

	var items []Item
	var errs []error
	if *fixture != "" {
		items, err = loadFixture(*fixture)
		if err != nil {
			fmt.Fprintln(os.Stderr, "horizonctl:", err)
			os.Exit(1)
		}
	} else {
		items, errs = Gather(*minPoints, time.Now().UTC())
	}

	// Every source failing is not a quiet week, it is a broken agent, and the
	// two must not produce the same output.
	if len(items) == 0 {
		fmt.Fprintln(os.Stderr, "horizonctl: every source failed — reporting nothing would look identical to a quiet week")
		for _, e := range errs {
			fmt.Fprintln(os.Stderr, "  "+e.Error())
		}
		os.Exit(1)
	}

	s := Signals{
		ScannedAt: time.Now().UTC().Format("2006-01-02"),
		Items:     len(items),
		MinPoints: *minPoints,
		Mechanics: Tally(items, mechanics, ship),
		Meta:      Tally(items, meta, map[string]bool{}),
	}
	s.Gaps = Gaps(s.Mechanics)
	for _, e := range errs {
		s.Unreadable = append(s.Unreadable, e.Error())
	}
	sort.Strings(s.Unreadable)

	fmt.Print(report(s))

	if *out != "" {
		b, _ := json.MarshalIndent(s, "", "  ")
		if err := os.WriteFile(*out, append(b, '\n'), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "horizonctl:", err)
			os.Exit(1)
		}
		fmt.Printf("\nwrote %s\n", *out)
	}

	if len(s.Gaps) > 0 {
		notify(s)
	}
}

func report(s Signals) string {
	var b strings.Builder
	fmt.Fprintf(&b, "horizon — %d items read, stories under %d points ignored\n\n", s.Items, s.MinPoints)

	fmt.Fprintf(&b, "%-16s %5s %7s %4s  %s\n", "mechanic", "hits", "points", "ship", "loudest example")
	for _, c := range s.Mechanics {
		ship := "no"
		if c.Ship {
			ship = "yes"
		}
		fmt.Fprintf(&b, "%-16s %5d %7d %4s  %s\n", c.Name, c.Hits, c.Points, ship, trunc(c.Example, 58))
	}

	fmt.Fprintf(&b, "\n%-16s %5s %7s\n", "pattern", "hits", "points")
	for _, c := range s.Meta {
		fmt.Fprintf(&b, "%-16s %5d %7d\n", c.Name, c.Hits, c.Points)
	}

	if len(s.Gaps) == 0 {
		fmt.Fprintf(&b, "\nNo gap worth raising. Nothing outside scored %d+ that we do not already ship.\n",
			noticeThreshold)
	} else {
		fmt.Fprintf(&b, "\nGAPS — attention outside, nothing of ours inside\n")
		for _, c := range s.Gaps {
			fmt.Fprintf(&b, "  %-14s %d points across %d posts, saturation ≈ %d\n",
				c.Name, c.Points, c.Hits, Saturation(c))
			// Titles are printed raw and unsummarised, exactly as feedbackctl
			// prints quarantined comments. Read them yourself; nothing here has
			// interpreted them.
			fmt.Fprintf(&b, "                 %q\n                 %s\n", c.Example, c.Link)
		}
	}

	if len(s.Unreadable) > 0 {
		fmt.Fprintf(&b, "\nCOULD NOT READ\n")
		for _, u := range s.Unreadable {
			fmt.Fprintf(&b, "  %s\n", u)
		}
	}
	return b.String()
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// shipped reads our current portfolio so a gap is measured against what we
// actually have, rather than against a list someone remembered to update.
func shipped(path string) (map[string]bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var l struct {
		Portfolio []struct {
			Mechanic string `json:"mechanic"`
		} `json:"portfolio"`
	}
	if err := json.Unmarshal(b, &l); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(l.Portfolio) == 0 {
		return nil, fmt.Errorf("%s: no portfolio, so every mechanic would read as a gap", path)
	}
	ship := map[string]bool{}
	for _, p := range l.Portfolio {
		ship[p.Mechanic] = true
	}
	return ship, nil
}

func loadFixture(path string) ([]Item, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var items []Item
	return items, json.Unmarshal(b, &items)
}

// notifyScript finds telegram.py by walking UP from the working directory.
//
// Agents run as `go -C tools/<name> run .`, so the working directory is the
// tool, not the repository root. Resolved by looking rather than by assuming a
// depth: a hard-coded "../../" is right until someone moves a tool one level
// deeper, and then it is wrong in the same silent way this bug already was.
func notifyScript() (string, error) {
	if p := os.Getenv("ADLAB_NOTIFY"); p != "" {
		return p, nil
	}
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for i := 0; i < 6; i++ {
		p := filepath.Join(dir, "tools", "notify", "telegram.py")
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("could not find tools/notify/telegram.py above the working directory")
}

func notify(s Signals) {
	var b strings.Builder
	fmt.Fprintf(&b, "Attention outside that we have nothing for:\n\n")
	for _, c := range s.Gaps {
		fmt.Fprintf(&b, "%s — %d points across %d posts\n  %q\n\n", c.Name, c.Points, c.Hits, c.Example)
	}
	fmt.Fprintf(&b, "Titles are quoted unsummarised. Read them yourself.")

	f, err := os.CreateTemp("", "horizon-*.md")
	if err != nil {
		return
	}
	defer os.Remove(f.Name())
	f.WriteString(b.String())
	f.Close()
	script, err := notifyScript()
	if err != nil {
		fmt.Fprintln(os.Stderr, "horizonctl:", err)
		return
	}
	cmd := exec.Command("python3", script,
		"--title", "Horizon: a mechanic we do not ship", "--body-file", f.Name())
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	_ = cmd.Run()
}
