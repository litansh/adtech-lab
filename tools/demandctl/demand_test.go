package main

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func lat(n, ms int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = ms
	}
	return out
}

// buyer-slow, as measured in the Lab: bids highest, times out on 75% of calls,
// wins nothing. Invisible to any revenue-based view, and the single most
// expensive thing in the auction.
func TestABuyerThatNeverAnswersIsThrottledHard(t *testing.T) {
	econ := Analyse([]BuyerStats{{
		BuyerID: "buyer-slow", Calls: 4000, Bids: 1000, Timeouts: 3000,
		Wins: 0, Revenue: 0, LatencyMS: lat(4000, 300),
	}})
	if econ[0].ValuePerCall >= 0 {
		t.Fatalf("a buyer with no revenue and real cost showed value %.9f", econ[0].ValuePerCall)
	}
	p := Propose(econ, nil)[0]
	if p.CallRate > MinCallRate+1e-9 {
		t.Fatalf("call rate %.3f for a buyer timing out 75%% of the time", p.CallRate)
	}
	if p.Reason == "" {
		t.Error("a throttle with no stated reason cannot be argued with")
	}
}

// But never to zero: a buyer we never call produces no observations, so we can
// never learn it recovered.
func TestNoBuyerIsEverSilencedCompletely(t *testing.T) {
	econ := Analyse([]BuyerStats{{
		BuyerID: "hopeless", Calls: 10000, Timeouts: 10000, LatencyMS: lat(10000, 500),
	}})
	for i := 0; i < 20; i++ { // repeated runs must not creep to zero
		cur := map[string]float64{"hopeless": MinCallRate}
		p := Propose(econ, cur)[0]
		if p.CallRate < MinCallRate-1e-9 {
			t.Fatalf("call rate fell to %.4f, below the %.2f floor", p.CallRate, MinCallRate)
		}
	}
}

func TestAProfitableBuyerIsRestoredTowardFull(t *testing.T) {
	econ := Analyse([]BuyerStats{{
		BuyerID: "good", Calls: 5000, Bids: 4000, Wins: 1200,
		Revenue: 6.00, LatencyMS: lat(5000, 25),
	}})
	if econ[0].ValuePerCall <= 0 {
		t.Fatalf("a buyer earning $6 on 5000 calls is not profitable? %.9f", econ[0].ValuePerCall)
	}
	p := Propose(econ, map[string]float64{"good": 0.4})[0]
	if p.CallRate <= 0.4 {
		t.Fatalf("a profitable buyer was not restored: %.2f", p.CallRate)
	}
	if p.CallRate > 0.4+MaxMovePerRun+1e-9 {
		t.Errorf("moved %.2f in one run, limit is %.2f", p.CallRate-0.4, MaxMovePerRun)
	}
}

// A bad rate on a small sample is a small sample, not a fact about the buyer.
func TestTooFewCallsMeansNoChange(t *testing.T) {
	econ := Analyse([]BuyerStats{{
		BuyerID: "new", Calls: 40, Timeouts: 40, LatencyMS: lat(40, 400),
	}})
	p := Propose(econ, map[string]float64{"new": 1.0})[0]
	if p.CallRate != 1.0 {
		t.Fatalf("judged a buyer on 40 calls: %.2f (%s)", p.CallRate, p.Reason)
	}
}

// The aggregate hides the case where one buyer is the whole problem, which is
// the usual case.
func TestShareOfTimeoutsNamesTheCulprit(t *testing.T) {
	econ := Analyse([]BuyerStats{
		{BuyerID: "fine1", Calls: 1000, Bids: 800, Wins: 200, Revenue: 1.0, Timeouts: 20, LatencyMS: lat(1000, 30)},
		{BuyerID: "fine2", Calls: 1000, Bids: 700, Wins: 150, Revenue: 0.8, Timeouts: 15, LatencyMS: lat(1000, 35)},
		{BuyerID: "slow", Calls: 1000, Bids: 100, Wins: 0, Timeouts: 800, LatencyMS: lat(1000, 400)},
	})
	var slow BuyerEconomics
	for _, e := range econ {
		if e.BuyerID == "slow" {
			slow = e
		}
	}
	if slow.ShareOfTimeouts < 0.9 {
		t.Fatalf("one buyer causing 800 of 835 timeouts got share %.2f", slow.ShareOfTimeouts)
	}
	// And it sorts to the top, because worst-value-first is what an operator
	// wants to read.
	if econ[0].BuyerID != "slow" {
		t.Errorf("worst buyer is %q, not first in the report", econ[0].BuyerID)
	}
}

