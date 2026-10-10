package xident

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// WebhookService provides methods to verify incoming webhook signatures and
// parse webhook event payloads.
//
// Xident sends webhooks to your configured callback URL when verification
// sessions are completed, failed, or expire. The signature format follows the
// Stripe convention:
//
//	X-Xident-Signature: t=1710345600,v1=5257a869abcdef...
//
// Access this service through Client.Webhooks:
//
//	client := xident.NewClient("sk_live_xxx")
//	event, err := client.Webhooks.ConstructEvent(payload, sig, secret)
type WebhookService service

// WebhookEvent represents a parsed webhook event from Xident.
type WebhookEvent struct {
	// Type is the event type (e.g., "session.success", "session.failed").
	//
	// The pass event was renamed from "session.completed" to "session.success"
	// in July 2026. An endpoint registered before then still receives the old
	// name, so handle both if your endpoint predates the rename.
	Type string `json:"type"`

	// Data contains the event payload. The structure depends on the event type.
	Data map[string]any `json:"data"`

	// ID is the unique event identifier, if provided.
	ID string `json:"id,omitempty"`

	// Created is the Unix timestamp when the event was created, if provided.
	Created int64 `json:"created,omitempty"`
}

// SessionResult decodes Data into a SessionResult.
//
// The data of every session.* and review.* event is the same tenant result
// that Verification.GetResult returns, so the same decision applies: grant
// access only when ProvesAge(requiredMinAge) holds and ExternalUserID is a
// user your server started a verification for.
//
// It returns an error when Data cannot be read as a result.
func (e *WebhookEvent) SessionResult() (*SessionResult, error) {
	raw, err := json.Marshal(e.Data)
	if err != nil {
		return nil, fmt.Errorf("xident: webhook data is not JSON: %w", err)
	}
	var result SessionResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("xident: webhook data is not a session result: %w", err)
	}
	return &result, nil
}

// DefaultWebhookTolerance is the default maximum age for webhook signatures
// (5 minutes). Signatures older than this are rejected as potential replays.
const DefaultWebhookTolerance = 5 * time.Minute

// ConstructEvent verifies the webhook signature and parses the event payload.
//
// This is a convenience method that combines VerifySignature + JSON parsing.
//
//   - payload is the raw request body ([]byte).
//   - signature is the value of the X-Xident-Signature header.
//   - secret is your webhook signing secret from the dashboard (whsec_xxx).
//   - tolerance is optional -- defaults to 5 minutes. Pass 0 to disable
//     replay protection (NOT recommended in production).
//
// Example:
//
//	event, err := client.Webhooks.ConstructEvent(body, sig, secret)
//	if err != nil {
//	    http.Error(w, "Invalid signature", 400)
//	    return
//	}
//	if event.Type == "session.success" {
//	    result, err := event.SessionResult()
//	    if err != nil {
//	        http.Error(w, "Invalid payload", 400)
//	        return
//	    }
//	    // Grant only when the result proves the age YOUR site requires
//	    // (a constant on your server) and belongs to a user your server
//	    // started a verification for. A success alone is not enough: an ID
//	    // verification passes without proving any age.
//	    if result.ProvesAge(requiredMinAge) && startedFor(result.ExternalUserID) {
//	        // grant access to result.ExternalUserID
//	    }
//	}
func (s *WebhookService) ConstructEvent(payload []byte, signature, secret string, tolerance ...time.Duration) (*WebhookEvent, error) {
	tol := DefaultWebhookTolerance
	if len(tolerance) > 0 {
		tol = tolerance[0]
	}

	if _, err := s.VerifySignature(payload, signature, secret, tol); err != nil {
		return nil, err
	}

	return s.parseEvent(payload)
}

// VerifySignature verifies a webhook HMAC-SHA256 signature.
//
// Returns (true, nil) if the signature is valid. Returns (false, error) with
// a descriptive error if the signature is invalid, malformed, or too old.
//
//   - payload is the raw request body.
//   - signature is the X-Xident-Signature header value.
//   - secret is your webhook signing secret.
//   - tolerance is the maximum signature age. Use 0 to disable replay
//     protection. Defaults to DefaultWebhookTolerance (5 minutes).
func (s *WebhookService) VerifySignature(payload []byte, signature, secret string, tolerance ...time.Duration) (bool, error) {
	if signature == "" {
		return false, fmt.Errorf("xident: missing webhook signature")
	}
	if secret == "" {
		return false, fmt.Errorf("xident: missing webhook secret")
	}

	// Parse "t=TIMESTAMP,v1=HMAC_HEX"
	parts := make(map[string]string)
	for _, pair := range strings.Split(signature, ",") {
		kv := strings.SplitN(pair, "=", 2)
		if len(kv) == 2 {
			parts[kv[0]] = kv[1]
		}
	}

	ts, hasT := parts["t"]
	sig, hasV1 := parts["v1"]
	if !hasT || !hasV1 {
		return false, fmt.Errorf("xident: invalid signature format -- expected t=TIMESTAMP,v1=HMAC")
	}

	timestamp, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return false, fmt.Errorf("xident: invalid signature timestamp: %w", err)
	}

	// Replay protection.
	tol := DefaultWebhookTolerance
	if len(tolerance) > 0 {
		tol = tolerance[0]
	}
	if tol > 0 {
		age := time.Since(time.Unix(timestamp, 0))
		if age > tol {
			return false, fmt.Errorf("xident: webhook timestamp too old (%s, tolerance %s)", age.Truncate(time.Second), tol)
		}
	}

	// Compute HMAC-SHA256: sign "{timestamp}.{payload}"
	signedPayload := fmt.Sprintf("%d.%s", timestamp, string(payload))
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signedPayload))
	computed := hex.EncodeToString(mac.Sum(nil))

	// Constant-time comparison to prevent timing attacks.
	if !hmac.Equal([]byte(computed), []byte(sig)) {
		return false, fmt.Errorf("xident: webhook signature verification failed")
	}

	return true, nil
}

// parseEvent parses a raw webhook payload into a WebhookEvent.
func (s *WebhookService) parseEvent(payload []byte) (*WebhookEvent, error) {
	var raw map[string]any
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, fmt.Errorf("xident: invalid webhook payload -- not valid JSON: %w", err)
	}

	event := &WebhookEvent{}

	// Support both "type" and "event_type" field names.
	if t, ok := raw["type"].(string); ok {
		event.Type = t
	} else if t, ok := raw["event_type"].(string); ok {
		event.Type = t
	}

	// Extract data.
	if d, ok := raw["data"].(map[string]any); ok {
		event.Data = d
	} else {
		// If no "data" wrapper, use the entire payload as data.
		event.Data = raw
	}

	// Extract optional fields.
	if id, ok := raw["id"].(string); ok {
		event.ID = id
	} else if id, ok := raw["event_id"].(string); ok {
		event.ID = id
	}

	if created, ok := raw["created"].(float64); ok {
		event.Created = int64(created)
	}

	return event, nil
}
