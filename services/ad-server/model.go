package main

import "time"

// ---------------------------------------------------------------------------
// Control plane.
//
// Changes rarely, is read on every request. In AWS this lives in DynamoDB and
// is loaded into Lambda memory with a short TTL -- the same trick every real ad
// server uses, because a per-request database lookup does not fit the latency
// budget. See docs/architecture-proposal.md.
// ---------------------------------------------------------------------------

type Publisher struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Share of media value credited to the Publisher. The remainder is the
	// platform fee. Kept on the Publisher because it is a commercial term
	// between two entities, not a property of a campaign.
	RevenueShare float64 `json:"revenue_share"`
}

type Site struct {
	ID          string `json:"id"`
	PublisherID string `json:"publisher_id"`
	Domain      string `json:"domain"`
}

// Placement is one ad slot on one page: the unit of inventory.
type Placement struct {
	ID     string `json:"id"`
	SiteID string `json:"site_id"`
	Width  int    `json:"w"`
	Height int    `json:"h"`
}

type Advertiser struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Campaign is the business intent: who is advertising, and between which dates.
type Campaign struct {
	ID           string    `json:"id"`
	AdvertiserID string    `json:"advertiser_id"`
	Name         string    `json:"name"`
	Status       string    `json:"status"` // ACTIVE | PAUSED
	FlightStart  time.Time `json:"flight_start"`
	FlightEnd    time.Time `json:"flight_end"`
}

// LineItem is the delivery contract: price, targeting, priority, budget.
//
// Targeting and priority live here rather than on the Campaign because one
// campaign routinely buys several different audiences at several different
// prices, and each of those is a separate promise about delivery.
type LineItem struct {
	ID         string `json:"id"`
	CampaignID string `json:"campaign_id"`
	Name       string `json:"name"`
	Status     string `json:"status"` // ACTIVE | PAUSED

	// Priority ranks demand ahead of price. A guaranteed line item outranks a
	// more valuable non-guaranteed one because the Publisher has contracted to
	// deliver it. Higher number = served first.
	Priority int `json:"priority"`

	PricingModel string  `json:"pricing_model"` // CPM | CPC | CPA
	Rate         float64 `json:"rate"`          // USD per 1000 imps (CPM), per click (CPC), per action (CPA)

	// Expected performance, used to normalise CPC/CPA into a comparable eCPM.
	// In production these come from observed history; here they are configured,
	// which teaches the same lesson without pretending we have data we do not.
	ExpectedCTR float64 `json:"expected_ctr"` // clicks / impressions
	ExpectedCVR float64 `json:"expected_cvr"` // conversions / clicks

	// Pacing spreads the daily budget across the day rather than spending it
	// as fast as demand allows. "even" (default) or "asap". See pacing.go.
	Pacing string `json:"pacing,omitempty"`

	// FrequencyCap limits how often one person sees this line item. Ours is
	// session-scoped, which is the honest limit of capping without a persistent
	// identifier -- see frequency.go.
	FrequencyCap FrequencyCap `json:"frequency_cap,omitempty"`

	DailyBudget float64   `json:"daily_budget"` // USD
	Targeting   Targeting `json:"targeting"`
	CreativeIDs []string  `json:"creative_ids"`
}

// Targeting predicates. An empty slice means "no constraint on this dimension"
// -- deliberately, so that a house fallback needs no special casing.
type Targeting struct {
	Countries   []string `json:"countries"`
	DeviceTypes []string `json:"device_types"`
	Games       []string `json:"games"`
	Placements  []string `json:"placements"`
}

type Creative struct {
	ID       string `json:"id"`
	Width    int    `json:"w"`
	Height   int    `json:"h"`
	HTML     string `json:"html"`
	ClickURL string `json:"click_url"`
}

