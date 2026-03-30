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
	// CallbackURL is where Xident sends the webhook when verification
	// completes. Required.
	CallbackURL string `json:"callback_url"`

	// MinAge is the minimum age threshold (12, 15, 18, 21, or 25).
	// Defaults to the rule's configured threshold if omitted.
	MinAge int `json:"min_age,omitempty"`

	// SuccessURL is where to redirect the user after successful verification.
	SuccessURL string `json:"success_url,omitempty"`

	// FailedURL is where to redirect the user after failed verification.
	FailedURL string `json:"failed_url,omitempty"`

	// UserID is your internal user identifier. Passed through to the
	// webhook for correlation.
	UserID string `json:"user_id,omitempty"`

	// Theme sets the verification widget theme ("light" or "dark").
	Theme string `json:"theme,omitempty"`

	// Locale sets the verification widget language (e.g., "en", "de", "fr").
	Locale string `json:"locale,omitempty"`

	// Metadata is an opaque string (up to 500 chars) passed through to the
	// webhook. Use it for order IDs, plan names, etc.
	Metadata string `json:"metadata,omitempty"`

	// LivenessDifficulty controls liveness challenge difficulty
	// ("easy", "normal", "hard").
	LivenessDifficulty string `json:"liveness_difficulty,omitempty"`

	// Purpose describes why verification is needed (shown to the user).
	Purpose string `json:"purpose,omitempty"`
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
// Call this after the user returns from the verification widget, or after
// receiving a webhook callback. NEVER trust URL parameters alone -- always
// re-verify server-side.
//
//	session, resp, err := client.Verification.GetResult(ctx, "xit_abc123")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	if session.IsVerified() {
//	    fmt.Printf("Verified! Age bracket: %d\n", *session.AgeBracket())
//	}
func (s *VerificationService) GetResult(ctx context.Context, token string) (*SessionResult, *Response, error) {
	if token == "" {
		return nil, nil, fmt.Errorf("xident: token cannot be empty")
	}

	path := "status/" + url.PathEscape(token)
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
