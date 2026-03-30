package xident

// InitResult is returned by VerificationService.Init. It contains the
// short-lived init token and the full URL to redirect the user to.
type InitResult struct {
	// Token is the init token (xit_ prefixed, 10-minute TTL).
	// Use this to construct the verification URL or pass it to the JS SDK.
	Token string `json:"token"`

	// VerifyURL is the full verification URL -- redirect the user here to
	// start the verification flow.
	VerifyURL string `json:"verify_url"`
}
