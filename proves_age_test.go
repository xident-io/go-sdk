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
//   - testdata/tenant_result_xident_id_reuse.json is the result of a passed
//     Xident ID reuse, built from the API code on api PR #45 at c2d6890
//     (internal/api/handlers/verify/account_reuse.go completes the session
//     with kind xident_id and reason xident_id_reused;
//     session_result_view.go buildResultChecks sets checks.age.gate from the
//     session's min_age and sets performed and passed only from an age result
//     or a document date of birth, which a reuse never stores). So: verified,
//     gate 21, every check performed false and passed false.
//   - testdata/tenant_result_eu_wallet.json is a passed EU wallet
//     presentation, built from the same code at c2d6890 (wallet.go stores
//     only the wallet result; the browser completion writes reason
//     all_methods_verified): verification_type eu_wallet, checks.eu_wallet
//     performed and passed, checks.age performed and passed false, gate 21.
//   - testdata/tenant_result_test_mode.json is a test-key settle
//     (requirements.go settleTestSession): reason test_mode,
//     verification_type age_check, every check performed and passed false,
//     gate 21, and "test": true.
//   - testdata/webhook_session_success_{age,id,reuse,eu_wallet,test_mode}.json
//     are the webhook envelopes the API sends around those five results
//     (webhook_service.go buildSessionEventPayload: id, type, api_version,
//     created, data = the tenant result).

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
// a passed result whose gate reaches the site's own required age.
//
// Mutations caught: dropping the IsVerified check; comparing with > instead
// of >=; treating a missing gate (an ID verification) as a pass; returning
// true for a required age of 0; requiring checks.age.passed or performed
// (the reuse rows fail, because a reuse stores no new age evidence).
func TestSessionResult_ProvesAge(t *testing.T) {
	age21 := readResultFixture(t, "testdata/tenant_result_v1.golden.json")
	idOnly := readResultFixture(t, "testdata/tenant_result_id_no_gate.json")
	reuse := readResultFixture(t, "testdata/tenant_result_xident_id_reuse.json")
	wallet := readResultFixture(t, "testdata/tenant_result_eu_wallet.json")
	testMode := readResultFixture(t, "testdata/tenant_result_test_mode.json")

	if age21.Checks.Age.Gate != 21 {
		t.Fatalf("fixture gate = %d, want 21: the golden file changed", age21.Checks.Age.Gate)
	}
	if idOnly.Checks.Age.Gate != 0 || !idOnly.Checks.Age.Passed || !idOnly.IsVerified() {
		t.Fatalf("ID fixture = %+v, want a passed result with no gate", idOnly.Checks.Age)
	}
	if reuse.Checks.Age.Gate != 21 || reuse.Checks.Age.Performed || reuse.Checks.Age.Passed ||
		!reuse.IsVerified() || reuse.Method() != "xident_id" {
		t.Fatalf("reuse fixture = %+v (%s), want a passed xident_id result with gate 21 and no age evidence",
			reuse.Checks.Age, reuse.Method())
	}

	if wallet.Checks.Age.Gate != 21 || wallet.Checks.Age.Passed || wallet.Method() != "eu_wallet" || wallet.Test {
		t.Fatalf("wallet fixture = %+v (%s), want a live eu_wallet result with gate 21", wallet.Checks.Age, wallet.Method())
	}
	if testMode.Checks.Age.Gate != 21 || !testMode.Test || !testMode.IsVerified() {
		t.Fatalf("test-mode fixture = %+v test=%v, want a passed test result with gate 21", testMode.Checks.Age, testMode.Test)
	}
	if age21.Test || idOnly.Test || reuse.Test {
		t.Fatal("the live fixtures must not decode as test results")
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
		{"a Xident ID reuse with gate 21 proves 21", reuse, 21, true},
		{"a Xident ID reuse with gate 21 proves 18", reuse, 18, true},
		{"a Xident ID reuse with gate 21 does not prove 25", reuse, 25, false},
		{"an EU wallet result with gate 21 proves 21", wallet, 21, true},
		{"an EU wallet result with gate 21 proves 18", wallet, 18, true},
		{"an EU wallet result with gate 21 does not prove 25", wallet, 25, false},
		{"a test-key result proves nothing, gate 21 against 18", testMode, 18, false},
		{"a test-key result proves nothing, gate 21 against 21", testMode, 21, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.result.ProvesAge(tt.minAge); got != tt.want {
				t.Errorf("ProvesAge(%d) = %v, want %v", tt.minAge, got, tt.want)
			}
		})
	}
}

