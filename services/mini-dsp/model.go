package main

import "time"

// ---------------------------------------------------------------------------
// A buyer, as a separate process.
//
// Until now our "buyers" were a loop inside the ad server: same memory, same
// clock, same truth. That taught the auction and hid everything that makes
// buying hard. Across a real network boundary, four things become true that
// were not true before:
//
//   1. The seller's tmax is a DEADLINE, not a parameter. If we answer after it,
//      we did the work, spent the money to do it, and got nothing.
//   2. We learn we won ASYNCHRONOUSLY, from a notification that can be lost.
//   3. We have our OWN budget and our OWN clock, and they disagree with the
//      seller's.
//   4. Our numbers will not match theirs. That disagreement is not a bug to be
//      fixed; it is a permanent feature of the industry, and understanding its
//      sources is most of what this service exists to teach.
// ---------------------------------------------------------------------------

// BidRequest is the subset of OpenRTB 2.6 we exchange. Deliberately small: the
// full object is enormous and almost none of it changes the lesson.
type BidRequest struct {
	ID     string    `json:"id"`
	Imp    []Imp     `json:"imp"`
	Site   *Site     `json:"site,omitempty"`
	Device *Device   `json:"device,omitempty"`
	User   *User     `json:"user,omitempty"`
	TMax   int       `json:"tmax"`
	Source *Source   `json:"source,omitempty"`
	Regs   *Regs     `json:"regs,omitempty"`
	At     time.Time `json:"-"`
}

type Imp struct {
	ID       string  `json:"id"`
	BidFloor float64 `json:"bidfloor"`
	Banner   *Banner `json:"banner,omitempty"`
	Secure   int     `json:"secure"`
}

type Banner struct {
	W int `json:"w"`
	H int `json:"h"`
}

type Site struct {
	ID     string `json:"id"`
	Domain string `json:"domain"`
	Page   string `json:"page,omitempty"`
	// Content carries the seller-defined audience taxonomy, which is the
	// privacy-safe way to describe an audience -- no identifier involved.
	Keywords string `json:"keywords,omitempty"`
}

type Device struct {
	UA         string `json:"ua,omitempty"`
	IP         string `json:"ip,omitempty"`
	Geo        *Geo   `json:"geo,omitempty"`
	DeviceType int    `json:"devicetype,omitempty"`
}

type Geo struct {
	Country string `json:"country,omitempty"`
}

type User struct {
	ID   string `json:"id,omitempty"`
	Data []Data `json:"data,omitempty"`
}

type Data struct {
	Name    string    `json:"name"`
	Segment []Segment `json:"segment"`
}

type Segment struct {
	ID string `json:"id"`
}

type Source struct {
	SChain *SChain `json:"schain,omitempty"`
}

type SChain struct {
	Complete int          `json:"complete"`
	Nodes    []SChainNode `json:"nodes"`
}

type SChainNode struct {
	ASI string `json:"asi"`
	SID string `json:"sid"`
	HP  int    `json:"hp"`
}

type Regs struct {
	GDPR int    `json:"gdpr,omitempty"`
	GPP  string `json:"gpp,omitempty"`
}

// BidResponse is what we send back. A no-bid is HTTP 204 with no body, which is
// the OpenRTB convention and meaningfully cheaper than a JSON object saying
// "no" at the volumes this runs at in reality.
type BidResponse struct {
	ID      string    `json:"id"`
	SeatBid []SeatBid `json:"seatbid"`
	Cur     string    `json:"cur"`
}

type SeatBid struct {
	Seat string `json:"seat"`
	Bid  []Bid  `json:"bid"`
}

type Bid struct {
	ID    string  `json:"id"`
	ImpID string  `json:"impid"`
	Price float64 `json:"price"`
	AdM   string  `json:"adm,omitempty"`
	CID   string  `json:"cid,omitempty"`
	CrID  string  `json:"crid,omitempty"`
	// NURL is the win notice. The seller calls it when we win -- and this is
	// where the discrepancy starts, because that call can be lost.
	NURL string `json:"nurl,omitempty"`
}

// Campaign is what this buyer is trying to spend.
type Campaign struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	MaxCPM       float64  `json:"max_cpm"`
	DailyBudget  float64  `json:"daily_budget"`
	Countries    []string `json:"countries"`
	Sizes        []string `json:"sizes"` // "300x250"
	CreativeHTML string   `json:"creative_html"`
	// FreqCapPerUser is buyer-side capping. The SELLER also caps, on its own
	// terms, and neither knows what the other is doing -- so the effective cap
	// a person experiences is the tighter of two caps nobody computed together.
	FreqCapPerUser int `json:"freq_cap_per_user"`
}
