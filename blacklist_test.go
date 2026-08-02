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

func TestBlacklist_List(t *testing.T) {
	client, mux, teardown := setup()
	defer teardown()

	mux.HandleFunc("/"+apiVersion+"/blacklist", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %q, want GET", r.Method)
		}
		if got := r.Header.Get("X-API-Key"); got != "sk_test_123" {
			t.Errorf("X-API-Key = %q, want %q", got, "sk_test_123")
		}
		if got := r.URL.Query().Get("page"); got != "2" {
			t.Errorf("page = %q, want %q", got, "2")
		}
		if got := r.URL.Query().Get("per_page"); got != "10" {
			t.Errorf("per_page = %q, want %q", got, "10")
		}

		fmt.Fprint(w, `{
			"success": true,
			"data": [
				{"id": 42, "reason": "fraud", "source": "session", "session_id": 7, "created_at": "2026-07-30T10:00:00Z"},
				{"id": 43, "reason": "chargeback abuse", "source": "image", "created_at": "2026-07-31T11:00:00Z"}
			],
			"meta": {
				"request_id": "req_bl_list",
				"pagination": {"page": 2, "per_page": 10, "total": 25, "total_pages": 3}
			}
		}`)
	})

	entries, resp, err := client.Blacklist.List(context.Background(), &BlacklistListOptions{
		Page:    2,
		PerPage: 10,
	})
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
	if entries[0].ID != 42 {
		t.Errorf("entries[0].ID = %d, want 42", entries[0].ID)
	}
	if entries[0].Reason != "fraud" {
		t.Errorf("entries[0].Reason = %q, want %q", entries[0].Reason, "fraud")
	}
	if entries[0].Source != "session" {
		t.Errorf("entries[0].Source = %q, want %q", entries[0].Source, "session")
	}
	if entries[0].SessionID == nil || *entries[0].SessionID != 7 {
		t.Errorf("entries[0].SessionID = %v, want 7", entries[0].SessionID)
	}
	if entries[0].CreatedAt != "2026-07-30T10:00:00Z" {
		t.Errorf("entries[0].CreatedAt = %q, want %q", entries[0].CreatedAt, "2026-07-30T10:00:00Z")
	}
	if entries[1].SessionID != nil {
		t.Errorf("entries[1].SessionID = %v, want nil", *entries[1].SessionID)
	}

	if resp.RequestID != "req_bl_list" {
		t.Errorf("RequestID = %q, want %q", resp.RequestID, "req_bl_list")
	}
	if resp.Pagination == nil {
		t.Fatal("Pagination should be present")
	}
	if resp.Pagination.Page != 2 {
		t.Errorf("Pagination.Page = %d, want 2", resp.Pagination.Page)
	}
	if resp.Pagination.PerPage != 10 {
		t.Errorf("Pagination.PerPage = %d, want 10", resp.Pagination.PerPage)
	}
	if resp.Pagination.Total != 25 {
		t.Errorf("Pagination.Total = %d, want 25", resp.Pagination.Total)
	}
	if resp.Pagination.TotalPages != 3 {
		t.Errorf("Pagination.TotalPages = %d, want 3", resp.Pagination.TotalPages)
	}
}

func TestBlacklist_List_NilOptions(t *testing.T) {
	client, mux, teardown := setup()
	defer teardown()

	mux.HandleFunc("/"+apiVersion+"/blacklist", func(w http.ResponseWriter, r *http.Request) {
		// Nil options must send no pagination params -- server defaults apply.
		if r.URL.RawQuery != "" {
			t.Errorf("query = %q, want empty", r.URL.RawQuery)
		}
		fmt.Fprint(w, `{
			"success": true,
			"data": [],
			"meta": {"pagination": {"page": 1, "per_page": 20, "total": 0, "total_pages": 0}}
		}`)
	})

	entries, resp, err := client.Blacklist.List(context.Background(), nil)
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("len(entries) = %d, want 0", len(entries))
	}
	if resp.Pagination == nil || resp.Pagination.PerPage != 20 {
		t.Errorf("Pagination = %+v, want per_page 20", resp.Pagination)
	}
}

func TestBlacklist_List_AuthError(t *testing.T) {
	client, mux, teardown := setup()
	defer teardown()

	mux.HandleFunc("/"+apiVersion+"/blacklist", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{
			"success": false,
			"error": {"code": "UNAUTHORIZED", "message": "not authenticated"}
		}`)
	})

	_, _, err := client.Blacklist.List(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error")
	}

	var authErr *AuthenticationError
	if !errors.As(err, &authErr) {
		t.Fatalf("expected *AuthenticationError, got %T: %v", err, err)
	}
}

func TestBlacklist_AddBySession(t *testing.T) {
	client, mux, teardown := setup()
	defer teardown()

	mux.HandleFunc("/"+apiVersion+"/blacklist/session", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}

		body, _ := io.ReadAll(r.Body)
		var params map[string]any
		json.Unmarshal(body, &params)

		if params["session_token"] != "xst_abc" {
			t.Errorf("session_token = %v, want %q", params["session_token"], "xst_abc")
		}
		if params["reason"] != "fraud attempt" {
			t.Errorf("reason = %v, want %q", params["reason"], "fraud attempt")
		}

		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{
			"success": true,
			"data": {"status": "processing"},
			"meta": {"request_id": "req_bl_sess"}
		}`)
	})

	result, resp, err := client.Blacklist.AddBySession(context.Background(), &BlacklistAddSessionParams{
		SessionToken: "xst_abc",
		Reason:       "fraud attempt",
	})
	if err != nil {
		t.Fatalf("AddBySession() error: %v", err)
	}
	if result.Status != "processing" {
		t.Errorf("Status = %q, want %q", result.Status, "processing")
	}
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("StatusCode = %d, want 201", resp.StatusCode)
	}
}

