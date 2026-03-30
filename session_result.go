package xident

import "encoding/json"

// SessionResult represents the full state of a verification session.
//
// Use the helper methods (IsVerified, IsFailed, IsPending, IsTerminal) to
// check the session outcome. Use AgeBracket and Method to inspect the
// verification details.
type SessionResult struct {
	// ID is the unique session identifier.
	ID string `json:"id"`

	// Status is the current lifecycle state of the session.
	Status SessionStatus `json:"status"`

	// LivenessResult contains the liveness check outcome, if performed.
	LivenessResult json.RawMessage `json:"liveness_result,omitempty"`

	// AgeResult contains the age verification outcome, if performed.
	AgeResult json.RawMessage `json:"age_result,omitempty"`

	// OCRResult contains the document OCR outcome, if performed.
	OCRResult json.RawMessage `json:"ocr_result,omitempty"`

	// FaceMatchResult contains the face matching outcome, if performed.
	FaceMatchResult json.RawMessage `json:"face_match_result,omitempty"`

	// OCRTaskID is the async OCR task identifier, if document verification
	// was triggered.
	OCRTaskID *string `json:"ocr_task_id,omitempty"`

	// CountryCode is the ISO 3166-1 alpha-2 country code determined from
	// the user's IP address.
	CountryCode *string `json:"country_code,omitempty"`

	// Regime is the verification regime applied (e.g., "low", "medium", "strict").
	Regime *string `json:"regime,omitempty"`

	// MinAge is the minimum age threshold for this session.
	MinAge *int `json:"min_age,omitempty"`

	// Purpose is the verification purpose provided at init time.
	Purpose *string `json:"purpose,omitempty"`

	// ExternalUserID is the consumer-provided user ID from the init call.
	ExternalUserID *string `json:"external_user_id,omitempty"`

	// RequiredMethods lists the verification methods required for this session.
	RequiredMethods []string `json:"required_methods,omitempty"`

	// RemainingAttempts is the number of verification attempts remaining
	// before the session is failed.
	RemainingAttempts *int `json:"remaining_attempts,omitempty"`

	// CreatedAt is when the session was created (RFC 3339).
	CreatedAt string `json:"created_at"`

	// StartedAt is when the user started verification (RFC 3339), or empty
	// if not yet started.
	StartedAt *string `json:"started_at,omitempty"`

	// CompletedAt is when the session reached a terminal state (RFC 3339),
	// or empty if still in progress.
	CompletedAt *string `json:"completed_at,omitempty"`

	// ExpiresAt is when the session expires (RFC 3339), or empty if no
	// expiration is set.
	ExpiresAt *string `json:"expires_at,omitempty"`
}

// IsVerified returns true if the session completed successfully, meaning
// the user passed age verification.
func (s *SessionResult) IsVerified() bool {
	return s.Status == SessionStatusCompleted
}

// IsFailed returns true if the session failed verification.
func (s *SessionResult) IsFailed() bool {
	return s.Status == SessionStatusFailed
}

// IsPending returns true if the session is still in progress (pending or
// in_progress). The user has not yet completed verification.
func (s *SessionResult) IsPending() bool {
	return s.Status == SessionStatusPending || s.Status == SessionStatusInProgress
}

// IsTerminal returns true if the session has reached a final state where
// no further changes are possible (completed, failed, canceled, or claimed).
func (s *SessionResult) IsTerminal() bool {
	return s.Status.IsTerminal()
}

// AgeBracket returns the verified age bracket (12, 15, 18, 21, or 25) if
// the age result contains one. Returns nil if not yet determined.
func (s *SessionResult) AgeBracket() *int {
	if s.AgeResult == nil {
		return nil
	}

	var result map[string]any
	if err := json.Unmarshal(s.AgeResult, &result); err != nil {
		return nil
	}

	if bracket, ok := result["verified_bracket"]; ok {
		if v, ok := bracket.(float64); ok {
			i := int(v)
			return &i
		}
	}

	if age, ok := result["estimated_age"]; ok {
		if v, ok := age.(float64); ok {
			i := int(v)
			return &i
		}
	}

	return nil
}

// Method returns the verification method used (e.g., "ml_fast", "ocr",
// "self_declaration"). Returns an empty string if not yet determined.
func (s *SessionResult) Method() string {
	if s.AgeResult == nil {
		return ""
	}

	var result map[string]any
	if err := json.Unmarshal(s.AgeResult, &result); err != nil {
		return ""
	}

	if method, ok := result["method"]; ok {
		if v, ok := method.(string); ok {
			return v
		}
	}

	return ""
}
