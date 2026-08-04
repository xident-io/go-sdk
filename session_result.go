package xident

// SessionResult represents the outcome of a verification session, exactly as
// returned by GET /result/{token} (the secret-key, tenant-facing view).
//
// This mirrors the API's TenantResult contract byte-for-byte: once shipped,
// that contract is FROZEN and additive-only -- fields are never renamed or
// removed, only added. See CHANGELOG.md / README.md "v2 breaking changes" for
// what moved when this SDK adopted it.
//
// Use the helper methods (IsVerified, IsFailed, IsPending, IsTerminal) to
// check the session outcome. Use AgeBracket and Method to inspect the
// verification details.
type SessionResult struct {
	// Token is the result token (xtk_ prefixed) identifying this session.
	//
	// Earlier versions of this SDK bound this field to `json:"id"`, but the
	// wire has always sent `token` -- the old fixtures in this SDK's own test
	// suite happened to send "id" too, so the mismatch never surfaced here.
	// It could still leave a caller's Token empty against the real API.
	Token string `json:"token"`

	// Status is the current state of the session. For a terminal session it
	// is the VERDICT -- SessionStatusSuccess only when the user passed.
	Status SessionStatus `json:"status"`

	// Verified mirrors the verdict as a plain bool. IsVerified() is still the
	// method to call -- it is defined on Status and does not read this field
	// -- but Verified is part of the frozen wire contract, so it is exposed
	// here too for callers who want it directly (e.g. logging, serialization).
	Verified bool `json:"verified"`

	// Reason explains a non-success terminal status. Empty when Status is
	// SessionStatusSuccess.
	//
	// Known values: "age_below_threshold", "dob_unreadable", "face_mismatch",
	// "face_not_detected", "docverify_reject", "blacklist_match". Treat the
	// set as open -- new reasons may be added, so switch with a default.
	Reason string `json:"reason,omitempty"`

	// VerificationMode is the server's internal verification-type distinction
	// for this session: "full" (document + biometric checks ran) or "token"
	// (a returning Xident-ID user, cheap token reuse). This is NOT the same
	// value space as InitParams.VerificationMode ("auto"/"document"/"facial"),
	// which selects a METHOD at session creation time -- this field reports
	// what actually happened. Method() returns this value.
	VerificationMode string `json:"verification_mode,omitempty"`

	// IPCountry is the ISO 3166-1 alpha-2 country the end user connected
	// from, IP-derived. Optional: absent on sessions created before
	// 2026-08-04 and on any session where IP geolocation failed. Distinct
	// from Checks.Document.Country, which is the document's issuing
	// country -- the two can legitimately differ (a passport issued in one
	// country, presented from another).
	IPCountry string `json:"ip_country,omitempty"`

	// ExternalUserID is the consumer-provided user ID from the init call.
	ExternalUserID string `json:"external_user_id,omitempty"`

	// Checks is the per-method breakdown of what ran and whether it passed.
	Checks Checks `json:"checks"`

	// CreatedAt is when the session was created (RFC 3339).
	CreatedAt string `json:"created_at"`

	// CompletedAt is when the session reached a terminal state (RFC 3339),
	// or empty if still in progress.
	CompletedAt string `json:"completed_at,omitempty"`

	// ExpiresAt is when the session expires (RFC 3339), or empty if no
	// expiration is set.
	ExpiresAt string `json:"expires_at,omitempty"`
}

// Checks is the per-check breakdown of a verification session's outcome.
//
// A check that was never attempted has Performed == false; its Passed field
// is then the zero value (false) too, but that is NOT a meaningful "did not
// pass" -- it means the check never ran. Always test Performed before making
// a decision based on Passed, or use AgeBracket() which does this for the
// age check.
type Checks struct {
	// Liveness reports whether a liveness check ran and its outcome.
	Liveness LivenessCheck `json:"liveness"`

	// Age reports whether an age check ran, its outcome, and the threshold
	// (Gate) that was tested.
	Age AgeCheck `json:"age"`

	// Document reports whether document (OCR) verification ran.
	Document DocumentCheck `json:"document"`

	// FaceMatch reports whether a face comparison (selfie vs. document photo)
	// ran. False on a blacklist-match short-circuit, even if an earlier
	// comparison inside the same pipeline run produced a result -- a
	// blacklist hit is reported as skipped, not as a face-match failure.
	FaceMatch FaceMatchCheck `json:"face_match"`
}

// LivenessCheck is a liveness detection outcome.
type LivenessCheck struct {
	Performed bool `json:"performed"`
	Passed    bool `json:"passed"`
}

// AgeCheck is an age verification outcome.
type AgeCheck struct {
	Performed bool `json:"performed"`
	Passed    bool `json:"passed"`

	// Gate is the age threshold that was tested (12, 15, 18, 21, or 25).
	// Meaningful only when Performed is true; use AgeBracket() rather than
	// reading Gate directly -- it applies the Passed guard for you.
	Gate int `json:"gate,omitempty"`
}

// DocumentCheck is a document (OCR) verification outcome.
type DocumentCheck struct {
	Performed bool `json:"performed"`
	Passed    bool `json:"passed"`

	// DocumentType is the kind of document read (e.g. "passport",
	// "drivers_license"), set when document verification was performed.
	DocumentType string `json:"document_type,omitempty"`

	// Country is the ISO 3166-1 alpha-2 country code the session's IP
	// resolved to. Set regardless of Performed -- it is IP-derived, not
	// something the document itself establishes.
	Country string `json:"country,omitempty"`
}

// FaceMatchCheck is a face-comparison outcome (selfie against document photo).
type FaceMatchCheck struct {
	Performed bool `json:"performed"`
	Passed    bool `json:"passed"`
}

// IsVerified returns true if the user PASSED verification.
//
// This is the check to gate on. It is false for a session that ran all the
// way through the flow but did not meet the age threshold -- that session is
// SessionStatusFailed with Reason "age_below_threshold".
func (s *SessionResult) IsVerified() bool {
	return s.Status == SessionStatusSuccess
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

// AgeBracket returns the age threshold the session PROVED the user is above
// (12, 15, 18, 21, or 25), or nil if the age check did not run or did not
// pass.
//
// It deliberately does not distinguish "never ran" from "ran and failed" --
// both are "no proven bracket", and coercing either into a number would hand
// the caller a threshold nobody actually cleared.
func (s *SessionResult) AgeBracket() *int {
	if !s.Checks.Age.Passed {
		return nil
	}
	gate := s.Checks.Age.Gate
	return &gate
}

// Method returns the verification mode used for this session ("full" for a
// document + biometric check, "token" for a returning Xident-ID user).
// Returns an empty string if the server did not send one.
func (s *SessionResult) Method() string {
	return s.VerificationMode
}
