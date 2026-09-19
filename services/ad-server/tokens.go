package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Measurement integrity.
//
// If any URL can mint a valid impression, every metric built on top is fiction
// -- and we would be teaching ourselves the exact failure mode that IVT
// exploits. So an event is only counted if it carries a token this server
// signed, for this request, that has not expired.
//
// Simplified deliberately: one static key, no rotation. Real systems rotate.
// ---------------------------------------------------------------------------

var ErrBadToken = errors.New("invalid or expired tracking token")

type TokenClaims struct {
	RequestID  string
	LineItemID string
	CreativeID string
	Expires    time.Time
}

func sign(key []byte, payload string) string {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func MintToken(key []byte, c TokenClaims) string {
	payload := fmt.Sprintf("%s|%s|%s|%d", c.RequestID, c.LineItemID, c.CreativeID, c.Expires.Unix())
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + sign(key, payload)
}

func VerifyToken(key []byte, token string, now time.Time) (*TokenClaims, error) {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return nil, ErrBadToken
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, ErrBadToken
	}
	payload := string(raw)

	// Constant-time comparison: a timing-variable check would leak the signature.
	if !hmac.Equal([]byte(sign(key, payload)), []byte(parts[1])) {
		return nil, ErrBadToken
	}

	f := strings.Split(payload, "|")
	if len(f) != 4 {
		return nil, ErrBadToken
	}
	exp, err := strconv.ParseInt(f[3], 10, 64)
	if err != nil {
		return nil, ErrBadToken
	}
	if now.After(time.Unix(exp, 0)) {
		return nil, ErrBadToken
	}
	return &TokenClaims{RequestID: f[0], LineItemID: f[1], CreativeID: f[2], Expires: time.Unix(exp, 0)}, nil
}
