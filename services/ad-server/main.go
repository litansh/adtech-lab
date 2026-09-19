package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	mrand "math/rand"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type Server struct {
	// cp returns the current control plane. Backed by a JSON file locally and
	// by DynamoDB-with-a-memory-cache in AWS, so configuration can change
	// without a redeploy while the hot path still issues ~zero reads.
	cp     func() *ControlPlane
	budget BudgetState
	// freq counts impressions per session for frequency capping. Session-scoped
	// on purpose -- see frequency.go for what that can and cannot do.
	freq FrequencyState
	sink EventSink
	key  []byte
	lab  bool
	// flush persists buffered events at the end of an invocation. No-op locally.
	flush func()

	// Auction parameters. The floor is the publisher's minimum acceptable
	// price; tmax is the buyer deadline, past which a buyer was never in the
	// auction at all.
	floor float64
	tmax  int

	// math/rand is not safe for concurrent use. A Lambda handles one request at
	// a time, but the local dev server does not.
	rngMu sync.Mutex
	rng   *mrand.Rand
}

func newRequestID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func newEventID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// deriveCountry resolves geo server-side. Never trust the client for targeting
// input -- an advertiser is paying for a specific audience.
//
// In AWS, CloudFront supplies CloudFront-Viewer-Country. The raw IP is used
// in-request and never persisted; see docs/privacy-baseline.md.
func deriveCountry(r *http.Request) string {
	if c := r.Header.Get("CloudFront-Viewer-Country"); c != "" {
		return c
	}
	if c := os.Getenv("ADLAB_DEFAULT_COUNTRY"); c != "" {
		return c
	}
	return "IL"
}

