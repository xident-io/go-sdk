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

func TestFace2FA_Register(t *testing.T) {
	client, mux, teardown := setup()
	defer teardown()

	mux.HandleFunc("/"+apiVersion+"/2fa/register", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if got := r.Header.Get("X-API-Key"); got != "sk_test_123" {
			t.Errorf("X-API-Key = %q, want %q", got, "sk_test_123")
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want %q", got, "application/json")
		}

		body, _ := io.ReadAll(r.Body)
		var params map[string]any
		json.Unmarshal(body, &params)

		if params["user_id"] != "usr_123" {
			t.Errorf("user_id = %v, want %q", params["user_id"], "usr_123")
		}
		if params["image"] != "aGVsbG8=" {
			t.Errorf("image = %v, want %q", params["image"], "aGVsbG8=")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{
			"success": true,
			"data": {"challenge_id": "f2f_abc123", "status": "processing"},
			"meta": {"request_id": "req_2fa_reg"}
		}`)
	})

	challenge, resp, err := client.Face2FA.Register(context.Background(), &Face2FAParams{
		UserID: "usr_123",
		Image:  "aGVsbG8=",
	})
	if err != nil {
		t.Fatalf("Register() error: %v", err)
	}
	if challenge.ChallengeID != "f2f_abc123" {
		t.Errorf("ChallengeID = %q, want %q", challenge.ChallengeID, "f2f_abc123")
	}
	if challenge.Status != Face2FAStatusProcessing {
		t.Errorf("Status = %q, want %q", challenge.Status, Face2FAStatusProcessing)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("StatusCode = %d, want 201", resp.StatusCode)
	}
	if resp.RequestID != "req_2fa_reg" {
		t.Errorf("RequestID = %q, want %q", resp.RequestID, "req_2fa_reg")
	}
}

func TestFace2FA_Verify(t *testing.T) {
	client, mux, teardown := setup()
	defer teardown()

	mux.HandleFunc("/"+apiVersion+"/2fa/verify", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}

		body, _ := io.ReadAll(r.Body)
		var params map[string]any
		json.Unmarshal(body, &params)

		if params["user_id"] != "usr_456" {
			t.Errorf("user_id = %v, want %q", params["user_id"], "usr_456")
		}
		if params["image"] != "c2VsZmll" {
			t.Errorf("image = %v, want %q", params["image"], "c2VsZmll")
		}

		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{
			"success": true,
			"data": {"challenge_id": "f2f_ver1", "status": "processing"}
		}`)
	})

	challenge, _, err := client.Face2FA.Verify(context.Background(), &Face2FAParams{
		UserID: "usr_456",
		Image:  "c2VsZmll",
	})
	if err != nil {
		t.Fatalf("Verify() error: %v", err)
	}
	if challenge.ChallengeID != "f2f_ver1" {
		t.Errorf("ChallengeID = %q, want %q", challenge.ChallengeID, "f2f_ver1")
	}
}

func TestFace2FA_Submit_NilParams(t *testing.T) {
	client := NewClient("sk_test_key")

	if _, _, err := client.Face2FA.Register(context.Background(), nil); err == nil {
		t.Error("Register(nil) expected error")
	}
	if _, _, err := client.Face2FA.Verify(context.Background(), nil); err == nil {
		t.Error("Verify(nil) expected error")
	}
}

