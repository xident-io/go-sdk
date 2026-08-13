package xident

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
)

// Every request must carry the dated API version this SDK was built against.
//
// Pinning in the SDK rather than relying on the project's dashboard setting is
// what guarantees the result types in this package match the payload the server
// sends: a customer pinned to an older version still receives the shape these
// structs parse. Without the header, a newer SDK reading an older shape would
// silently leave fields empty — the same failure class as the three fields this
// SDK dropped for a week in August.
func TestEveryRequestCarriesTheAPIVersion(t *testing.T) {
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("X-API-Version")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"token":"xtk_x","status":"success"}}`))
	}))
	defer srv.Close()

	c := NewClient("sk_test_abc", WithBaseURL(srv.URL))
	_, _, _ = c.Verification.GetResult(context.Background(), "xtk_x")

	if seen != PinnedAPIVersion {
		t.Errorf("X-API-Version = %q, want %q", seen, PinnedAPIVersion)
	}
}

// The override exists so a customer can trial a newer API version before changing
// their dashboard pin.
func TestWithAPIVersionOverrides(t *testing.T) {
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("X-API-Version")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"token":"xtk_x","status":"success"}}`))
	}))
	defer srv.Close()

	c := NewClient("sk_test_abc", WithBaseURL(srv.URL), WithAPIVersion("2027-01-01"))
	_, _, _ = c.Verification.GetResult(context.Background(), "xtk_x")
	if seen != "2027-01-01" {
		t.Errorf("override not applied: %q", seen)
	}

	// An empty override must be ignored, not sent: the server rejects an empty
	// X-API-Version as invalid, so honouring "" would break every request.
	c2 := NewClient("sk_test_abc", WithBaseURL(srv.URL), WithAPIVersion(""))
	_, _, _ = c2.Verification.GetResult(context.Background(), "xtk_x")
	if seen != PinnedAPIVersion {
		t.Errorf("an empty override was honoured: %q", seen)
	}
}

// The pinned version must be a real date, and must NOT be derived from SDKVersion
// — they move on different clocks, and an SDK patch release must never change
// which API shape a customer receives.
func TestPinnedAPIVersionIsADateAndIndependentOfSDKVersion(t *testing.T) {
	if !regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`).MatchString(PinnedAPIVersion) {
		t.Errorf("PinnedAPIVersion %q is not YYYY-MM-DD", PinnedAPIVersion)
	}
	if PinnedAPIVersion == SDKVersion {
		t.Error("PinnedAPIVersion equals SDKVersion; they must be independent")
	}
	// And it must not be confused with the URL path prefix.
	if PinnedAPIVersion == apiVersion {
		t.Error("PinnedAPIVersion equals the URL path prefix; these are different things")
	}
}
