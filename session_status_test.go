package xident

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The existing helper tests (session_result_test.go) compare SessionStatus
// CONSTANTS against each other, so they pass whatever string those constants
// hold. That is what let this SDK ship comparing against "completed" for the
// entire window after the API renamed the verdict to "success": every test was
// green and every verified user was reported unverified.
//
// The tests below pin the wire LITERALS instead.

func TestSessionStatusWireValues(t *testing.T) {
	// These strings are the API contract. Changing one here without changing
	// the API is how an SDK silently stops recognising a real verdict.
	for _, tt := range []struct {
		status SessionStatus
		want   string
	}{
		{SessionStatusPending, "pending"},
		{SessionStatusInProgress, "in_progress"},
		{SessionStatusSuccess, "success"},
		{SessionStatusFailed, "failed"},
		{SessionStatusCanceled, "canceled"},
		{SessionStatusClaimed, "claimed"},
	} {
		if string(tt.status) != tt.want {
			t.Errorf("wire value = %q, want %q", string(tt.status), tt.want)
		}
	}
}

// The deprecated alias must resolve to the CURRENT value, not the historical
// one. Existing consumer code says `result.Status == SessionStatusCompleted`;
// if that constant still held "completed" the comparison would compile and
// quietly return false for every verified user.
func TestDeprecatedCompletedAliasResolvesToSuccess(t *testing.T) {
	if SessionStatusCompleted != SessionStatusSuccess {
		t.Fatalf("SessionStatusCompleted = %q, must equal SessionStatusSuccess (%q)",
			SessionStatusCompleted, SessionStatusSuccess)
	}

	// The end-to-end shape of the compatibility promise.
	r := &SessionResult{Status: SessionStatusSuccess}
	if r.Status != SessionStatusCompleted {
		t.Error("pre-rename consumer code comparing against SessionStatusCompleted no longer matches a verified session")
	}
}

func TestSessionStatusUnmarshalNormalisesLegacyValue(t *testing.T) {
	for _, tt := range []struct {
		name     string
		wire     string
		want     SessionStatus
		verified bool
		terminal bool
	}{
		{"current value", `"success"`, SessionStatusSuccess, true, true},
		{"legacy value from a pre-migration-120 deployment", `"completed"`, SessionStatusSuccess, true, true},
		{"failed", `"failed"`, SessionStatusFailed, false, true},
		{"canceled, American spelling is the only spelling", `"canceled"`, SessionStatusCanceled, false, true},
		{"pending", `"pending"`, SessionStatusPending, false, false},
		// Forgiving on purpose: a status added server-side later must not
		// break an already-released SDK, and an unrecognised value must not
		// be mistaken for a finished verification.
		{"unknown value passes through and is not terminal", `"quarantined"`, SessionStatus("quarantined"), false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var got SessionStatus
			if err := json.Unmarshal([]byte(tt.wire), &got); err != nil {
				t.Fatalf("unmarshal %s: %v", tt.wire, err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
			if gotT := got.IsTerminal(); gotT != tt.terminal {
				t.Errorf("IsTerminal() = %v, want %v", gotT, tt.terminal)
			}
			if gotV := (&SessionResult{Status: got}).IsVerified(); gotV != tt.verified {
				t.Errorf("IsVerified() = %v, want %v", gotV, tt.verified)
			}
		})
	}
}

// Normalisation must survive being nested in the real DTO -- a custom
// UnmarshalJSON on a field type is easy to bypass by accident (for example by
// decoding into map[string]any and casting).
func TestSessionResultNormalisesLegacyStatusAndCarriesReason(t *testing.T) {
	var r SessionResult
	body := `{"id":"sess_1","status":"completed","created_at":"2026-07-30T00:00:00Z"}`
	if err := json.Unmarshal([]byte(body), &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !r.IsVerified() {
		t.Errorf("legacy %q status did not normalise inside SessionResult: got %q", "completed", r.Status)
	}

	var failed SessionResult
	body = `{"id":"sess_2","status":"failed","reason":"age_below_threshold","created_at":"2026-07-30T00:00:00Z"}`
	if err := json.Unmarshal([]byte(body), &failed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if failed.IsVerified() {
		t.Error("a session that missed the age threshold must not report verified")
	}
	if failed.Reason != "age_below_threshold" {
		t.Errorf("Reason = %q, want %q", failed.Reason, "age_below_threshold")
	}
}

func TestSessionStatusUnmarshalRejectsNonString(t *testing.T) {
	var s SessionStatus
	if err := json.Unmarshal([]byte(`{"not":"a string"}`), &s); err == nil {
		t.Error("expected an error decoding an object into SessionStatus")
	}
}

// TestNoStaleStatusLiteralsInSource is this repo's own copy of the guard that
// exists in the api repo.
//
// It is duplicated rather than shared BECAUSE the api guard walks only the api
// module's AST -- it is structurally incapable of seeing a sibling repo, and
// that blind spot is precisely how all seven SDKs kept shipping the old
// vocabulary after the rename. There is no CI spanning the eight repos, so
// each one needs its own.
func TestNoStaleStatusLiteralsInSource(t *testing.T) {
	// Walk subdirectories too. examples/ is shipped documentation -- an
	// integrator copies it verbatim, so a stale literal there is as harmful
	// as one in the library, and the first version of this guard scanned only
	// the root and missed five example files.
	var files []string
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Ext(path) == ".go" && !strings.HasSuffix(path, "_test.go") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	scanned := 0
	for _, name := range files {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		scanned++

		// The British spelling is gone from every surface, including the
		// browser callback, so it is stale in prose as well as in code --
		// scan the raw text, comments included.
		for i, line := range strings.Split(string(raw), "\n") {
			if strings.Contains(line, "cancelled") {
				t.Errorf(`%s:%d uses the British "cancelled"; the API uses "canceled" on every surface: %s`,
					name, i+1, strings.TrimSpace(line))
			}
		}

		// "completed" is different: a comment may legitimately explain the
		// rename, so a text scan flags its own documentation. Walk the AST
		// and look at real string literals only.
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, name, raw, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			// Two legitimate occurrences:
			//   - legacyStatusSuccess: the constant naming the pre-rename
			//     SESSION wire value so UnmarshalJSON can normalise it.
			//   - Face2FAStatusCompleted: face 2FA challenges are a different
			//     resource with a different vocabulary, and "completed" is
			//     their CURRENT wire value -- the July 2026 rename applied to
			//     session verdicts only.
			if spec, ok := n.(*ast.ValueSpec); ok {
				for _, id := range spec.Names {
					if id.Name == "legacyStatusSuccess" || id.Name == "Face2FAStatusCompleted" {
						return false
					}
				}
			}
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING || lit.Value != `"completed"` {
				return true
			}
			t.Errorf(`%s:%d hardcodes the literal "completed"; the session pass verdict is "success" (use legacyStatusSuccess for the legacy wire value, or Face2FAStatusCompleted for the 2FA challenge status)`,
				name, fset.Position(lit.Pos()).Line)
			return true
		})
	}

	if scanned == 0 {
		t.Fatal("scanned no source files -- the guard would pass vacuously")
	}
}
