package xident

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"testing"
)

func TestVerification_Init(t *testing.T) {
	client, mux, teardown := setup()
	defer teardown()

	mux.HandleFunc("/"+apiVersion+"/init", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}

		body, _ := io.ReadAll(r.Body)
		var params map[string]any
		json.Unmarshal(body, &params)

		if params["callback_url"] != "https://example.com/cb" {
			t.Errorf("callback_url = %v, want %q", params["callback_url"], "https://example.com/cb")
		}
		if int(params["min_age"].(float64)) != 18 {
			t.Errorf("min_age = %v, want 18", params["min_age"])
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"success": true,
			"data": {
				"token": "xit_abc123",
				"verify_url": "https://verify.xident.io?t=xit_abc123"
			},
			"meta": {"request_id": "req_init_1"}
		}`)
	})

	result, resp, err := client.Verification.Init(context.Background(), &InitParams{
		CallbackURL: "https://example.com/cb",
		MinAge:      18,
	})
	if err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	if result.Token != "xit_abc123" {
		t.Errorf("Token = %q, want %q", result.Token, "xit_abc123")
	}
	if result.VerifyURL != "https://verify.xident.io?t=xit_abc123" {
		t.Errorf("VerifyURL = %q, want %q", result.VerifyURL, "https://verify.xident.io?t=xit_abc123")
	}
	if resp.RequestID != "req_init_1" {
		t.Errorf("RequestID = %q, want %q", resp.RequestID, "req_init_1")
	}
}

func TestVerification_Init_AllParams(t *testing.T) {
	client, mux, teardown := setup()
	defer teardown()

	mux.HandleFunc("/"+apiVersion+"/init", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var params map[string]any
		json.Unmarshal(body, &params)

		checks := map[string]any{
			"callback_url": "https://example.com/cb",
			"min_age":      float64(21),
			"success_url":  "https://example.com/success",
			"failed_url":   "https://example.com/failed",
			"user_id":      "usr_456",
			"theme":        "system",
			"locale":       "de",
			"purpose":      "id_verification",
			"metadata":     `{"plan":"pro"}`,
		}

		for k, want := range checks {
			if params[k] != want {
				t.Errorf("%s = %v, want %v", k, params[k], want)
			}
		}

		fmt.Fprint(w, `{"success":true,"data":{"token":"xit_all","verify_url":"https://v.io?t=xit_all"}}`)
	})

	_, _, err := client.Verification.Init(context.Background(), &InitParams{
		CallbackURL: "https://example.com/cb",
		MinAge:      21,
		SuccessURL:  "https://example.com/success",
		FailedURL:   "https://example.com/failed",
		UserID:      "usr_456",
		Theme:       "system",
		Locale:      "de",
		Purpose:     "id_verification",
		Metadata:    `{"plan":"pro"}`,
	})
	if err != nil {
		t.Fatalf("Init() error: %v", err)
	}
}

func TestVerification_Init_MinimalParams(t *testing.T) {
	client, mux, teardown := setup()
	defer teardown()

	mux.HandleFunc("/"+apiVersion+"/init", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var params map[string]any
		json.Unmarshal(body, &params)

		if params["callback_url"] != "https://example.com/cb" {
			t.Errorf("callback_url = %v", params["callback_url"])
		}
		// Optional fields should be omitted when zero.
		if _, ok := params["min_age"]; ok {
			t.Error("min_age should not be present when zero")
		}

		fmt.Fprint(w, `{"success":true,"data":{"token":"xit_min","verify_url":"https://v.io?t=xit_min"}}`)
	})

	result, _, err := client.Verification.Init(context.Background(), &InitParams{
		CallbackURL: "https://example.com/cb",
	})
	if err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	if result.Token != "xit_min" {
		t.Errorf("Token = %q, want %q", result.Token, "xit_min")
	}
}

func TestVerification_Init_NilParams(t *testing.T) {
	client := NewClient("sk_test_key")

	_, _, err := client.Verification.Init(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil params")
	}
}

func TestVerification_Init_ValidationError(t *testing.T) {
	client, mux, teardown := setup()
	defer teardown()

	mux.HandleFunc("/"+apiVersion+"/init", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{
			"success": false,
			"error": {"code": "MISSING_CALLBACK_URL", "message": "callback_url is required"},
			"meta": {"request_id": "req_val_1"}
		}`)
	})

	_, _, err := client.Verification.Init(context.Background(), &InitParams{})
	if err == nil {
		t.Fatal("expected error")
	}

	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Fatalf("expected *ValidationError, got %T: %v", err, err)
	}
	if valErr.Code != "MISSING_CALLBACK_URL" {
		t.Errorf("Code = %q, want %q", valErr.Code, "MISSING_CALLBACK_URL")
	}
}