func TestBlacklist_AddBySession_Errors(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantErr    func(error) bool
	}{
		{
			name:       "session not found",
			statusCode: http.StatusNotFound,
			body:       `{"success": false, "error": {"code": "NOT_FOUND", "message": "session not found"}}`,
			wantErr: func(err error) bool {
				var e *NotFoundError
				return errors.As(err, &e)
			},
		},
		{
			name:       "session still in progress (409)",
			statusCode: http.StatusConflict,
			body:       `{"success": false, "error": {"code": "CONFLICT", "message": "session is still in progress"}}`,
			wantErr: func(err error) bool {
				var e *ValidationError
				return errors.As(err, &e)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, mux, teardown := setup()
			defer teardown()

			mux.HandleFunc("/"+apiVersion+"/blacklist/session", func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				fmt.Fprint(w, tt.body)
			})

			_, _, err := client.Blacklist.AddBySession(context.Background(), &BlacklistAddSessionParams{
				SessionToken: "xst_abc",
				Reason:       "fraud",
			})
			if err == nil {
				t.Fatal("expected error")
			}
			if !tt.wantErr(err) {
				t.Fatalf("wrong error type: %T: %v", err, err)
			}
		})
	}
}

func TestBlacklist_AddBySession_NilParams(t *testing.T) {
	client := NewClient("sk_test_key")

	_, _, err := client.Blacklist.AddBySession(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil params")
	}
}

func TestBlacklist_AddByImage(t *testing.T) {
	client, mux, teardown := setup()
	defer teardown()

	mux.HandleFunc("/"+apiVersion+"/blacklist/image", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}

		body, _ := io.ReadAll(r.Body)
		var params map[string]any
		json.Unmarshal(body, &params)

		if params["image"] != "aW1hZ2U=" {
			t.Errorf("image = %v, want %q", params["image"], "aW1hZ2U=")
		}
		if params["reason"] != "known bad actor" {
			t.Errorf("reason = %v, want %q", params["reason"], "known bad actor")
		}

		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"success": true, "data": {"status": "processing"}}`)
	})

	result, _, err := client.Blacklist.AddByImage(context.Background(), &BlacklistAddImageParams{
		Image:  "aW1hZ2U=",
		Reason: "known bad actor",
	})
	if err != nil {
		t.Fatalf("AddByImage() error: %v", err)
	}
	if result.Status != "processing" {
		t.Errorf("Status = %q, want %q", result.Status, "processing")
	}
}

func TestBlacklist_AddByImage_NilParams(t *testing.T) {
	client := NewClient("sk_test_key")

	_, _, err := client.Blacklist.AddByImage(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil params")
	}
}

func TestBlacklist_Remove(t *testing.T) {
	client, mux, teardown := setup()
	defer teardown()

	mux.HandleFunc("/"+apiVersion+"/blacklist/42", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %q, want DELETE", r.Method)
		}
		fmt.Fprint(w, `{
			"success": true,
			"data": {"message": "blacklist entry removed"},
			"meta": {"request_id": "req_bl_rm"}
		}`)
	})

	result, resp, err := client.Blacklist.Remove(context.Background(), 42)
	if err != nil {
		t.Fatalf("Remove() error: %v", err)
	}
	if result.Message != "blacklist entry removed" {
		t.Errorf("Message = %q, want %q", result.Message, "blacklist entry removed")
	}
	if resp.RequestID != "req_bl_rm" {
		t.Errorf("RequestID = %q, want %q", resp.RequestID, "req_bl_rm")
	}
}

func TestBlacklist_Remove_InvalidID(t *testing.T) {
	client := NewClient("sk_test_key")

	for _, id := range []int64{0, -1} {
		if _, _, err := client.Blacklist.Remove(context.Background(), id); err == nil {
			t.Errorf("Remove(%d) expected error", id)
		}
	}
}

func TestBlacklist_Remove_NotFound(t *testing.T) {
	client, mux, teardown := setup()
	defer teardown()

	mux.HandleFunc("/"+apiVersion+"/blacklist/999", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{
			"success": false,
			"error": {"code": "NOT_FOUND", "message": "blacklist entry not found"}
		}`)
	})

	_, _, err := client.Blacklist.Remove(context.Background(), 999)
	if err == nil {
		t.Fatal("expected error")
	}

	var notFound *NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("expected *NotFoundError, got %T: %v", err, err)
	}
}