// ControlPlane is the whole configured world, as loaded into memory.
type ControlPlane struct {
	Publishers  []Publisher  `json:"publishers"`
	Sites       []Site       `json:"sites"`
	Placements  []Placement  `json:"placements"`
	Advertisers []Advertiser `json:"advertisers"`
	Campaigns   []Campaign   `json:"campaigns"`
	LineItems   []LineItem   `json:"line_items"`
	Creatives   []Creative   `json:"creatives"`
	Buyers      []Buyer      `json:"buyers"`

	// Experiments live in the control plane rather than in configuration so the
	// cold-path controller can shift traffic between arms without a deploy.
	// See experiment.go and ADR 0006.
	Experiments []Experiment `json:"experiments,omitempty"`
}

func (c *ControlPlane) placement(id string) *Placement {
	for i := range c.Placements {
		if c.Placements[i].ID == id {
			return &c.Placements[i]
		}
	}
	return nil
}

func (c *ControlPlane) lineItem(id string) *LineItem {
	for i := range c.LineItems {
		if c.LineItems[i].ID == id {
			return &c.LineItems[i]
		}
	}
	return nil
}

func (c *ControlPlane) campaign(id string) *Campaign {
	for i := range c.Campaigns {
		if c.Campaigns[i].ID == id {
			return &c.Campaigns[i]
		}
	}
	return nil
}