func TestVerification_Init_AuthError(t *testing.T) {
	client, mux, teardown := setup()
	defer teardown()

	mux.HandleFunc("/"+apiVersion+"/init", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{
			"success": false,
			"error": {"code": "UNAUTHORIZED", "message": "Invalid API key"}
		}`)
	})

	_, _, err := client.Verification.Init(context.Background(), &InitParams{
		CallbackURL: "https://example.com/cb",
	})
	if err == nil {
		t.Fatal("expected error")
	}

	var authErr *AuthenticationError
	if !errors.As(err, &authErr) {
		t.Fatalf("expected *AuthenticationError, got %T", err)
	}
}

func TestVerification_GetResult_Completed(t *testing.T) {
	client, mux, teardown := setup()
	defer teardown()

	mux.HandleFunc("/"+apiVersion+"/result/xtk_abc", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %q, want GET", r.Method)
		}

		fmt.Fprint(w, `{
			"success": true,
			"data": {
				"token": "xtk_abc",
				"status": "success",
				"verified": true,
				"verification_type": "full",
				"checks": {
					"liveness": {"performed": true, "passed": true},
					"age": {"performed": true, "passed": true, "gate": 18},
					"document": {"performed": false, "passed": false},
					"face_match": {"performed": false, "passed": false}
				},
				"created_at": "2026-03-23T12:00:00Z",
				"completed_at": "2026-03-23T12:01:00Z"
			},
			"meta": {"request_id": "req_res_1"}
		}`)
	})

	session, resp, err := client.Verification.GetResult(context.Background(), "xtk_abc")
	if err != nil {
		t.Fatalf("GetResult() error: %v", err)
	}

	if session.Token != "xtk_abc" {
		t.Errorf("Token = %q, want %q", session.Token, "xtk_abc")
	}
	if !session.IsVerified() {
		t.Error("IsVerified() should be true")
	}
	if session.IsFailed() {
		t.Error("IsFailed() should be false")
	}
	if session.IsPending() {
		t.Error("IsPending() should be false")
	}
	if !session.IsTerminal() {
		t.Error("IsTerminal() should be true")
	}

	bracket := session.AgeBracket()
	if bracket == nil || *bracket != 18 {
		t.Errorf("AgeBracket() = %v, want 18", bracket)
	}

	method := session.Method()
	if method != "full" {
		t.Errorf("Method() = %q, want %q", method, "full")
	}

	if resp.RequestID != "req_res_1" {
		t.Errorf("RequestID = %q, want %q", resp.RequestID, "req_res_1")
	}
}

func TestVerification_GetResult_Pending(t *testing.T) {
	client, mux, teardown := setup()
	defer teardown()

	mux.HandleFunc("/"+apiVersion+"/result/xtk_p", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{
			"success": true,
			"data": {
				"token": "xtk_p",
				"status": "in_progress",
				"verified": false,
				"created_at": "2026-03-23T12:00:00Z"
			}
		}`)
	})

	session, _, err := client.Verification.GetResult(context.Background(), "xtk_p")
	if err != nil {
		t.Fatalf("GetResult() error: %v", err)
	}

	if !session.IsPending() {
		t.Error("IsPending() should be true")
	}
	if session.IsVerified() {
		t.Error("IsVerified() should be false")
	}
	if session.IsTerminal() {
		t.Error("IsTerminal() should be false")
	}
}

