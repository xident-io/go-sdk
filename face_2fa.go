package xident

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// Face2FAService provides methods for the standalone face 2FA product:
// enroll a face for one of your users, then verify later logins against it.
//
// Both Register and Verify are asynchronous -- they return a challenge that
// you poll with GetStatus until it reaches a terminal state. The API never
// returns confidence scores or biometric data, only pass/fail plus a failure
// reason.
//
// Access this service through Client.Face2FA:
//
//	client := xident.NewClient("sk_live_xxx")
//	challenge, _, err := client.Face2FA.Register(ctx, &xident.Face2FAParams{
//	    UserID: "usr_123",
//	    Image:  base64Selfie,
//	})
type Face2FAService service

// Face2FAStatus represents the lifecycle state of a face 2FA challenge.
type Face2FAStatus string

const (
	// Face2FAStatusProcessing means the challenge was accepted and the
	// worker has not finished processing it yet. Keep polling.
	Face2FAStatusProcessing Face2FAStatus = "processing"

	// Face2FAStatusCompleted means the challenge PASSED: the face was
	// enrolled (register) or matched the enrolled face (verify).
	Face2FAStatusCompleted Face2FAStatus = "completed"

	// Face2FAStatusFailed means the challenge did not pass. Inspect
	// FailureReason for why.
	Face2FAStatusFailed Face2FAStatus = "failed"

	// Face2FAStatusExpired means the challenge was not processed within its
	// lifetime. Submit a new one.
	Face2FAStatusExpired Face2FAStatus = "expired"
)

// IsTerminal returns true if the challenge has reached a final state where
// no further changes are possible (completed, failed, or expired).
func (s Face2FAStatus) IsTerminal() bool {
	switch s {
	case Face2FAStatusCompleted, Face2FAStatusFailed, Face2FAStatusExpired:
		return true
	default:
		return false
	}
}

// Face2FAFailureReason explains why a face 2FA challenge failed.
//
// Treat the set as open -- new reasons may be added, so switch with a default.
type Face2FAFailureReason string

const (
	// Face2FAFailInvalidImage means the submitted image could not be decoded.
	Face2FAFailInvalidImage Face2FAFailureReason = "invalid_image"

	// Face2FAFailNoFaceDetected means no face was found in the image.
	Face2FAFailNoFaceDetected Face2FAFailureReason = "no_face_detected"

	// Face2FAFailNotEnrolled means Verify was called for a user who has no
	// enrolled face. Call Register first.
	Face2FAFailNotEnrolled Face2FAFailureReason = "not_enrolled"

	// Face2FAFailFaceMismatch means the submitted face did not match the
	// enrolled face.
	Face2FAFailFaceMismatch Face2FAFailureReason = "face_mismatch"

	// Face2FAFailBlacklistMatch means the submitted face matched an entry on
	// your face blacklist.
	Face2FAFailBlacklistMatch Face2FAFailureReason = "blacklist_match"

	// Face2FAFailExpired means the challenge expired before processing
	// finished.
	Face2FAFailExpired Face2FAFailureReason = "expired"

	// Face2FAFailInternalError means processing failed on the Xident side.
	// Safe to retry with a new challenge.
	Face2FAFailInternalError Face2FAFailureReason = "internal_error"
)

// Face 2FA challenge kinds, as reported in Face2FAChallengeStatus.Kind.
const (
	// Face2FAKindEnroll is a challenge created by Register.
	Face2FAKindEnroll = "enroll"

	// Face2FAKindVerify is a challenge created by Verify.
	Face2FAKindVerify = "verify"
)

// Face2FAParams contains the parameters for Register and Verify. Both fields
// are required.
type Face2FAParams struct {
	// UserID is YOUR user identifier -- an opaque string in your own
	// namespace, max 255 chars. Xident never interprets it.
	UserID string `json:"user_id"`

	// Image is the face image as a base64 string (max ~10MB of base64,
	// roughly 7.5MB decoded -- the same ceiling as document uploads).
	Image string `json:"image"`
}

// Face2FAChallenge is the immediate result of Register or Verify: the
// challenge to poll with GetStatus.
type Face2FAChallenge struct {
	// ChallengeID identifies the async challenge. Pass it to GetStatus.
	ChallengeID string `json:"challenge_id"`

	// Status is the initial challenge state, always
	// Face2FAStatusProcessing on creation.
	Status Face2FAStatus `json:"status"`
}

// Face2FAChallengeStatus is the polled state of a face 2FA challenge.
type Face2FAChallengeStatus struct {
	// ChallengeID identifies the challenge.
	ChallengeID string `json:"challenge_id"`

	// Kind is what the challenge does: Face2FAKindEnroll or
	// Face2FAKindVerify.
	Kind string `json:"kind"`

	// Status is the current challenge state.
	Status Face2FAStatus `json:"status"`

	// Passed is the outcome: true on completed, false on failed or expired,
	// nil while the challenge is still processing.
	Passed *bool `json:"passed"`

	// FailureReason explains a non-passing outcome. Nil unless the
	// challenge failed or expired.
	FailureReason *Face2FAFailureReason `json:"failure_reason,omitempty"`

	// ExpiresAt is when the challenge expires (RFC 3339).
	ExpiresAt string `json:"expires_at"`

	// CompletedAt is when the challenge reached a terminal state (RFC 3339),
	// or nil if still processing.
	CompletedAt *string `json:"completed_at,omitempty"`
}