func TestFace2FA_Register_AuthError(t *testing.T) {
	client, mux, teardown := setup()
	defer teardown()

	mux.HandleFunc("/"+apiVersion+"/2fa/register", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{
			"success": false,
			"error": {"code": "UNAUTHORIZED", "message": "not authenticated"}
		}`)
	})

	_, _, err := client.Face2FA.Register(context.Background(), &Face2FAParams{
		UserID: "usr_123",
		Image:  "aGVsbG8=",
	})
	if err == nil {
		t.Fatal("expected error")
	}

	var authErr *AuthenticationError
	if !errors.As(err, &authErr) {
		t.Fatalf("expected *AuthenticationError, got %T: %v", err, err)
	}
	if authErr.Code != "UNAUTHORIZED" {
		t.Errorf("Code = %q, want %q", authErr.Code, "UNAUTHORIZED")
	}
}

func TestFace2FA_GetStatus(t *testing.T) {
	tests := []struct {
		name         string
		body         string
		wantStatus   Face2FAStatus
		wantPassed   *bool
		wantReason   *Face2FAFailureReason
		wantTerminal bool
		wantComplete bool // CompletedAt present
	}{
		{
			name: "processing",
			body: `{
				"challenge_id": "f2f_p", "kind": "verify", "status": "processing",
				"passed": null, "expires_at": "2026-08-02T12:05:00Z"
			}`,
			wantStatus:   Face2FAStatusProcessing,
			wantPassed:   nil,
			wantTerminal: false,
		},
		{
			name: "completed pass",
			body: `{
				"challenge_id": "f2f_c", "kind": "enroll", "status": "completed",
				"passed": true, "expires_at": "2026-08-02T12:05:00Z",
				"completed_at": "2026-08-02T12:01:00Z"
			}`,
			wantStatus:   Face2FAStatusCompleted,
			wantPassed:   boolPtr(true),
			wantTerminal: true,
			wantComplete: true,
		},
		{
			name: "failed with reason",
			body: `{
				"challenge_id": "f2f_f", "kind": "verify", "status": "failed",
				"passed": false, "failure_reason": "face_mismatch",
				"expires_at": "2026-08-02T12:05:00Z",
				"completed_at": "2026-08-02T12:02:00Z"
			}`,
			wantStatus:   Face2FAStatusFailed,
			wantPassed:   boolPtr(false),
			wantReason:   reasonPtr(Face2FAFailFaceMismatch),
			wantTerminal: true,
			wantComplete: true,
		},
		{
			name: "expired",
			body: `{
				"challenge_id": "f2f_e", "kind": "verify", "status": "expired",
				"passed": false, "failure_reason": "expired",
				"expires_at": "2026-08-02T12:05:00Z"
			}`,
			wantStatus:   Face2FAStatusExpired,
			wantPassed:   boolPtr(false),
			wantReason:   reasonPtr(Face2FAFailExpired),
			wantTerminal: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, mux, teardown := setup()
			defer teardown()

			mux.HandleFunc("/"+apiVersion+"/2fa/status/f2f_x", func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("method = %q, want GET", r.Method)
				}
				fmt.Fprintf(w, `{"success": true, "data": %s}`, tt.body)
			})

			status, _, err := client.Face2FA.GetStatus(context.Background(), "f2f_x")
			if err != nil {
				t.Fatalf("GetStatus() error: %v", err)
			}

			if status.Status != tt.wantStatus {
				t.Errorf("Status = %q, want %q", status.Status, tt.wantStatus)
			}
			if status.Status.IsTerminal() != tt.wantTerminal {
				t.Errorf("IsTerminal() = %v, want %v", status.Status.IsTerminal(), tt.wantTerminal)
			}
			if (status.Passed == nil) != (tt.wantPassed == nil) {
				t.Fatalf("Passed = %v, want %v", status.Passed, tt.wantPassed)
			}
			if status.Passed != nil && *status.Passed != *tt.wantPassed {
				t.Errorf("*Passed = %v, want %v", *status.Passed, *tt.wantPassed)
			}
			if (status.FailureReason == nil) != (tt.wantReason == nil) {
				t.Fatalf("FailureReason = %v, want %v", status.FailureReason, tt.wantReason)
			}
			if status.FailureReason != nil && *status.FailureReason != *tt.wantReason {
				t.Errorf("*FailureReason = %q, want %q", *status.FailureReason, *tt.wantReason)
			}
			if status.ExpiresAt != "2026-08-02T12:05:00Z" {
				t.Errorf("ExpiresAt = %q, want %q", status.ExpiresAt, "2026-08-02T12:05:00Z")
			}
			if tt.wantComplete && status.CompletedAt == nil {
				t.Error("CompletedAt should be present")
			}
			if !tt.wantComplete && status.CompletedAt != nil {
				t.Errorf("CompletedAt = %v, want nil", *status.CompletedAt)
			}
		})
	}
}

func TestFace2FA_GetStatus_EmptyID(t *testing.T) {
	client := NewClient("sk_test_key")

	_, _, err := client.Face2FA.GetStatus(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty challenge id")
	}
}

func TestFace2FA_GetStatus_NotFound(t *testing.T) {
	client, mux, teardown := setup()
	defer teardown()

	mux.HandleFunc("/"+apiVersion+"/2fa/status/missing", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{
			"success": false,
			"error": {"code": "NOT_FOUND", "message": "challenge not found"}
		}`)
	})

	_, _, err := client.Face2FA.GetStatus(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected error")
	}

	var notFound *NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("expected *NotFoundError, got %T: %v", err, err)
	}
}

