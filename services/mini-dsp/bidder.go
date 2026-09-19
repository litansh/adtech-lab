package main

import (
	"fmt"
	"math/rand"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Bidder holds this buyer's own state: its budget, its counters, its clock.
// None of it is shared with the seller, which is the entire point.
type Bidder struct {
	mu        sync.Mutex
	campaigns []Campaign

	spend map[string]float64 // campaign -> spent today, by OUR count
	freq  map[string]int     // campaign|user -> impressions, by OUR count

	// Three counters that SHOULD agree and never will. Keeping them separate is
	// what makes the discrepancy visible instead of averaged away.
	bidsSubmitted map[string]int // we returned a bid
	winsNotified  map[string]int // the seller told us we won
	// bidsSubmitted - winsNotified is not "losses". It is losses PLUS wins whose
	// notification never arrived PLUS bids that arrived after tmax and were
	// never in the auction at all.

	// baseURL makes the win notice ABSOLUTE. A relative nurl is silently
	// useless: the seller cannot resolve it, the call never happens, and the
	// buyer's numbers are permanently zero with no error on either side. That
	// is not a hypothetical -- it is what this returned first, and the only
	// symptom was a 100% discrepancy.
	baseURL string

	rng *rand.Rand
	now func() time.Time
}

func NewBidder(cs []Campaign, seed int64, baseURL string) *Bidder {
	return &Bidder{
		campaigns:     cs,
		baseURL:       strings.TrimRight(baseURL, "/"),
		spend:         map[string]float64{},
		freq:          map[string]int{},
		bidsSubmitted: map[string]int{},
		winsNotified:  map[string]int{},
		rng:           rand.New(rand.NewSource(seed)),
		now:           time.Now,
	}
}

// NoBidReason is recorded rather than returned, because a DSP that cannot say
// why it declined cannot be debugged by the seller OR by itself. Most real
// no-bids are silent, which is why bid density is such a mystery in practice.
const (
	NoBidNoCampaign = "no_eligible_campaign"
	NoBidBelowFloor = "below_floor"
	NoBidBudget     = "budget_exhausted"
	NoBidFreqCap    = "frequency_capped"
	NoBidGeo        = "geo_mismatch"
	NoBidSize       = "size_mismatch"
	NoBidNoConsent  = "no_consent"
)

type Decision struct {
	Bid    *Bid
	Reason string
	Camp   *Campaign
}

// Decide runs the buy-side decision. Pure with respect to time and randomness
// so it can be tested without a clock or a coin.
func (b *Bidder) Decide(req BidRequest) Decision {
	if len(req.Imp) == 0 {
		return Decision{Reason: NoBidNoCampaign}
	}
	imp := req.Imp[0]

	// A buyer that ignores consent is a buyer with a legal problem, not an
	// optimisation opportunity. GDPR applies and no consent string present:
	// decline. This is the one no-bid that is not about money.
	if req.Regs != nil && req.Regs.GDPR == 1 && (req.Regs.GPP == "") {
		return Decision{Reason: NoBidNoConsent}
	}

	country := ""
	if req.Device != nil && req.Device.Geo != nil {
		country = req.Device.Geo.Country
	}
	size := ""
	if imp.Banner != nil {
		size = fmt.Sprintf("%dx%d", imp.Banner.W, imp.Banner.H)
	}
	userID := ""
	if req.User != nil {
		userID = req.User.ID
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	lastReason := NoBidNoCampaign
	for i := range b.campaigns {
		c := &b.campaigns[i]

		if len(c.Countries) > 0 && !contains(c.Countries, country) {
			lastReason = NoBidGeo
			continue
		}
		if len(c.Sizes) > 0 && !contains(c.Sizes, size) {
			lastReason = NoBidSize
			continue
		}
		if c.DailyBudget > 0 && b.spend[c.ID] >= c.DailyBudget {
			lastReason = NoBidBudget
			continue
		}
		if c.FreqCapPerUser > 0 && userID != "" &&
			b.freq[c.ID+"|"+userID] >= c.FreqCapPerUser {
			lastReason = NoBidFreqCap
			continue
		}

		// Price. A real DSP shades below its true value; ours bids a random
		// fraction of max, which produces a spread without pretending to model
		// anything it has not measured.
		price := c.MaxCPM * (0.6 + b.rng.Float64()*0.4)
		if price < imp.BidFloor {
			// Bidding under a disclosed floor is spending money to be rejected.
			lastReason = NoBidBelowFloor
			continue
		}

		bidID := fmt.Sprintf("%s-%d", req.ID, b.rng.Int63())
		b.bidsSubmitted[c.ID]++
		return Decision{
			Camp: c,
			Bid: &Bid{
				ID: bidID, ImpID: imp.ID, Price: round2(price),
				AdM: c.CreativeHTML, CID: c.ID, CrID: c.ID + "-cr",
				NURL: b.baseURL + "/win?bid=" + bidID + "&cid=" + c.ID +
					"&user=" + url.QueryEscape(userID) + "&price=${AUCTION_PRICE}",
			},
		}
	}
	return Decision{Reason: lastReason}
}

// NotifyWin is called by the seller. Everything the buyer believes about
// delivery flows through here -- which is why a lost notification is a
// permanent, silent discrepancy rather than a retryable error.
func (b *Bidder) NotifyWin(campaignID, userID string, priceCPM float64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.winsNotified[campaignID]++
	b.spend[campaignID] += priceCPM / 1000
	if userID != "" {
		b.freq[campaignID+"|"+userID]++
	}
}

type Stats struct {
	CampaignID    string  `json:"campaign_id"`
	BidsSubmitted int     `json:"bids_submitted"`
	WinsNotified  int     `json:"wins_notified"`
	SpendUSD      float64 `json:"spend_usd"`
	DailyBudget   float64 `json:"daily_budget"`
	WinRate       float64 `json:"win_rate"`
}

func (b *Bidder) Stats() []Stats {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]Stats, 0, len(b.campaigns))
	for i := range b.campaigns {
		c := b.campaigns[i]
		s := Stats{
			CampaignID: c.ID, BidsSubmitted: b.bidsSubmitted[c.ID],
			WinsNotified: b.winsNotified[c.ID], SpendUSD: b.spend[c.ID],
			DailyBudget: c.DailyBudget,
		}
		if s.BidsSubmitted > 0 {
			s.WinRate = float64(s.WinsNotified) / float64(s.BidsSubmitted)
		}
		out = append(out, s)
	}
	return out
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func round2(f float64) float64 { return float64(int(f*100+0.5)) / 100 }