func TestP95LatencyIsReported(t *testing.T) {
	// 90 fast and 10 slow. With exactly 5 slow of 100 the p95 IS the fast
	// value -- 95% of calls really are under 20ms -- which is the percentile
	// behaving correctly and my first version of this test being wrong.
	ms := append(lat(90, 20), lat(10, 400)...)
	econ := Analyse([]BuyerStats{{BuyerID: "spiky", Calls: 100, LatencyMS: ms}})
	if econ[0].P95LatencyMS < 300 {
		t.Fatalf("p95 %dms hides a tail that times out", econ[0].P95LatencyMS)
	}
}

// A recommendation with no number is an opinion. And the cost of acting has to
// be stated alongside the saving.
func TestSavingsStatesBothSidesOfTheTrade(t *testing.T) {
	econ := Analyse([]BuyerStats{{
		BuyerID: "marginal", Calls: 10000, Bids: 3000, Wins: 100,
		Revenue: 0.002, Timeouts: 6000, LatencyMS: lat(10000, 200),
	}})
	props := Propose(econ, map[string]float64{"marginal": 1.0})
	calls, saved, atRisk := Savings(econ, props)

	if calls <= 0 || saved <= 0 {
		t.Fatalf("throttling 10000 calls saved nothing: calls=%d saved=%.9f", calls, saved)
	}
	if atRisk <= 0 {
		t.Error("fewer calls to a buyer that sometimes wins is revenue we chose " +
			"not to pursue; reporting only the saving hides half the trade")
	}
}

func TestMoveLimitHoldsInBothDirections(t *testing.T) {
	econ := Analyse([]BuyerStats{{
		BuyerID: "b", Calls: 5000, Bids: 4000, Wins: 1000, Revenue: 5.0, LatencyMS: lat(5000, 20),
	}})
	for _, start := range []float64{0.05, 0.5, 1.0} {
		p := Propose(econ, map[string]float64{"b": start})[0]
		if math.Abs(p.CallRate-start) > MaxMovePerRun+1e-9 {
			t.Fatalf("from %.2f moved to %.2f, limit is %.2f", start, p.CallRate, MaxMovePerRun)
		}
	}
}

// A buyer cannot win without bidding. The first version counted only status
// "bid", while the auction records winners as "won" and valid losers as
// "lost" -- so bid rate read 0% for a buyer with a 35% win rate. Impossible
// numbers are the useful kind of bug: they announce themselves.
func TestWinRateCanNeverExceedBidRate(t *testing.T) {
	econ := Analyse([]BuyerStats{
		{BuyerID: "selective", Calls: 1000, Bids: 348, Wins: 348,
			Revenue: 3.2, LatencyMS: lat(1000, 40)},
		{BuyerID: "eager", Calls: 1000, Bids: 905, Wins: 22,
			Revenue: 0.1, LatencyMS: lat(1000, 55)},
	})
	for _, e := range econ {
		if e.WinRate > e.BidRate+1e-9 {
			t.Fatalf("%s wins %.1f%% but bids %.1f%% -- impossible",
				e.BuyerID, e.WinRate*100, e.BidRate*100)
		}
	}
}

// A buyer that bids rarely and wins whenever it does is the shape worth
// protecting: low bid rate is not the same as low value.
func TestSelectiveHighValueBuyerIsNotThrottled(t *testing.T) {
	econ := Analyse([]BuyerStats{{
		BuyerID: "selective", Calls: 1000, Bids: 348, Wins: 348,
		Revenue: 3.2, LatencyMS: lat(1000, 40),
	}})
	p := Propose(econ, map[string]float64{"selective": 1.0})[0]
	if p.CallRate < 1.0 {
		t.Fatalf("throttled a buyer worth $%.2f on 1000 calls to %.2f (%s)",
			econ[0].RevenueUSD, p.CallRate, p.Reason)
	}
}

// "I looked and there is nothing" and "I could not look" must not be the same
// outcome. The first is a report and exits zero; the second is a failure.
//
// This ran on a schedule and exited 1 for the first case, which would have made
// the Fleet workflow red every day until the first buyer call -- and a workflow
// that is always red gets muted, which makes every later finding worthless.
func TestNoMatchingFilesIsAnErrorButNoBuyerDataIsNot(t *testing.T) {
	if _, err := load(filepath.Join(t.TempDir(), "nothing", "*.ndjson"), false); err == nil {
		t.Fatal("a glob matching no files is a misconfiguration and must error")
	}

	dir := t.TempDir()
	// A real event, carrying no auction_buyers -- exactly what the ad server
	// emitted before that field existed.
	line := `{"event":"impression","session_id":"s","ts":1,"price_cpm":1.5}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "a.ndjson"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	stats, err := load(filepath.Join(dir, "*.ndjson"), false)
	if err != nil {
		t.Fatalf("readable files with no buyer data must not error: %v", err)
	}
	if len(stats) != 0 {
		t.Fatalf("want no buyer stats, got %d", len(stats))
	}
}
