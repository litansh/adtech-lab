package main

import (
	"net"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Traffic quality, and why the obvious implementation is wrong.
//
// The reflex is "detect bots, block bots". In the agentic era that reflex
// destroys revenue and breaks the buyer relationship, because it collapses two
// completely different populations into one bucket:
//
//   DECLARED AUTOMATION  an agent acting for a real person -- a shopping
//                        assistant, a comparison agent, an accessibility tool.
//                        There is a human intent behind it and, increasingly,
//                        a human wallet. It is not fraud. It IS a different
//                        product: display advertising against it is close to
//                        worthless, because nothing is being displayed to
//                        anyone, while the underlying intent may be extremely
//                        valuable to the right buyer.
//
//   UNDECLARED NON-HUMAN traffic pretending to be a human viewer in order to
//                        be paid for as one. That is IVT, and the industry
//                        term for paying for it is "fraud".
//
// The distinction is DECLARATION, not automation. A crawler that identifies
// itself and obeys robots.txt is a good citizen. A headless browser spoofing
// an iPhone user agent from a datacentre is not, and the difference between
// them is honesty rather than technology.
//
// So this classifier has three outcomes, not two:
//
//   human           -> full auction, billable
//   declared_agent  -> served, logged, and EXCLUDED from billable impressions
//   suspected_ivt   -> house ad only; no buyer is ever charged
//
// The third rule is the one with teeth. We do not block suspected IVT -- we
// serve it something unpaid. Blocking teaches the operator what our detection
// looks like; serving a house ad does not, and costs us nothing because there
// was never any revenue in it.
//
// Honest limitations, recorded rather than glossed:
//   - Every signal here is heuristic. There is no ground truth in this dataset,
//     so precision and recall are unmeasured. Numbers below are thresholds, not
//     accuracies.
//   - IP-based datacentre detection uses a tiny static list. A real
//     implementation uses a maintained feed; ours will miss most of it.
//   - This runs pre-auction, on request signals only. Post-impression signals
//     (viewability, time-on-page, click timing) are strictly better evidence
//     and belong in the cold path, which can revoke billability after the fact.
// ---------------------------------------------------------------------------

type TrafficClass string

const (
	ClassHuman         TrafficClass = "human"
	ClassDeclaredAgent TrafficClass = "declared_agent"
	ClassSuspectedIVT  TrafficClass = "suspected_ivt"
)

// TrafficAssessment is attached to every request and written to the event log.
// Reasons are enumerated so they can be counted over time -- an unexplained
// classification is not auditable, and "why was this not billable?" is a
// question a buyer is entitled to ask.
type TrafficAssessment struct {
	Class     TrafficClass `json:"class"`
	Billable  bool         `json:"billable"`
	Reasons   []string     `json:"reasons,omitempty"`
	AgentName string       `json:"agent_name,omitempty"`
}

const (
	ReasonDeclaredAgentHeader = "declared_agent_header"
	ReasonKnownCrawler        = "known_crawler_ua"
	ReasonHeadlessUA          = "headless_user_agent"
	ReasonDatacenterIP        = "datacenter_ip"
	ReasonNoUserAgent         = "missing_user_agent"
	ReasonImpossibleViewport  = "impossible_viewport"
	ReasonPrerender           = "prerender_or_prefetch"
	ReasonSessionRate         = "session_request_rate"
	ReasonUAMismatch          = "ua_platform_mismatch"
)

// RequestSignals is everything the classifier is allowed to see. Deliberately a
// struct rather than *http.Request: the classifier must be testable without a
// server, and must not be able to reach for something it has not declared.
type RequestSignals struct {
	UserAgent  string
	IP         string
	SecPurpose string // "prefetch" / "prerender" -- Sec-Purpose, RFC 9110 style
	// AgentDeclaration carries a self-identifying automation header. The IAB's
	// Agentic RTB work and the emerging Web-Bot-Auth drafts both converge on
	// "declare yourself"; we accept the declaration without verifying it,
	// because an honest declaration is the only kind we can act on anyway.
	AgentDeclaration string
	Width, Height    int
	DeviceType       string
	SessionRequests  int           // requests already seen for this session
	SessionAge       time.Duration // since the session's first request
}

// knownCrawlerTokens are substrings of user agents that identify themselves.
// Lowercase; matched as substrings.
var knownCrawlerTokens = []string{
	"googlebot", "bingbot", "duckduckbot", "baiduspider", "yandexbot",
	"applebot", "slurp", "ia_archiver", "ahrefsbot", "semrushbot",
	"gptbot", "oai-searchbot", "chatgpt-user", "claudebot", "claude-web",
	"perplexitybot", "ccbot", "google-extended", "bytespider", "amazonbot",
	"crawler", "spider", "scraper", "bot/",
}

// headlessTokens indicate automation that is NOT declaring itself as an agent.
// A headless browser has no reason to render an ad to anybody.
var headlessTokens = []string{
	"headlesschrome", "phantomjs", "electron/", "puppeteer", "playwright",
	"selenium", "webdriver", "python-requests", "curl/", "wget/", "go-http-client",
	"java/", "okhttp", "libwww-perl", "scrapy",
}

// datacenterCIDRs is a token list, not a solution. Real detection needs a
// maintained feed; this catches the laziest cases and is honest about the rest.
var datacenterCIDRs = []string{
	"3.0.0.0/8", "13.32.0.0/12", "15.177.0.0/16", "18.32.0.0/11", "35.180.0.0/14",
	"52.0.0.0/8", "54.0.0.0/8", "34.64.0.0/10", "35.184.0.0/13",
	"104.196.0.0/14", "130.211.0.0/16", "146.148.0.0/17",
	"20.33.0.0/16", "40.64.0.0/10", "168.62.0.0/15",
	"159.65.0.0/16", "165.227.0.0/16", "167.71.0.0/16", "134.209.0.0/16",
	"45.55.0.0/16", "128.199.0.0/16", "139.59.0.0/16",
}

var datacenterNets = func() []*net.IPNet {
	out := make([]*net.IPNet, 0, len(datacenterCIDRs))
	for _, c := range datacenterCIDRs {
		if _, n, err := net.ParseCIDR(c); err == nil {
			out = append(out, n)
		}
	}
	return out
}()

func isDatacenterIP(s string) bool {
	ip := net.ParseIP(strings.TrimSpace(s))
	if ip == nil {
		return false
	}
	for _, n := range datacenterNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// maxSessionRate is the rate above which a "session" stops being plausible as
// one person playing a game. A human finishing a fast game of 2048 might
// generate a handful of ad requests a minute; sixty is not a person.
const maxSessionRate = 60.0

// ClassifyTraffic is pure: same signals in, same assessment out. Pure because
// this decision must be reproducible from the event log months later, when a
// buyer asks why a particular impression was not billed.
func ClassifyTraffic(s RequestSignals) TrafficAssessment {
	ua := strings.ToLower(s.UserAgent)
	var reasons []string

	// --- 1. Declared automation. Checked FIRST, before any suspicion, because
	// honesty must be the cheapest path. A declaration that got a request
	// treated as fraud is a declaration nobody makes twice. ---
	if d := strings.TrimSpace(s.AgentDeclaration); d != "" {
		return TrafficAssessment{
			Class: ClassDeclaredAgent, Billable: false,
			Reasons: []string{ReasonDeclaredAgentHeader}, AgentName: d,
		}
	}
	for _, tok := range knownCrawlerTokens {
		if strings.Contains(ua, tok) {
			return TrafficAssessment{
				Class: ClassDeclaredAgent, Billable: false,
				Reasons: []string{ReasonKnownCrawler}, AgentName: tok,
			}
		}
	}

	// --- 2. Undeclared automation. Each of these alone is enough. ---
	for _, tok := range headlessTokens {
		if strings.Contains(ua, tok) {
			reasons = append(reasons, ReasonHeadlessUA)
			break
		}
	}
	if ua == "" {
		reasons = append(reasons, ReasonNoUserAgent)
	}
	// A prefetch or prerender is a real browser acting for a real person, but
	// nothing has been shown to them yet. Charging for it is charging for a
	// maybe. This is a correctness rule, not a fraud rule.
	if p := strings.ToLower(s.SecPurpose); p == "prefetch" || p == "prerender" {
		reasons = append(reasons, ReasonPrerender)
	}
	if isDatacenterIP(s.IP) {
		reasons = append(reasons, ReasonDatacenterIP)
	}
	// Only IMPLAUSIBLE dimensions count. Absent ones do not: the placement
	// defines the slot size, so a client that omits w/h is normal, not
	// suspicious. Treating "missing" as "impossible" would have marked most
	// legitimate traffic as fraud -- which is exactly what it did the first
	// time this shipped, and what the existing handler tests caught.
	if s.Width < 0 || s.Height < 0 || s.Width > 8000 || s.Height > 8000 {
		reasons = append(reasons, ReasonImpossibleViewport)
	}
	// A desktop user agent claiming to be a mobile device, or the reverse, is
	// a spoof rather than a configuration.
	if s.DeviceType == "mobile" && strings.Contains(ua, "windows nt") {
		reasons = append(reasons, ReasonUAMismatch)
	}
	// Request rate. Requires a session old enough to have a meaningful rate --
	// two requests in the first second is a page loading, not an attack.
	if s.SessionAge >= 10*time.Second && s.SessionRequests > 5 {
		if perMin := float64(s.SessionRequests) / s.SessionAge.Minutes(); perMin > maxSessionRate {
			reasons = append(reasons, ReasonSessionRate)
		}
	}

	if len(reasons) > 0 {
		return TrafficAssessment{Class: ClassSuspectedIVT, Billable: false, Reasons: reasons}
	}
	return TrafficAssessment{Class: ClassHuman, Billable: true}
}
