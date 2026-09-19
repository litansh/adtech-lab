package main

import (
	"fmt"
	"strings"
	"testing"
)

func fetcher(files map[string]string) Fetcher {
	return func(url string) ([]byte, error) {
		if v, ok := files[url]; ok {
			return []byte(v), nil
		}
		return nil, fmt.Errorf("404")
	}
}

const goodSellers = `{"version":"1.0","sellers":[
  {"seller_id":"xoxoxo-1","name":"The Game Room","domain":"xoxoxo.live","seller_type":"PUBLISHER"}]}`
const goodAds = "# comment\nxoxoxo.live, xoxoxo-1, DIRECT\nCONTACT=a@b.c\n"

func goodChain() SChain {
	return SChain{Complete: 1, Ver: "1.0",
		Nodes: []SChainNode{{ASI: "xoxoxo.live", SID: "xoxoxo-1", HP: 1}}}
}

func goodFiles() map[string]string {
	return map[string]string{
		"https://xoxoxo.live/sellers.json": goodSellers,
		"https://xoxoxo.live/ads.txt":      goodAds,
	}
}

func hasDetail(fs []Finding, level, substr string) bool {
	for _, f := range fs {
		if f.Level == level && strings.Contains(f.Detail, substr) {
			return true
		}
	}
	return false
}

func TestAValidChainVerifies(t *testing.T) {
	fs := Verify(goodChain(), fetcher(goodFiles()))
	if w := Worst(fs); w != "OK" {
		t.Fatalf("valid chain reported %s: %+v", w, fs)
	}
}

// The bug this caught in our own ad server: the schain carried the SITE id
// where the SELLER id belongs. The chain is well-formed and fails verification.
func TestSiteIdInsteadOfSellerIdFails(t *testing.T) {
	c := goodChain()
	c.Nodes[0].SID = "site-xoxoxo" // what we sent before Phase 5
	fs := Verify(c, fetcher(goodFiles()))
	if Worst(fs) != "FAIL" {
		t.Fatalf("a sid absent from sellers.json passed: %+v", fs)
	}
	if !hasDetail(fs, "FAIL", "does not claim this seller") {
		t.Errorf("unhelpful failure: %+v", fs)
	}
}

// The placeholder record is correct when there are no sellers, and it means
// exactly one thing to a buyer: nothing here is authorised.
func TestPlaceholderOnlyAdsTxtIsUnauthorised(t *testing.T) {
	files := goodFiles()
	files["https://xoxoxo.live/ads.txt"] = "placeholder.example.com, placeholder, DIRECT, placeholder\n"
	fs := Verify(goodChain(), fetcher(files))
	if Worst(fs) != "FAIL" {
		t.Fatalf("placeholder-only ads.txt passed: %+v", fs)
	}
	if !hasDetail(fs, "FAIL", "NO authorised sellers") {
		t.Errorf("the failure should name the placeholder: %+v", fs)
	}
}

func TestUnauthorisedInAdsTxtFails(t *testing.T) {
	files := goodFiles()
	files["https://xoxoxo.live/ads.txt"] = "someoneelse.com, 999, DIRECT\n"
	fs := Verify(goodChain(), fetcher(files))
	if !hasDetail(fs, "FAIL", "unauthorised inventory") {
		t.Fatalf("an unlisted seller passed: %+v", fs)
	}
}

func TestMissingFilesFailRatherThanPass(t *testing.T) {
	fs := Verify(goodChain(), fetcher(map[string]string{}))
	if Worst(fs) != "FAIL" {
		t.Fatalf("a missing sellers.json passed: %+v", fs)
	}
	files := goodFiles()
	delete(files, "https://xoxoxo.live/ads.txt")
	if Worst(Verify(goodChain(), fetcher(files))) != "FAIL" {
		t.Fatal("a missing ads.txt passed")
	}
}

// complete=0 is legal and honest. It is also refused by many buyers, so it
// warns rather than passing silently.
func TestIncompleteChainWarns(t *testing.T) {
	c := goodChain()
	c.Complete = 0
	fs := Verify(c, fetcher(goodFiles()))
	if !hasDetail(fs, "WARN", "complete=0") {
		t.Fatalf("complete=0 did not warn: %+v", fs)
	}
	if Worst(fs) == "FAIL" {
		t.Error("complete=0 is legal; it must not FAIL")
	}
}

// The two sides disagreeing about what a seller IS is a real smell.
func TestRelationshipMismatchWarns(t *testing.T) {
	files := goodFiles()
	files["https://xoxoxo.live/ads.txt"] = "xoxoxo.live, xoxoxo-1, RESELLER\n"
	fs := Verify(goodChain(), fetcher(files))
	if !hasDetail(fs, "WARN", "disagree about what this seller is") {
		t.Fatalf("PUBLISHER vs RESELLER mismatch not flagged: %+v", fs)
	}
}

// A confidential seller legitimately has no domain. The chain simply ends --
// a known, accepted hole rather than a failure.
func TestConfidentialSellerEndsTheChainWithoutFailing(t *testing.T) {
	files := goodFiles()
	files["https://xoxoxo.live/sellers.json"] = `{"version":"1.0","sellers":[
	  {"seller_id":"xoxoxo-1","name":"n","seller_type":"INTERMEDIARY","is_confidential":1}]}`
	fs := Verify(goodChain(), fetcher(files))
	if Worst(fs) == "FAIL" {
		t.Fatalf("a confidential seller was treated as a failure: %+v", fs)
	}
	if !hasDetail(fs, "WARN", "chain ends here") {
		t.Errorf("the chain ending was not reported: %+v", fs)
	}
}

// Real ads.txt files are full of stray whitespace and comments. A parser that
// rejects them reports "unauthorised" for perfectly authorised inventory --
// a worse failure than being permissive.
func TestAdsTxtParsingIsLenientAboutFormatting(t *testing.T) {
	body := "  \n# header\n  xoxoxo.live ,  xoxoxo-1 , direct  # trailing\n\nOWNERDOMAIN=xoxoxo.live\n"
	entries, problems := ParseAdsTxt(body)
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1: %+v (problems %v)", len(entries), entries, problems)
	}
	if entries[0].Relationship != "DIRECT" {
		t.Errorf("lowercase 'direct' not normalised: %q", entries[0].Relationship)
	}
	if len(problems) != 0 {
		t.Errorf("well-formed-but-messy file reported problems: %v", problems)
	}
}

// A variable record is not an authorisation and must not be parsed as one.
func TestVariableRecordsAreNotAuthorisations(t *testing.T) {
	entries, _ := ParseAdsTxt("CONTACT=a@b.c\nSUBDOMAIN=sub.example.com\n")
	if len(entries) != 0 {
		t.Fatalf("variable records parsed as authorisations: %+v", entries)
	}
}

func TestMalformedLinesAreReportedNotSilentlyDropped(t *testing.T) {
	_, problems := ParseAdsTxt("only.two, fields\nfoo.com, 1, SOMETHINGELSE\n")
	if len(problems) != 2 {
		t.Fatalf("got %d problems, want 2: %v", len(problems), problems)
	}
}

func TestEmptyChainFails(t *testing.T) {
	if Worst(Verify(SChain{Complete: 1, Ver: "1.0"}, fetcher(goodFiles()))) != "FAIL" {
		t.Fatal("an empty chain passed")
	}
}