func (c *ControlPlane) creative(id string) *Creative {
	for i := range c.Creatives {
		if c.Creatives[i].ID == id {
			return &c.Creatives[i]
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Request / response
// ---------------------------------------------------------------------------

type AdRequest struct {
	PlacementID string `json:"placement_id"`
	Game        string `json:"game"`
	SessionID   string `json:"session_id"`
	DeviceType  string `json:"device_type"`
	Width       int    `json:"w"`
	Height      int    `json:"h"`

	// Env separates Lab traffic from real traffic. CLAUDE.md requires that
	// synthetic activity never appears in any figure reported as real, so it is
	// stamped on every event and every query filters on it.
	//
	// Honest limitation: this is CLIENT-ASSERTED. A real visitor could send
	// env=lab and hide their activity. That is acceptable only because there is
	// no real money and no external partner yet -- when either exists, the Lab
	// needs its own stack, not a field. Recorded rather than glossed over.
	Env string `json:"env,omitempty"`

	// Derived server-side, never trusted from the client.
	Country string `json:"-"`
	// UserAgent and IP are used in-request only -- passed to buyers when the
	// field experiments allow it, never persisted. See docs/privacy-baseline.md.
	UserAgent string `json:"-"`
	IP        string `json:"-"`
}

type AdResponse struct {
	RequestID string `json:"request_id"`
	// Traffic is the classifier's verdict, returned in the Lab only. Without
	// it, a no_ad caused by traffic classification is indistinguishable from
	// one caused by having no eligible demand -- and those send you to
	// completely different places to investigate.
	Traffic *TrafficAssessment `json:"traffic,omitempty"`
	Ad      *ServedAd          `json:"ad,omitempty"`
	NoAd    bool               `json:"no_ad,omitempty"`
	Trace   *Trace             `json:"trace,omitempty"`   // Lab only. Never in production.
	Auction *AuctionResult     `json:"auction,omitempty"` // Lab only.
}

type ServedAd struct {
	CreativeID    string `json:"creative_id"`
	LineItemID    string `json:"line_item_id"`
	HTML          string `json:"html"`
	TrackingToken string `json:"tracking_token"`
}

// ---------------------------------------------------------------------------
// Phase 2: buyers.
//
// A Buyer is an independent source of demand that responds to an opportunity
// with a PRICE, rather than a line item the publisher booked in advance. This
// is the distinction between direct-sold and programmatic demand, and it is why
// bidding vocabulary becomes correct from this phase and was wrong before it.
//
// These buyers are simulated inside the ad server. Phase 4 moves one of them
// across a real network boundary and it becomes a DSP. The auction does not
// change when that happens -- only the transport does.
// ---------------------------------------------------------------------------

type Buyer struct {
	ID   string `json:"id"`
	Name string `json:"name"`

	// Willingness to pay, per 1000 impressions. Each buyer draws around this.
	BaseCPM  float64 `json:"base_cpm"`
	Variance float64 `json:"variance"` // +/- this fraction of BaseCPM

	// Deliberately imperfect behaviour. A buyer population where everyone
	// always bids instantly is not a marketplace, it is a sorted list -- and it
	// would teach nothing about why bid density is low in reality.
	BidRate     float64 `json:"bid_rate"`     // 0..1 -- how often it bids at all
	TimeoutRate float64 `json:"timeout_rate"` // 0..1 -- how often it answers too late
	LatencyMS   int     `json:"latency_ms"`   // typical response time

	Targeting   Targeting `json:"targeting"`
	DailyBudget float64   `json:"daily_budget"`
	CreativeIDs []string  `json:"creative_ids"`

	// Endpoint makes this a REAL buyer, reached over HTTP with tmax as a hard
	// deadline. Empty means the simulated in-process buyer, which is still what
	// the Lab uses for deterministic tests.
	Endpoint string `json:"endpoint,omitempty"`

	// FieldSensitivity is the fraction of this buyer's bid rate lost when an
	// optional bid-request field is absent. Lab only -- it is how we simulate
	// the thing the field experiments exist to DISCOVER. A real DSP never tells
	// you this; you find it by testing, which is the whole point.
	//
	// Example: {"user_eids": 0.6} means omitting user.eids costs 60% of this
	// buyer's bid rate.
	FieldSensitivity map[string]float64 `json:"field_sensitivity,omitempty"`
}

// Bid is one buyer's response to one opportunity.
type Bid struct {
	BuyerID    string  `json:"buyer_id"`
	BuyerName  string  `json:"buyer_name"`
	CPM        float64 `json:"cpm,omitempty"`
	CreativeID string  `json:"creative_id,omitempty"`
	LatencyMS  int     `json:"latency_ms"`
	Status     string  `json:"status"` // see the Bid* constants
	Reason     string  `json:"reason,omitempty"`

	// AdM and NURL come from a real buyer over the network. AdM is the markup
	// it wants rendered; NURL is the win notice we call if it wins -- and the
	// single largest source of reporting discrepancy, because that call can be
	// lost and nobody sees an error when it is.
	AdM  string `json:"-"`
	NURL string `json:"-"`
}

const (
	BidPlaced      = "bid"
	BidNoBid       = "no_bid"       // the buyer chose not to bid
	BidTimeout     = "timeout"      // answered after tmax; not in the auction at all
	BidBelowFloor  = "below_floor"  // bid, but under the publisher's price
	BidNotEligible = "not_eligible" // targeting or budget excluded it before bidding
	BidWon         = "won"
	BidLost        = "lost"
)

// AuctionResult is a replayable record of one auction.
type AuctionResult struct {
	AuctionID     string  `json:"auction_id"`
	Floor         float64 `json:"floor"`
	TmaxMS        int     `json:"tmax_ms"`
	Bids          []Bid   `json:"bids"`
	Winner        *Bid    `json:"winner,omitempty"`
	ClearingPrice float64 `json:"clearing_price"`
	Reason        string  `json:"reason"`

	// Marketplace health, computed per auction so it can be aggregated later.
	BuyersCalled int `json:"buyers_called"`
	BidsReceived int `json:"bids_received"` // bid density
	TimedOut     int `json:"timed_out"`
	BelowFloor   int `json:"below_floor"`
}

// publisherFor resolves placement -> site -> publisher. Cost attribution needs
// the publisher on every event: the placement is the unit of inventory, but the
// publisher is the counterparty whose margin we care about.
func (c *ControlPlane) publisherFor(placementID string) string {
	pl := c.placement(placementID)
	if pl == nil {
		return ""
	}
	for i := range c.Sites {
		if c.Sites[i].ID == pl.SiteID {
			return c.Sites[i].PublisherID
		}
	}
	return ""
}
