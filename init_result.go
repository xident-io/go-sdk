package xident

// InitResult is returned by VerificationService.Init. It contains the
// short-lived init token and the full URL to send the user to.
type InitResult struct {
	// Token is the init token (xit_ prefixed, 10-minute TTL). It is already
	// inside VerifyURL; you do not need it to start the verification.
	// Do not pass it to GetResult: the result token (xtk_) arrives on your
	// callback instead.
	Token string `json:"token"`

	// VerifyURL is the full verification URL. Send the user here, unchanged:
	// redirect the browser to it, or hand it to your page, which opens it
	// with the browser SDK (@xident/browser 2.x):
	//
	//	Xident.start({ verifyUrl })
	//
	// Do not build the URL yourself from Token, and never send your server
	// key to the page.
	VerifyURL string `json:"verify_url"`
}
