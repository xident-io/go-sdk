package xident

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

func TestSessionResult_IsVerified(t *testing.T) {
	tests := []struct {
		status SessionStatus
		want   bool
	}{
		{SessionStatusSuccess, true},
		{SessionStatusFailed, false},
		{SessionStatusPending, false},
		{SessionStatusInProgress, false},
		{SessionStatusCanceled, false},
		{SessionStatusClaimed, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			s := &SessionResult{Status: tt.status}
			if got := s.IsVerified(); got != tt.want {
				t.Errorf("IsVerified() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSessionResult_IsFailed(t *testing.T) {
	tests := []struct {
		status SessionStatus
		want   bool
	}{
		{SessionStatusFailed, true},
		{SessionStatusSuccess, false},
		{SessionStatusPending, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			s := &SessionResult{Status: tt.status}
			if got := s.IsFailed(); got != tt.want {
				t.Errorf("IsFailed() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSessionResult_IsPending(t *testing.T) {
	tests := []struct {
		status SessionStatus
		want   bool
	}{
		{SessionStatusPending, true},
		{SessionStatusInProgress, true},
		{SessionStatusSuccess, false},
		{SessionStatusFailed, false},
		{SessionStatusCanceled, false},
		{SessionStatusClaimed, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			s := &SessionResult{Status: tt.status}
			if got := s.IsPending(); got != tt.want {
				t.Errorf("IsPending() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSessionResult_IsTerminal(t *testing.T) {
	tests := []struct {
		status SessionStatus
		want   bool
	}{
		{SessionStatusSuccess, true},
		{SessionStatusFailed, true},
		{SessionStatusCanceled, true},
		{SessionStatusClaimed, true},
		{SessionStatusPending, false},
		{SessionStatusInProgress, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			s := &SessionResult{Status: tt.status}
			if got := s.IsTerminal(); got != tt.want {
				t.Errorf("IsTerminal() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestSessionResult_ParsesGoldenFixture decodes the byte-for-byte copy of the
// API's frozen v1 tenant result contract (api/internal/domain/services/testdata
// /tenant_result_v1.golden.json, copied verbatim into ./testdata here).
//
// This is the test that catches the pre-v1 bug: SessionResult used to bind
// `json:"id"`, but the wire has always sent `token`. Because the old fixtures
// in this SDK's own tests sent "id", the bug never showed up here -- it only
// bit an integrator reading a real API response. Asserting Token against the
// real contract's "token" field is what pins the fix.
func TestSessionResult_ParsesGoldenFixture(t *testing.T) {
	data, err := os.ReadFile("testdata/tenant_result_v1.golden.json")
	if err != nil {
		t.Fatalf("read golden fixture: %v", err)
	}

	var s SessionResult
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("unmarshal golden fixture: %v", err)
	}

	if s.Token != "xtk_golden0001" {
		t.Errorf("Token = %q, want %q", s.Token, "xtk_golden0001")
	}
	if s.Status != SessionStatusSuccess {
		t.Errorf("Status = %q, want %q", s.Status, SessionStatusSuccess)
	}
	if !s.Verified {
		t.Error("Verified = false, want true")
	}
	if s.Reason != "" {
		t.Errorf("Reason = %q, want empty (this session succeeded)", s.Reason)
	}
	if s.VerificationType != "full" {
		t.Errorf("VerificationType = %q, want %q", s.VerificationType, "full")
	}
	if s.IPCountry != "DE" {
		t.Errorf("IPCountry = %q, want %q", s.IPCountry, "DE")
	}
	if s.ExternalUserID != "cust-4711" {
		t.Errorf("ExternalUserID = %q, want %q", s.ExternalUserID, "cust-4711")
	}

	if !s.Checks.Liveness.Performed || !s.Checks.Liveness.Passed {
		t.Errorf("Checks.Liveness = %+v, want {Performed:true Passed:true}", s.Checks.Liveness)
	}

	if !s.Checks.Age.Performed || !s.Checks.Age.Passed {
		t.Errorf("Checks.Age = %+v, want Performed/Passed true", s.Checks.Age)
	}
	if s.Checks.Age.Gate != 21 {
		t.Errorf("Checks.Age.Gate = %d, want 21", s.Checks.Age.Gate)
	}

	if !s.Checks.Document.Performed || !s.Checks.Document.Passed {
		t.Errorf("Checks.Document = %+v, want Performed/Passed true", s.Checks.Document)
	}
	if s.Checks.Document.DocumentType != "passport" {
		t.Errorf("Checks.Document.DocumentType = %q, want %q", s.Checks.Document.DocumentType, "passport")
	}
	if s.Checks.Document.Country != "DE" {
		t.Errorf("Checks.Document.Country = %q, want %q", s.Checks.Document.Country, "DE")
	}

	if !s.Checks.FaceMatch.Performed || !s.Checks.FaceMatch.Passed {
		t.Errorf("Checks.FaceMatch = %+v, want Performed/Passed true", s.Checks.FaceMatch)
	}

	if s.CreatedAt != "2026-08-03T10:00:00Z" {
		t.Errorf("CreatedAt = %q, want %q", s.CreatedAt, "2026-08-03T10:00:00Z")
	}
	if s.CompletedAt != "2026-08-03T10:02:30Z" {
		t.Errorf("CompletedAt = %q, want %q", s.CompletedAt, "2026-08-03T10:02:30Z")
	}
	if s.ExpiresAt != "2026-08-03T10:15:00Z" {
		t.Errorf("ExpiresAt = %q, want %q", s.ExpiresAt, "2026-08-03T10:15:00Z")
	}

	if !s.IsVerified() {
		t.Error("IsVerified() = false, want true")
	}

	bracket := s.AgeBracket()
	if bracket == nil || *bracket != 21 {
		t.Errorf("AgeBracket() = %v, want 21", bracket)
	}

	// VerificationType reports which PATH produced the verdict: "full"
	// (document + face match), "age_check" (browser-only), "xident_id"
	// (Xident-ID reuse) or "eu_wallet". NOT the "auto"/"document"/"facial"
	// method-selection override InitParams.VerificationMode sends at session
	// creation. Method() surfaces whichever string the wire sent. This fixture
	// is a document session, so "full" is the expected value.
	if got := s.Method(); got != "full" {
		t.Errorf("Method() = %q, want %q", got, "full")
	}
}

// TestSessionResult_TolerantOfLegacyVerbosePayload pins forward/backward
// compatibility across the v1 rollout: a tenant's integration may still be
// pointed at an old deployment (or receive a cached/replayed old-shape
// response) that sends the pre-v1 blob fields -- id, liveness_result,
// age_result, ocr_result, face_match_result, ocr_task_id, country_code,
// regime, min_age, purpose, required_methods, remaining_attempts, started_at
// -- none of which exist on the new struct.
//
// encoding/json silently ignores JSON object keys with no matching struct
// field (it does not opt into DisallowUnknownFields), so this must decode
// without error, and the fields the two shapes share (status, reason,
// external_user_id, created_at, completed_at) must still populate. It must
// NOT populate Checks or VerificationType -- those are new v1-only fields
// absent from the old payload, so they stay at their zero value rather than
// (incorrectly) inheriting anything from the old blob fields.
func TestSessionResult_TolerantOfLegacyVerbosePayload(t *testing.T) {
	oldPayload := []byte(`{
		"id": "sess_legacy_1",
		"status": "success",
		"liveness_result": {"passed": true},
		"age_result": {"verified_bracket": 18, "method": "ml_fast"},
		"ocr_result": null,
		"face_match_result": null,
		"ocr_task_id": null,
		"country_code": "US",
		"regime": "medium",
		"min_age": 18,
		"purpose": "age_verification",
		"external_user_id": "cust-legacy",
		"required_methods": ["liveness", "age"],
		"remaining_attempts": 3,
		"created_at": "2026-03-23T12:00:00Z",
		"started_at": "2026-03-23T12:00:05Z",
		"completed_at": "2026-03-23T12:01:00Z",
		"expires_at": "2026-03-23T12:10:00Z"
	}`)

	var s SessionResult
	if err := json.Unmarshal(oldPayload, &s); err != nil {
		t.Fatalf("unmarshal legacy verbose payload: %v (must decode without error during the rollout window)", err)
	}

	if !s.IsVerified() {
		t.Error("IsVerified() = false, want true -- Status alone must still drive the verdict")
	}
	if s.ExternalUserID != "cust-legacy" {
		t.Errorf("ExternalUserID = %q, want %q (field name unchanged across v1)", s.ExternalUserID, "cust-legacy")
	}
	if s.CreatedAt != "2026-03-23T12:00:00Z" {
		t.Errorf("CreatedAt = %q, want %q", s.CreatedAt, "2026-03-23T12:00:00Z")
	}
	if s.CompletedAt != "2026-03-23T12:01:00Z" {
		t.Errorf("CompletedAt = %q, want %q", s.CompletedAt, "2026-03-23T12:01:00Z")
	}

	// The old payload's age_result blob has no v1 "checks" object, so the new
	// helpers must report "unknown", never a value coerced out of the legacy
	// shape.
	if bracket := s.AgeBracket(); bracket != nil {
		t.Errorf("AgeBracket() = %d, want nil -- no v1 checks object was present", *bracket)
	}
	if method := s.Method(); method != "" {
		t.Errorf("Method() = %q, want empty -- no v1 verification_type was present", method)
	}
}

// TestSessionResult_AgeBracket exercises the new derivation: the bracket is
// the Gate the session tested, surfaced only when Age.Passed is true. A
// performed-but-failed age check, or one never performed at all, both report
// "unknown" -- coercing either into a number would hand a caller a threshold
// nobody actually cleared.
func TestSessionResult_AgeBracket(t *testing.T) {
	tests := []struct {
		name   string
		status SessionStatus
		test   bool
		check  AgeCheck
		want   *int
	}{
		{
			name:   "passed session, age check passed, gate 18",
			status: SessionStatusSuccess,
			check:  AgeCheck{Performed: true, Passed: true, Gate: 18},
			want:   intPtr(18),
		},
		{
			name:   "passed session, gate 12",
			status: SessionStatusSuccess,
			check:  AgeCheck{Performed: true, Passed: true, Gate: 12},
			want:   intPtr(12),
		},
		{
			// A Xident ID reuse or an EU wallet presentation: the session
			// passed on an age proven elsewhere, so its own age check did not
			// run. The gate is the band the API stands behind.
			name:   "passed session, age check not performed, gate 21",
			status: SessionStatusSuccess,
			check:  AgeCheck{Gate: 21},
			want:   intPtr(21),
		},
		{
			// The same rule as ProvesAge: a failed session proves no band,
			// whatever its age check said.
			name:   "failed session, age check passed, gate 21",
			status: SessionStatusFailed,
			check:  AgeCheck{Performed: true, Passed: true, Gate: 21},
			want:   nil,
		},
		{
			name:   "pending session, gate 18",
			status: SessionStatusPending,
			check:  AgeCheck{Gate: 18},
			want:   nil,
		},
		{
			name:   "passed session, no age check at all",
			status: SessionStatusSuccess,
			check:  AgeCheck{},
			want:   nil,
		},
		{
			// An ID verification tests no age threshold, so the API sends no
			// gate, while a read date of birth makes the check performed and
			// passed. 0 is not a proven age.
			name:   "passed ID verification, no gate",
			status: SessionStatusSuccess,
			check:  AgeCheck{Performed: true, Passed: true},
			want:   nil,
		},
		{
			name:   "passed test-key session, gate 21",
			status: SessionStatusSuccess,
			test:   true,
			check:  AgeCheck{Gate: 21},
			want:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &SessionResult{Status: tt.status, Test: tt.test, Checks: Checks{Age: tt.check}}
			got := s.AgeBracket()

			if tt.want == nil {
				if got != nil {
					t.Errorf("AgeBracket() = %d, want nil", *got)
				}
				return
			}
			if got == nil {
				t.Fatalf("AgeBracket() = nil, want %d", *tt.want)
			}
			if *got != *tt.want {
				t.Errorf("AgeBracket() = %d, want %d", *got, *tt.want)
			}
		})
	}
}

// TestSessionResult_AgeBracket_Goldens runs AgeBracket over the five real
// result shapes (see proves_age_test.go for where each comes from) and checks
// it never disagrees with ProvesAge.
//
// Mutations caught: requiring checks.age.passed (the reuse and wallet rows
// fail); dropping the IsVerified check; dropping the test-key check;
// returning a pointer to 0 for the ID result.
func TestSessionResult_AgeBracket_Goldens(t *testing.T) {
	tests := []struct {
		file          string
		want          *int // AgeBracket()
		wantAllowTest *int // AgeBracketAllowingTest()
	}{
		{"testdata/tenant_result_v1.golden.json", intPtr(21), intPtr(21)},
		{"testdata/tenant_result_xident_id_reuse.json", intPtr(21), intPtr(21)},
		{"testdata/tenant_result_eu_wallet.json", intPtr(21), intPtr(21)},
		{"testdata/tenant_result_id_no_gate.json", nil, nil},
		{"testdata/tenant_result_test_mode.json", nil, intPtr(21)},
	}
	same := func(a, b *int) bool { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }
	show := func(p *int) string {
		if p == nil {
			return "nil"
		}
		return fmt.Sprint(*p)
	}

	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			s := readResultFixture(t, tt.file)
			if got := s.AgeBracket(); !same(got, tt.want) {
				t.Errorf("AgeBracket() = %s, want %s", show(got), show(tt.want))
			}
			if got := s.AgeBracketAllowingTest(); !same(got, tt.wantAllowTest) {
				t.Errorf("AgeBracketAllowingTest() = %s, want %s", show(got), show(tt.wantAllowTest))
			}
			// AgeBracket and ProvesAge must agree for every age.
			for _, age := range []int{12, 15, 18, 21, 25} {
				b := s.AgeBracket()
				if want := b != nil && *b >= age; s.ProvesAge(age) != want {
					t.Errorf("ProvesAge(%d) = %v, but AgeBracket() = %s", age, s.ProvesAge(age), show(b))
				}
				ba := s.AgeBracketAllowingTest()
				if want := ba != nil && *ba >= age; s.ProvesAgeAllowingTest(age) != want {
					t.Errorf("ProvesAgeAllowingTest(%d) = %v, but AgeBracketAllowingTest() = %s",
						age, s.ProvesAgeAllowingTest(age), show(ba))
				}
			}
		})
	}
}

// TestSessionResult_AgeBracket_ReturnsACopy guards against a caller mutating
// the pointer AgeBracket returns and corrupting the SessionResult it came
// from -- the two must not alias the same int.
func TestSessionResult_AgeBracket_ReturnsACopy(t *testing.T) {
	s := &SessionResult{Status: SessionStatusSuccess, Checks: Checks{Age: AgeCheck{Performed: true, Passed: true, Gate: 18}}}

	got := s.AgeBracket()
	if got == nil {
		t.Fatal("AgeBracket() = nil, want a pointer to 18")
	}
	*got = 99

	if s.Checks.Age.Gate != 18 {
		t.Errorf("Checks.Age.Gate = %d after mutating the returned pointer, want 18 (AgeBracket must return a copy)", s.Checks.Age.Gate)
	}
}

// TestSessionResult_Method covers the new, much simpler derivation: it is a
// direct field read, not a blob parse, so there is no malformed-JSON case to
// guard -- an empty string means "the server didn't send one" and needs no
// error handling on the caller's part.
func TestSessionResult_Method(t *testing.T) {
	tests := []struct {
		name string
		mode string
		want string
	}{
		{name: "full", mode: "full", want: "full"},
		{name: "token", mode: "token", want: "token"},
		{name: "empty", mode: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &SessionResult{VerificationType: tt.mode}
			if got := s.Method(); got != tt.want {
				t.Errorf("Method() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSessionResult_JSONRoundtrip(t *testing.T) {
	// Verify that a session result can be marshaled and unmarshaled.
	original := &SessionResult{
		Token:            "xtk_test",
		Status:           SessionStatusSuccess,
		Verified:         true,
		VerificationType: "full",
		ExternalUserID:   "cust-1",
		Checks: Checks{
			Age: AgeCheck{Performed: true, Passed: true, Gate: 18},
		},
		CreatedAt:   "2026-03-23T12:00:00Z",
		CompletedAt: "2026-03-23T12:01:00Z",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded SessionResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if decoded.Token != original.Token {
		t.Errorf("Token = %q, want %q", decoded.Token, original.Token)
	}
	if decoded.Status != original.Status {
		t.Errorf("Status = %q, want %q", decoded.Status, original.Status)
	}
	if !decoded.IsVerified() {
		t.Error("IsVerified() should be true after roundtrip")
	}
	bracket := decoded.AgeBracket()
	if bracket == nil || *bracket != 18 {
		t.Errorf("AgeBracket() after roundtrip = %v, want 18", bracket)
	}
}

// Helper function for test pointers.
func intPtr(i int) *int { return &i }

// TestSessionResult_IDVerificationWithoutGate parses the result shape an ID
// verification has from the 2026-10 release: the session stores min_age 0,
// so checks.age.gate is absent, while the document's date of birth makes the
// age check performed and passed. The result must parse, report the pass,
// offer no bracket, and write no gate back out.
//
// Mutation caught: dropping the Gate guard in AgeBracket (it then returns a
// pointer to 0, a proven age nobody tested).
func TestSessionResult_IDVerificationWithoutGate(t *testing.T) {
	const payload = `{
		"token": "xtk_id_1",
		"status": "success",
		"verified": true,
		"verification_type": "full",
		"external_user_id": "usr_1",
		"checks": {
			"liveness":   {"performed": true, "passed": true},
			"age":        {"performed": true, "passed": true},
			"document":   {"performed": true, "passed": true, "document_type": "passport", "country": "DE"},
			"face_match": {"performed": true, "passed": true}
		},
		"created_at": "2026-10-05T10:00:00Z",
		"completed_at": "2026-10-05T10:02:00Z"
	}`

	var s SessionResult
	if err := json.Unmarshal([]byte(payload), &s); err != nil {
		t.Fatalf("Unmarshal() error: %v", err)
	}
	if !s.IsVerified() {
		t.Error("IsVerified() = false, want true")
	}
	if !s.Checks.Age.Performed || !s.Checks.Age.Passed {
		t.Errorf("Checks.Age = %+v, want performed and passed", s.Checks.Age)
	}
	if s.Checks.Age.Gate != 0 {
		t.Errorf("Checks.Age.Gate = %d, want 0 (absent)", s.Checks.Age.Gate)
	}
	if b := s.AgeBracket(); b != nil {
		t.Errorf("AgeBracket() = %d, want nil: an ID verification proves no age band", *b)
	}

	out, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("Marshal() error: %v", err)
	}
	var back map[string]any
	if err := json.Unmarshal(out, &back); err != nil {
		t.Fatalf("Unmarshal(roundtrip) error: %v", err)
	}
	age := back["checks"].(map[string]any)["age"].(map[string]any)
	if _, present := age["gate"]; present {
		t.Errorf("roundtrip wrote checks.age.gate = %v; the wire shape has no gate for an ID verification", age["gate"])
	}
}
