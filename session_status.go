package xident

import "encoding/json"

// SessionStatus represents the lifecycle state of a verification session.
//
// The API uses ONE vocabulary for the outcome across every surface -- the
// result endpoint, the webhook payload and the browser callback's ?status=
// query param all use these same words. There is no spelling difference
// between surfaces; earlier versions of this SDK documented one, and that is
// no longer true.
type SessionStatus string

const (
	// SessionStatusPending means the session was created but verification
	// has not started yet.
	SessionStatusPending SessionStatus = "pending"

	// SessionStatusInProgress means the user is actively going through
	// the verification flow.
	SessionStatusInProgress SessionStatus = "in_progress"

	// SessionStatusSuccess is the PASS verdict: the user met the age
	// threshold and every required check passed.
	//
	// It is a verdict, not a lifecycle marker. A session that ran to the end
	// of the flow but did not meet the threshold is SessionStatusFailed, not
	// this. Do not read it as "the flow finished".
	SessionStatusSuccess SessionStatus = "success"

	// SessionStatusCompleted is a deprecated alias for SessionStatusSuccess.
	//
	// Deprecated: the API renamed this verdict from "completed" to "success"
	// in July 2026, because "completed" described a lifecycle and was read by
	// at least one integrator as "the flow finished" regardless of the age
	// result. Use SessionStatusSuccess.
	//
	// This alias deliberately carries the NEW value, so an existing
	// `result.Status == SessionStatusCompleted` comparison keeps returning
	// true for a verified user. Had it kept the old literal it would still
	// compile and silently report every verified user as unverified -- on an
	// age gate, a silent wrong answer is worse than a compile error.
	SessionStatusCompleted = SessionStatusSuccess

	// SessionStatusFailed means the session failed -- the user did not
	// pass verification. Inspect SessionResult.Reason for why.
	SessionStatusFailed SessionStatus = "failed"

	// SessionStatusCanceled means the session was canceled by the user
	// or the system before completion.
	SessionStatusCanceled SessionStatus = "canceled"

	// SessionStatusClaimed means the verification result has been retrieved
	// by the consumer and the token is spent.
	SessionStatusClaimed SessionStatus = "claimed"
)

// legacyStatusSuccess is the value this verdict carried before July 2026.
//
// Kept only so that an SDK pointed at a deployment older than that rename
// still behaves. An SDK outlives a rollout window -- it gets aimed at
// self-hosted and lagging installs long after our own migration finished.
const legacyStatusSuccess = "completed"

// UnmarshalJSON normalises a legacy wire value to the current vocabulary, so
// that a caller comparing against SessionStatusSuccess gets the right answer
// whichever deployment it is talking to.
//
// Unrecognised values pass through UNCHANGED rather than erroring. That is
// deliberate on two counts: rejecting an unknown status would mean any status
// added server-side in future breaks every already-released SDK, and an
// unknown value falls through IsTerminal to false -- so a caller polling for a
// terminal state keeps polling instead of treating something it does not
// understand as a finished verification.
func (s *SessionStatus) UnmarshalJSON(data []byte) error {
	// Decode into a plain string, NOT a SessionStatus: unmarshalling into the
	// named type would re-enter this method and recurse until the stack dies.
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	if raw == legacyStatusSuccess {
		raw = string(SessionStatusSuccess)
	}

	*s = SessionStatus(raw)
	return nil
}

// IsTerminal returns true if the session has reached a final state where
// no further changes are possible.
func (s SessionStatus) IsTerminal() bool {
	switch s {
	case SessionStatusSuccess, SessionStatusFailed, SessionStatusCanceled, SessionStatusClaimed:
		return true
	default:
		return false
	}
}