// signalsFrom collects everything the traffic classifier is allowed to see.
// The IP is used in-request and never persisted; see docs/privacy-baseline.md.
func signalsFrom(r *http.Request, req AdRequest) RequestSignals {
	ip := r.Header.Get("CloudFront-Viewer-Address")
	if i := strings.LastIndex(ip, ":"); i > 0 {
		ip = ip[:i] // CloudFront sends ip:port
	}
	if ip == "" {
		ip, _, _ = net.SplitHostPort(r.RemoteAddr)
	}
	return RequestSignals{
		UserAgent:  r.Header.Get("User-Agent"),
		IP:         ip,
		SecPurpose: r.Header.Get("Sec-Purpose"),
		// Accept any of the competing declaration headers. None is a ratified
		// standard yet; refusing all of them until one wins would mean treating
		// every honest agent as fraud in the meantime.
		AgentDeclaration: firstNonEmpty(
			r.Header.Get("X-Agent-Declaration"),
			r.Header.Get("Sec-Agent"),
			r.Header.Get("X-Automated-Client"),
		),
		Width:      req.Width,
		Height:     req.Height,
		DeviceType: req.DeviceType,
	}
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

// clientIP prefers CloudFront's header. Used in-request and never stored.
func clientIP(r *http.Request) string {
	if v := r.Header.Get("CloudFront-Viewer-Address"); v != "" {
		if i := strings.LastIndex(v, ":"); i > 0 {
			return v[:i]
		}
		return v
	}
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		if i := strings.Index(v, ","); i > 0 {
			return strings.TrimSpace(v[:i])
		}
		return strings.TrimSpace(v)
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	return host
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

// --------------------------------------------------------------------------
// POST /ad/request
// --------------------------------------------------------------------------

func (s *Server) handleAdRequest(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req AdRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	req.Country = deriveCountry(r)
	// In-request only, never persisted. Passed to buyers when the field
	// experiments allow it -- see docs/privacy-baseline.md.
	req.UserAgent = r.Header.Get("User-Agent")
	req.IP = clientIP(r)
	traffic := ClassifyTraffic(signalsFrom(r, req))

	requestID := newRequestID()
	now := time.Now().UTC()

	cp := s.cp()
	pl := cp.placement(req.PlacementID)
	if pl == nil {
		// Unknown placement: not an error the browser can fix, and not billable.
		s.sink.Emit(map[string]any{
			"event": "ad_request", "event_id": newEventID(), "request_id": requestID,
			"placement_id": req.PlacementID, "session_id": req.SessionID,
			"decision_reason": "unknown_placement", "ts": now.UnixMilli(),
		})
		writeJSON(w, http.StatusOK, AdResponse{RequestID: requestID, NoAd: true})
		return
	}

	// Every auction parameter is a variant, not a constant. Assignment is a
	// hash and a range lookup -- no learning happens here. See ADR 0006.
	// Traffic we will not bill for does not go to the auction at all. Calling
	// buyers for an impression we have already decided is unbillable would
	// spend their tmax budget on inventory they must never be charged for --
	// and a bid we refuse to honour is worse than no bid requested.
	if !traffic.Billable {
		s.serveUnbillable(w, &req, requestID, traffic, now, start)
		return
	}

	exp := Experiments{Set: cp.Experiments}
	floorVar, floorAssign := exp.Assign(ExpFloorPrice, req, requestID)
	tmaxVar, tmaxAssign := exp.Assign(ExpBuyerTmax, req, requestID)
	floor := floorVar.Float("floor_cpm", s.floor)
	tmax := tmaxVar.Int("tmax_ms", s.tmax)

	// Which optional bid-request fields this request carries. Protected fields
	// are not represented here at all -- they are always sent, and no code path
	// omits them. See bidrequest.go.
	fields, fieldAssigns := ResolveFields(exp, req, requestID)

	// math/rand is not safe for concurrent use, and a Lambda handles one request
	// at a time but the local server does not.
	s.rngMu.Lock()
	// Real buyers are called BEFORE the auction, in parallel, against one shared
	// deadline. Doing it inside the auction would make it sequential, and five
	// buyers at a 100ms tmax each is a 500ms auction -- a queue, not an auction.
	httpBids := callBuyers(r.Context(),
		cp.Buyers, buildRequest(requestID, req, pl, floor, tmax, fields), tmax)

	alloc := Allocate(cp, &req, pl, s.budget, floor, tmax, fields, s.freq, httpBids, s.rng, now)
	s.rngMu.Unlock()

	// Tell the winner it won. Fire-and-forget: the player is waiting for an ad,
	// not for our bookkeeping.
	if alloc != nil && alloc.NURL != "" {
		notifyWin(alloc.NURL, alloc.PriceCPM)
	}

	// Counted at DECISION time, not impression time.
	//
	// The trade: decision-time counts what we SERVED, impression-time counts
	// what was SEEN. Impression-time is more truthful but needs the session id
	// inside the signed token, and it under-counts during the gap between
	// deciding and the pixel firing -- so a burst of requests in one session
	// can all pass a cap that should have stopped the second one.
	//
	// Decision-time over-counts slightly (a served ad that never renders still
	// counts) and therefore errs towards RESPECTING the cap, which is the right
	// direction to be wrong in: under-delivering a capped line item is a
	// delivery problem, over-delivering it is a broken promise to the
	// advertiser.
	if s.freq != nil && alloc != nil && alloc.LineItemID != "" {
		if li := cp.lineItem(alloc.LineItemID); li != nil && li.FrequencyCap.Impressions > 0 {
			s.freq.Increment(req.SessionID, li.ID)
		}
	}

	trace := alloc.Trace
	trace.RequestID = requestID
	trace.LatencyMS = float64(time.Since(start).Microseconds()) / 1000.0

	env := req.Env
	if env != "lab" {
		env = "production"
	}
	base := map[string]any{
		"env":      env,
		"event_id": newEventID(), "request_id": requestID, "session_id": req.SessionID,
		"placement_id": req.PlacementID, "site_id": pl.SiteID, "game": req.Game,
		"country": req.Country, "device_type": req.DeviceType, "ts": now.UnixMilli(),
		// Cost attribution is per counterparty, and the placement is not the
		// counterparty. Resolved here so the cold path never has to join.
		"publisher_id": cp.publisherFor(req.PlacementID),
	}

	if alloc.Source == "none" {
		ev := map[string]any{"event": "ad_request", "decision_reason": "no_eligible_candidates",
			"candidates_evaluated": len(trace.Candidates), "latency_ms": trace.LatencyMS}
		for k, v := range base {
			ev[k] = v
		}
		s.addAuction(ev, alloc)
		addExperiments(ev, append([]Assignment{floorAssign, tmaxAssign}, fieldAssigns...)...)
		addTraffic(ev, traffic)
		ev["fields_sent"] = fields.Sorted()
		s.sink.Emit(ev)

		resp := AdResponse{RequestID: requestID, NoAd: true}
		if s.lab {
			resp.Traffic = &traffic
			resp.Trace = trace
			resp.Auction = alloc.Auction
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}

	// A REAL network buyer supplies its own markup. Looking its creative up in
	// our control plane finds nothing, because its creatives were never ours --
	// which silently turned every win by the mini-DSP into a blank slot, with
	// no error and no trace. The seller does not hold the buyer's creatives;
	// that is the point of `adm`.
	markup := alloc.AdM
	if markup == "" {
		cr := cp.creative(alloc.CreativeID)
		if cr == nil {
			// Only reachable for our OWN demand, where a missing creative is a
			// real control-plane fault worth seeing.
			resp := AdResponse{RequestID: requestID, NoAd: true}
			if s.lab {
				resp.Traffic = &traffic
				resp.Trace = trace
			}
			writeJSON(w, http.StatusOK, resp)
			return
		}
		markup = cr.HTML
	}

	// The token binds the event to THIS decision. line_item_id carries either a
	// booked line item or a buyer id, prefixed, so billing can tell them apart.
	billTo := alloc.LineItemID
	if alloc.Source == "auction" {
		billTo = "buyer:" + alloc.BuyerID
	}
	token := MintToken(s.key, TokenClaims{
		RequestID: requestID, LineItemID: billTo, CreativeID: alloc.CreativeID,
		Expires: now.Add(30 * time.Minute),
	})

	ev := map[string]any{"event": "ad_request", "decision_reason": ReasonSelected,
		"allocation_source": alloc.Source, "decision_rule": alloc.Reason,
		"line_item_id": alloc.LineItemID, "buyer_id": alloc.BuyerID,
		"creative_id": alloc.CreativeID, "price_cpm": alloc.PriceCPM,
		"candidates_evaluated": len(trace.Candidates), "latency_ms": trace.LatencyMS}
	for k, v := range base {
		ev[k] = v
	}
	s.addAuction(ev, alloc)
	addExperiments(ev, append([]Assignment{floorAssign, tmaxAssign}, fieldAssigns...)...)
	addTraffic(ev, traffic)
	ev["fields_sent"] = fields.Sorted()
	s.sink.Emit(ev)

	resp := AdResponse{
		RequestID: requestID,
		Ad:        &ServedAd{CreativeID: alloc.CreativeID, LineItemID: billTo, HTML: markup, TrackingToken: token},
	}
	if s.lab {
		resp.Trace = trace
		resp.Auction = alloc.Auction
	}
	writeJSON(w, http.StatusOK, resp)
}

// addAuction records marketplace health on the ad_request event. Bid density,
// timeouts and below-floor counts are the numbers that explain WHY a price was
// what it was -- and they are what the floor experiments in Phase 3 will move.
func (s *Server) addAuction(ev map[string]any, a *Allocation) {
	if a.Auction == nil {
		return
	}
	ev["auction_buyers_called"] = a.Auction.BuyersCalled
	ev["auction_bids_received"] = a.Auction.BidsReceived
	ev["auction_timed_out"] = a.Auction.TimedOut
	ev["auction_below_floor"] = a.Auction.BelowFloor
	ev["auction_floor"] = a.Auction.Floor
	ev["auction_clearing_price"] = a.Auction.ClearingPrice

	// Per-buyer outcomes, compactly.
	//
	// Without this the log answers "how did the auction go?" and cannot answer
	// "how is THIS buyer doing?", which is the only question the Demand agent
	// asks. The aggregate says 23% of calls timed out; it cannot say that one
	// buyer accounts for all of them.
	//
	// Four fields per buyer rather than the whole Bid struct: buyer, status,
	// price, latency. Everything else is either derivable or noise, and the
	// event is written on every request.
	if len(a.Auction.Bids) > 0 {
		rows := make([]map[string]any, 0, len(a.Auction.Bids))
		for _, b := range a.Auction.Bids {
			row := map[string]any{"b": b.BuyerID, "s": b.Status, "l": b.LatencyMS}
			if b.CPM > 0 {
				row["p"] = b.CPM
			}
			if a.Auction.Winner != nil && a.Auction.Winner.BuyerID == b.BuyerID {
				row["w"] = 1
			}
			rows = append(rows, row)
		}
		ev["auction_buyers"] = rows
	}
}

// serveUnbillable answers traffic we have decided not to charge for. It returns
// a genuine no-ad rather than an error: a crawler or a prefetch is not a
// failure, and an agent that gets a 4xx learns to stop declaring itself.
//
// The event is still emitted, with billable=false. These requests are real
// traffic and we want to know how much of it there is -- the volume of declared
// agent traffic is, on its own, one of the more interesting numbers this
// project can produce.
func (s *Server) serveUnbillable(w http.ResponseWriter, req *AdRequest, requestID string,
	t TrafficAssessment, now time.Time, start time.Time) {

	env := req.Env
	if env != "lab" {
		env = "production"
	}
	ev := map[string]any{
		"event": "ad_request", "event_id": newEventID(), "request_id": requestID,
		"placement_id": req.PlacementID, "session_id": req.SessionID,
		"game": req.Game, "country": req.Country, "device_type": req.DeviceType,
		"env": env, "decision_reason": "not_billable_traffic",
		"latency_ms": float64(time.Since(start).Microseconds()) / 1000.0,
		"ts":         now.UnixMilli(),
	}
	addTraffic(ev, t)
	s.sink.Emit(ev)

	resp := AdResponse{RequestID: requestID, NoAd: true}
	if s.lab {
		resp.Traffic = &t
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleDelivery answers the question pacing makes possible and nobody asks:
// will these line items actually spend their budgets today?
//
// It existed as a tested function that nothing called, which is its own kind of
// gap -- a projection nobody can see is a projection nobody acts on.
func (s *Server) handleDelivery(w http.ResponseWriter, r *http.Request) {
	if !s.lab {
		// Delivery state is commercially sensitive: it says which advertisers
		// are struggling to spend. Not something to expose in production
		// without an authenticated caller.
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	cp := s.cp()
	now := time.Now().UTC()

	out := make([]map[string]any, 0, len(cp.LineItems))
	for i := range cp.LineItems {
		li := &cp.LineItems[i]
		p := Project(li, s.budget.SpendToday(li.ID), now)
		row := map[string]any{
			"line_item_id": p.LineItemID,
			"pacing":       li.Pacing,
			"elapsed":      round4(p.Elapsed),
			"spend":        round4(p.Spend),
			"daily_budget": p.DailyBudget,
			"on_track":     p.OnTrack,
		}
		if math.IsNaN(p.Projected) {
			// Too early to extrapolate. Saying so is more useful than a number
			// that will be wrong in a direction nobody can predict.
			row["projected"] = nil
			row["note"] = "too early to project"
		} else {
			row["projected"] = round4(p.Projected)
			row["shortfall"] = round4(p.Shortfall)
		}
		if li.FrequencyCap.Impressions > 0 {
			// The interaction worth surfacing: a cap constrains opportunities
			// per person, a budget assumes enough people, and nothing checks
			// that those assumptions agree.
			row["frequency_cap"] = li.FrequencyCap.Impressions
			if !p.OnTrack {
				row["note"] = "under-delivering, and capped -- the cap may be the reason"
			}
		}
		out = append(out, row)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"as_of": now.Format(time.RFC3339),
		"note": "Linear extrapolation of today's rate. Traffic is not uniform " +
			"across the day, so this is wrong in a known direction and is still " +
			"enough to answer 'will this miss by a lot'.",
		"line_items": out,
	})
}

func round4(f float64) float64 { return float64(int(f*10000+0.5)) / 10000 }

// addTraffic records the traffic assessment on every event. "Why was this not
// billed?" is a question a buyer is entitled to ask months later, and it can
// only be answered from the log.
func addTraffic(ev map[string]any, t TrafficAssessment) {
	ev["traffic_class"] = string(t.Class)
	ev["billable"] = t.Billable
	if len(t.Reasons) > 0 {
		ev["traffic_reasons"] = t.Reasons
	}
	if t.AgentName != "" {
		ev["agent_name"] = t.AgentName
	}
}

// addExperiments stamps every arm this request was assigned to. Attribution is
// the entire value of an experiment: a variant that served but was not recorded
// is indistinguishable from one that never ran, and its revenue silently
// credits whichever arm the analyst assumes.
func addExperiments(ev map[string]any, as ...Assignment) {
	arms := make(map[string]string, len(as))
	holdout := false
	for _, a := range as {
		if a.VariantID == "" {
			continue
		}
		arms[a.Key] = a.VariantID
		if a.Holdout {
			holdout = true
		}
	}
	ev["experiments"] = arms
	ev["experiment_holdout"] = holdout
}

// --------------------------------------------------------------------------
// GET /event/impression  and  GET /event/click
//
// Both require a token this server signed. A hand-crafted URL cannot mint an
// event. The click destination is resolved from the creative record server-side
// -- it is never taken from a query parameter, so this is not an open redirect.
// --------------------------------------------------------------------------

var gif = []byte{
	0x47, 0x49, 0x46, 0x38, 0x39, 0x61, 0x01, 0x00, 0x01, 0x00, 0x80, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x21, 0xf9, 0x04, 0x01, 0x00, 0x00, 0x00,
	0x00, 0x2c, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00, 0x02, 0x02,
	0x44, 0x01, 0x00, 0x3b,
}

func (s *Server) verify(r *http.Request) (*TokenClaims, error) {
	tok := r.URL.Query().Get("token")
	if tok == "" {
		return nil, ErrBadToken
	}
	return VerifyToken(s.key, tok, time.Now().UTC())
}

func (s *Server) handleImpression(w http.ResponseWriter, r *http.Request) {
	c, err := s.verify(r)
	if err != nil {
		http.Error(w, "invalid token", http.StatusForbidden)
		return
	}
	now := time.Now().UTC()

	// Charge the serving budget at impression time, not at decision time: a
	// decision that never renders costs the advertiser nothing.
	cp := s.cp()
	var li *LineItem
	for i := range cp.LineItems {
		if cp.LineItems[i].ID == c.LineItemID {
			li = &cp.LineItems[i]
		}
	}
	spend := 0.0
	if li != nil && li.PricingModel == "CPM" {
		spend = li.Rate / 1000.0
		s.budget.AddSpend(li.ID, spend)
	}

	s.sink.Emit(map[string]any{
		"event": "ad_impression", "event_id": newEventID(), "request_id": c.RequestID,
		"line_item_id": c.LineItemID, "creative_id": c.CreativeID,
		"advertiser_spend": spend, "ts": now.UnixMilli(),
	})

	w.Header().Set("Content-Type", "image/gif")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	w.Write(gif)
}

func (s *Server) handleClick(w http.ResponseWriter, r *http.Request) {
	c, err := s.verify(r)
	if err != nil {
		http.Error(w, "invalid token", http.StatusForbidden)
		return
	}
	cr := s.cp().creative(c.CreativeID)
	if cr == nil {
		http.Error(w, "unknown creative", http.StatusForbidden)
		return
	}

	cp := s.cp()
	var li *LineItem
	for i := range cp.LineItems {
		if cp.LineItems[i].ID == c.LineItemID {
			li = &cp.LineItems[i]
		}
	}
	spend := 0.0
	if li != nil && li.PricingModel == "CPC" {
		spend = li.Rate
		s.budget.AddSpend(li.ID, spend)
	}

	s.sink.Emit(map[string]any{
		"event": "ad_click", "event_id": newEventID(), "request_id": c.RequestID,
		"line_item_id": c.LineItemID, "creative_id": c.CreativeID,
		"advertiser_spend": spend, "ts": time.Now().UTC().UnixMilli(),
	})

	// Destination comes from the creative record. Never from the request.
	http.Redirect(w, r, cr.ClickURL, http.StatusFound)
}

// --------------------------------------------------------------------------
// POST /collect  -- batched product analytics from the Publisher.
// --------------------------------------------------------------------------

type collectBody struct {
	SessionID string           `json:"session_id"`
	Env       string           `json:"env,omitempty"`
	Events    []map[string]any `json:"events"`
}

func (s *Server) handleCollect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var b collectBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10)).Decode(&b); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if len(b.Events) > 50 { // matches the client-side cap
		b.Events = b.Events[:50]
	}
	now := time.Now().UTC().UnixMilli()
	env := b.Env
	if env != "lab" {
		env = "production"
	}
	for _, e := range b.Events {
		e["env"] = env
		e["event_id"] = newEventID()
		e["session_id"] = b.SessionID
		e["received_ts"] = now
		s.sink.Emit(e)
	}
	w.WriteHeader(http.StatusNoContent)
}

// --------------------------------------------------------------------------

func envFloat(k string, def float64) float64 {
	if v := os.Getenv(k); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

func loadControlPlane(path string) (*ControlPlane, error) {
	f, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cp ControlPlane
	if err := json.Unmarshal(f, &cp); err != nil {
		return nil, err
	}
	if len(cp.LineItems) == 0 {
		return nil, errors.New("control plane has no line items")
	}
	// Experiment problems are refused at LOAD, not per request. A field
	// experiment targeting a protected field, or a split with a gap in its
	// bucket ranges, must never reach the serving path -- a silently biased
	// split is the failure mode that looks like a result.
	exp := Experiments{Set: cp.Experiments}
	problems := append(exp.Validate(), exp.ValidateFieldExperiments()...)
	if len(problems) > 0 {
		return nil, fmt.Errorf("control plane experiments are invalid:\n  %s",
			strings.Join(problems, "\n  "))
	}
	return &cp, nil
}

func (s *Server) routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/ad/request", s.handleAdRequest)
	mux.HandleFunc("/event/impression", s.handleImpression)
	mux.HandleFunc("/event/click", s.handleClick)
	mux.HandleFunc("/collect", s.handleCollect)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	// Lab only: delivery projections. Registered unconditionally but gated
	// inside, so the route table is the same in both environments and there is
	// one place to look for what exists.
	mux.HandleFunc("/delivery", s.handleDelivery)
	return mux
}

func main() {
	key := []byte(os.Getenv("ADLAB_TOKEN_KEY"))
	if len(key) == 0 {
		key = make([]byte, 32)
		rand.Read(key)
		log.Println("ADLAB_TOKEN_KEY unset: generated an ephemeral key (dev only)")
	}

	s := &Server{
		key:   key,
		lab:   strings.EqualFold(os.Getenv("ADLAB_ENV"), "lab"),
		floor: envFloat("ADLAB_FLOOR_CPM", 0.50),
		tmax:  int(envFloat("ADLAB_TMAX_MS", defaultTmaxMS)),
		rng:   mrand.New(mrand.NewSource(time.Now().UnixNano())),
	}

	// Storage is chosen at runtime, not by build tag, so the same binary runs
	// locally against a JSON fixture and in AWS against DynamoDB -- and the
	// decisioning code has no idea which it is talking to.
	if table := os.Getenv("ADLAB_CONTROL_PLANE_TABLE"); table != "" {
		cfg, err := config.LoadDefaultConfig(context.Background())
		if err != nil {
			log.Fatalf("aws config: %v", err)
		}
		ddb := dynamodb.NewFromConfig(cfg)

		provider := NewDynamoControlPlane(ddb, table, 60*time.Second)
		if _, err := provider.Get(context.Background()); err != nil {
			log.Fatalf("initial control plane load: %v", err)
		}
		s.cp = func() *ControlPlane {
			cp, err := provider.Get(context.Background())
			if err != nil {
				log.Printf("control plane refresh failed: %v", err)
				return &ControlPlane{}
			}
			return cp
		}
		state := os.Getenv("ADLAB_SERVING_STATE_TABLE")
		s.budget = NewDynamoBudget(ddb, state, 10*time.Second)
		// Frequency capping was wired ONLY in the local branch below, so this
		// was nil in Lambda and every cap silently did nothing. Two hours of
		// TTL: long enough for a session, short enough that rows expire rather
		// than accumulate.
		s.freq = NewDynamoFrequency(ddb, state, 2*time.Hour)
		// Firehose is unavailable on the AWS Free plan, so events go straight
		// to S3, batched per invocation. See sink_s3.go.
		sink := NewS3EventSink(s3.NewFromConfig(cfg), os.Getenv("ADLAB_EVENTS_BUCKET"))
		s.sink = sink
		s.flush = func() { sink.Flush(context.Background()) }
		log.Printf("control plane from DynamoDB table %s", table)
	} else {
		path := os.Getenv("ADLAB_CONTROL_PLANE")
		if path == "" {
			path = "fixtures/control-plane.json"
		}
		static, err := loadControlPlane(path)
		if err != nil {
			log.Fatalf("control plane: %v", err)
		}
		s.cp = func() *ControlPlane { return static }
		s.budget = NewMemoryBudget()
		// A session-scoped counter has no value once the session ends, so rows
		// expire rather than accumulate. That is a cost property and a data
		// minimisation property at once -- two reasons pointing the same way.
		s.freq = NewMemoryFrequency(2 * time.Hour)
		s.sink = &StdoutSink{}
	}

	serve(s)
}
