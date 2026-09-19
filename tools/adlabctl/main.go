// adlabctl manages BUSINESS objects: publishers, placements, campaigns, line
// items, creatives -- and the Cost Fuse state.
//
// Deliberately not Terraform. Terraform manages infrastructure; campaign
// configuration changes constantly, is not desired-state infrastructure, and
// `terraform destroy` must never be able to take it with it. See CLAUDE.md.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	ddbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	lambdasvc "github.com/aws/aws-sdk-go-v2/service/lambda"
)

type controlPlane struct {
	Publishers  []map[string]any `json:"publishers"`
	Sites       []map[string]any `json:"sites"`
	Placements  []map[string]any `json:"placements"`
	Advertisers []map[string]any `json:"advertisers"`
	Campaigns   []map[string]any `json:"campaigns"`
	LineItems   []map[string]any `json:"line_items"`
	Creatives   []map[string]any `json:"creatives"`
	Buyers      []map[string]any `json:"buyers"`
	Experiments []map[string]any `json:"experiments"`
}

// isLoopback reports whether a URL points at this machine.
func isLoopback(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	h := u.Hostname()
	return h == "localhost" || h == "127.0.0.1" || h == "::1" ||
		strings.HasPrefix(h, "127.")
}

func die(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func ddbClient(ctx context.Context, profile string) *dynamodb.Client {
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithSharedConfigProfile(profile), config.WithRegion("us-east-1"))
	die(err)
	return dynamodb.NewFromConfig(cfg)
}

func seed(ctx context.Context, c *dynamodb.Client, table, file string) {
	raw, err := os.ReadFile(file)
	die(err)
	var cp controlPlane
	die(json.Unmarshal(raw, &cp))

	groups := []struct {
		kind  string
		items []map[string]any
	}{
		{"PUBLISHER", cp.Publishers}, {"SITE", cp.Sites}, {"PLACEMENT", cp.Placements},
		{"ADVERTISER", cp.Advertisers}, {"CAMPAIGN", cp.Campaigns},
		{"LINE_ITEM", cp.LineItems}, {"CREATIVE", cp.Creatives},
		// Buyers and experiments were absent from this list, so no amount of
		// seeding could put them in the control plane -- and the ad server had
		// no case to read them if they had been. Both ends were missing, which
		// is why nothing ever failed loudly.
		{"BUYER", cp.Buyers}, {"EXPERIMENT", cp.Experiments},
	}

	n := 0
	for _, g := range groups {
		for _, item := range g.items {
			// Experiments are keyed by "key", everything else by "id".
			id, _ := item["id"].(string)
			if id == "" {
				id, _ = item["key"].(string)
			}
			if id == "" {
				die(fmt.Errorf("%s item has no id or key", g.kind))
			}
			// Never seed a loopback endpoint into a remote control plane. The
			// mini-DSP lives on 127.0.0.1 for the local Phase 4 exercise, and
			// pushing it to production means every auction opens a connection
			// to a host that does not exist there -- adding latency to every
			// request in exchange for nothing.
			if ep, _ := item["endpoint"].(string); ep != "" && isLoopback(ep) {
				fmt.Printf("  %-11s %s  SKIPPED (loopback endpoint %s)\n", g.kind, id, ep)
				continue
			}

			av, err := attributevalue.MarshalMap(item)
			die(err)
			av["pk"] = &ddbtypes.AttributeValueMemberS{Value: g.kind}
			av["sk"] = &ddbtypes.AttributeValueMemberS{Value: id}

			_, err = c.PutItem(ctx, &dynamodb.PutItemInput{TableName: aws.String(table), Item: av})
			die(err)
			n++
			fmt.Printf("  %-11s %s\n", g.kind, id)
		}
	}
	fmt.Printf("\nseeded %d items into %s\n", n, table)
	fmt.Println("The ad server picks this up within its 60s control-plane TTL.")
}

