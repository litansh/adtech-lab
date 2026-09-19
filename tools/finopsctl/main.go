// finopsctl -- cost attribution from the event log.
//
//	finopsctl publishers --events 'events/*.ndjson'
//	finopsctl buyers     --events 'events/*.ndjson'
//
// Costs are attributed by measured ACTIVITY, never in proportion to revenue.
// See docs/finops.md for why that distinction is the whole exercise.
package main

import (
	"bufio"
	"compress/gzip"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed prices.json
var pricesJSON []byte

type event struct {
	Event        string  `json:"event"`
	PublisherID  string  `json:"publisher_id"`
	PlacementID  string  `json:"placement_id"`
	LatencyMS    float64 `json:"latency_ms"`
	BuyersCalled int     `json:"auction_buyers_called"`
	TimedOut     int     `json:"auction_timed_out"`
	BidsReceived int     `json:"auction_bids_received"`
	BuyerID      string  `json:"buyer_id"`
	PriceCPM     float64 `json:"price_cpm"`
	Env          string  `json:"env"`
}

type agg struct {
	drivers Drivers
	revenue float64
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

	var p Prices
	if err := json.Unmarshal(pricesJSON, &p); err != nil {
		die(err)
	}

	byPub, byBuyer, err := load(*events, *includeLab)
	if err != nil {
		die(err)
	}

	switch cmd {
	case "publishers":
		reportPublishers(byPub, p)
	case "buyers":
		reportBuyers(byBuyer, p)
	default:
		usage()
	}

	fmt.Println("\nExcludes shared platform cost (control plane, monitoring, cold path)")
	fmt.Println("by design -- see docs/finops.md. List prices, no free tier: an")
	fmt.Println("underestimate, consistently, for everyone. Good for ranking, not billing.")
}

type buyerAgg struct {
	calls, bids, timeouts, wins int
	revenue                     float64
}

func load(glob string, includeLab bool) (map[string]*agg, map[string]*buyerAgg, error) {
	files, err := filepath.Glob(glob)
	if err != nil {
		return nil, nil, err
	}
	if len(files) == 0 {
		return nil, nil, fmt.Errorf("no files matched %q", glob)
	}

	pubs := map[string]*agg{}
	buyers := map[string]*buyerAgg{}

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
			key := e.PublisherID
			if key == "" {
				key = "(unattributed)"
			}
			a := pubs[key]
			if a == nil {
				a = &agg{}
				pubs[key] = a
			}
			a.drivers.EventsOut++

			switch e.Event {
			case "ad_request":
				a.drivers.Requests++
				a.drivers.ComputeMS += e.LatencyMS
				a.drivers.BuyerCalls += e.BuyersCalled
			case "impression":
				a.revenue += e.PriceCPM / 1000
				if e.BuyerID != "" {
					b := buyers[e.BuyerID]
					if b == nil {
						b = &buyerAgg{}
						buyers[e.BuyerID] = b
					}
					b.wins++
					b.revenue += e.PriceCPM / 1000
				}
			}
		}
		fh.Close()
	}
	return pubs, buyers, nil
}

func reportPublishers(pubs map[string]*agg, p Prices) {
	names := make([]string, 0, len(pubs))
	for k := range pubs {
		names = append(names, k)
	}
	sort.Strings(names)

	rows := make([]Margin, 0, len(names))
	for _, n := range names {
		a := pubs[n]
		rows = append(rows, Summarise(n, a.drivers.Requests, a.revenue, Attribute(a.drivers, p)))
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].MarginPer1k < rows[j].MarginPer1k })

	fmt.Printf("%-22s %10s %12s %12s %12s %12s %10s\n",
		"publisher", "requests", "revenue", "cost", "rev/1k", "cost/1k", "margin/1k")
	for _, m := range rows {
		fmt.Printf("%-22s %10d %12.6f %12.6f %12.6f %12.6f %10.6f",
			trunc(m.Name, 22), m.Requests, m.RevenueUSD, m.CostUSD,
			m.RevenuePer1k, m.CostPer1k, m.MarginPer1k)
		if m.MarginPer1k < 0 {
			fmt.Print("   LOSS")
		}
		fmt.Println()
	}
}

func reportBuyers(buyers map[string]*buyerAgg, p Prices) {
	if len(buyers) == 0 {
		fmt.Println("no buyer-attributed events found")
		return
	}
	ids := make([]string, 0, len(buyers))
	for k := range buyers {
		ids = append(ids, k)
	}
	sort.Strings(ids)

	rows := make([]BuyerEconomics, 0, len(ids))
	for _, id := range ids {
		b := buyers[id]
		rows = append(rows, SummariseBuyer(id, b.calls, b.bids, b.timeouts, b.wins, b.revenue, p))
	}
	// Worst net first: a buyer that costs and never wins belongs at the top.
	sort.Slice(rows, func(i, j int) bool { return rows[i].NetUSD < rows[j].NetUSD })

	fmt.Printf("%-20s %8s %8s %10s %12s %12s\n",
		"buyer", "wins", "calls", "timeout%", "revenue", "net")
	for _, b := range rows {
		to := "n/a"
		if b.Calls > 0 {
			to = fmt.Sprintf("%.1f%%", b.TimeoutRate*100)
		}
		fmt.Printf("%-20s %8d %8d %10s %12.6f %12.6f",
			trunc(b.BuyerID, 20), b.Wins, b.Calls, to, b.RevenueUSD, b.NetUSD)
		if math.IsInf(b.CostPerWin, 1) {
			fmt.Print("   NEVER WINS -- pure cost")
		}
		fmt.Println()
	}
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func usage() {
	fmt.Fprintln(os.Stderr, `finopsctl publishers|buyers --events GLOB [--include-lab]`)
	os.Exit(2)
}

func die(err error) {
	fmt.Fprintln(os.Stderr, "finopsctl:", err)
	os.Exit(1)
}
