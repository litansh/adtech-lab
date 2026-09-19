package main

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	ddbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// ---------------------------------------------------------------------------
// Control plane, backed by DynamoDB and cached in Lambda memory.
//
// This is the trick every real ad server uses. A per-request database lookup
// does not fit a p99 latency budget measured in tens of milliseconds, so the
// whole configured world is loaded once and refreshed on a TTL. GAM and TTD
// push config snapshots to their serving nodes for exactly this reason.
//
// The economic consequence is just as important as the latency one: the hot
// path issues ~zero DynamoDB reads, so reads cost effectively nothing no matter
// how much traffic arrives. See docs/cost-model.md.
// ---------------------------------------------------------------------------

type DynamoControlPlane struct {
	client *dynamodb.Client
	table  string
	ttl    time.Duration

	mu       sync.RWMutex
	cached   *ControlPlane
	loadedAt time.Time
}

func NewDynamoControlPlane(c *dynamodb.Client, table string, ttl time.Duration) *DynamoControlPlane {
	return &DynamoControlPlane{client: c, table: table, ttl: ttl}
}

// Get returns the cached control plane, refreshing it if the TTL has passed.
// On a refresh error the previous snapshot is kept and served: stale config is
// very much better than serving no ads at all.
func (d *DynamoControlPlane) Get(ctx context.Context) (*ControlPlane, error) {
	d.mu.RLock()
	fresh := d.cached != nil && time.Since(d.loadedAt) < d.ttl
	cp := d.cached
	d.mu.RUnlock()
	if fresh {
		return cp, nil
	}

	loaded, err := d.load(ctx)
	if err != nil {
		if cp != nil {
			return cp, nil // serve stale rather than nothing
		}
		return nil, err
	}

	d.mu.Lock()
	d.cached, d.loadedAt = loaded, time.Now()
	d.mu.Unlock()
	return loaded, nil
}

// load scans the whole table. Legitimate here and nowhere near a hot path: the
// configured world is a few hundred small items, read once per TTL per warm
// Lambda instance.
// applyItem routes one DynamoDB row into the control plane, and reports
// whether the kind was recognised.
//
// Extracted from load() so the set of handled kinds is testable. BUYER and
// EXPERIMENT were absent from this switch AND from adlabctl's seed list, so
// production could not run an auction at all -- and nothing failed, because
// "no buyer bid" and "no buyer was asked" produce the same empty auction.
func applyItem(cp *ControlPlane, kind string, item map[string]ddbtypes.AttributeValue,
	unmarshal func(map[string]ddbtypes.AttributeValue, any) error) bool {

	switch kind {
	case "PUBLISHER":
		var v Publisher
		if unmarshal(item, &v) == nil {
			cp.Publishers = append(cp.Publishers, v)
		}
	case "SITE":
		var v Site
		if unmarshal(item, &v) == nil {
			cp.Sites = append(cp.Sites, v)
		}
	case "PLACEMENT":
		var v Placement
		if unmarshal(item, &v) == nil {
			cp.Placements = append(cp.Placements, v)
		}
	case "ADVERTISER":
		var v Advertiser
		if unmarshal(item, &v) == nil {
			cp.Advertisers = append(cp.Advertisers, v)
		}
	case "CAMPAIGN":
		var v Campaign
		if unmarshal(item, &v) == nil {
			cp.Campaigns = append(cp.Campaigns, v)
		}
	case "LINE_ITEM":
		var v LineItem
		if unmarshal(item, &v) == nil {
			cp.LineItems = append(cp.LineItems, v)
		}
	case "CREATIVE":
		var v Creative
		if unmarshal(item, &v) == nil {
			cp.Creatives = append(cp.Creatives, v)
		}
	case "BUYER":
		var v Buyer
		if unmarshal(item, &v) == nil {
			cp.Buyers = append(cp.Buyers, v)
		}
	case "EXPERIMENT":
		var v Experiment
		if unmarshal(item, &v) == nil {
			cp.Experiments = append(cp.Experiments, v)
		}
	default:
		return false
	}
	return true
}

// ControlPlaneKinds is every kind the loader understands. adlabctl seeds
// exactly these; a mismatch in either direction is a silent capability gap.
var ControlPlaneKinds = []string{
	"PUBLISHER", "SITE", "PLACEMENT", "ADVERTISER", "CAMPAIGN",
	"LINE_ITEM", "CREATIVE", "BUYER", "EXPERIMENT",
}

