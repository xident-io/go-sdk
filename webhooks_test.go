package xident

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"
)

const testWebhookSecret = "whsec_test_secret_key"

// makeTestSignature creates a valid webhook signature for testing.
func makeTestSignature(payload, secret string, timestamp int64) string {
	signed := fmt.Sprintf("%d.%s", timestamp, payload)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signed))
	sig := hex.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("t=%d,v1=%s", timestamp, sig)
}

func TestWebhooks_VerifySignature_Valid(t *testing.T) {
	client := NewClient("sk_test_key")

	payload := `{"type":"session.completed","data":{"session_id":"abc"}}`
	ts := time.Now().Unix()
	sig := makeTestSignature(payload, testWebhookSecret, ts)

	ok, err := client.Webhooks.VerifySignature([]byte(payload), sig, testWebhookSecret)
	if err != nil {
		t.Fatalf("VerifySignature() error: %v", err)
	}
	if !ok {
		t.Error("VerifySignature() should return true")
	}
}

func TestWebhooks_VerifySignature_InvalidHMAC(t *testing.T) {
	client := NewClient("sk_test_key")

	payload := `{"type":"session.completed"}`
	sig := fmt.Sprintf("t=%d,v1=invalid_hmac_hex", time.Now().Unix())

	_, err := client.Webhooks.VerifySignature([]byte(payload), sig, testWebhookSecret)
	if err == nil {
		t.Fatal("expected error for invalid HMAC")
	}
	if !strings.Contains(err.Error(), "verification failed") {
		t.Errorf("error should mention verification failed, got: %s", err.Error())
	}
}

func TestWebhooks_VerifySignature_ExpiredTimestamp(t *testing.T) {
	client := NewClient("sk_test_key")

	payload := `{"type":"session.completed"}`
	oldTS := time.Now().Add(-10 * time.Minute).Unix()
	sig := makeTestSignature(payload, testWebhookSecret, oldTS)

	_, err := client.Webhooks.VerifySignature([]byte(payload), sig, testWebhookSecret, 5*time.Minute)
	if err == nil {
		t.Fatal("expected error for expired timestamp")
	}
	if !strings.Contains(err.Error(), "too old") {
		t.Errorf("error should mention 'too old', got: %s", err.Error())
	}
}

func TestWebhooks_VerifySignature_ZeroToleranceDisablesReplay(t *testing.T) {
	client := NewClient("sk_test_key")

	payload := `{"type":"session.completed"}`
	oldTS := time.Now().Add(-24 * time.Hour).Unix() // 24 hours ago
	sig := makeTestSignature(payload, testWebhookSecret, oldTS)

	ok, err := client.Webhooks.VerifySignature([]byte(payload), sig, testWebhookSecret, 0)
	if err != nil {
		t.Fatalf("expected no error with zero tolerance, got: %v", err)
	}
	if !ok {
		t.Error("should return true with zero tolerance")
	}
}

func TestWebhooks_VerifySignature_MissingSignature(t *testing.T) {
	client := NewClient("sk_test_key")

	_, err := client.Webhooks.VerifySignature([]byte("{}"), "", testWebhookSecret)
	if err == nil {
		t.Fatal("expected error for missing signature")
	}
	if !strings.Contains(err.Error(), "missing webhook signature") {
		t.Errorf("error should mention missing signature, got: %s", err.Error())
	}
}

func TestWebhooks_VerifySignature_MissingSecret(t *testing.T) {
	client := NewClient("sk_test_key")

	_, err := client.Webhooks.VerifySignature([]byte("{}"), "t=1,v1=abc", "")
	if err == nil {
		t.Fatal("expected error for missing secret")
	}
	if !strings.Contains(err.Error(), "missing webhook secret") {
		t.Errorf("error should mention missing secret, got: %s", err.Error())
	}
}

func TestWebhooks_VerifySignature_MalformedSignature(t *testing.T) {
	client := NewClient("sk_test_key")

	_, err := client.Webhooks.VerifySignature([]byte("{}"), "not-a-valid-sig", testWebhookSecret)
	if err == nil {
		t.Fatal("expected error for malformed signature")
	}
	if !strings.Contains(err.Error(), "invalid signature format") {
		t.Errorf("error should mention format, got: %s", err.Error())
	}
}

