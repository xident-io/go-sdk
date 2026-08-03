// Example: Pure Go HTTP server with Xident verification.
//
// This demonstrates the complete integration flow:
//  1. POST /verify        - create a verification session and redirect the user
//  2. GET  /callback      - the browser is redirected back here with the result
//     token; re-verify server-side (this is the primary flow)
//  3. POST /webhook       - OPTIONAL signed server-to-server notification
//
// The callback_url is a plain browser GET redirect, NOT a signed webhook. The
// widget appends ?status=success|failed|canceled, token=xtk_... (the RESULT
// token, distinct from the xit_ init token), and user_id (if you supplied one).
//
// Run with:
//
//	go run main.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	xident "github.com/xident-io/go-sdk"
)

func main() {
	apiKey := os.Getenv("XIDENT_SECRET_KEY")
	if apiKey == "" {
		log.Fatal("XIDENT_SECRET_KEY environment variable is required")
	}

	client := xident.NewClient(apiKey,
		xident.WithTimeout(15*time.Second),
	)

	mux := http.NewServeMux()

	// Start verification: creates a session and redirects the user.
	mux.HandleFunc("/verify", func(w http.ResponseWriter, r *http.Request) {
		result, _, err := client.Verification.Init(r.Context(), &xident.InitParams{
			// The browser is redirected back to CallbackURL when the flow ends.
			CallbackURL: "https://example.com/callback",
			MinAge:      18,
			UserID:      "user_123",
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
	// ALWAYS re-verify server-side -- never trust the query params alone.
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		status := q.Get("status") // "success", "failed", or "canceled"
		token := q.Get("token")   // the RESULT token (xtk_...)
		userID := q.Get("user_id")

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

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"callback_status": status, // same vocabulary as the result endpoint
			"user_id":         userID,
			"verified":        session.IsVerified(),
			"status":          session.Status, // American "canceled" from /result
			// AgeBracket() reads session.Checks.Age (Gate, guarded by Passed) --
			// no more parsing an "age_result" blob by hand.
			"age_bracket": session.AgeBracket(),
			// Method() is session.VerificationMode: "full" (document +
			// biometric checks ran) or "token" (returning Xident-ID user), not
			// the client-side ML method name older SDK versions returned here.
			"method":   session.Method(),
			"terminal": session.IsTerminal(),
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
