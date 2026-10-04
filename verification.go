package xident

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// VerificationService provides methods to create verification sessions and
// retrieve their results.
//
// Access this service through Client.Verification:
//
//	client := xident.NewClient("sk_live_xxx")
//	result, _, err := client.Verification.Init(ctx, &xident.InitParams{...})
type VerificationService service

// InitParams contains the parameters for creating a verification session.
//
// CallbackURL and UserID are always required. MinAge is required for an age
// verification (the default Purpose) and must be left at 0 for an ID
// verification. Init checks these rules before it sends anything, and
// answers a broken rule with a *ValidationError carrying the same code the
// API would answer (MISSING_USER_ID, INVALID_MIN_AGE or
// INVALID_VERIFICATION_MODE).
//
// Init needs a server key (sk_live_, sk_test_, ak_live_ or ak_test_). A
// public key (pk_) gets 403 SECRET_KEY_REQUIRED. Never put a server key in a
// browser or a mobile app.
type InitParams struct {
	// CallbackURL is where the verification widget redirects the browser back
	// to once the flow finishes. Required. Must be an https URL (http is
	// allowed only for localhost during development).
	//
	// The redirect is a plain browser GET with these query parameters
	// appended:
	//
	//   - status:  "success", "failed", or "canceled" -- the same three words
	//              the result endpoint uses. "success" means the user PASSED;
	//              a session that ran to the end but missed the age threshold
	//              arrives as "failed".
	//   - token:   the RESULT token (xtk_ prefixed). Pass this token, NOT the
	//              init token, to GetResult to fetch the outcome.
	//   - user_id: the UserID you passed to Init, echoed back.
	//
	// This is a browser redirect, not a signed webhook -- never trust these
	// query params on their own. Always re-verify server-side with GetResult.
	// (For the separate, optional signed-webhook feature, see WebhookService.)
	CallbackURL string `json:"callback_url"`

	// MinAge is required for an age verification: a whole number from 12 to
	// 25. Xident rounds it up to the next of 12, 15, 18, 21 or 25 and enforces
	// that band, so 19 is enforced as 21. The SDK sends the value as given;
	// the API does the rounding.
	//
	// An ID verification (Purpose "id_verification") takes no MinAge: leave it
	// at 0. Any other value is refused with INVALID_MIN_AGE.
	MinAge int `json:"min_age,omitempty"`

	// SuccessURL is where to redirect the user after successful verification.
	SuccessURL string `json:"success_url,omitempty"`

	// FailedURL is where to redirect the user after failed verification.
	FailedURL string `json:"failed_url,omitempty"`

	// UserID is required. It is your own identifier for the person being
	// verified. It comes back on the callback (as the user_id query param)
	// and in the result (SessionResult.ExternalUserID). It is sent exactly as
	// given; a value that is empty or only spaces is refused with
	// MISSING_USER_ID. It must not be a Xident key or token.
	UserID string `json:"user_id,omitempty"`

	// Theme sets the verification widget theme: "light", "dark", or "system"
	// (follow the user's OS preference).
	Theme string `json:"theme,omitempty"`

	// Locale sets the verification widget language (e.g., "en", "de", "fr").
	Locale string `json:"locale,omitempty"`

	// Purpose selects the verification intent: "age_verification" (default)
	// or "id_verification". An ID verification requires liveness, a document
	// and a face match, and takes no MinAge.
	Purpose string `json:"purpose,omitempty"`

	// VerificationMode overrides the rule engine's choice of methods for this
	// session: "auto" (default), "document" to force document + face match, or
	// "facial" to force on-device age estimation.
	//
	// It composes with MinAge rather than replacing it: "document" with
	// MinAge 21 still enforces 21, it just insists the proof be a document.
	//
	// "facial" cannot be combined with Purpose "id_verification", which always
	// needs a document. That pair is refused with INVALID_VERIFICATION_MODE.
	VerificationMode string `json:"verification_mode,omitempty"`

	// Metadata is an opaque string (up to 500 chars) passed through verbatim
	// and returned unchanged on the session result. Xident does not parse,
	// encode, or base64 it. Use it for order IDs, plan names, etc.
	Metadata string `json:"metadata,omitempty"`

	// Expected is identity data you already hold about the user, to be
	// checked against the document they present (data match, since
	// 2026-09-05). Any subset of the fields. Needs a document, so pair it with
	// Purpose "id_verification" or VerificationMode "document". The values
	// never reach the browser; the result carries only verdicts, per field,
	// in Checks.DataMatch.
	Expected *ExpectedIdentity `json:"expected,omitempty"`

	// MismatchPolicy decides what a mismatch does. MismatchPolicyReport
	// (default): the mismatch is reported in the result and the outcome is
	// unchanged. MismatchPolicyReview: any mismatch sends the session to your
	// review queue with reason "data_mismatch". Only meaningful with Expected.
	MismatchPolicy string `json:"mismatch_policy,omitempty"`
}

// ExpectedIdentity is what you already know about the user, for the data
// match. Every field is optional; an empty field is not sent.
type ExpectedIdentity struct {
	FirstName string `json:"first_name,omitempty"`
	LastName  string `json:"last_name,omitempty"`
	// DateOfBirth in YYYY-MM-DD.
	DateOfBirth    string `json:"date_of_birth,omitempty"`
	DocumentNumber string `json:"document_number,omitempty"`
	// Nationality as ISO 3166-1 alpha-2 (e.g. "DE").
	Nationality string `json:"nationality,omitempty"`
}