func TestWebhooks_VerifySignature_MissingTimestampComponent(t *testing.T) {
	client := NewClient("sk_test_key")

	_, err := client.Webhooks.VerifySignature([]byte("{}"), "v1=abc123", testWebhookSecret)
	if err == nil {
		t.Fatal("expected error for missing timestamp")
	}
}

func TestWebhooks_VerifySignature_MissingHMACComponent(t *testing.T) {
	client := NewClient("sk_test_key")

	_, err := client.Webhooks.VerifySignature([]byte("{}"), "t=12345", testWebhookSecret)
	if err == nil {
		t.Fatal("expected error for missing HMAC")
	}
}

func TestWebhooks_ConstructEvent(t *testing.T) {
	client := NewClient("sk_test_key")

	payload := `{
		"type": "session.completed",
		"data": {"session_id": "sess_abc", "status": "completed"},
		"id": "evt_001",
		"created": 1710345600
	}`
	ts := time.Now().Unix()
	sig := makeTestSignature(payload, testWebhookSecret, ts)

	event, err := client.Webhooks.ConstructEvent([]byte(payload), sig, testWebhookSecret)
	if err != nil {
		t.Fatalf("ConstructEvent() error: %v", err)
	}

	if event.Type != "session.completed" {
		t.Errorf("Type = %q, want %q", event.Type, "session.completed")
	}
	if event.Data["session_id"] != "sess_abc" {
		t.Errorf("Data[session_id] = %v, want %q", event.Data["session_id"], "sess_abc")
	}
	if event.ID != "evt_001" {
		t.Errorf("ID = %q, want %q", event.ID, "evt_001")
	}
	if event.Created != 1710345600 {
		t.Errorf("Created = %d, want %d", event.Created, 1710345600)
	}
}

func TestWebhooks_ConstructEvent_InvalidSignature(t *testing.T) {
	client := NewClient("sk_test_key")

	payload := `{"type":"session.completed"}`
	sig := fmt.Sprintf("t=%d,v1=bad", time.Now().Unix())

	_, err := client.Webhooks.ConstructEvent([]byte(payload), sig, testWebhookSecret)
	if err == nil {
		t.Fatal("expected error for invalid signature")
	}
}

func TestWebhooks_ConstructEvent_InvalidJSON(t *testing.T) {
	client := NewClient("sk_test_key")

	payload := `not json`
	ts := time.Now().Unix()
	sig := makeTestSignature(payload, testWebhookSecret, ts)

	_, err := client.Webhooks.ConstructEvent([]byte(payload), sig, testWebhookSecret)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "not valid JSON") {
		t.Errorf("error should mention JSON, got: %s", err.Error())
	}
}

func TestWebhooks_ConstructEvent_EventTypeField(t *testing.T) {
	client := NewClient("sk_test_key")

	payload := `{"event_type":"session.expired","data":{}}`
	ts := time.Now().Unix()
	sig := makeTestSignature(payload, testWebhookSecret, ts)

	event, err := client.Webhooks.ConstructEvent([]byte(payload), sig, testWebhookSecret)
	if err != nil {
		t.Fatalf("ConstructEvent() error: %v", err)
	}
	if event.Type != "session.expired" {
		t.Errorf("Type = %q, want %q", event.Type, "session.expired")
	}
}

func TestWebhooks_ConstructEvent_CustomTolerance(t *testing.T) {
	client := NewClient("sk_test_key")

	payload := `{"type":"session.completed","data":{}}`
	ts := time.Now().Add(-3 * time.Minute).Unix()
	sig := makeTestSignature(payload, testWebhookSecret, ts)

	// Should succeed with 10 minute tolerance.
	_, err := client.Webhooks.ConstructEvent([]byte(payload), sig, testWebhookSecret, 10*time.Minute)
	if err != nil {
		t.Fatalf("ConstructEvent with 10m tolerance should succeed: %v", err)
	}

	// Should fail with 1 minute tolerance.
	_, err = client.Webhooks.ConstructEvent([]byte(payload), sig, testWebhookSecret, 1*time.Minute)
	if err == nil {
		t.Fatal("expected error with 1m tolerance for 3m old signature")
	}
}