func TestVerification_GetResult_Failed(t *testing.T) {
	client, mux, teardown := setup()
	defer teardown()

	mux.HandleFunc("/"+apiVersion+"/result/sess_f", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{
			"success": true,
			"data": {
				"id": "sess_f",
				"status": "failed",
				"created_at": "2026-03-23T12:00:00Z"
			}
		}`)
	})

	session, _, err := client.Verification.GetResult(context.Background(), "sess_f")
	if err != nil {
		t.Fatalf("GetResult() error: %v", err)
	}

	if !session.IsFailed() {
		t.Error("IsFailed() should be true")
	}
	if session.IsVerified() {
		t.Error("IsVerified() should be false")
	}
	if !session.IsTerminal() {
		t.Error("IsTerminal() should be true")
	}
}

func TestVerification_GetResult_Canceled(t *testing.T) {
	client, mux, teardown := setup()
	defer teardown()

	mux.HandleFunc("/"+apiVersion+"/result/sess_c", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{
			"success": true,
			"data": {
				"id": "sess_c",
				"status": "canceled",
				"created_at": "2026-03-23T12:00:00Z"
			}
		}`)
	})

	session, _, err := client.Verification.GetResult(context.Background(), "sess_c")
	if err != nil {
		t.Fatalf("GetResult() error: %v", err)
	}

	if session.IsVerified() {
		t.Error("IsVerified() should be false")
	}
	if !session.IsTerminal() {
		t.Error("IsTerminal() should be true")
	}
}

func TestVerification_GetResult_EmptyToken(t *testing.T) {
	client := NewClient("sk_test_key")

	_, _, err := client.Verification.GetResult(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty token")
	}
}

func TestVerification_GetResult_NotFound(t *testing.T) {
	client, mux, teardown := setup()
	defer teardown()

	mux.HandleFunc("/"+apiVersion+"/result/nonexistent", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{
			"success": false,
			"error": {"code": "NOT_FOUND", "message": "Session not found"}
		}`)
	})

	_, _, err := client.Verification.GetResult(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error")
	}

	var notFound *NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("expected *NotFoundError, got %T: %v", err, err)
	}
}

// Data match (2026-09-05): `expected` is a nested object and `mismatch_policy`
// a sibling string; both are omitted entirely when unset so an integration
// that never heard of them sends exactly the body it sent before.
func TestVerification_Init_Expected(t *testing.T) {
	client, mux, teardown := setup()
	defer teardown()

	mux.HandleFunc("/"+apiVersion+"/init", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var params map[string]any
		json.Unmarshal(body, &params)

		exp, ok := params["expected"].(map[string]any)
		if !ok {
			t.Fatalf("expected must be a nested object, got %T", params["expected"])
		}
		want := map[string]any{
			"first_name":    "Jane",
			"last_name":     "Smith",
			"date_of_birth": "1990-05-14",
			"nationality":   "GB",
		}
		for k, v := range want {
			if exp[k] != v {
				t.Errorf("expected.%s = %v, want %v", k, exp[k], v)
			}
		}
		if _, present := exp["document_number"]; present {
			t.Error("an empty expected field must be omitted, not sent as \"\"")
		}
		if params["mismatch_policy"] != "review" {
			t.Errorf("mismatch_policy = %v, want review", params["mismatch_policy"])
		}
		fmt.Fprint(w, `{"success":true,"data":{"token":"xit_dm","verify_url":"https://v.io?t=xit_dm"}}`)
	})

	_, _, err := client.Verification.Init(context.Background(), &InitParams{
		CallbackURL: "https://example.com/cb",
		Purpose:     "id_verification",
		Expected: &ExpectedIdentity{
			FirstName:   "Jane",
			LastName:    "Smith",
			DateOfBirth: "1990-05-14",
			Nationality: "GB",
		},
		MismatchPolicy: MismatchPolicyReview,
	})
	if err != nil {
		t.Fatalf("Init() error: %v", err)
	}
}

func TestVerification_Init_OmitsExpectedWhenUnset(t *testing.T) {
	client, mux, teardown := setup()
	defer teardown()

	mux.HandleFunc("/"+apiVersion+"/init", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var params map[string]any
		json.Unmarshal(body, &params)
		for _, k := range []string{"expected", "mismatch_policy"} {
			if _, present := params[k]; present {
				t.Errorf("%s must be absent when unset", k)
			}
		}
		fmt.Fprint(w, `{"success":true,"data":{"token":"xit_plain","verify_url":"https://v.io?t=xit_plain"}}`)
	})

	if _, _, err := client.Verification.Init(context.Background(), &InitParams{CallbackURL: "https://example.com/cb", MinAge: 18}); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
}