// Face2FAEnrollment is the enrollment state of one of your users.
type Face2FAEnrollment struct {
	// Enrolled is true if the user has an active face enrollment.
	Enrolled bool `json:"enrolled"`

	// EnrolledAt is when the current enrollment was created or last
	// replaced (RFC 3339). Nil when Enrolled is false.
	EnrolledAt *string `json:"enrolled_at,omitempty"`
}

// Face2FADeleteResult is the result of DeleteUser.
type Face2FADeleteResult struct {
	// Deleted is always true -- the call is idempotent and succeeds whether
	// or not an enrollment existed.
	Deleted bool `json:"deleted"`
}

// Register stores (or replaces) the face enrollment for one of your users.
//
// Asynchronous: poll GetStatus with the returned ChallengeID until the
// status is terminal. Registration is free of charge.
//
//	challenge, _, err := client.Face2FA.Register(ctx, &xident.Face2FAParams{
//	    UserID: "usr_123",
//	    Image:  base64Selfie,
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	// Poll client.Face2FA.GetStatus(ctx, challenge.ChallengeID)
func (s *Face2FAService) Register(ctx context.Context, params *Face2FAParams) (*Face2FAChallenge, *Response, error) {
	return s.submit(ctx, "2fa/register", params)
}

// Verify compares a face against the user's enrolled 2FA embedding (a 1:1
// comparison, server-side).
//
// Asynchronous: poll GetStatus with the returned ChallengeID for the
// pass/fail outcome. A user without an enrollment fails with
// Face2FAFailNotEnrolled.
func (s *Face2FAService) Verify(ctx context.Context, params *Face2FAParams) (*Face2FAChallenge, *Response, error) {
	return s.submit(ctx, "2fa/verify", params)
}

// submit is shared by Register and Verify -- both take the same parameters
// and return the same challenge shape; only the path differs.
func (s *Face2FAService) submit(ctx context.Context, path string, params *Face2FAParams) (*Face2FAChallenge, *Response, error) {
	if params == nil {
		return nil, nil, fmt.Errorf("xident: params cannot be nil")
	}

	req, err := s.client.newRequest(http.MethodPost, path, params)
	if err != nil {
		return nil, nil, err
	}

	result := new(Face2FAChallenge)
	resp, err := s.client.do(ctx, req, result)
	if err != nil {
		return nil, resp, err
	}

	return result, resp, nil
}

// GetStatus retrieves the current state of a face 2FA challenge.
//
// Poll this after Register or Verify until Status.IsTerminal() is true, then
// gate on Passed.
//
//	status, _, err := client.Face2FA.GetStatus(ctx, challenge.ChallengeID)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	if status.Status.IsTerminal() && status.Passed != nil && *status.Passed {
//	    // Second factor passed
//	}
func (s *Face2FAService) GetStatus(ctx context.Context, challengeID string) (*Face2FAChallengeStatus, *Response, error) {
	if challengeID == "" {
		return nil, nil, fmt.Errorf("xident: challengeID cannot be empty")
	}

	path := "2fa/status/" + url.PathEscape(challengeID)
	req, err := s.client.newRequest(http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	result := new(Face2FAChallengeStatus)
	resp, err := s.client.do(ctx, req, result)
	if err != nil {
		return nil, resp, err
	}

	return result, resp, nil
}

// GetUser reports whether one of your users has a 2FA face enrolled.
func (s *Face2FAService) GetUser(ctx context.Context, userID string) (*Face2FAEnrollment, *Response, error) {
	if userID == "" {
		return nil, nil, fmt.Errorf("xident: userID cannot be empty")
	}

	path := "2fa/users/" + url.PathEscape(userID)
	req, err := s.client.newRequest(http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	result := new(Face2FAEnrollment)
	resp, err := s.client.do(ctx, req, result)
	if err != nil {
		return nil, resp, err
	}

	return result, resp, nil
}

// DeleteUser hard-deletes a user's 2FA face enrollment (GDPR).
//
// Idempotent -- it succeeds whether or not an enrollment existed.
func (s *Face2FAService) DeleteUser(ctx context.Context, userID string) (*Face2FADeleteResult, *Response, error) {
	if userID == "" {
		return nil, nil, fmt.Errorf("xident: userID cannot be empty")
	}

	path := "2fa/users/" + url.PathEscape(userID)
	req, err := s.client.newRequest(http.MethodDelete, path, nil)
	if err != nil {
		return nil, nil, err
	}

	result := new(Face2FADeleteResult)
	resp, err := s.client.do(ctx, req, result)
	if err != nil {
		return nil, resp, err
	}

	return result, resp, nil
}