func TestFace2FA_GetUser_Enrolled(t *testing.T) {
	client, mux, teardown := setup()
	defer teardown()

	mux.HandleFunc("/"+apiVersion+"/2fa/users/usr_123", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %q, want GET", r.Method)
		}
		fmt.Fprint(w, `{
			"success": true,
			"data": {"enrolled": true, "enrolled_at": "2026-07-01T09:00:00Z"}
		}`)
	})

	info, _, err := client.Face2FA.GetUser(context.Background(), "usr_123")
	if err != nil {
		t.Fatalf("GetUser() error: %v", err)
	}
	if !info.Enrolled {
		t.Error("Enrolled should be true")
	}
	if info.EnrolledAt == nil || *info.EnrolledAt != "2026-07-01T09:00:00Z" {
		t.Errorf("EnrolledAt = %v, want %q", info.EnrolledAt, "2026-07-01T09:00:00Z")
	}
}

func TestFace2FA_GetUser_NotEnrolled(t *testing.T) {
	client, mux, teardown := setup()
	defer teardown()

	mux.HandleFunc("/"+apiVersion+"/2fa/users/usr_new", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"success": true, "data": {"enrolled": false}}`)
	})

	info, _, err := client.Face2FA.GetUser(context.Background(), "usr_new")
	if err != nil {
		t.Fatalf("GetUser() error: %v", err)
	}
	if info.Enrolled {
		t.Error("Enrolled should be false")
	}
	if info.EnrolledAt != nil {
		t.Errorf("EnrolledAt = %v, want nil", *info.EnrolledAt)
	}
}

func TestFace2FA_GetUser_EscapesUserID(t *testing.T) {
	client, mux, teardown := setup()
	defer teardown()

	// A user id with a path-hostile character must be escaped, not spliced.
	mux.HandleFunc("/"+apiVersion+"/2fa/users/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/"+apiVersion+"/2fa/users/usr%2F123" {
			t.Errorf("path = %q, want escaped user id", r.URL.EscapedPath())
		}
		fmt.Fprint(w, `{"success": true, "data": {"enrolled": false}}`)
	})

	if _, _, err := client.Face2FA.GetUser(context.Background(), "usr/123"); err != nil {
		t.Fatalf("GetUser() error: %v", err)
	}
}

func TestFace2FA_GetUser_EmptyID(t *testing.T) {
	client := NewClient("sk_test_key")

	_, _, err := client.Face2FA.GetUser(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty user id")
	}
}

func TestFace2FA_DeleteUser(t *testing.T) {
	client, mux, teardown := setup()
	defer teardown()

	mux.HandleFunc("/"+apiVersion+"/2fa/users/usr_123", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %q, want DELETE", r.Method)
		}
		fmt.Fprint(w, `{"success": true, "data": {"deleted": true}, "meta": {"request_id": "req_del_1"}}`)
	})

	result, resp, err := client.Face2FA.DeleteUser(context.Background(), "usr_123")
	if err != nil {
		t.Fatalf("DeleteUser() error: %v", err)
	}
	if !result.Deleted {
		t.Error("Deleted should be true")
	}
	if resp.RequestID != "req_del_1" {
		t.Errorf("RequestID = %q, want %q", resp.RequestID, "req_del_1")
	}
}

func TestFace2FA_DeleteUser_EmptyID(t *testing.T) {
	client := NewClient("sk_test_key")

	_, _, err := client.Face2FA.DeleteUser(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty user id")
	}
}

func TestFace2FA_DeleteUser_ServerError(t *testing.T) {
	client, mux, teardown := setup()
	defer teardown()

	mux.HandleFunc("/"+apiVersion+"/2fa/users/usr_123", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{
			"success": false,
			"error": {"code": "INTERNAL_ERROR", "message": "failed to delete enrollment"}
		}`)
	})

	_, _, err := client.Face2FA.DeleteUser(context.Background(), "usr_123")
	if err == nil {
		t.Fatal("expected error")
	}

	var srvErr *ServerError
	if !errors.As(err, &srvErr) {
		t.Fatalf("expected *ServerError, got %T: %v", err, err)
	}
}

func TestFace2FAStatus_IsTerminal(t *testing.T) {
	tests := []struct {
		status Face2FAStatus
		want   bool
	}{
		{Face2FAStatusProcessing, false},
		{Face2FAStatusCompleted, true},
		{Face2FAStatusFailed, true},
		{Face2FAStatusExpired, true},
		{Face2FAStatus("some_future_status"), false},
	}

	for _, tt := range tests {
		if got := tt.status.IsTerminal(); got != tt.want {
			t.Errorf("%q.IsTerminal() = %v, want %v", tt.status, got, tt.want)
		}
	}
}

func boolPtr(b bool) *bool { return &b }

func reasonPtr(r Face2FAFailureReason) *Face2FAFailureReason { return &r }
