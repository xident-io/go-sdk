// Example: Pure Go HTTP server with Xident verification.
//
// This demonstrates the complete integration flow:
//  1. POST /verify        - create a verification session for the signed-in
//     user and redirect the browser to the widget
//  2. GET  /callback      - the browser is redirected back here with the result
//     token; decide server-side (this is the primary flow)
//  3. POST /webhook       - OPTIONAL signed server-to-server notification
//
// The callback_url is a plain browser GET redirect, NOT a signed webhook. The
// widget appends ?status=success|failed|canceled, token=xtk_... (the RESULT
// token, distinct from the xit_ init token), and user_id (the UserID you sent).
// Anyone can edit those query parameters, so the callback decides from the
// result alone, and checks it against what THIS server asked for.
//
// Run with:
//
//	XIDENT_SECRET_KEY=sk_test_xxx go run main.go
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	xident "github.com/xident-io/go-sdk/v3"
)

// requiredMinAge is the age this site needs. It is set here, on the server,
// and never read from a request: a browser that could send it could also
// lower it.
const requiredMinAge = 18

const sessionCookie = "example_session"

// sessionStore stands in for your app's login sessions. It keeps the
// signed-in user's id on the server, behind an unguessable cookie, so a
// browser cannot claim to be another user by editing a cookie or the
// callback URL. In your app, use your real session instead.
type sessionStore struct {
	mu    sync.Mutex
	users map[string]string // session id -> user id
}

func (s *sessionStore) userID(r *http.Request) (string, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.users[c.Value]
	return id, ok
}

// signIn creates a demo user and a session for it. Your app does this at
// login; the example has no login, so every new visitor gets a user.
func (s *sessionStore) signIn(w http.ResponseWriter, r *http.Request) string {
	sessionID, userID := randomHex(32), "user_"+randomHex(8)
	s.mu.Lock()
	s.users[sessionID] = userID
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: sessionID, Path: "/",
		HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteLaxMode,
	})
	return userID
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func main() {
	apiKey := os.Getenv("XIDENT_SECRET_KEY") // sk_live_, sk_test_, ak_live_ or ak_test_
	// XIDENT_ALLOW_TEST_RESULTS=1 lets a test-key result count as proof, so
	// a local run with a sk_test_ key can show a pass. A test session settles
	// without checking anyone: never set it in production. It is refused
	// with a live key.
	allowTestResults := os.Getenv("XIDENT_ALLOW_TEST_RESULTS") == "1"
	if allowTestResults && !strings.HasPrefix(apiKey, "sk_test_") && !strings.HasPrefix(apiKey, "ak_test_") {
		log.Fatal("XIDENT_ALLOW_TEST_RESULTS=1 is only for a test key (sk_test_ or ak_test_)")
	}
	if apiKey == "" {
		log.Fatal("XIDENT_SECRET_KEY environment variable is required")
	}

	client := xident.NewClient(apiKey,
		xident.WithTimeout(15*time.Second),
	)
	sessions := &sessionStore{users: map[string]string{}}

	mux := http.NewServeMux()

	// Start verification: creates a session and redirects the user.
	mux.HandleFunc("/verify", func(w http.ResponseWriter, r *http.Request) {
		userID, ok := sessions.userID(r)
		if !ok {
			userID = sessions.signIn(w, r)
		}

		result, _, err := client.Verification.Init(r.Context(), &xident.InitParams{
			// The browser is redirected back to CallbackURL when the flow ends.
			CallbackURL: "https://example.com/callback",
			// The age this site needs, from the server. Xident rounds it up
			// to the next of 12, 15, 18, 21 or 25 (19 is enforced as 21).
			MinAge: requiredMinAge,
			// UserID is required: the signed-in user's id, from the server.
			UserID: userID,
			// SuccessURL / FailedURL are optional status-specific redirect
			// overrides; the callback query params carry the result either way.
		})
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to init: %v", err), 500)
			return
		}

		// result.Token is the init token (xit_) — do NOT pass it to GetResult.
		// Redirect user to the verification widget.
		http.Redirect(w, r, result.VerifyURL, http.StatusFound)
	})

	// Callback: the widget redirects the browser here after verification with
	//   ?status=success|failed|canceled&token=xtk_...&user_id=...
	// Decide server-side from the result -- never from the query params.
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		userID, ok := sessions.userID(r)
		if !ok {
			http.Error(w, "Sign in first", http.StatusUnauthorized)
			return
		}
		token := r.URL.Query().Get("token") // the RESULT token (xtk_...)
		if token == "" {
			http.Error(w, "Missing token", 400)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		session, _, err := client.Verification.GetResult(ctx, token)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to get result: %v", err), 500)
			return
		}

		// Grant access only when both hold:
		//   - the session passed and proves the age THIS site needs
		//     (ProvesAge: success, and checks.age.gate >= requiredMinAge;
		//     an ID verification has no gate and proves no age);
		//   - the result belongs to THIS signed-in user, not to whoever
		//     earned the token someone pasted into the URL.
		provesAge := session.ProvesAge(requiredMinAge) // refuses a test-key result
		if allowTestResults {
			provesAge = session.ProvesAgeAllowingTest(requiredMinAge)
		}
		verified := provesAge && session.ExternalUserID == userID

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"verified": verified,
		})
	})

	// OPTIONAL: signed server-to-server webhook. This is a separate feature
	// from the callback redirect above and is NOT required for the core flow.
	// Enable it only if you configured a webhook secret in the dashboard.
	if webhookSecret := os.Getenv("XIDENT_WEBHOOK_SECRET"); webhookSecret != "" {
		mux.HandleFunc("/webhook", func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, "Failed to read body", 400)
				return
			}

			signature := r.Header.Get("X-Xident-Signature")

			event, err := client.Webhooks.ConstructEvent(body, signature, webhookSecret)
			if err != nil {
				log.Printf("Webhook verification failed: %v", err)
				http.Error(w, "Invalid signature", 400)
				return
			}

			// event.Data is the same tenant result GetResult returns. To grant
			// access from a webhook, decode it into xident.SessionResult and
			// apply the same checks as the callback: ProvesAge with your own
			// required age, and ExternalUserID against your own user.
			switch event.Type {
			// "session.completed" is the pre-July-2026 name; an endpoint
			// registered before then still receives it.
			case "session.success", "session.completed":
				log.Printf("Verification completed: %v", event.Data)
			case "session.failed":
				log.Printf("Verification failed: %v", event.Data)
			default:
				log.Printf("Unknown event type: %s", event.Type)
			}

			w.WriteHeader(http.StatusOK)
		})
	}

	addr := ":8080"
	log.Printf("Server starting on %s", addr)

	// Use TLS in production. For local development, generate self-signed certs:
	//   openssl req -x509 -newkey rsa:4096 -keyout key.pem -out cert.pem -days 365 -nodes
	certFile := os.Getenv("TLS_CERT_FILE")
	keyFile := os.Getenv("TLS_KEY_FILE")
	if certFile != "" && keyFile != "" {
		log.Fatal(http.ListenAndServeTLS(addr, certFile, keyFile, mux))
	} else {
		log.Println("WARNING: Running without TLS. Set TLS_CERT_FILE and TLS_KEY_FILE for production.")
		server := &http.Server{Addr: addr, Handler: mux}
		log.Fatal(server.ListenAndServe())
	}
}
