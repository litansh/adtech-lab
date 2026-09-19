package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// ---------------------------------------------------------------------------
// S3EventSink: the data plane, written straight to S3.
//
// This exists because **Amazon Data Firehose is unavailable on the AWS Free
// account plan** (SubscriptionRequiredException, every region). Firehose was
// the design; this is the constraint we actually have.
//
// Design: buffer every event produced during ONE invocation, then write them as
// a single gzipped NDJSON object on flush. Nothing is held across invocations,
// so no event can be lost to a container shutdown -- which matters, because
// this stream is the billing ledger's source of truth.
//
// The honest trade-off, in cost terms:
//
//	Phase 1   ~23k ad requests/month -> ~46k PUTs   -> ~$0.23/month.  Fine.
//	1M/month  ~2M PUTs at $5/million -> ~$10/month. NOT fine.
//
// At that point the answer is a real buffering layer -- SQS plus a batching
// consumer, or Kinesis -- which is precisely what Firehose is. We are hand-
// rolling the cheap half of it because the managed version is off the menu.
// Documented in docs/cost-model.md rather than discovered later on a bill.
// ---------------------------------------------------------------------------

type S3EventSink struct {
	client *s3.Client
	bucket string

	mu     sync.Mutex
	buffer []map[string]any
}

func NewS3EventSink(c *s3.Client, bucket string) *S3EventSink {
	return &S3EventSink{client: c, bucket: bucket}
}

func (s *S3EventSink) Emit(e map[string]any) {
	s.mu.Lock()
	s.buffer = append(s.buffer, e)
	s.mu.Unlock()
}

// Flush writes the buffered events and empties the buffer. Called once at the
// end of each invocation.
func (s *S3EventSink) Flush(ctx context.Context) {
	s.mu.Lock()
	batch := s.buffer
	s.buffer = nil
	s.mu.Unlock()

	if len(batch) == 0 {
		return
	}

	var raw bytes.Buffer
	gz := gzip.NewWriter(&raw)
	for _, e := range batch {
		b, err := json.Marshal(e)
		if err != nil {
			continue
		}
		gz.Write(append(b, '\n'))
	}
	if err := gz.Close(); err != nil {
		log.Printf("event flush: gzip: %v", err)
		return
	}

	// Hourly partitions, matching what Firehose would have produced, so the
	// Athena table definition and every query stay unchanged if we ever move
	// to a managed pipeline.
	now := time.Now().UTC()
	suffix := make([]byte, 6)
	rand.Read(suffix)
	key := fmt.Sprintf("events/dt=%s/hh=%s/%s-%s.ndjson.gz",
		now.Format("2006-01-02"), now.Format("15"),
		now.Format("150405"), hex.EncodeToString(suffix))

	if _, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:          aws.String(s.bucket),
		Key:             aws.String(key),
		Body:            bytes.NewReader(raw.Bytes()),
		ContentType:     aws.String("application/x-ndjson"),
		ContentEncoding: aws.String("gzip"),
	}); err != nil {
		// Telemetry must never break serving. An ad already served is not
		// un-served because we failed to record it -- but this IS the billing
		// ledger, so it is logged loudly rather than swallowed.
		log.Printf("EVENT LOSS: failed to write %d events to s3://%s/%s: %v",
			len(batch), s.bucket, key, err)
	}
}
