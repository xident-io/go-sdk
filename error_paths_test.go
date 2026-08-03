package xident

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestServiceMethods_PropagateRequestConstructionError checks that every
// service method reports a request it could not even build, rather than
// panicking on the nil request or returning a nil error.
//
// The methods share one shape -- build, send, return -- so a missed error
// check in any one of them is invisible until that call site is exercised.
// Table them all so adding a service method without the check fails here.
func TestServiceMethods_PropagateRequestConstructionError(t *testing.T) {
	// A control character in the host makes url.Parse fail inside
	// http.NewRequest, so newRequest fails for every path.
	c := NewClient("sk_test_key", WithBaseURL("http://local\x7fhost"))
	ctx := context.Background()

	tests := []struct {
		name string
		call func() (*Response, error)
	}{
		{"Verification.Init", func() (*Response, error) {
			_, resp, err := c.Verification.Init(ctx, &InitParams{CallbackURL: "https://example.com/cb"})
			return resp, err
		}},
		{"Verification.GetResult", func() (*Response, error) {
			_, resp, err := c.Verification.GetResult(ctx, "xtk_abc")
			return resp, err
		}},
		{"Face2FA.Register", func() (*Response, error) {
			_, resp, err := c.Face2FA.Register(ctx, &Face2FAParams{UserID: "usr_1", Image: "aGk="})
			return resp, err
		}},
		{"Face2FA.Verify", func() (*Response, error) {
			_, resp, err := c.Face2FA.Verify(ctx, &Face2FAParams{UserID: "usr_1", Image: "aGk="})
			return resp, err
		}},
		{"Face2FA.GetStatus", func() (*Response, error) {
			_, resp, err := c.Face2FA.GetStatus(ctx, "chl_1")
			return resp, err
		}},
		{"Face2FA.GetUser", func() (*Response, error) {
			_, resp, err := c.Face2FA.GetUser(ctx, "usr_1")
			return resp, err
		}},
		{"Face2FA.DeleteUser", func() (*Response, error) {
			_, resp, err := c.Face2FA.DeleteUser(ctx, "usr_1")
			return resp, err
		}},
		{"Blacklist.List", func() (*Response, error) {
			_, resp, err := c.Blacklist.List(ctx, &BlacklistListOptions{Page: 1, PerPage: 10})
			return resp, err
		}},
		{"Blacklist.AddBySession", func() (*Response, error) {
			_, resp, err := c.Blacklist.AddBySession(ctx, &BlacklistAddSessionParams{SessionToken: "xtk_1", Reason: "fraud"})
			return resp, err
		}},
		{"Blacklist.AddByImage", func() (*Response, error) {
			_, resp, err := c.Blacklist.AddByImage(ctx, &BlacklistAddImageParams{Image: "aGk=", Reason: "fraud"})
			return resp, err
		}},
		{"Blacklist.Remove", func() (*Response, error) {
			_, resp, err := c.Blacklist.Remove(ctx, 42)
			return resp, err
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := tt.call()
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			if resp != nil {
				t.Errorf("resp = %v, want nil -- no request was ever sent", resp)
			}
			if !strings.Contains(err.Error(), "xident: failed to create request") {
				t.Errorf("error = %q, want it to report request construction failure", err)
			}
			var urlErr *url.Error
			if !errors.As(err, &urlErr) {
				t.Errorf("error %v does not wrap *url.Error", err)
			}
		})
	}
}

