package xident

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

// TestVerification_Init_LocalValidation checks the init rules Init applies
// before it sends anything: UserID is required, an age verification needs a
// MinAge from 12 to 25, and an ID verification takes no MinAge and no
// "facial" mode. A refused call returns a *ValidationError with the API's own
// code and makes no request at all.
//
// Mutations caught: dropping the UserID check or the TrimSpace in it; a range
// of 1 to 99 or 0 to 99; letting an ID verification keep a MinAge; dropping
// the facial check; sending the request before validating.
func TestVerification_Init_LocalValidation(t *testing.T) {
	const cb = "https://example.com/cb"

	tests := []struct {
		name   string
		params InitParams
		code   string // empty: the call must reach the API
	}{
		{"user id missing", InitParams{CallbackURL: cb, MinAge: 18}, "MISSING_USER_ID"},
		{"user id empty", InitParams{CallbackURL: cb, UserID: "", MinAge: 18}, "MISSING_USER_ID"},
		{"user id only spaces", InitParams{CallbackURL: cb, UserID: "  \t ", MinAge: 18}, "MISSING_USER_ID"},
		{"user id missing on an ID verification", InitParams{CallbackURL: cb, Purpose: "id_verification"}, "MISSING_USER_ID"},

		{"age 11", InitParams{CallbackURL: cb, UserID: "usr_1", MinAge: 11}, "INVALID_MIN_AGE"},
		{"age 26", InitParams{CallbackURL: cb, UserID: "usr_1", MinAge: 26}, "INVALID_MIN_AGE"},
		{"age 99", InitParams{CallbackURL: cb, UserID: "usr_1", MinAge: 99}, "INVALID_MIN_AGE"},
		{"age missing (0)", InitParams{CallbackURL: cb, UserID: "usr_1"}, "INVALID_MIN_AGE"},
		{"age negative", InitParams{CallbackURL: cb, UserID: "usr_1", MinAge: -18}, "INVALID_MIN_AGE"},
		{"explicit age purpose, age 0", InitParams{CallbackURL: cb, UserID: "usr_1", Purpose: "age_verification"}, "INVALID_MIN_AGE"},
		{"unknown purpose is refused before the age", InitParams{CallbackURL: cb, UserID: "usr_1", Purpose: "kyc"}, "INVALID_PURPOSE"},
		{"unknown purpose is refused even with a valid age", InitParams{CallbackURL: cb, UserID: "usr_1", Purpose: "kyc", MinAge: 18}, "INVALID_PURPOSE"},
		{"unknown purpose is refused before an invalid age", InitParams{CallbackURL: cb, UserID: "usr_1", Purpose: "kyc", MinAge: 30}, "INVALID_PURPOSE"},
		{"age 12 passes", InitParams{CallbackURL: cb, UserID: "usr_1", MinAge: 12}, ""},
		{"age 25 passes", InitParams{CallbackURL: cb, UserID: "usr_1", MinAge: 25}, ""},
		{"age 18 with facial passes", InitParams{CallbackURL: cb, UserID: "usr_1", MinAge: 18, VerificationMode: "facial"}, ""},
		{"age 21 with document passes", InitParams{CallbackURL: cb, UserID: "usr_1", MinAge: 21, VerificationMode: "document"}, ""},
		{"explicit age purpose with age 19 passes", InitParams{CallbackURL: cb, UserID: "usr_1", Purpose: "age_verification", MinAge: 19}, ""},

		{"ID verification with age 18", InitParams{CallbackURL: cb, UserID: "usr_1", Purpose: "id_verification", MinAge: 18}, "INVALID_MIN_AGE"},
		{"ID verification with facial", InitParams{CallbackURL: cb, UserID: "usr_1", Purpose: "id_verification", VerificationMode: "facial"}, "INVALID_VERIFICATION_MODE"},
		{"ID verification without age passes", InitParams{CallbackURL: cb, UserID: "usr_1", Purpose: "id_verification"}, ""},
		{"ID verification with document passes", InitParams{CallbackURL: cb, UserID: "usr_1", Purpose: "id_verification", VerificationMode: "document"}, ""},
		{"ID verification with auto passes", InitParams{CallbackURL: cb, UserID: "usr_1", Purpose: "id_verification", VerificationMode: "auto"}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, mux, teardown := setup()
			defer teardown()

			var calls atomic.Int32
			var gotMethod string
			var gotBody map[string]any
			mux.HandleFunc("/"+apiVersion+"/init", func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				gotMethod = r.Method
				raw, _ := io.ReadAll(r.Body)
				if err := json.Unmarshal(raw, &gotBody); err != nil {
					t.Errorf("request body is not JSON: %v", err)
				}
				_, _ = fmt.Fprint(w, `{"success":true,"data":{"token":"xit_ok","verify_url":"https://verify.xident.io?t=xit_ok"}}`)
			})

			params := tt.params
			result, resp, err := client.Verification.Init(context.Background(), &params)

			if tt.code == "" {
				if err != nil {
					t.Fatalf("Init() error: %v, want the call to reach the API", err)
				}
				if result.Token != "xit_ok" {
					t.Errorf("Token = %q, want xit_ok", result.Token)
				}
				if got := calls.Load(); got != 1 {
					t.Errorf("API calls = %d, want 1", got)
				}
				// The accepted call must reach POST /init with every setting
				// intact: a field dropped on the way (a json:"-" tag, a
				// renamed key) would let the API apply its default instead.
				if gotMethod != http.MethodPost {
					t.Errorf("method = %s, want POST", gotMethod)
				}
				if want := wantInitBody(tt.params); !reflect.DeepEqual(gotBody, want) {
					t.Errorf("request body = %v, want %v", gotBody, want)
				}
				return
			}

			if err == nil {
				t.Fatalf("Init() error = nil, want %s", tt.code)
			}
			if got := calls.Load(); got != 0 {
				t.Errorf("API calls = %d, want 0: a refused call must not reach the API", got)
			}
			if resp != nil || result != nil {
				t.Errorf("Init() = (%v, %v), want nil result and nil response", result, resp)
			}

			var valErr *ValidationError
			if !errors.As(err, &valErr) {
				t.Fatalf("error type = %T, want *ValidationError", err)
			}
			if valErr.Code != tt.code {
				t.Errorf("Code = %q, want %q", valErr.Code, tt.code)
			}
			// Caller code written for API errors reads Response.StatusCode:
			// a local refusal must not make it dereference nil.
			if valErr.Response == nil || valErr.Response.StatusCode != http.StatusBadRequest ||
				valErr.Response.Status != "400 Bad Request" {
				t.Fatalf("Response = %+v, want a synthetic 400 Bad Request", valErr.Response)
			}
			if body, _ := io.ReadAll(valErr.Response.Body); len(body) != 0 {
				t.Errorf("Response body = %q, want empty", body)
			}
			if !valErr.Local() || valErr.RequestID != "" {
				t.Errorf("Local() = %v, RequestID = %q; want true and empty: no request was made", valErr.Local(), valErr.RequestID)
			}
			if msg := err.Error(); !strings.Contains(msg, "400 "+tt.code) || !strings.Contains(msg, "no request was sent") {
				t.Errorf("Error() = %q, want the status, the code and a note that nothing was sent", msg)
			}
		})
	}
}

