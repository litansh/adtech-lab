// productctl -- the Product agent. Is this still worth playing?
//
//	productctl engagement --events 'events/*.ndjson'
//	productctl placement  --events 'events/*.ndjson'
//	productctl benchmark                    -- against what is already out there
//
// Reports only. It proposes product changes; it does not make them.
package main

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type event struct {
	Event      string  `json:"event"`
	Game       string  `json:"game"`
	SessionID  string  `json:"session_id"`
	Env        string  `json:"env"`
	Seconds    float64 `json:"seconds"`
	Duration   float64 `json:"duration_s"`
	PriceCPM   float64 `json:"price_cpm"`
	EndgameArm string  `json:"endgame_arm"`
}

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	cmd := os.Args[1]

	// benchmark looks OUTWARD and needs no events. Handled before the flag set
	// below, which requires them: the newest game we could build is exactly the
	// question you ask when you have no traffic to analyse yet.
	if cmd == "benchmark" {
		fs := flag.NewFlagSet(cmd, flag.ExitOnError)
		path := fs.String("landscape", "product/landscape.json", "what is out there")
		propose := fs.String("propose", "", "the record of what has already been raised; READ, never written here")
		body := fs.String("issue-body", "", "write the issue body here when it changed")
		_ = fs.Parse(os.Args[2:])
		l, err := LoadLandscape(*path)
		if err != nil {
			fmt.Fprintln(os.Stderr, "productctl:", err)
			os.Exit(1)
		}
		fmt.Print(Benchmark(l))

		if *propose == "" {
			return
		}
		next, direction, ok := Recommend(l)
		if !ok {
			return
		}
		prev := LoadProposal(*propose)
		if !Changed(prev, next, direction) {
			// Silence on purpose. An agent that files the same issue every
			// night gets muted, and then the night it finds something new is
			// the night nobody reads it.
			fmt.Printf("\nunchanged since %s — nothing to raise\n", prev.RaisedOn)
			return
		}
		fmt.Printf("\nCHANGED %s / %s\n", next.ID, direction.ID)
		if *body != "" {
			if err := os.WriteFile(*body, []byte(IssueBody(l, next, direction)), 0o644); err != nil {
				fmt.Fprintln(os.Stderr, "productctl:", err)
				os.Exit(1)
			}
		}
		// The record is NOT written here.
		//
		// It means "an issue exists for this recommendation", and only the
		// step that creates the issue knows whether that is true. Writing it
		// here recorded the proposal as raised while the create was failing, so
		// the finding was suppressed permanently by the very run that lost it.
		//
		// The caller writes it after a successful create. See fleet.yml.
		return
	}

	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	events := fs.String("events", "", "glob of event files (.ndjson or .ndjson.gz)")
	includeLab := fs.Bool("include-lab", false, "include env=lab traffic")
	seed := fs.Int64("seed", 1, "rng seed, so a run is reproducible")
	_ = fs.Parse(os.Args[2:])
	if *events == "" {
		usage()
	}

	games, arms, err := load(*events, *includeLab)
	if err != nil {
		fmt.Fprintln(os.Stderr, "productctl:", err)
		os.Exit(1)
	}

	switch cmd {
	case "engagement":
		reports := Report(games)
		if len(reports) == 0 {
			fmt.Println("no gameplay events found")
			return
		}
		fmt.Printf("%-14s %9s %8s %11s %11s %9s %9s\n",
			"game", "sessions", "starts", "completion", "replay", "per sess", "median s")
		for _, r := range reports {
			fmt.Printf("%-14s %9d %8d %10.1f%% %10.1f%% %9.2f %9.0f\n",
				r.Game, r.Sessions, r.Starts, r.CompletionRate*100,
				r.ReplayRate*100, r.GamesPerSession, r.MedianSeconds)
		}
		if fl := Flags(reports); len(fl) > 0 {
			fmt.Println("\nWorth looking at")
			for _, f := range fl {
				fmt.Println("  - " + f)
			}
		} else {
			fmt.Println("\nNothing flagged.")
		}

	case "placement":
		on, off := arms["on"], arms["off"]
		if on.Ends == 0 && off.Ends == 0 {
			fmt.Println("no placement-arm data found (endgame_arm is not on these events)")
			return
		}
		v := JudgePlacement(on, off, rand.New(rand.NewSource(*seed)))
		fmt.Println("end-of-game ad placement")
		fmt.Printf("  arm ON   sessions %-6d completions %-6d replay %.1f%%  rev/session $%.6f\n",
			on.Sessions, on.Ends, on.ReplayRate()*100, on.RevenuePerSession())
		fmt.Printf("  arm OFF  sessions %-6d completions %-6d replay %.1f%%  rev/session $%.6f\n",
			off.Sessions, off.Ends, off.ReplayRate()*100, off.RevenuePerSession())
		fmt.Printf("\n  replay   %+.1f%%   95%% CI [%+.1f%%, %+.1f%%]\n",
			v.ReplayDelta*100, v.ReplayCILow*100, v.ReplayCIHigh*100)
		fmt.Printf("  revenue  %+.1f%%\n", v.RevenueDelta*100)
		fmt.Printf("\n  DECISION: %s -- %s\n", v.Decision, v.Why)
		fmt.Println("\n  The burden is on the placement: it ships only if replay rate is")
		fmt.Println("  NOT WORSE. Revenue is immediate and easy to measure; the damage a bad")
		fmt.Println("  placement does is slow and shows up as traffic that never returns.")

	default:
		usage()
	}
}

