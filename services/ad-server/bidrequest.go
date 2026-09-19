package main

import (
	"sort"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// Bid request shaping, as an experiment.
//
// Every SSP maintains a per-DSP configuration: which OpenRTB fields to send,
// which extensions, which version. It is accumulated tribal knowledge -- encoded
// in configs, half-remembered by two people, never revalidated after the DSP
// changed its parser. Nobody knows which settings still earn their place,
// because nobody ever tested one in isolation.
//
// The reframing: this is not configuration, it is an experiment. "Send
// device.ua" versus "omit it" is an A/B test with a revenue outcome, so it needs
// no new machinery -- it is ADR 0006 applied to the bid request instead of the
// floor.
//
// Assignment is by PLACEMENT, not session. A DSP learns a supply path's shape
// over days; splitting per session teaches it an average that exists nowhere.
// ---------------------------------------------------------------------------

// OptionalField is a field whose inclusion may be experimented on.
type OptionalField struct {
	Name string
	// Path is where it sits in an OpenRTB 2.6 request, for the log and for
	// humans reading a proposal.
	Path string
	// Why records what the field is for, so a proposal to drop it is legible.
	Why string
}

// optionalFields is the experimentable surface. Deliberately small: with n
// fields there are 2^n combinations, and at realistic traffic only a handful of
// arms resolve at once. One field at a time, prioritised by suspected impact.
var optionalFields = []OptionalField{
	{"device_ua", "device.ua", "user agent; many buyers key device models off it"},
	{"device_ip", "device.ip", "coarse geo and fraud signals"},
	{"user_eids", "user.eids", "alternative identity; often the single biggest bid-rate lever"},
	{"site_keywords", "site.keywords", "contextual signal"},
	{"imp_bidfloor", "imp.bidfloor", "whether we disclose our floor at all"},
	{"device_geo_lat", "device.geo.lat/lon", "precise location"},
	{"site_content", "site.content", "content taxonomy for contextual targeting"},
	{"user_data", "user.data", "seller-defined audiences"},
}

// protectedFields may NEVER be experimented on or omitted.
//
// This is a hard-coded refusal rather than configuration, for the same reason
// age assurance sits outside the agent layer: an agent that can experiment on
// schain is an agent that will eventually discover that lying pays. Removing
// supply-chain transparency to see whether bid rate improves is fraud-adjacent
// whatever the result, and a consent signal is not an optimisation surface.
var protectedFields = map[string]string{
	"source_schain":   "supply chain transparency (source.schain / source.ext.schain)",
	"regs_gdpr":       "GDPR applicability flag (regs.gdpr)",
	"regs_gpp":        "Global Privacy Platform string (regs.gpp)",
	"regs_us_privacy": "US privacy string (regs.ext.us_privacy)",
	"regs_coppa":      "COPPA flag (regs.coppa)",
	"user_consent":    "TCF consent string (user.ext.consent)",
	"imp_secure":      "secure flag; misreporting it misrepresents the inventory",
}

// IsProtected reports whether a field may never be experimented on. Accepts
// either the bare name or the "field.<name>" experiment key.
func IsProtected(name string) (string, bool) {
	name = strings.TrimPrefix(name, fieldExpPrefix)
	why, ok := protectedFields[name]
	return why, ok
}

const fieldExpPrefix = "field."

// FieldExpKey is the experiment key for one optional field.
func FieldExpKey(field string) string { return fieldExpPrefix + field }

// FieldSet is the set of optional fields included in one bid request. Protected
// fields are not represented here at all: they are always sent, and there is no
// code path that omits them.
type FieldSet map[string]bool

// Sorted returns the included fields, for logging and attribution.
func (f FieldSet) Sorted() []string {
	out := make([]string, 0, len(f))
	for k, on := range f {
		if on {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// ResolveFields decides which optional fields this request carries, per buyer.
//
// A field with no experiment defined is INCLUDED. That default matters: the
// experiment layer exists to test removing things we already send, and a
// missing or malformed experiment must never silently strip a field a buyer
// depends on.
func ResolveFields(exp Experiments, req AdRequest, requestID string) (FieldSet, []Assignment) {
	fs := make(FieldSet, len(optionalFields))
	var assigns []Assignment

	for _, f := range optionalFields {
		key := FieldExpKey(f.Name)
		if !exp.has(key) {
			fs[f.Name] = true // no experiment: send it
			continue
		}
		v, a := exp.Assign(key, req, requestID)
		// "omit" is the only value that removes a field. Anything else -- a
		// typo, an empty param, a variant that forgot to set it -- includes it.
		// Failing towards sending is the safe direction: an omitted field can
		// silently cost bid rate for days, and a redundant one costs bytes.
		fs[f.Name] = v.Params["mode"] != "omit"
		assigns = append(assigns, a)
	}
	return fs, assigns
}

// has reports whether an experiment key is defined at all.
func (e Experiments) has(key string) bool {
	for _, x := range e.Set {
		if x.Key == key {
			return true
		}
	}
	return false
}

// ValidateFieldExperiments refuses any experiment that targets a protected
// field, or that names a field we do not know how to send.
//
// Run at load time alongside Experiments.Validate, so a bad control-plane edit
// is refused rather than serving.
func (e Experiments) ValidateFieldExperiments() []string {
	known := map[string]bool{}
	for _, f := range optionalFields {
		known[f.Name] = true
	}

	var problems []string
	for _, x := range e.Set {
		if !strings.HasPrefix(x.Key, fieldExpPrefix) {
			continue
		}
		field := strings.TrimPrefix(x.Key, fieldExpPrefix)
		if why, protected := IsProtected(field); protected {
			problems = append(problems, x.Key+
				": REFUSED -- "+field+" is protected ("+why+
				") and may never be experimented on")
			continue
		}
		if !known[field] {
			problems = append(problems, x.Key+
				": names field "+strconv.Quote(field)+", which is not in optionalFields")
		}
	}
	return problems
}