// wantInitBody is the JSON body Init must send for p: callback_url and
// user_id always, and min_age, purpose and verification_mode whenever they
// are set. Numbers decode as float64.
func wantInitBody(p InitParams) map[string]any {
	body := map[string]any{"callback_url": p.CallbackURL, "user_id": p.UserID}
	if p.MinAge != 0 {
		body["min_age"] = float64(p.MinAge)
	}
	if p.Purpose != "" {
		body["purpose"] = p.Purpose
	}
	if p.VerificationMode != "" {
		body["verification_mode"] = p.VerificationMode
	}
	return body
}

// TestVerification_Init_LocalValidationMessages pins the messages to the
// API's, so a caller sees one text whichever side refused the call.
func TestVerification_Init_LocalValidationMessages(t *testing.T) {
	client := NewClient("sk_test_key", WithBaseURL("http://127.0.0.1:1")) // never dialled

	tests := []struct {
		params  InitParams
		message string
	}{
		{InitParams{MinAge: 18}, "user_id is required: pass your own identifier for the person being verified"},
		{InitParams{UserID: "usr_1", MinAge: 30}, "min_age must be between 12 and 25; it is rounded up to the next of 12, 15, 18, 21 or 25 (19 is enforced as 21). An id_verification takes no min_age."},
		{InitParams{UserID: "usr_1", Purpose: "id_verification", VerificationMode: "facial"}, "verification_mode facial cannot be combined with purpose id_verification, which always requires a document"},
		{InitParams{UserID: "usr_1", Purpose: "kyc", MinAge: 18}, "purpose must be 'age_verification' or 'id_verification'"},
	}
	for _, tt := range tests {
		params := tt.params
		_, _, err := client.Verification.Init(context.Background(), &params)
		var valErr *ValidationError
		if !errors.As(err, &valErr) {
			t.Fatalf("error = %v, want *ValidationError", err)
		}
		if valErr.Message != tt.message {
			t.Errorf("Message = %q, want %q", valErr.Message, tt.message)
		}
	}
}

// TestVerification_Init_SendsMinAgeAsGiven checks that the SDK does not round
// MinAge itself: 19 goes on the wire as 19, and the API enforces band 21.
// The user id goes exactly as given, spaces included.
//
// Mutation caught: rounding to the band in the SDK, or trimming the user id
// before sending it.
func TestVerification_Init_SendsMinAgeAsGiven(t *testing.T) {
	client, mux, teardown := setup()
	defer teardown()

	mux.HandleFunc("/"+apiVersion+"/init", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var params map[string]any
		if err := json.Unmarshal(body, &params); err != nil {
			t.Fatalf("body is not JSON: %v", err)
		}
		if params["min_age"] != float64(19) {
			t.Errorf("min_age = %v, want 19", params["min_age"])
		}
		if params["user_id"] != " usr_19 " {
			t.Errorf("user_id = %q, want %q", params["user_id"], " usr_19 ")
		}
		_, _ = fmt.Fprint(w, `{"success":true,"data":{"token":"xit_19","verify_url":"https://verify.xident.io?t=xit_19"}}`)
	})

	if _, _, err := client.Verification.Init(context.Background(), &InitParams{
		CallbackURL: "https://example.com/cb",
		UserID:      " usr_19 ",
		MinAge:      19,
	}); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
}