func load(glob string, includeLab bool) ([]GameStats, map[string]ArmStats, error) {
	files, err := filepath.Glob(glob)
	if err != nil {
		return nil, nil, err
	}
	if len(files) == 0 {
		return nil, nil, fmt.Errorf("no files matched %q", glob)
	}

	games := map[string]*GameStats{}
	arms := map[string]*ArmStats{"on": {Arm: "on"}, "off": {Arm: "off"}}
	armSessions := map[string]map[string]bool{"on": {}, "off": {}}

	for _, f := range files {
		fh, err := os.Open(f)
		if err != nil {
			return nil, nil, err
		}
		var sc *bufio.Scanner
		if strings.HasSuffix(f, ".gz") {
			gz, gerr := gzip.NewReader(bufio.NewReader(fh))
			if gerr != nil {
				fh.Close()
				return nil, nil, fmt.Errorf("%s: %w", f, gerr)
			}
			sc = bufio.NewScanner(gz)
		} else {
			sc = bufio.NewScanner(bufio.NewReader(fh))
		}
		sc.Buffer(make([]byte, 1<<20), 1<<20)

		for sc.Scan() {
			var e event
			if json.Unmarshal(sc.Bytes(), &e) != nil {
				continue
			}
			if !includeLab && e.Env == "lab" {
				continue
			}

			if e.Game != "" {
				g := games[e.Game]
				if g == nil {
					g = &GameStats{Game: e.Game, Sessions: map[string]bool{}}
					games[e.Game] = g
				}
				if e.SessionID != "" {
					g.Sessions[e.SessionID] = true
				}
				switch e.Event {
				case "game_start":
					g.Starts++
				case "game_end":
					g.Ends++
					if d := e.Seconds + e.Duration; d > 0 {
						g.Durations = append(g.Durations, d)
					}
				case "game_replay":
					g.Replays++
				}
			}

			// Placement arms. Every event carries the arm, which is what makes
			// the comparison possible at all.
			if a := arms[e.EndgameArm]; a != nil {
				if e.SessionID != "" {
					armSessions[e.EndgameArm][e.SessionID] = true
				}
				switch e.Event {
				case "game_end":
					a.Ends++
				case "game_replay":
					a.Replays++
				case "impression", "ad_impression":
					a.Revenue += e.PriceCPM / 1000
				}
			}
		}
		fh.Close()
	}

	names := make([]string, 0, len(games))
	for n := range games {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]GameStats, 0, len(names))
	for _, n := range names {
		out = append(out, *games[n])
	}

	res := map[string]ArmStats{}
	for k, a := range arms {
		a.Sessions = len(armSessions[k])
		res[k] = *a
	}
	return out, res, nil
}

func usage() {
	fmt.Fprintln(os.Stderr, "productctl engagement|placement --events GLOB [--include-lab]")
	fmt.Fprintln(os.Stderr, "productctl benchmark [--landscape product/landscape.json]")
	os.Exit(2)
}