func (d *DynamoControlPlane) load(ctx context.Context) (*ControlPlane, error) {
	cp := &ControlPlane{}
	var start map[string]ddbtypes.AttributeValue

	for {
		out, err := d.client.Scan(ctx, &dynamodb.ScanInput{
			TableName:         aws.String(d.table),
			ExclusiveStartKey: start,
		})
		if err != nil {
			return nil, fmt.Errorf("scan control plane: %w", err)
		}
		// attributevalue uses the `dynamodbav` tag and otherwise falls back to Go
		// FIELD NAMES -- not `json` tags. Our items are seeded from the same JSON
		// fixtures the local dev server reads, so point the decoder at the json
		// tags rather than duplicating every tag on every struct.
		unmarshal := func(item map[string]ddbtypes.AttributeValue, out any) error {
			return attributevalue.UnmarshalMapWithOptions(item, out,
				func(o *attributevalue.DecoderOptions) { o.TagKey = "json" })
		}

		for _, item := range out.Items {
			kind := ""
			if v, ok := item["pk"].(*ddbtypes.AttributeValueMemberS); ok {
				kind = v.Value
			}
			if !applyItem(cp, kind, item, unmarshal) {
				// An unknown kind is silently dropped, which is how BUYER and
				// EXPERIMENT went missing for weeks. Logged now, so a kind that
				// exists in the table and not in this switch is visible.
				log.Printf("control plane: ignoring unknown kind %q", kind)
			}
		}

		if out.LastEvaluatedKey == nil {
			break
		}
		start = out.LastEvaluatedKey
	}
	return cp, nil
}

// ---------------------------------------------------------------------------
// serving_budget_state, backed by DynamoDB atomic counters.
//
// Deliberately approximate. Reads are cached for a short window, so several
// concurrent invocations can each see the same slightly-stale spend and each
// decide the line item is still eligible. That is bounded over-delivery, and it
// is a normal, contracted-for property of real ad servers -- not a bug.
//
// The durable record of what is owed is the billing ledger, derived from the
// event stream. These counters are never the source of truth for money.
// ---------------------------------------------------------------------------

type DynamoBudget struct {
	client *dynamodb.Client
	table  string

	mu       sync.RWMutex
	cache    map[string]float64
	cachedAt time.Time
	ttl      time.Duration
}

func NewDynamoBudget(c *dynamodb.Client, table string, ttl time.Duration) *DynamoBudget {
	return &DynamoBudget{client: c, table: table, cache: map[string]float64{}, ttl: ttl}
}

func spendKey(lineItemID string) string {
	return "SPEND#" + lineItemID + "#" + time.Now().UTC().Format("2006-01-02")
}

// cachedSpend returns the cached value, expiring the whole cache first if the
// TTL has passed. Split out so the locking can be tested without a client.
func (d *DynamoBudget) cachedSpend(lineItemID string) (float64, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if time.Since(d.cachedAt) >= d.ttl {
		d.cache = map[string]float64{} // holds at most a handful of line items
		d.cachedAt = time.Now()
	}
	v, ok := d.cache[lineItemID]
	return v, ok
}

func (d *DynamoBudget) SpendToday(lineItemID string) float64 {
	if v, ok := d.cachedSpend(lineItemID); ok {
		return v
	}

	out, err := d.client.GetItem(context.Background(), &dynamodb.GetItemInput{
		TableName: aws.String(d.table),
		Key:       map[string]ddbtypes.AttributeValue{"pk": &ddbtypes.AttributeValueMemberS{Value: spendKey(lineItemID)}},
	})
	if err != nil {
		return 0 // fail open: a lookup failure must not stop delivery
	}
	spend := 0.0
	if out.Item != nil {
		if n, ok := out.Item["spend"].(*ddbtypes.AttributeValueMemberN); ok {
			spend, _ = strconv.ParseFloat(n.Value, 64)
		}
	}
	d.mu.Lock()
	d.cache[lineItemID] = spend
	d.mu.Unlock()
	return spend
}