// TestBlacklist_AddByImage_APIError covers AddByImage's failure branch: the
// typed error must reach the caller and the *Response must come back with it,
// because that is where the request id for a support ticket lives.
func TestBlacklist_AddByImage_APIError(t *testing.T) {
	client, mux, teardown := setup()
	defer teardown()

	mux.HandleFunc("/"+apiVersion+"/blacklist/image", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"success":false,"error":{"code":"INVALID_IMAGE","message":"Image could not be decoded"},"meta":{"request_id":"req_img"}}`)
	})

	result, resp, err := client.Blacklist.AddByImage(context.Background(),
		&BlacklistAddImageParams{Image: "not-base64", Reason: "fraud"})

	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if result != nil {
		t.Errorf("result = %v, want nil on error", result)
	}
	if resp == nil {
		t.Fatal("resp = nil, want the response carrying the request id")
	}
	if resp.RequestID != "req_img" {
		t.Errorf("resp.RequestID = %q, want %q", resp.RequestID, "req_img")
	}

	var vErr *ValidationError
	if !errors.As(err, &vErr) {
		t.Fatalf("error %v is not a *ValidationError", err)
	}
	if vErr.Code != "INVALID_IMAGE" {
		t.Errorf("Code = %q, want %q", vErr.Code, "INVALID_IMAGE")
	}
	if vErr.Message != "Image could not be decoded" {
		t.Errorf("Message = %q, want %q", vErr.Message, "Image could not be decoded")
	}
	if vErr.RequestID != "req_img" {
		t.Errorf("RequestID = %q, want %q", vErr.RequestID, "req_img")
	}
}

// TestFace2FA_GetUser_APIError covers GetUser's failure branch and pins the
// 5xx-to-*ServerError mapping for it.
func TestFace2FA_GetUser_APIError(t *testing.T) {
	client, mux, teardown := setup()
	defer teardown()

	mux.HandleFunc("/"+apiVersion+"/2fa/users/usr_boom", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"success":false,"error":{"code":"INTERNAL","message":"Backend unavailable"},"meta":{"request_id":"req_usr"}}`)
	})

	result, resp, err := client.Face2FA.GetUser(context.Background(), "usr_boom")

	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if result != nil {
		t.Errorf("result = %v, want nil on error", result)
	}
	if resp == nil {
		t.Fatal("resp = nil, want the response carrying the request id")
	}
	if resp.RequestID != "req_usr" {
		t.Errorf("resp.RequestID = %q, want %q", resp.RequestID, "req_usr")
	}

	var sErr *ServerError
	if !errors.As(err, &sErr) {
		t.Fatalf("error %v is not a *ServerError", err)
	}
	if sErr.Code != "INTERNAL" {
		t.Errorf("Code = %q, want %q", sErr.Code, "INTERNAL")
	}
	if sErr.Response.StatusCode != http.StatusInternalServerError {
		t.Errorf("Response.StatusCode = %d, want %d", sErr.Response.StatusCode, http.StatusInternalServerError)
	}
}

// TestSessionResult_AgeCheckDoesNotOverrideStatusVerdict guards the same
// invariant the pre-v1 AgeResult-blob tests used to: even when Status is
// Success, AgeBracket() and Method() must still report exactly what Checks
// and VerificationMode carry -- never something inferred from Status.
//
// Under the old json.RawMessage design this needed malformed-payload cases
// (truncated JSON, wrong types) because the helpers had to parse untrusted
// bytes. Checks.Age is now a typed struct field populated by the same
// encoding/json decode as the rest of SessionResult, so there is no separate
// parse step left to fail -- the equivalent "performed but not passed" /
// "never performed" cases are covered by TestSessionResult_AgeBracket in
// session_result_test.go.
func TestSessionResult_AgeCheckDoesNotOverrideStatusVerdict(t *testing.T) {
	// Status is Success on purpose. "The session passed, so there must be a
	// bracket" is the tempting inference, and it is wrong: the bracket has to
	// come from Checks.Age, not be invented because the verdict passed.
	s := &SessionResult{
		Status: SessionStatusSuccess,
		Checks: Checks{Age: AgeCheck{}}, // zero value: never performed
	}

	if got := s.AgeBracket(); got != nil {
		t.Errorf("AgeBracket() = %d, want nil when Checks.Age was never performed", *got)
	}
	if got := s.Method(); got != "" {
		t.Errorf("Method() = %q, want empty string when VerificationMode was not sent", got)
	}
	if !s.IsVerified() {
		t.Error("IsVerified() = false; the verdict must come from Status, not from Checks.Age")
	}
}

