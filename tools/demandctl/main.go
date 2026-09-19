// demandctl -- the Demand agent. Which buyers are worth calling.
//
//	demandctl analyse --events 'events/*.ndjson'
//	demandctl propose --events 'events/*.ndjson'
//
// Reports only. Changing which buyers are called is a control-plane change and
// stays a deliberate step -- see docs/agent-fleet.md.
package main

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// event is the subset we read. The per-buyer array is what makes this agent
// possible at all; without it the log answers "how did the auction go?" and
// cannot answer "how is this buyer doing?".
type event struct {
	Event    string  `json:"event"`
	Env      string  `json:"env"`
	Clearing float64 `json:"auction_clearing_price"`
	Buyers   []struct {
		B string  `json:"b"` // buyer id
		S string  `json:"s"` // status
		L int     `json:"l"` // latency ms
		P float64 `json:"p"` // price cpm
		W int     `json:"w"` // 1 if it won
	} `json:"auction_buyers"`
}

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	cmd := os.Args[1]
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	events := fs.String("events", "", "glob of event files (.ndjson or .ndjson.gz)")
	includeLab := fs.Bool("include-lab", false, "include env=lab traffic")
	_ = fs.Parse(os.Args[2:])
	if *events == "" {
		usage()
	}

	stats, err := load(*events, *includeLab)
	if err != nil {
		fmt.Fprintln(os.Stderr, "demandctl:", err)
		os.Exit(1)
	}
	if len(stats) == 0 {
		// Exit ZERO. This branch means files were read and none of them carried
		// auction_buyers -- "I looked and there is nothing yet", which is a
		// report. `load` already exits 1 when the glob matches nothing at all,
		// which is "I could not look", and that IS a failure.
		//
		// The distinction matters because this runs on a schedule. A workflow
		// that is red every day until the first buyer call gets muted, and a
		// muted agent makes every later finding worthless -- the same argument
		// that keeps the Reliability agent quiet when nothing is wrong.
		fmt.Println("no per-buyer auction data yet. The ad server logs " +
			"auction_buyers only from the version that added it, so this stays " +
			"empty until traffic runs through that build. Nothing is wrong.")
		return
	}

	econ := Analyse(stats)

	fmt.Printf("%-20s %8s %9s %9s %9s %11s %8s %9s\n",
		"buyer", "calls", "bid%", "timeout%", "win%", "value/call", "p95ms", "of timeouts")
	for _, e := range econ {
		fmt.Printf("%-20s %8d %8.1f%% %8.1f%% %8.1f%% %11.8f %8d %8.1f%%\n",
			trunc(e.BuyerID, 20), e.Calls, e.BidRate*100, e.TimeoutRate*100,
			e.WinRate*100, e.ValuePerCall, e.P95LatencyMS, e.ShareOfTimeouts*100)
	}

	if cmd == "analyse" || cmd == "analyze" {
		return
	}
	if cmd != "propose" {
		usage()
	}

	props := Propose(econ, nil)
	fmt.Println("\nProposed call rates (nothing applied)")
	for _, p := range props {
		fmt.Printf("  %-20s %5.0f%% -> %5.0f%%   %s\n",
			trunc(p.BuyerID, 20), p.WasCalled*100, p.CallRate*100, p.Reason)
	}

	calls, saved, atRisk := Savings(econ, props)
	fmt.Printf("\n  calls avoided     %d\n", calls)
	fmt.Printf("  infra saved       $%.6f\n", saved)
	fmt.Printf("  revenue at risk   $%.6f\n", atRisk)
	if atRisk > saved {
		// Say it plainly rather than presenting the saving alone.
		fmt.Println("\n  WARNING: this proposal risks more revenue than it saves in cost.")
		fmt.Println("  Throttling is worth it for buyers that cost more than they return,")
		fmt.Println("  not for every buyer that returns little.")
	}
	fmt.Println("\nReports only. Changing the call list is a control-plane change.")
}

func load(glob string, includeLab bool) ([]BuyerStats, error) {
	files, err := filepath.Glob(glob)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no files matched %q", glob)
	}

	agg := map[string]*BuyerStats{}
	for _, f := range files {
		fh, err := os.Open(f)
		if err != nil {
			return nil, err
		}
		var sc *bufio.Scanner
		if strings.HasSuffix(f, ".gz") {
			gz, gerr := gzip.NewReader(bufio.NewReader(fh))
			if gerr != nil {
				fh.Close()
				return nil, fmt.Errorf("%s: %w", f, gerr)
			}
			sc = bufio.NewScanner(gz)
		} else {
			sc = bufio.NewScanner(bufio.NewReader(fh))
		}
		sc.Buffer(make([]byte, 1<<20), 1<<20)

		for sc.Scan() {
			var e event
			if json.Unmarshal(sc.Bytes(), &e) != nil || len(e.Buyers) == 0 {
				continue
			}
			if !includeLab && e.Env == "lab" {
				continue
			}
			for _, b := range e.Buyers {
				s := agg[b.B]
				if s == nil {
					s = &BuyerStats{BuyerID: b.B}
					agg[b.B] = s
				}
				s.Calls++
				s.LatencyMS = append(s.LatencyMS, b.L)
				// A bid is anything that produced a price. The winner's status
				// is "won" and a valid loser's is "lost" -- matching only
				// "bid" counted neither, so bid rate read 0% for buyers with a
				// 35% win rate, which is impossible and was the giveaway.
				switch b.S {
				case "timeout":
					s.Timeouts++
				case "won", "lost", "bid", "below_floor":
					s.Bids++
				}
				if b.W == 1 {
					s.Wins++
					// Our revenue from this win is the clearing price, which is
					// what the seller was paid -- not the buyer's bid.
					s.Revenue += e.Clearing / 1000
				}
			}
		}
		fh.Close()
	}

	ids := make([]string, 0, len(agg))
	for id := range agg {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]BuyerStats, 0, len(ids))
	for _, id := range ids {
		out = append(out, *agg[id])
	}
	return out, nil
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func usage() {
	fmt.Fprintln(os.Stderr, "demandctl analyse|propose --events GLOB [--include-lab]")
	os.Exit(2)
}
