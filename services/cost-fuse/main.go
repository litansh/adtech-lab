// cost-fuse trips the project's fail-closed switches when a CloudWatch alarm
// fires. It bounds how LONG abuse can bill, where throttling only bounds how
// FAST it bills.
//
// Two independent fuses, because product events outnumber ad requests ~9:1 and
// a popular Publisher must never be able to stop ad serving:
//
//	AD FUSE         alarms named adtech-lab-fuse-*      -> ad-server concurrency 0
//	ANALYTICS FUSE  alarms named adtech-lab-analytics-* -> drop /collect writes only
//	WARNING         alarms named adtech-lab-warn-*      -> notify, take no action
//
// Recovery is deliberately manual (`adlabctl fuse reset`). A fuse that resets
// itself is a fuse that lets an attacker run a duty cycle.
package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	ddbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	lambdasvc "github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/sns"
)

type alarmMessage struct {
	AlarmName       string `json:"AlarmName"`
	AlarmDesc       string `json:"AlarmDescription"`
	NewStateReason  string `json:"NewStateReason"`
	StateChangeTime string `json:"StateChangeTime"`
}

const (
	fuseAd        = "AD_FUSE"
	fuseAnalytics = "ANALYTICS_FUSE"
	fuseWarnOnly  = "WARNING"
)

func classify(alarmName string) string {
	switch {
	case strings.HasPrefix(alarmName, "adtech-lab-fuse-"):
		return fuseAd
	case strings.HasPrefix(alarmName, "adtech-lab-analytics-"):
		return fuseAnalytics
	default:
		return fuseWarnOnly
	}
}

type deps struct {
	lam   *lambdasvc.Client
	ddb   *dynamodb.Client
	sns   *sns.Client
	adFn  string
	table string
	topic string
}

func (d *deps) recordTrip(ctx context.Context, kind string, m alarmMessage) error {
	_, err := d.ddb.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(d.table),
		Item: map[string]ddbtypes.AttributeValue{
			"pk":         &ddbtypes.AttributeValueMemberS{Value: "FUSE#" + kind},
			"state":      &ddbtypes.AttributeValueMemberS{Value: "TRIPPED"},
			"alarm":      &ddbtypes.AttributeValueMemberS{Value: m.AlarmName},
			"reason":     &ddbtypes.AttributeValueMemberS{Value: m.NewStateReason},
			"tripped_at": &ddbtypes.AttributeValueMemberS{Value: time.Now().UTC().Format(time.RFC3339)},
		},
	})
	return err
}

func (d *deps) notify(ctx context.Context, subject, body string) {
	if _, err := d.sns.Publish(ctx, &sns.PublishInput{
		TopicArn: aws.String(d.topic),
		Subject:  aws.String(subject),
		Message:  aws.String(body),
	}); err != nil {
		log.Printf("notify failed: %v", err) // never fail the trip because the email failed
	}
}

func (d *deps) handle(ctx context.Context, ev events.SNSEvent) error {
	for _, rec := range ev.Records {
		var m alarmMessage
		if err := json.Unmarshal([]byte(rec.SNS.Message), &m); err != nil {
			log.Printf("not a CloudWatch alarm payload, ignoring: %v", err)
			continue
		}
		kind := classify(m.AlarmName)
		log.Printf("alarm=%s kind=%s reason=%s", m.AlarmName, kind, m.NewStateReason)

		switch kind {
		case fuseAd:
			// Zero reserved concurrency: every invocation is throttled at the
			// Lambda service boundary, whatever route it arrived on. This stops
			// Lambda duration, DynamoDB writes and Firehose records.
			if _, err := d.lam.PutFunctionConcurrency(ctx, &lambdasvc.PutFunctionConcurrencyInput{
				FunctionName:                 aws.String(d.adFn),
				ReservedConcurrentExecutions: aws.Int32(0),
			}); err != nil {
				log.Printf("FAILED to set concurrency to 0: %v", err)
				d.notify(ctx, "AD FUSE FAILED TO TRIP", "Alarm "+m.AlarmName+" fired but concurrency could not be set to 0: "+err.Error())
				return err
			}
			if err := d.recordTrip(ctx, kind, m); err != nil {
				log.Printf("trip recorded in Lambda but not DynamoDB: %v", err)
			}
			d.notify(ctx, "AD FUSE TRIPPED - ad serving stopped",
				"Alarm: "+m.AlarmName+"\n"+m.AlarmDesc+"\n\n"+m.NewStateReason+
					"\n\nAd serving is stopped. The games site is UNAFFECTED and still online."+
					"\n\nInvestigate, then re-enable deliberately:\n  adlabctl fuse reset --ad")

		case fuseAnalytics:
			// Ad serving is deliberately untouched. Without WAF we cannot block
			// the route at the edge, so the ad server reads this flag and drops
			// /collect writes. Honest limitation: this stops the Firehose and
			// DynamoDB cost, not the API Gateway request cost. WAF closes that
			// gap when there is traffic worth paying $7-8/month to protect.
			if err := d.recordTrip(ctx, kind, m); err != nil {
				log.Printf("failed to record analytics trip: %v", err)
				return err
			}
			d.notify(ctx, "ANALYTICS FUSE TRIPPED - ad serving unaffected",
				"Alarm: "+m.AlarmName+"\n"+m.AlarmDesc+"\n\n"+m.NewStateReason+
					"\n\n/collect writes are dropped. Ad serving and the games are UNAFFECTED."+
					"\n\nRe-enable with:\n  adlabctl fuse reset --analytics")

		default:
			d.notify(ctx, "adtech-lab warning: "+m.AlarmName,
				m.AlarmDesc+"\n\n"+m.NewStateReason+"\n\nNo action taken. This is a warning threshold.")
		}
	}
	return nil
}

func main() {
	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		log.Fatalf("aws config: %v", err)
	}
	d := &deps{
		lam:   lambdasvc.NewFromConfig(cfg),
		ddb:   dynamodb.NewFromConfig(cfg),
		sns:   sns.NewFromConfig(cfg),
		adFn:  os.Getenv("AD_SERVER_FUNCTION"),
		table: os.Getenv("STATE_TABLE"),
		topic: os.Getenv("ALERT_TOPIC_ARN"),
	}
	lambda.Start(d.handle)
}