// Mismatch policies for InitParams.MismatchPolicy.
const (
	// MismatchPolicyReport reports mismatches in the result; the outcome is unchanged.
	MismatchPolicyReport = "report"
	// MismatchPolicyReview sends any mismatch to the review queue (reason "data_mismatch").
	MismatchPolicyReview = "review"
)

// Init creates a new verification session and returns an init token.
//
// The token is valid for 10 minutes. Redirect the user to the VerifyURL.
//
// Init checks UserID, MinAge, Purpose and VerificationMode before it sends
// anything (see InitParams). A broken rule returns a *ValidationError with
// the API's code, a synthetic 400 Response and Local() true, and no request
// is made.
//
//	result, resp, err := client.Verification.Init(ctx, &xident.InitParams{
//	    CallbackURL: "https://example.com/xident/callback",
//	    UserID:      userID,         // the signed-in user, from your session
//	    MinAge:      requiredMinAge, // your own constant, never from the request
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	// Redirect user to result.VerifyURL
func (s *VerificationService) Init(ctx context.Context, params *InitParams) (*InitResult, *Response, error) {
	if params == nil {
		return nil, nil, fmt.Errorf("xident: params cannot be nil")
	}
	if err := params.validate(); err != nil {
		return nil, nil, err
	}

	req, err := s.client.newRequest(http.MethodPost, "init", params)
	if err != nil {
		return nil, nil, err
	}

	result := new(InitResult)
	resp, err := s.client.do(ctx, req, result)
	if err != nil {
		return nil, resp, err
	}

	return result, resp, nil
}

// The rules of POST /verify/v1/init that Init checks before it sends
// anything. The codes and messages are the API's own, so a caller handles one
// set of codes whether the SDK or the API refused the call.
const (
	purposeIDVerification  = "id_verification"
	verificationModeFacial = "facial"

	// minAgeFloor and minAgeCeiling bound MinAge for an age verification.
	// The browser age models decide only the bands 12, 15, 18, 21 and 25, so
	// an age outside 12 to 25 cannot be enforced as asked.
	minAgeFloor   = 12
	minAgeCeiling = 25

	purposeAgeVerification = "age_verification"

	codeMissingUserID           = "MISSING_USER_ID"
	codeInvalidPurpose          = "INVALID_PURPOSE"
	codeInvalidMinAge           = "INVALID_MIN_AGE"
	codeInvalidVerificationMode = "INVALID_VERIFICATION_MODE"

	msgMissingUserID  = "user_id is required: pass your own identifier for the person being verified"
	msgInvalidPurpose = "purpose must be 'age_verification' or 'id_verification'"
	msgInvalidMinAge  = "min_age must be between 12 and 25; it is rounded up to the next of 12, 15, 18, 21 or 25 (19 is enforced as 21). An id_verification takes no min_age."
	msgFacialWithID   = "verification_mode facial cannot be combined with purpose id_verification, which always requires a document"
)

// validate applies the init rules the API enforces for UserID, Purpose,
// MinAge and VerificationMode, in the API's order. An empty Purpose is an age
// verification; any other value than the two purposes is refused.
// It returns nil or a local *ValidationError (see newLocalValidationError).
func (p *InitParams) validate() error {
	if strings.TrimSpace(p.UserID) == "" {
		return newLocalValidationError(codeMissingUserID, msgMissingUserID)
	}
	if p.Purpose != "" && p.Purpose != purposeAgeVerification && p.Purpose != purposeIDVerification {
		return newLocalValidationError(codeInvalidPurpose, msgInvalidPurpose)
	}
	if p.Purpose == purposeIDVerification {
		if p.MinAge != 0 {
			return newLocalValidationError(codeInvalidMinAge, msgInvalidMinAge)
		}
		if p.VerificationMode == verificationModeFacial {
			return newLocalValidationError(codeInvalidVerificationMode, msgFacialWithID)
		}
		return nil
	}
	if p.MinAge < minAgeFloor || p.MinAge > minAgeCeiling {
		return newLocalValidationError(codeInvalidMinAge, msgInvalidMinAge)
	}
	return nil
}

// GetResult retrieves the verification result for a token.
//
// The token argument is the RESULT token (xtk_ prefixed) that the widget
// appends as the "token" query parameter when it redirects the browser back
// to your CallbackURL. It is NOT the init token (xit_) returned by Init.
//
// Call this after the user returns from the verification widget. NEVER trust
// URL parameters alone -- always re-verify server-side.
//
//	// token := r.URL.Query().Get("token") // the xtk_ result token
//	session, resp, err := client.Verification.GetResult(ctx, "xtk_abc123")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	// Grant access only when the result proves the age your site needs
//	// and belongs to the user your server started it for.
//	if session.ProvesAge(requiredMinAge) && session.ExternalUserID == userID {
//	    fmt.Println("Verified!")
//	}
func (s *VerificationService) GetResult(ctx context.Context, token string) (*SessionResult, *Response, error) {
	if token == "" {
		return nil, nil, fmt.Errorf("xident: token cannot be empty")
	}

	path := "result/" + url.PathEscape(token)
	req, err := s.client.newRequest(http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	result := new(SessionResult)
	resp, err := s.client.do(ctx, req, result)
	if err != nil {
		return nil, resp, err
	}

	return result, resp, nil
}
