package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Calling a real buyer over the network.
//
// Until now buyers were a loop in this process: same memory, same clock, and a
// "timeout" that was a random number. Across an actual HTTP boundary three
// things stop being simulations:
//
//   1. tmax is a DEADLINE enforced by a context, and a buyer that misses it is
//      cancelled mid-flight. It still did the work; we still pay for the
//      connection; nobody gets an ad.
//   2. Buyers are called in PARALLEL against a shared deadline. Calling five
//      buyers sequentially with a 100ms tmax each is a 500ms auction, which is
//      not an auction, it is a queue.
//   3. Failures are ordinary. Connection refused, malformed JSON, a 500 -- all
//      are no-bids, and none of them may take the auction down with them.
// ---------------------------------------------------------------------------

// Our identity in the supply chain. These three values must agree with
// apps/publisher/ads.txt and apps/publisher/sellers.json, and
// tools/supplychain checks that they do.
const (
	sellerDomain = "xoxoxo.live"
	sellerID     = "xoxoxo-1"
)

// httpClient is shared so connections are reused. A new client per request
// means a new TCP and TLS handshake per request, which at bid-path latencies is
// most of the budget spent before the buyer has read anything.
var httpClient = &http.Client{
	Transport: &http.Transport{
		MaxIdleConns:        200,
		MaxIdleConnsPerHost: 50,
		IdleConnTimeout:     90 * time.Second,
	},
}

// ortbRequest is the subset we send. Protected fields are always present --
// see bidrequest.go; the optional ones are governed by the field experiments.
type ortbRequest struct {
	ID     string      `json:"id"`
	Imp    []ortbImp   `json:"imp"`
	Site   *ortbSite   `json:"site,omitempty"`
	Device *ortbDevice `json:"device,omitempty"`
	User   *ortbUser   `json:"user,omitempty"`
	TMax   int         `json:"tmax"`
	Source *ortbSource `json:"source,omitempty"`
	Regs   *ortbRegs   `json:"regs,omitempty"`
}

type ortbImp struct {
	ID       string      `json:"id"`
	BidFloor float64     `json:"bidfloor,omitempty"`
	Banner   *ortbBanner `json:"banner,omitempty"`
	Secure   int         `json:"secure"`
}
type ortbBanner struct{ W, H int }
type ortbSite struct {
	ID       string `json:"id"`
	Domain   string `json:"domain"`
	Keywords string `json:"keywords,omitempty"`
}
type ortbDevice struct {
	UA         string   `json:"ua,omitempty"`
	IP         string   `json:"ip,omitempty"`
	Geo        *ortbGeo `json:"geo,omitempty"`
	DeviceType int      `json:"devicetype,omitempty"`
}
type ortbGeo struct {
	Country string `json:"country,omitempty"`
}
type ortbUser struct {
	ID string `json:"id,omitempty"`
}
type ortbSource struct {
	SChain *ortbSChain `json:"schain,omitempty"`
}
type ortbSChain struct {
	Complete int              `json:"complete"`
	Ver      string           `json:"ver"`
	Nodes    []ortbSChainNode `json:"nodes"`
}
type ortbSChainNode struct {
	ASI string `json:"asi"`
	SID string `json:"sid"`
	HP  int    `json:"hp"`
}
type ortbRegs struct {
	GDPR int    `json:"gdpr,omitempty"`
	GPP  string `json:"gpp,omitempty"`
}

type ortbResponse struct {
	ID      string `json:"id"`
	Cur     string `json:"cur"`
	SeatBid []struct {
		Seat string `json:"seat"`
		Bid  []struct {
			ID    string  `json:"id"`
			ImpID string  `json:"impid"`
			Price float64 `json:"price"`
			AdM   string  `json:"adm"`
			CID   string  `json:"cid"`
			CrID  string  `json:"crid"`
			NURL  string  `json:"nurl"`
		} `json:"bid"`
	} `json:"seatbid"`
}

