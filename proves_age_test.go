package xident

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

// The fixtures read here are real API payloads:
//
//   - testdata/tenant_result_v1.golden.json is the API's frozen tenant result
//     (internal/domain/services/testdata/tenant_result_v1.golden.json in the
//     api repository): a passed session with checks.age.gate 21.
//   - testdata/tenant_result_id_no_gate.json is the same result as the API
//     renders it for an ID verification on the api branch
//     fix/session-settings-from-init-token (xident-io/api#44): the session
//     stores min_age 0, so checks.age.gate is omitted, while checks.age is
//     performed and passed because the document's date of birth was read
//     (session_result_view.go buildResultChecks).
//   - testdata/webhook_session_success_age.json is the webhook envelope the
//     API sends for that result (webhook_service.go buildSessionEventPayload:
//     id, type, api_version, created, data = the tenant result).

func readResultFixture(t *testing.T, path string) *SessionResult {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var s SessionResult
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
	return &s
}

// TestSessionResult_ProvesAge pins the age decision a callback should make:
// a passed result whose tested gate reaches the site's own required age.
//
// Mutations caught: dropping the IsVerified check, the Passed check or the
// Performed check; comparing with > instead of >=; treating a missing gate
// (an ID verification) as a pass; returning true for a required age of 0.
func TestSessionResult_ProvesAge(t *testing.T) {
	age21 := readResultFixture(t, "testdata/tenant_result_v1.golden.json")
	idOnly := readResultFixture(t, "testdata/tenant_result_id_no_gate.json")

	if age21.Checks.Age.Gate != 21 {
		t.Fatalf("fixture gate = %d, want 21: the golden file changed", age21.Checks.Age.Gate)
	}
	if idOnly.Checks.Age.Gate != 0 || !idOnly.Checks.Age.Passed || !idOnly.IsVerified() {
		t.Fatalf("ID fixture = %+v, want a passed result with no gate", idOnly.Checks.Age)
	}

	tests := []struct {
		name   string
		result *SessionResult
		minAge int
		want   bool
	}{
		{"gate 21 proves 21", age21, 21, true},
		{"gate 21 proves 18", age21, 18, true},
		{"gate 21 proves 19 (the site asked 19, Xident enforced 21)", age21, 19, true},
		{"gate 21 does not prove 25", age21, 25, false},
		{"gate 21 does not prove 22", age21, 22, false},
		{"a required age of 0 is never proven", age21, 0, false},
		{"a negative required age is never proven", age21, -1, false},
		{"an ID verification proves no age", idOnly, 18, false},
		{"an ID verification proves no age, even 12", idOnly, 12, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.result.ProvesAge(tt.minAge); got != tt.want {
				t.Errorf("ProvesAge(%d) = %v, want %v", tt.minAge, got, tt.want)
			}
		})
	}
}

// TestSessionResult_ProvesAge_NeedsEveryPart changes one part of a passing
// result at a time: each change alone must turn the answer to false.
func TestSessionResult_ProvesAge_NeedsEveryPart(t *testing.T) {
	cases := map[string]func(s *SessionResult){
		"session failed":         func(s *SessionResult) { s.Status = SessionStatusFailed },
		"session pending":        func(s *SessionResult) { s.Status = SessionStatusPending },
		"age check did not pass": func(s *SessionResult) { s.Checks.Age.Passed = false },
		"age check did not run":  func(s *SessionResult) { s.Checks.Age.Performed = false },
		"gate lower than needed": func(s *SessionResult) { s.Checks.Age.Gate = 18 },
		"no gate":                func(s *SessionResult) { s.Checks.Age.Gate = 0 },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			s := readResultFixture(t, "testdata/tenant_result_v1.golden.json")
			if !s.ProvesAge(21) {
				t.Fatal("the unchanged fixture must prove 21")
			}
			change(s)
			if s.ProvesAge(21) {
				t.Errorf("ProvesAge(21) = true after %q, want false", name)
			}
		})
	}
}

// TestWebhooks_ConstructEvent_RealNestedPayload parses the webhook the API
// sends for a passed age session. data is the tenant result, with nested
// checks, and decodes into a SessionResult that proves the same age.
func TestWebhooks_ConstructEvent_RealNestedPayload(t *testing.T) {
	client, _, teardown := setup()
	defer teardown()

	payload, err := os.ReadFile("testdata/webhook_session_success_age.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	secret := "whsec_fixture"
	event, err := client.Webhooks.ConstructEvent(payload, makeTestSignature(string(payload), secret, time.Now().Unix()), secret)
	if err != nil {
		t.Fatalf("ConstructEvent() error: %v", err)
	}
	if event.Type != "session.success" || event.ID != "evt_0001" {
		t.Errorf("event = %q %q, want session.success evt_0001", event.Type, event.ID)
	}
	checks, ok := event.Data["checks"].(map[string]any)
	if !ok {
		t.Fatalf("data.checks = %T, want a nested object", event.Data["checks"])
	}
	if age, _ := checks["age"].(map[string]any); age["gate"] != float64(21) {
		t.Errorf("data.checks.age.gate = %v, want 21", age["gate"])
	}

	// The data object is a tenant result, so it decodes into SessionResult.
	raw, _ := json.Marshal(event.Data)
	var result SessionResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("decode data into SessionResult: %v", err)
	}
	if !result.ProvesAge(21) || result.ExternalUserID != "cust-4711" {
		t.Errorf("decoded data = %+v, want a 21+ result for cust-4711", result)
	}
}