func TestWebhooks_VerifySignature_NonNumericTimestamp(t *testing.T) {
	client := NewClient("sk_test_key")

	ok, err := client.Webhooks.VerifySignature(
		[]byte(`{"type":"session.success"}`),
		"t=not-a-number,v1=deadbeef",
		testWebhookSecret,
	)

	if ok {
		t.Error("VerifySignature() = true, want false for a non-numeric timestamp")
	}
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "xident: invalid signature timestamp") {
		t.Errorf("error = %q, want it to report an invalid timestamp", err)
	}

	var numErr *strconv.NumError
	if !errors.As(err, &numErr) {
		t.Fatalf("error %v does not wrap *strconv.NumError", err)
	}
	if numErr.Func != "ParseInt" {
		t.Errorf("NumError.Func = %q, want %q", numErr.Func, "ParseInt")
	}
	if !errors.Is(err, strconv.ErrSyntax) {
		t.Errorf("error %v does not unwrap to strconv.ErrSyntax", err)
	}
}

// TestWebhooks_ConstructEvent_UnwrappedPayload covers the fallback for a
// payload delivered without a "data" envelope: the whole body becomes the
// data, so a handler reading event.Data still sees the fields.
func TestWebhooks_ConstructEvent_UnwrappedPayload(t *testing.T) {
	client := NewClient("sk_test_key")

	payload := `{"type":"session.failed","session_id":"sess_1","reason":"age_below_threshold"}`
	sig := makeTestSignature(payload, testWebhookSecret, time.Now().Unix())

	event, err := client.Webhooks.ConstructEvent([]byte(payload), sig, testWebhookSecret)
	if err != nil {
		t.Fatalf("ConstructEvent() error: %v", err)
	}

	if event.Type != "session.failed" {
		t.Errorf("Type = %q, want %q", event.Type, "session.failed")
	}
	if got := event.Data["session_id"]; got != "sess_1" {
		t.Errorf(`Data["session_id"] = %v, want %q`, got, "sess_1")
	}
	if got := event.Data["reason"]; got != "age_below_threshold" {
		t.Errorf(`Data["reason"] = %v, want %q`, got, "age_below_threshold")
	}
	// With no wrapper the type field is part of the data too -- the payload is
	// passed through whole rather than filtered.
	if got := event.Data["type"]; got != "session.failed" {
		t.Errorf(`Data["type"] = %v, want the whole payload to be carried through`, got)
	}
}

// TestWebhooks_ConstructEvent_EventIDFallback covers the "event_id" spelling,
// accepted only when "id" is absent.
func TestWebhooks_ConstructEvent_EventIDFallback(t *testing.T) {
	client := NewClient("sk_test_key")

	payload := `{"event_type":"session.success","event_id":"evt_legacy","data":{"session_id":"sess_2"},"created":1710345600}`
	sig := makeTestSignature(payload, testWebhookSecret, time.Now().Unix())

	event, err := client.Webhooks.ConstructEvent([]byte(payload), sig, testWebhookSecret)
	if err != nil {
		t.Fatalf("ConstructEvent() error: %v", err)
	}

	if event.ID != "evt_legacy" {
		t.Errorf("ID = %q, want %q from the event_id field", event.ID, "evt_legacy")
	}
	if event.Type != "session.success" {
		t.Errorf("Type = %q, want %q from the event_type field", event.Type, "session.success")
	}
	if event.Created != 1710345600 {
		t.Errorf("Created = %d, want %d", event.Created, 1710345600)
	}
	if got := event.Data["session_id"]; got != "sess_2" {
		t.Errorf(`Data["session_id"] = %v, want %q`, got, "sess_2")
	}
}

// TestWebhooks_ConstructEvent_IDWinsOverEventID pins the precedence: when both
// spellings are present, "id" is the one that counts.
func TestWebhooks_ConstructEvent_IDWinsOverEventID(t *testing.T) {
	client := NewClient("sk_test_key")

	payload := `{"type":"session.success","id":"evt_current","event_id":"evt_legacy","data":{}}`
	sig := makeTestSignature(payload, testWebhookSecret, time.Now().Unix())

	event, err := client.Webhooks.ConstructEvent([]byte(payload), sig, testWebhookSecret)
	if err != nil {
		t.Fatalf("ConstructEvent() error: %v", err)
	}
	if event.ID != "evt_current" {
		t.Errorf("ID = %q, want %q -- id must win over event_id", event.ID, "evt_current")
	}
}