// buildRequest constructs the bid request. Protected fields are unconditional:
// there is no code path that omits schain, consent or the secure flag.
func buildRequest(requestID string, req AdRequest, pl *Placement, floor float64,
	tmax int, fields FieldSet) ortbRequest {

	r := ortbRequest{
		ID:   requestID,
		TMax: tmax,
		Imp: []ortbImp{{
			ID: "1", Banner: &ortbBanner{W: pl.Width, H: pl.Height},
			Secure: 1,
		}},
		Site: &ortbSite{ID: pl.SiteID, Domain: "xoxoxo.live"},
		// Always sent. Supply chain transparency is not optimisation surface.
		//
		// SID must be the SELLER ID from sellers.json -- not the site id, which
		// is what this sent first. A buyer verifies by looking sid up in the
		// asi domain's sellers.json; a site id is not there, so the chain fails
		// verification while looking perfectly well-formed.
		//
		// complete=1 asserts every node between the publisher and here is
		// listed. It is true for owned-and-operated inventory with one hop, and
		// claiming it when an upstream node is missing is a false statement to
		// the buyer -- the one field in schain that is a lie rather than a bug
		// when it is wrong.
		Source: &ortbSource{SChain: &ortbSChain{
			Complete: 1,
			Ver:      "1.0",
			Nodes: []ortbSChainNode{{
				ASI: sellerDomain, SID: sellerID, HP: 1,
			}},
		}},
		Regs: &ortbRegs{},
	}

	// Everything below is governed by field experiments.
	if fields == nil || fields["imp_bidfloor"] {
		r.Imp[0].BidFloor = floor
	}
	dev := &ortbDevice{}
	if fields == nil || fields["device_ua"] {
		dev.UA = req.UserAgent
	}
	if fields == nil || fields["device_ip"] {
		dev.IP = req.IP
	}
	if req.Country != "" {
		dev.Geo = &ortbGeo{Country: req.Country}
	}
	if req.DeviceType == "mobile" {
		dev.DeviceType = 1
	} else {
		dev.DeviceType = 2
	}
	r.Device = dev

	if fields == nil || fields["site_keywords"] {
		r.Site.Keywords = req.Game
	}
	// No persistent advertising identifier in production, by policy. The
	// session id is sent so a buyer can frequency-cap WITHIN a session, which
	// is the same limit our own capping has and for the same reason.
	if req.SessionID != "" {
		r.User = &ortbUser{ID: req.SessionID}
	}
	return r
}

// httpBidResult is one buyer's outcome, including the outcomes that are not bids.
type httpBidResult struct {
	BuyerID     string
	Price       float64
	AdM         string
	CreativeID  string
	NURL        string
	LatencyMS   int
	TimedOut    bool
	Err         string
	NoBidReason string
}

// callBuyers fans out to every HTTP buyer in PARALLEL against one shared
// deadline. Sequential calls with a per-buyer timeout would make the auction as
// slow as the sum of its buyers.
func callBuyers(ctx context.Context, buyers []Buyer, body ortbRequest, tmax int) []httpBidResult {
	var endpoints []Buyer
	for _, b := range buyers {
		if b.Endpoint != "" {
			endpoints = append(endpoints, b)
		}
	}
	if len(endpoints) == 0 {
		return nil
	}

	deadline, cancel := context.WithTimeout(ctx, time.Duration(tmax)*time.Millisecond)
	defer cancel()

	payload, err := json.Marshal(body)
	if err != nil {
		return nil
	}

	out := make([]httpBidResult, len(endpoints))
	var wg sync.WaitGroup
	for i := range endpoints {
		wg.Add(1)
		go func(i int, b Buyer) {
			defer wg.Done()
			out[i] = callOne(deadline, b, payload)
		}(i, endpoints[i])
	}
	wg.Wait()
	return out
}

// The return value is NAMED so the deferred latency assignment lands in it.
// With an unnamed return, `return res` copies the struct before the defer runs
// and every buyer reports 0ms -- which is exactly what happened, and it is the
// kind of bug that makes a latency dashboard quietly useless rather than
// obviously broken.
func callOne(ctx context.Context, b Buyer, payload []byte) (res httpBidResult) {
	res = httpBidResult{BuyerID: b.ID}
	start := time.Now()
	defer func() { res.LatencyMS = int(time.Since(start).Milliseconds()) }()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.Endpoint, bytes.NewReader(payload))
	if err != nil {
		res.Err = err.Error()
		return res
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-OpenRTB-Version", "2.6")

	resp, err := httpClient.Do(req)
	if err != nil {
		// A cancelled context is a TIMEOUT, which is a different fact from a
		// connection failure: one buyer was too slow, the other was not there.
		// Merging them hides which one you have.
		if ctx.Err() != nil {
			res.TimedOut = true
		} else {
			res.Err = err.Error()
		}
		return res
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		res.NoBidReason = resp.Header.Get("X-Nobid-Reason")
		if res.NoBidReason == "" {
			res.NoBidReason = "no_bid"
		}
		return res
	}
	if resp.StatusCode != http.StatusOK {
		res.Err = fmt.Sprintf("http %d", resp.StatusCode)
		return res
	}

	var body ortbResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		res.Err = "malformed response"
		return res
	}
	for _, sb := range body.SeatBid {
		for _, bid := range sb.Bid {
			if bid.Price > res.Price {
				res.Price, res.AdM, res.CreativeID, res.NURL =
					bid.Price, bid.AdM, bid.CrID, bid.NURL
			}
		}
	}
	if res.Price <= 0 {
		res.NoBidReason = "empty_seatbid"
	}
	return res
}

// notifyWin calls the buyer's nurl. Fire-and-forget with a short timeout: the
// player is waiting for an ad, not for our bookkeeping.
//
// This call is also the origin of most reporting discrepancy. It can fail, and
// when it does the buyer never learns it won -- so its numbers are permanently
// lower than ours, with no error anyone sees.
func notifyWin(nurl string, clearingPriceCPM float64) {
	if nurl == "" {
		return
	}
	url := strings.ReplaceAll(nurl, "${AUCTION_PRICE}",
		fmt.Sprintf("%.4f", clearingPriceCPM))
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return
		}
		if resp, err := httpClient.Do(req); err == nil {
			resp.Body.Close()
		}
	}()
}
