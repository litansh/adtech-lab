package main

import "math"

// ---------------------------------------------------------------------------
// Activity-based costing.
//
// Cloud billing is organised by RESOURCE; AdTech economics are organised by
// COUNTERPARTY. One Lambda serves every publisher, so AWS can say what the
// Lambda cost and not which publisher caused it.
//
// So we do not read the bill. We price the DRIVERS -- the measurable units that
// cause spend -- and multiply by the volume each counterparty generated, taken
// from our own event log.
//
// What this deliberately does NOT do is attribute cost in proportion to
// revenue. That is the industry's usual approach and it is circular: a
// publisher earning 2% of revenue is assigned 2% of cost, so its margin equals
// the platform average BY CONSTRUCTION. An unprofitable publisher cannot be
// discovered that way -- not unlikely to be, cannot be.
// ---------------------------------------------------------------------------

type Prices struct {
	LambdaGBSecondUSD    float64 `json:"lambda_gb_second_usd"`
	LambdaRequestUSD     float64 `json:"lambda_request_usd"`
	LambdaMemoryMB       float64 `json:"lambda_memory_mb"`
	APIGWRequestUSD      float64 `json:"apigw_request_usd"`
	DynamoWriteUnitUSD   float64 `json:"dynamodb_write_unit_usd"`
	S3PutUSD             float64 `json:"s3_put_usd"`
	S3GBMonthUSD         float64 `json:"s3_gb_month_usd"`
	CloudFrontRequestUSD float64 `json:"cloudfront_request_usd"`
	CloudFrontGBEgress   float64 `json:"cloudfront_gb_egress_usd"`
	BuyerCallEgressKB    float64 `json:"buyer_call_egress_kb"`
	AvgEventBytes        float64 `json:"avg_event_bytes"`
	AvgResponseBytes     float64 `json:"avg_response_bytes"`
}

// Drivers is the measured activity attributable to one counterparty.
type Drivers struct {
	Requests   int
	ComputeMS  float64 // summed in-handler latency
	EventsOut  int     // events written to the log
	BuyerCalls int
	RespBytes  float64 // 0 means "not logged", fall back to the average
}

// Cost is the attributed cost, broken out so a surprising total can be
// explained rather than merely disputed.
type Cost struct {
	Lambda     float64
	APIGateway float64
	Dynamo     float64
	S3         float64
	CloudFront float64
	BuyerCalls float64
}

func (c Cost) Total() float64 {
	return c.Lambda + c.APIGateway + c.Dynamo + c.S3 + c.CloudFront + c.BuyerCalls
}

// Attribute prices one counterparty's measured drivers.
//
// Honest limitation, stated in code because it is easy to forget: ComputeMS is
// measured in-handler, so it excludes cold starts and runtime overhead, and
// Lambda's per-invocation minimum is not modelled. Attributed Lambda cost is
// therefore an UNDERESTIMATE, consistently, for everyone -- which is why this
// model is fit for ranking and unfit for invoicing.
func Attribute(d Drivers, p Prices) Cost {
	gbSeconds := (d.ComputeMS / 1000.0) * (p.LambdaMemoryMB / 1024.0)

	respBytes := d.RespBytes
	if respBytes == 0 {
		respBytes = float64(d.Requests) * p.AvgResponseBytes
	}

	return Cost{
		Lambda:     gbSeconds*p.LambdaGBSecondUSD + float64(d.Requests)*p.LambdaRequestUSD,
		APIGateway: float64(d.Requests) * p.APIGWRequestUSD,
		// One write unit per KB, rounded up, per event.
		Dynamo: float64(d.EventsOut) * math.Ceil(p.AvgEventBytes/1024.0) * p.DynamoWriteUnitUSD,
		// Events are batched into objects, so PUTs are amortised rather than
		// one per event. Storage is charged on the bytes written.
		S3: float64(d.EventsOut)*p.AvgEventBytes/1e9*p.S3GBMonthUSD +
			float64(d.EventsOut)/1000.0*p.S3PutUSD,
		CloudFront: float64(d.Requests)*p.CloudFrontRequestUSD +
			respBytes/1e9*p.CloudFrontGBEgress,
		// A buyer call is egress out and back. The compute spent WAITING is
		// already inside ComputeMS, which is why a timing-out buyer is
		// expensive rather than free: it holds the handler for the full tmax
		// and returns nothing.
		BuyerCalls: float64(d.BuyerCalls) * p.BuyerCallEgressKB * 2 / 1e6 * p.CloudFrontGBEgress,
	}
}

// Margin is the number the whole exercise exists to produce.
type Margin struct {
	Name         string
	Requests     int
	RevenueUSD   float64
	CostUSD      float64
	RevenuePer1k float64
	CostPer1k    float64
	MarginPer1k  float64
	MarginPct    float64
}

func Summarise(name string, requests int, revenue float64, c Cost) Margin {
	m := Margin{
		Name: name, Requests: requests,
		RevenueUSD: revenue, CostUSD: c.Total(),
	}
	if requests > 0 {
		k := 1000.0 / float64(requests)
		m.RevenuePer1k = revenue * k
		m.CostPer1k = c.Total() * k
		m.MarginPer1k = m.RevenuePer1k - m.CostPer1k
	}
	if revenue > 0 {
		m.MarginPct = (revenue - c.Total()) / revenue * 100
	} else if c.Total() > 0 {
		// No revenue and non-zero cost is not "0% margin", it is unbounded
		// loss. Reporting 0 would hide exactly the case we built this to find.
		m.MarginPct = math.Inf(-1)
	}
	return m
}

// BuyerEconomics answers the demand-side question nobody asks: what does asking
// this buyer actually cost, and what did each win cost to obtain?
type BuyerEconomics struct {
	BuyerID     string
	Calls       int
	Bids        int
	Timeouts    int
	Wins        int
	RevenueUSD  float64
	CostUSD     float64
	CostPerCall float64
	CostPerWin  float64
	TimeoutRate float64
	NetUSD      float64
}

func SummariseBuyer(id string, calls, bids, timeouts, wins int, revenue float64, p Prices) BuyerEconomics {
	cost := float64(calls) * p.BuyerCallEgressKB * 2 / 1e6 * p.CloudFrontGBEgress
	b := BuyerEconomics{
		BuyerID: id, Calls: calls, Bids: bids, Timeouts: timeouts, Wins: wins,
		RevenueUSD: revenue, CostUSD: cost, NetUSD: revenue - cost,
	}
	if calls > 0 {
		b.CostPerCall = cost / float64(calls)
		b.TimeoutRate = float64(timeouts) / float64(calls)
	}
	if wins > 0 {
		b.CostPerWin = cost / float64(wins)
	} else if cost > 0 {
		// A buyer that never wins has no cost-per-win, it has pure cost. Left
		// as +Inf so it sorts to the top rather than disappearing as zero.
		b.CostPerWin = math.Inf(1)
	}
	return b
}
