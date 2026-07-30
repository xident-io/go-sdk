package xident

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
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
// CallbackURL is the only required field. All other fields are optional.
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
	//   - user_id: echoed back only if you supplied UserID at init time.
	//
	// This is a browser redirect, not a signed webhook -- never trust these
	// query params on their own. Always re-verify server-side with GetResult.
	// (For the separate, optional signed-webhook feature, see WebhookService.)
	CallbackURL string `json:"callback_url"`

	// MinAge is the minimum age threshold, 1-99 (0-99 when Purpose is
	// "id_verification"). Defaults to the rule's configured threshold if
	// omitted. The trained age-bracket models cover 12, 15, 18, 21, and 25;
	// other values fall back to document verification.
	MinAge int `json:"min_age,omitempty"`

	// SuccessURL is where to redirect the user after successful verification.
	SuccessURL string `json:"success_url,omitempty"`

	// FailedURL is where to redirect the user after failed verification.
	FailedURL string `json:"failed_url,omitempty"`

	// UserID is your internal user identifier. Echoed back on the callback
	// redirect (as the user_id query param) for correlation.
	UserID string `json:"user_id,omitempty"`

	// Theme sets the verification widget theme: "light", "dark", or "system"
	// (follow the user's OS preference).
	Theme string `json:"theme,omitempty"`

	// Locale sets the verification widget language (e.g., "en", "de", "fr").
	Locale string `json:"locale,omitempty"`

	// Purpose selects the verification intent: "age_verification" (default)
	// or "id_verification". With "id_verification", MinAge may be 0-99.
	Purpose string `json:"purpose,omitempty"`

	// Metadata is an opaque string (up to 500 chars) passed through verbatim
	// and returned unchanged on the session result. Xident does not parse,
	// encode, or base64 it. Use it for order IDs, plan names, etc.
	Metadata string `json:"metadata,omitempty"`
}

// Init creates a new verification session and returns an init token.
//
// The token is valid for 10 minutes. Redirect the user to the VerifyURL,
// or pass the token to the Xident JS SDK.
//
//	result, resp, err := client.Verification.Init(ctx, &xident.InitParams{
//	    CallbackURL: "https://example.com/webhook",
//	    MinAge:      18,
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	// Redirect user to result.VerifyURL
func (s *VerificationService) Init(ctx context.Context, params *InitParams) (*InitResult, *Response, error) {
	if params == nil {
		return nil, nil, fmt.Errorf("xident: params cannot be nil")
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
//	if session.IsVerified() {
//	    if b := session.AgeBracket(); b != nil {
//	        fmt.Printf("Verified! Age bracket: %d\n", *b)
//	    }
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
