package xident

// SessionStatus represents the lifecycle state of a verification session.
type SessionStatus string

const (
	// SessionStatusPending means the session was created but verification
	// has not started yet.
	SessionStatusPending SessionStatus = "pending"

	// SessionStatusInProgress means the user is actively going through
	// the verification flow.
	SessionStatusInProgress SessionStatus = "in_progress"

	// SessionStatusCompleted means the session completed successfully --
	// the user passed age verification.
	SessionStatusCompleted SessionStatus = "completed"

	// SessionStatusFailed means the session failed -- the user did not
	// pass verification.
	SessionStatusFailed SessionStatus = "failed"

	// SessionStatusCanceled means the session was canceled by the user
	// or the system before completion.
	SessionStatusCanceled SessionStatus = "canceled"

	// SessionStatusClaimed means the verification result has been retrieved
	// by the consumer and the token is spent.
	SessionStatusClaimed SessionStatus = "claimed"
)

// IsTerminal returns true if the session has reached a final state where
// no further changes are possible.
func (s SessionStatus) IsTerminal() bool {
	switch s {
	case SessionStatusCompleted, SessionStatusFailed, SessionStatusCanceled, SessionStatusClaimed:
		return true
	default:
		return false
	}
}
