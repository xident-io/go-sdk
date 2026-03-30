package xident

import (
	"encoding/json"
	"testing"
)

func TestSessionResult_IsVerified(t *testing.T) {
	tests := []struct {
		status SessionStatus
		want   bool
	}{
		{SessionStatusCompleted, true},
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
		{SessionStatusCompleted, false},
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
		{SessionStatusCompleted, false},
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
		{SessionStatusCompleted, true},
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

func TestSessionResult_AgeBracket(t *testing.T) {
	tests := []struct {
		name      string
		ageResult json.RawMessage
		want      *int
	}{
		{
			name:      "verified bracket",
			ageResult: json.RawMessage(`{"verified_bracket": 18, "method": "ml_fast"}`),
			want:      intPtr(18),
		},
		{
			name:      "estimated age fallback",
			ageResult: json.RawMessage(`{"estimated_age": 25}`),
			want:      intPtr(25),
		},
		{
			name:      "nil age result",
			ageResult: nil,
			want:      nil,
		},
		{
			name:      "empty object",
			ageResult: json.RawMessage(`{}`),
			want:      nil,
		},
		{
			name:      "bracket 12",
			ageResult: json.RawMessage(`{"verified_bracket": 12}`),
			want:      intPtr(12),
		},
		{
			name:      "bracket 21",
			ageResult: json.RawMessage(`{"verified_bracket": 21}`),
			want:      intPtr(21),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &SessionResult{AgeResult: tt.ageResult}
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

func TestSessionResult_Method(t *testing.T) {
	tests := []struct {
		name      string
		ageResult json.RawMessage
		want      string
	}{
		{
			name:      "ml_fast",
			ageResult: json.RawMessage(`{"method": "ml_fast", "verified_bracket": 18}`),
			want:      "ml_fast",
		},
		{
			name:      "ocr",
			ageResult: json.RawMessage(`{"method": "ocr"}`),
			want:      "ocr",
		},
		{
			name:      "nil age result",
			ageResult: nil,
			want:      "",
		},
		{
			name:      "no method field",
			ageResult: json.RawMessage(`{"verified_bracket": 18}`),
			want:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &SessionResult{AgeResult: tt.ageResult}
			if got := s.Method(); got != tt.want {
				t.Errorf("Method() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSessionResult_JSONRoundtrip(t *testing.T) {
	// Verify that a session result can be marshaled and unmarshaled.
	original := &SessionResult{
		ID:          "sess_test",
		Status:      SessionStatusCompleted,
		AgeResult:   json.RawMessage(`{"verified_bracket":18,"method":"ml_fast"}`),
		CountryCode: strPtr("US"),
		MinAge:      intPtr(18),
		CreatedAt:   "2026-03-23T12:00:00Z",
		CompletedAt: strPtr("2026-03-23T12:01:00Z"),
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded SessionResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if decoded.ID != original.ID {
		t.Errorf("ID = %q, want %q", decoded.ID, original.ID)
	}
	if decoded.Status != original.Status {
		t.Errorf("Status = %q, want %q", decoded.Status, original.Status)
	}
	if !decoded.IsVerified() {
		t.Error("IsVerified() should be true after roundtrip")
	}
}

// Helper functions for test pointers.
func intPtr(i int) *int       { return &i }
func strPtr(s string) *string { return &s }
