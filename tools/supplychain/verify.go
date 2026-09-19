package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"strings"
)

// ---------------------------------------------------------------------------
// Supply chain verification, done the way a buyer does it.
//
// Three artefacts, published by two different parties, that only mean something
// together:
//
//   schain        in the bid request. "This inventory reached you via these
//                 nodes, in this order."
//   sellers.json  published by an ADVERTISING SYSTEM at its own domain. "These
//                 are my sellers, and here is what each one is."
//   ads.txt       published by a PUBLISHER at its own domain. "These systems
//                 may sell my inventory."
//
// The join is the point, and it runs in one direction:
//
//   1. take a schain node: asi = an advertising system, sid = a seller there
//   2. fetch https://<asi>/sellers.json and find sid
//   3. read that seller's domain -- the publisher it sells for
//   4. fetch https://<domain>/ads.txt and check it authorises (asi, sid)
//
// Any one artefact alone proves nothing. An advertising system can claim any
// seller it likes; a publisher can list any system it likes. The chain is
// trustworthy only where BOTH sides independently say the same thing.
// ---------------------------------------------------------------------------

type SChainNode struct {
	ASI string `json:"asi"`
	SID string `json:"sid"`
	HP  int    `json:"hp"`
}

type SChain struct {
	Complete int          `json:"complete"`
	Ver      string       `json:"ver"`
	Nodes    []SChainNode `json:"nodes"`
}

type Seller struct {
	SellerID       string `json:"seller_id"`
	Name           string `json:"name"`
	Domain         string `json:"domain"`
	SellerType     string `json:"seller_type"`
	IsConfidential int    `json:"is_confidential"`
}

type SellersJSON struct {
	Version string   `json:"version"`
	Sellers []Seller `json:"sellers"`
}

// AdsTxtEntry is one authorisation record.
type AdsTxtEntry struct {
	Domain        string
	SellerAccount string
	Relationship  string // DIRECT | RESELLER
	CertID        string
}

