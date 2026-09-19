//go:build lambda

package main

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

// Bridges API Gateway HTTP API (payload format 2.0) onto the same http.Handler
// the local dev server uses, so there is exactly one code path for decisioning
// and one set of tests covering it.
func lambdaHandler(mux http.Handler, flush func()) func(context.Context, events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	return func(ctx context.Context, req events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
		body := req.Body
		if req.IsBase64Encoded {
			if b, err := base64.StdEncoding.DecodeString(body); err == nil {
				body = string(b)
			}
		}

		target := req.RawPath
		if req.RawQueryString != "" {
			target += "?" + req.RawQueryString
		}

		r := httptest.NewRequest(req.RequestContext.HTTP.Method, target, strings.NewReader(body))
		for k, v := range req.Headers {
			r.Header.Set(k, v)
		}

		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		res := w.Result()

		// Persist this invocation's events before returning. Nothing is held
		// across invocations, so a container shutdown cannot lose the billing
		// ledger's source of truth.
		if flush != nil {
			flush()
		}

		headers := map[string]string{}
		for k := range res.Header {
			headers[k] = res.Header.Get(k)
		}

		// Binary responses (the tracking pixel) must be base64 for API Gateway.
		raw := w.Body.Bytes()
		isBinary := strings.HasPrefix(res.Header.Get("Content-Type"), "image/")
		out := string(raw)
		if isBinary {
			out = base64.StdEncoding.EncodeToString(raw)
		}

		return events.APIGatewayV2HTTPResponse{
			StatusCode:      res.StatusCode,
			Headers:         headers,
			Body:            out,
			IsBase64Encoded: isBinary,
		}, nil
	}
}

func serve(s *Server) {
	lambda.Start(lambdaHandler(s.routes(), s.flush))
}
