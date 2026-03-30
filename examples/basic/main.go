// Example: Pure Go HTTP server with Xident verification.
//
// This demonstrates the complete integration flow:
// 1. Create a verification session and redirect the user
// 2. Handle the callback webhook
// 3. Handle the redirect and check the result server-side
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

	xident "github.com/xident-io/xident-go"
)

func main() {
	apiKey := os.Getenv("XIDENT_SECRET_KEY")
	if apiKey == "" {
		log.Fatal("XIDENT_SECRET_KEY environment variable is required")
	}

	webhookSecret := os.Getenv("XIDENT_WEBHOOK_SECRET")
	if webhookSecret == "" {
		log.Fatal("XIDENT_WEBHOOK_SECRET environment variable is required")
	}

	client := xident.NewClient(apiKey,
		xident.WithTimeout(15*time.Second),
	)

	mux := http.NewServeMux()

	// Start verification: creates a session and redirects the user.
	mux.HandleFunc("/verify", func(w http.ResponseWriter, r *http.Request) {
		result, _, err := client.Verification.Init(r.Context(), &xident.InitParams{
			CallbackURL: "https://example.com/webhook",
			MinAge:      18,
			SuccessURL:  "https://example.com/success",
			FailedURL:   "https://example.com/failed",
			UserID:      "user_123",
		})
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to init: %v", err), 500)
			return
		}

		// Redirect user to the verification widget.
		http.Redirect(w, r, result.VerifyURL, http.StatusFound)
	})

	// Webhook handler: receives notifications when verification completes.
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
		case "session.completed":
			log.Printf("Verification completed: %v", event.Data)
		case "session.failed":
			log.Printf("Verification failed: %v", event.Data)
		default:
			log.Printf("Unknown event type: %s", event.Type)
		}

		w.WriteHeader(http.StatusOK)
	})

	// Success redirect: user lands here after successful verification.
	// ALWAYS re-verify server-side -- never trust URL parameters alone.
	mux.HandleFunc("/success", func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
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
			"verified":    session.IsVerified(),
			"status":      session.Status,
			"age_bracket": session.AgeBracket(),
			"method":      session.Method(),
			"terminal":    session.IsTerminal(),
		})
	})

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
