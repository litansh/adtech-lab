package main

import (
	"math"
	"testing"
)

func prices() Prices {
	return Prices{
		LambdaGBSecondUSD: 0.0000166667, LambdaRequestUSD: 0.0000002,
		LambdaMemoryMB: 512, APIGWRequestUSD: 0.000001,
		DynamoWriteUnitUSD: 0.000000625, S3PutUSD: 0.000005, S3GBMonthUSD: 0.023,
		CloudFrontRequestUSD: 0.0000012, CloudFrontGBEgress: 0.085,
		BuyerCallEgressKB: 4.0, AvgEventBytes: 700, AvgResponseBytes: 900,
	}
}

// The defining property: a publisher's cost must depend on the ACTIVITY it
// caused, not on the revenue it produced. Attributing by revenue is circular
// and cannot discover an unprofitable publisher.
func TestCostIsIndependentOfRevenue(t *testing.T) {
	d := Drivers{Requests: 10000, ComputeMS: 120000, EventsOut: 12000, BuyerCalls: 30000}
	c := Attribute(d, prices())

	// Attributed cost for this activity is about $0.0046 per 1000 requests, so
	// "unprofitable" means earning less than that -- not merely earning little.
	// The first version of this test used $0.10 and failed, because $0.10 on
	// 10k requests is comfortably profitable at these unit prices.
	rich := Summarise("rich", d.Requests, 50.0, c)
	poor := Summarise("poor", d.Requests, 0.02, c)

	if rich.CostUSD != poor.CostUSD {
		t.Fatalf("identical activity produced different cost: %.6f vs %.6f",
			rich.CostUSD, poor.CostUSD)
	}
	if !(poor.MarginPer1k < 0) {
		t.Errorf("a publisher earning $0.10 on 10k requests should be negative, got %.6f",
			poor.MarginPer1k)
	}
	if !(rich.MarginPer1k > 0) {
		t.Errorf("a publisher earning $50 on 10k requests should be positive, got %.6f",
			rich.MarginPer1k)
	}
}

// The case the whole model exists to surface: high volume, low fill.
func TestHighVolumeLowFillPublisherShowsNegativeMargin(t *testing.T) {
	p := prices()
	// Same revenue, but one publisher needed 20x the requests to earn it.
	efficient := Attribute(Drivers{Requests: 5000, ComputeMS: 60000, EventsOut: 6000, BuyerCalls: 15000}, p)
	wasteful := Attribute(Drivers{Requests: 100000, ComputeMS: 1200000, EventsOut: 120000, BuyerCalls: 300000}, p)

	e := Summarise("efficient", 5000, 2.00, efficient)
	w := Summarise("wasteful", 100000, 2.00, wasteful)

	if !(w.CostUSD > e.CostUSD*10) {
		t.Fatalf("20x the activity should cost far more: %.6f vs %.6f", w.CostUSD, e.CostUSD)
	}
	if w.MarginPct >= e.MarginPct {
		t.Errorf("the wasteful publisher should have worse margin: %.1f%% vs %.1f%%",
			w.MarginPct, e.MarginPct)
	}
}

// Zero revenue with real cost is unbounded loss, not 0% margin. Reporting 0
// would hide precisely the case this is built to find.
func TestZeroRevenueWithCostIsNotZeroMargin(t *testing.T) {
	c := Attribute(Drivers{Requests: 1000, ComputeMS: 12000, EventsOut: 1200, BuyerCalls: 3000}, prices())
	m := Summarise("freeloader", 1000, 0, c)
	if !math.IsInf(m.MarginPct, -1) {
		t.Fatalf("margin was %.2f, want -Inf", m.MarginPct)
	}
	if m.MarginPer1k >= 0 {
		t.Errorf("margin per 1k was %.6f, want negative", m.MarginPer1k)
	}
}

func TestMoreBuyerCallsCostMore(t *testing.T) {
	p := prices()
	few := Attribute(Drivers{Requests: 1000, ComputeMS: 10000, EventsOut: 1000, BuyerCalls: 1000}, p)
	many := Attribute(Drivers{Requests: 1000, ComputeMS: 10000, EventsOut: 1000, BuyerCalls: 5000}, p)
	if !(many.BuyerCalls > few.BuyerCalls) {
		t.Fatal("fanning out to five buyers must cost more than one")
	}
	if many.Lambda != few.Lambda {
		t.Error("Lambda cost should not change with buyer count at equal compute time")
	}
}

// buyer-slow in the Lab: bids highest, times out on 75% of calls, wins nothing.
// Under any revenue-based view it is invisible. It must not be here.
func TestBuyerThatNeverWinsIsPureCost(t *testing.T) {
	b := SummariseBuyer("buyer-slow", 4000, 1000, 3000, 0, 0, prices())
	if !math.IsInf(b.CostPerWin, 1) {
		t.Fatalf("cost per win was %.6f, want +Inf so it sorts to the top", b.CostPerWin)
	}
	if b.NetUSD >= 0 {
		t.Errorf("a buyer with no revenue and real cost must be net negative, got %.6f", b.NetUSD)
	}
	if math.Abs(b.TimeoutRate-0.75) > 1e-9 {
		t.Errorf("timeout rate %.3f, want 0.75", b.TimeoutRate)
	}
}

func TestProfitableBuyerIsNetPositive(t *testing.T) {
	b := SummariseBuyer("buyer-premium", 4000, 3000, 100, 900, 12.50, prices())
	if b.NetUSD <= 0 {
		t.Fatalf("a buyer winning 900 times for $12.50 should be net positive, got %.6f", b.NetUSD)
	}
	if math.IsInf(b.CostPerWin, 1) {
		t.Error("a buyer with wins should have a finite cost per win")
	}
}

func TestResponseBytesFallBackToTheAverageWhenNotLogged(t *testing.T) {
	p := prices()
	withBytes := Attribute(Drivers{Requests: 1000, RespBytes: 900000}, p)
	without := Attribute(Drivers{Requests: 1000}, p) // 1000 * 900 = 900000
	if math.Abs(withBytes.CloudFront-without.CloudFront) > 1e-12 {
		t.Fatalf("fallback disagreed with the explicit value: %.9f vs %.9f",
			withBytes.CloudFront, without.CloudFront)
	}
}

func TestCostBreakdownSumsToTotal(t *testing.T) {
	c := Attribute(Drivers{Requests: 5000, ComputeMS: 60000, EventsOut: 6000, BuyerCalls: 15000}, prices())
	sum := c.Lambda + c.APIGateway + c.Dynamo + c.S3 + c.CloudFront + c.BuyerCalls
	if math.Abs(sum-c.Total()) > 1e-12 {
		t.Fatalf("breakdown %.12f != total %.12f", sum, c.Total())
	}
	if c.Total() <= 0 {
		t.Fatal("real activity produced zero cost")
	}
}