func (d *DynamoBudget) AddSpend(lineItemID string, usd float64) {
	expires := time.Now().UTC().Add(48 * time.Hour).Unix() // daily counters die on their own
	_, err := d.client.UpdateItem(context.Background(), &dynamodb.UpdateItemInput{
		TableName:        aws.String(d.table),
		Key:              map[string]ddbtypes.AttributeValue{"pk": &ddbtypes.AttributeValueMemberS{Value: spendKey(lineItemID)}},
		UpdateExpression: aws.String("ADD spend :v SET expires_at = :e"),
		ExpressionAttributeValues: map[string]ddbtypes.AttributeValue{
			":v": &ddbtypes.AttributeValueMemberN{Value: strconv.FormatFloat(usd, 'f', -1, 64)},
			":e": &ddbtypes.AttributeValueMemberN{Value: strconv.FormatInt(expires, 10)},
		},
	})
	if err == nil {
		d.mu.Lock()
		d.cache[lineItemID] += usd
		d.mu.Unlock()
	}
}

// ---------------------------------------------------------------------------
// DynamoFrequency: session-scoped frequency capping that survives Lambda.
//
// MemoryFrequency is correct locally and INERT in production, which is how this
// shipped: the cap was wired in the local branch of main() only, so s.freq was
// nil in Lambda and Capped() returned false for every request. The feature was
// tested, passing, and doing nothing -- caught only by counting impressions
// against the live endpoint.
//
// In-memory would not have been enough even if it had been wired. Each Lambda
// invocation may run in a different container, so an in-process counter sees a
// fraction of a session's requests and caps almost nothing.
//
// The TTL is the design: a session-scoped counter has no value once the session
// ends, so rows expire rather than accumulate. That is a cost property and a
// data-minimisation property at once.
// ---------------------------------------------------------------------------

type DynamoFrequency struct {
	client *dynamodb.Client
	table  string
	ttl    time.Duration

	// A per-invocation cache. A Lambda handles one request at a time, so this
	// mostly saves a read when the same session hits several placements on one
	// page -- it is not a substitute for the durable counter.
	mu    sync.RWMutex
	cache map[string]int
}

func NewDynamoFrequency(c *dynamodb.Client, table string, ttl time.Duration) *DynamoFrequency {
	return &DynamoFrequency{client: c, table: table, ttl: ttl, cache: map[string]int{}}
}

func freqRowKey(sessionID, subject string) string {
	return "FREQ#" + sessionID + "#" + subject
}

func (d *DynamoFrequency) Count(sessionID, subject string) int {
	if sessionID == "" {
		// No session means no way to count, so no cap applies. A cap that
		// fires when it cannot measure silently kills delivery.
		return 0
	}
	k := freqRowKey(sessionID, subject)

	d.mu.RLock()
	v, ok := d.cache[k]
	d.mu.RUnlock()
	if ok {
		return v
	}

	out, err := d.client.GetItem(context.Background(), &dynamodb.GetItemInput{
		TableName: aws.String(d.table),
		Key:       map[string]ddbtypes.AttributeValue{"pk": &ddbtypes.AttributeValueMemberS{Value: k}},
	})
	if err != nil {
		return 0 // fail open: a lookup failure must not stop delivery
	}
	n := 0
	if out.Item != nil {
		if a, ok := out.Item["n"].(*ddbtypes.AttributeValueMemberN); ok {
			n, _ = strconv.Atoi(a.Value)
		}
	}
	d.mu.Lock()
	d.cache[k] = n
	d.mu.Unlock()
	return n
}

func (d *DynamoFrequency) Increment(sessionID, subject string) {
	if sessionID == "" {
		return
	}
	k := freqRowKey(sessionID, subject)
	expires := time.Now().UTC().Add(d.ttl).Unix()

	_, err := d.client.UpdateItem(context.Background(), &dynamodb.UpdateItemInput{
		TableName:        aws.String(d.table),
		Key:              map[string]ddbtypes.AttributeValue{"pk": &ddbtypes.AttributeValueMemberS{Value: k}},
		UpdateExpression: aws.String("ADD n :one SET expires_at = :e"),
		ExpressionAttributeValues: map[string]ddbtypes.AttributeValue{
			":one": &ddbtypes.AttributeValueMemberN{Value: "1"},
			":e":   &ddbtypes.AttributeValueMemberN{Value: strconv.FormatInt(expires, 10)},
		},
	})
	if err == nil {
		d.mu.Lock()
		d.cache[k]++
		d.mu.Unlock()
	}
}