// ParseAdsTxt is deliberately lenient about whitespace and strict about shape.
// Real ads.txt files in the wild are full of stray spaces and trailing commas,
// and a parser that rejects them reports "unauthorised" for inventory that is
// perfectly authorised -- which is a worse failure than being permissive.
func ParseAdsTxt(body string) ([]AdsTxtEntry, []string) {
	var out []AdsTxtEntry
	var problems []string

	sc := bufio.NewScanner(strings.NewReader(body))
	line := 0
	for sc.Scan() {
		line++
		t := strings.TrimSpace(sc.Text())
		if i := strings.Index(t, "#"); i >= 0 {
			t = strings.TrimSpace(t[:i])
		}
		if t == "" {
			continue
		}
		// Variable records (CONTACT=, OWNERDOMAIN=, SUBDOMAIN=) are not
		// authorisations and must not be parsed as one.
		if strings.Contains(t, "=") && !strings.Contains(t, ",") {
			continue
		}
		parts := strings.Split(t, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		if len(parts) < 3 {
			problems = append(problems,
				fmt.Sprintf("line %d: expected at least 3 fields, got %d: %q", line, len(parts), t))
			continue
		}
		rel := strings.ToUpper(parts[2])
		if rel != "DIRECT" && rel != "RESELLER" {
			problems = append(problems,
				fmt.Sprintf("line %d: relationship %q is neither DIRECT nor RESELLER", line, parts[2]))
			continue
		}
		e := AdsTxtEntry{Domain: strings.ToLower(parts[0]), SellerAccount: parts[1], Relationship: rel}
		if len(parts) > 3 {
			e.CertID = parts[3]
		}
		out = append(out, e)
	}
	return out, problems
}

func ParseSellersJSON(body []byte) (*SellersJSON, error) {
	var s SellersJSON
	if err := json.Unmarshal(body, &s); err != nil {
		return nil, fmt.Errorf("sellers.json: %w", err)
	}
	return &s, nil
}

// Finding is one verification result. Severity matters: a chain that FAILS is
// unsellable to a buyer that checks, while a WARN is a smell.
type Finding struct {
	Level  string // OK | WARN | FAIL
	Check  string
	Detail string
}

// Fetcher lets the verifier be tested without a network.
type Fetcher func(url string) ([]byte, error)

// Verify walks the chain for every node and reports what a buyer would conclude.
func Verify(chain SChain, fetch Fetcher) []Finding {
	var out []Finding

	if len(chain.Nodes) == 0 {
		return append(out, Finding{"FAIL", "schain.nodes", "no nodes: the chain says nothing"})
	}
	if chain.Ver == "" {
		out = append(out, Finding{"WARN", "schain.ver", "no version; 1.0 is expected"})
	}
	if chain.Complete != 1 {
		// complete=0 is legal and honest -- it says "there are nodes I cannot
		// see". Many buyers refuse it outright, which is the point of saying so.
		out = append(out, Finding{"WARN", "schain.complete",
			"complete=0: the seller states the chain is partial. Many buyers refuse this inventory"})
	}

	for i, n := range chain.Nodes {
		label := fmt.Sprintf("node[%d] %s/%s", i, n.ASI, n.SID)

		if n.ASI == "" || n.SID == "" {
			out = append(out, Finding{"FAIL", label, "asi and sid are both required"})
			continue
		}
		if n.HP != 1 {
			// hp=1 means this node is paid. The field is deprecated in newer
			// drafts but still widely required, and omitting it makes some
			// buyers drop the request.
			out = append(out, Finding{"WARN", label, "hp is not 1"})
		}

		// --- step 1: the advertising system's sellers.json ---
		sellersURL := "https://" + n.ASI + "/sellers.json"
		raw, err := fetch(sellersURL)
		if err != nil {
			out = append(out, Finding{"FAIL", label,
				"cannot fetch " + sellersURL + ": " + err.Error()})
			continue
		}
		sj, err := ParseSellersJSON(raw)
		if err != nil {
			out = append(out, Finding{"FAIL", label, err.Error()})
			continue
		}

		var seller *Seller
		for j := range sj.Sellers {
			if sj.Sellers[j].SellerID == n.SID {
				seller = &sj.Sellers[j]
				break
			}
		}
		if seller == nil {
			out = append(out, Finding{"FAIL", label,
				"sid not found in " + sellersURL + ": the advertising system does not claim this seller"})
			continue
		}
		out = append(out, Finding{"OK", label,
			fmt.Sprintf("found in sellers.json as %q (%s)", seller.Name, seller.SellerType)})

		if seller.IsConfidential == 1 {
			// Confidential sellers legitimately omit the domain, and the chain
			// simply cannot be verified further. That is a known, accepted hole.
			out = append(out, Finding{"WARN", label,
				"seller is confidential: no domain to verify against, chain ends here"})
			continue
		}
		if seller.Domain == "" {
			out = append(out, Finding{"FAIL", label,
				"seller is not confidential but declares no domain"})
			continue
		}

		// --- step 2: the publisher's ads.txt ---
		adsURL := "https://" + seller.Domain + "/ads.txt"
		raw, err = fetch(adsURL)
		if err != nil {
			out = append(out, Finding{"FAIL", label,
				"cannot fetch " + adsURL + ": " + err.Error()})
			continue
		}
		entries, problems := ParseAdsTxt(string(raw))
		for _, p := range problems {
			out = append(out, Finding{"WARN", label, "ads.txt " + p})
		}

		authorised := false
		placeholderOnly := len(entries) > 0
		for _, e := range entries {
			if e.Domain != "placeholder.example.com" {
				placeholderOnly = false
			}
			if e.Domain == strings.ToLower(n.ASI) && e.SellerAccount == n.SID {
				authorised = true
				out = append(out, Finding{"OK", label,
					fmt.Sprintf("authorised in %s as %s", adsURL, e.Relationship)})
				if e.Relationship == "RESELLER" && seller.SellerType == "PUBLISHER" {
					out = append(out, Finding{"WARN", label,
						"ads.txt says RESELLER but sellers.json says PUBLISHER: the two sides disagree about what this seller is"})
				}
				break
			}
		}
		if placeholderOnly {
			out = append(out, Finding{"FAIL", label,
				"ads.txt contains only the spec placeholder: the publisher declares NO authorised sellers"})
			continue
		}
		if !authorised {
			out = append(out, Finding{"FAIL", label,
				fmt.Sprintf("%s does not authorise %s/%s: unauthorised inventory", adsURL, n.ASI, n.SID)})
		}
	}
	return out
}

// Worst returns the highest severity present, which is what a buyer acts on.
func Worst(fs []Finding) string {
	worst := "OK"
	for _, f := range fs {
		if f.Level == "FAIL" {
			return "FAIL"
		}
		if f.Level == "WARN" {
			worst = "WARN"
		}
	}
	return worst
}