// TestSessionResult_ProvesAgeAllowingTest pins the opt-in for local
// development: a test-key result counts only through ProvesAgeAllowingTest,
// and an ID verification proves no age even there.
//
// Mutations caught: ProvesAge accepting a test result; the opt-in ignoring
// the gate or the verdict.
func TestSessionResult_ProvesAgeAllowingTest(t *testing.T) {
	testMode := readResultFixture(t, "testdata/tenant_result_test_mode.json")
	idOnly := readResultFixture(t, "testdata/tenant_result_id_no_gate.json")
	reuse := readResultFixture(t, "testdata/tenant_result_xident_id_reuse.json")

	tests := []struct {
		name   string
		result *SessionResult
		minAge int
		want   bool
	}{
		{"a test-key result with gate 21 proves 21 with the opt-in", testMode, 21, true},
		{"a test-key result with gate 21 proves 18 with the opt-in", testMode, 18, true},
		{"a test-key result with gate 21 does not prove 25, even with the opt-in", testMode, 25, false},
		{"an ID verification proves no age, even with the opt-in", idOnly, 18, false},
		{"a live reuse result is accepted with the opt-in too", reuse, 21, true},
		{"a required age of 0 is never proven, even with the opt-in", testMode, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.result.ProvesAgeAllowingTest(tt.minAge); got != tt.want {
				t.Errorf("ProvesAgeAllowingTest(%d) = %v, want %v", tt.minAge, got, tt.want)
			}
		})
	}

	failedTest := readResultFixture(t, "testdata/tenant_result_test_mode.json")
	failedTest.Status = SessionStatusFailed
	if failedTest.ProvesAgeAllowingTest(18) {
		t.Error("ProvesAgeAllowingTest(18) = true for a failed test session, want false")
	}
}

// TestSessionResult_ProvesAge_NeedsEveryPart changes one part of a passing
// result at a time: each change alone must turn the answer to false.
func TestSessionResult_ProvesAge_NeedsEveryPart(t *testing.T) {
	cases := map[string]func(s *SessionResult){
		"session failed":         func(s *SessionResult) { s.Status = SessionStatusFailed },
		"session pending":        func(s *SessionResult) { s.Status = SessionStatusPending },
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
				t.Errorf("ProvesAge(%d) = true after %q, want false", 21, name)
			}
		})
	}
}

// TestSessionResult_ProvesAge_IgnoresAgeEvidenceFlags pins that the age
// check's own performed and passed flags do not decide. Only the verdict and
// the gate do: a Xident ID reuse has neither flag set, and a failed session
// can have both set.
func TestSessionResult_ProvesAge_IgnoresAgeEvidenceFlags(t *testing.T) {
	s := readResultFixture(t, "testdata/tenant_result_v1.golden.json")
	s.Checks.Age.Performed = false
	s.Checks.Age.Passed = false
	if !s.ProvesAge(21) {
		t.Error("ProvesAge(21) = false for a passed result with gate 21 and no age evidence flags, want true")
	}

	failed := readResultFixture(t, "testdata/tenant_result_v1.golden.json")
	failed.Status = SessionStatusFailed
	failed.Verified = false
	if failed.ProvesAge(18) {
		t.Error("ProvesAge(18) = true for a failed session whose age check passed, want false")
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

// TestWebhookEvent_SessionResult reads the result out of each real webhook
// the API sends and makes the age decision a webhook handler should make.
//
// Mutations caught: SessionResult returning an empty result; ProvesAge
// requiring checks.age.passed (the reuse webhook then proves nothing).
func TestWebhookEvent_SessionResult(t *testing.T) {
	client, _, teardown := setup()
	defer teardown()
	secret := "whsec_fixture"

	tests := []struct {
		fixture  string
		wantType string
		proves21 bool
		proves25 bool
	}{
		{"testdata/webhook_session_success_age.json", "full", true, false},
		{"testdata/webhook_session_success_reuse.json", "xident_id", true, false},
		{"testdata/webhook_session_success_eu_wallet.json", "eu_wallet", true, false},
		{"testdata/webhook_session_success_id.json", "full", false, false},
		{"testdata/webhook_session_success_test_mode.json", "age_check", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			payload, err := os.ReadFile(tt.fixture)
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			event, err := client.Webhooks.ConstructEvent(payload, makeTestSignature(string(payload), secret, time.Now().Unix()), secret)
			if err != nil {
				t.Fatalf("ConstructEvent() error: %v", err)
			}
			result, err := event.SessionResult()
			if err != nil {
				t.Fatalf("SessionResult() error: %v", err)
			}
			if result.ExternalUserID != "cust-4711" || result.Method() != tt.wantType || !result.IsVerified() {
				t.Errorf("result = %s %s %s, want cust-4711 %s success",
					result.ExternalUserID, result.Method(), result.Status, tt.wantType)
			}
			if got := result.ProvesAge(21); got != tt.proves21 {
				t.Errorf("ProvesAge(21) = %v, want %v", got, tt.proves21)
			}
			if got := result.ProvesAge(25); got != tt.proves25 {
				t.Errorf("ProvesAge(25) = %v, want %v", got, tt.proves25)
			}
		})
	}
}

// TestWebhookEvent_SessionResult_BadData refuses data that is not a result,
// instead of handing back an empty result a caller might act on.
func TestWebhookEvent_SessionResult_BadData(t *testing.T) {
	event := &WebhookEvent{Type: "session.success", Data: map[string]any{"checks": "not an object"}}
	if result, err := event.SessionResult(); err == nil {
		t.Fatalf("SessionResult() = %+v, nil; want an error", result)
	}
}