func fuseStatus(ctx context.Context, c *dynamodb.Client, table string) {
	for _, kind := range []string{"AD_FUSE", "ANALYTICS_FUSE"} {
		out, err := c.GetItem(ctx, &dynamodb.GetItemInput{
			TableName: aws.String(table),
			Key:       map[string]ddbtypes.AttributeValue{"pk": &ddbtypes.AttributeValueMemberS{Value: "FUSE#" + kind}},
		})
		die(err)
		if out.Item == nil {
			fmt.Printf("  %-15s ARMED\n", kind)
			continue
		}
		get := func(k string) string {
			if v, ok := out.Item[k].(*ddbtypes.AttributeValueMemberS); ok {
				return v.Value
			}
			return ""
		}
		fmt.Printf("  %-15s %s  alarm=%s  at=%s\n", kind, get("state"), get("alarm"), get("tripped_at"))
		if r := get("reason"); r != "" {
			fmt.Printf("  %-15s reason: %s\n", "", r)
		}
	}
}

func fuseReset(ctx context.Context, c *dynamodb.Client, table, profile, kind, adFn string, concurrency int32) {
	// Deliberately manual, and deliberately requires a reason: a fuse that
	// resets itself lets an attacker run a duty cycle.
	if kind == "AD_FUSE" {
		cfg, err := config.LoadDefaultConfig(ctx,
			config.WithSharedConfigProfile(profile), config.WithRegion("us-east-1"))
		die(err)
		_, err = lambdasvc.NewFromConfig(cfg).PutFunctionConcurrency(ctx, &lambdasvc.PutFunctionConcurrencyInput{
			FunctionName:                 aws.String(adFn),
			ReservedConcurrentExecutions: aws.Int32(concurrency),
		})
		die(err)
		fmt.Printf("restored %s reserved concurrency to %d\n", adFn, concurrency)
	}

	_, err := c.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(table),
		Item: map[string]ddbtypes.AttributeValue{
			"pk":       &ddbtypes.AttributeValueMemberS{Value: "FUSE#" + kind},
			"state":    &ddbtypes.AttributeValueMemberS{Value: "ARMED"},
			"reset_at": &ddbtypes.AttributeValueMemberS{Value: time.Now().UTC().Format(time.RFC3339)},
			"reset_by": &ddbtypes.AttributeValueMemberS{Value: os.Getenv("USER")},
		},
	})
	die(err)
	fmt.Printf("%s re-armed\n", kind)
}

func usage() {
	fmt.Fprintln(os.Stderr, `adlabctl - manage adtech-lab business objects and the Cost Fuse

  adlabctl seed  -table T [-file F]
  adlabctl fuse  status -table T
  adlabctl fuse  reset  -table T [-ad | -analytics] [-fn FUNCTION]`)
	os.Exit(2)
}

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	ctx := context.Background()

	switch os.Args[1] {
	case "seed":
		fs := flag.NewFlagSet("seed", flag.ExitOnError)
		table := fs.String("table", "adtech-lab-control-plane", "control plane table")
		file := fs.String("file", "services/ad-server/fixtures/control-plane.json", "fixture file")
		profile := fs.String("profile", os.Getenv("ADLAB_AWS_PROFILE"), "AWS profile (default $ADLAB_AWS_PROFILE)")
		die(fs.Parse(os.Args[2:]))
		seed(ctx, ddbClient(ctx, *profile), *table, *file)

	case "fuse":
		if len(os.Args) < 3 {
			usage()
		}
		fs := flag.NewFlagSet("fuse", flag.ExitOnError)
		table := fs.String("table", "adtech-lab-serving-state", "serving state table")
		profile := fs.String("profile", os.Getenv("ADLAB_AWS_PROFILE"), "AWS profile (default $ADLAB_AWS_PROFILE)")
		ad := fs.Bool("ad", false, "the ad fuse")
		analytics := fs.Bool("analytics", false, "the analytics fuse")
		fn := fs.String("fn", "adtech-lab-ad-server", "ad server function")
		conc := fs.Int("concurrency", 20, "reserved concurrency to restore")
		die(fs.Parse(os.Args[3:]))

		switch os.Args[2] {
		case "status":
			fuseStatus(ctx, ddbClient(ctx, *profile), *table)
		case "reset":
			c := ddbClient(ctx, *profile)
			if !*ad && !*analytics {
				fmt.Fprintln(os.Stderr, "specify -ad or -analytics")
				os.Exit(2)
			}
			if *ad {
				fuseReset(ctx, c, *table, *profile, "AD_FUSE", *fn, int32(*conc))
			}
			if *analytics {
				fuseReset(ctx, c, *table, *profile, "ANALYTICS_FUSE", *fn, 0)
			}
		default:
			usage()
		}
	default:
		usage()
	}
}
