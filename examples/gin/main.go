// Example: Gin framework integration with Xident verification.
//
// This demonstrates how to use the Xident Go SDK with the Gin web framework.
//
// Requirements:
//
//	go get github.com/gin-gonic/gin
//
// Run with:
//
//	XIDENT_SECRET_KEY=sk_test_xxx XIDENT_WEBHOOK_SECRET=whsec_xxx go run main.go
package main

import (
	"context"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	xident "github.com/xident-io/go-sdk/v3"
)

func main() {
	apiKey := os.Getenv("XIDENT_SECRET_KEY")
	webhookSecret := os.Getenv("XIDENT_WEBHOOK_SECRET")

	if apiKey == "" || webhookSecret == "" {
		log.Fatal("XIDENT_SECRET_KEY and XIDENT_WEBHOOK_SECRET are required")
	}

	client := xident.NewClient(apiKey,
		xident.WithTimeout(15*time.Second),
	)

	r := gin.Default()

	// Start verification session.
	r.POST("/verify", func(c *gin.Context) {
		result, _, err := client.Verification.Init(c.Request.Context(), &xident.InitParams{
			// The browser is redirected back to CallbackURL when the flow ends.
			CallbackURL: "https://example.com/callback",
			MinAge:      18,
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// result.Token is the init token (xit_) — redirect the browser to VerifyURL.
		c.JSON(http.StatusOK, gin.H{
			"token":      result.Token,
			"verify_url": result.VerifyURL,
		})
	})

	// Callback: the widget redirects the browser here with
	//   ?status=success|failed|canceled&token=xtk_...&user_id=...
	// Re-verify server-side; never trust the query params alone.
	r.GET("/callback", func(c *gin.Context) {
		token := c.Query("token") // the RESULT token (xtk_...)

		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()

		session, _, err := client.Verification.GetResult(ctx, token)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"callback_status": c.Query("status"),
			"verified":        session.IsVerified(),
			"status":          session.Status,
			// session.Checks holds the per-check breakdown (liveness, age,
			// document, face_match); AgeBracket() reads Checks.Age for you.
			"age_bracket": session.AgeBracket(),
			// "full" (document + biometric checks ran) or "token" (returning
			// Xident-ID user) -- session.VerificationType, not an ML method name.
			"method":   session.Method(),
			"terminal": session.IsTerminal(),
		})
	})

	// Webhook handler. OPTIONAL, separate feature -- not part of the core
	// redirect flow above. Enable only if you configured a webhook secret.
	r.POST("/webhook", func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to read body"})
			return
		}

		signature := c.GetHeader("X-Xident-Signature")
		event, err := client.Webhooks.ConstructEvent(body, signature, webhookSecret)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid signature"})
			return
		}

		switch event.Type {
		// "session.completed" is the pre-July-2026 name; an endpoint
		// registered before then still receives it.
		case "session.success", "session.completed":
			log.Printf("Verification completed: %v", event.Data)
		case "session.failed":
			log.Printf("Verification failed: %v", event.Data)
		}

		c.Status(http.StatusOK)
	})

	// Check verification result.
	r.GET("/result/:token", func(c *gin.Context) {
		token := c.Param("token")

		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()

		session, _, err := client.Verification.GetResult(ctx, token)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"verified": session.IsVerified(),
			"status":   session.Status,
			// AgeBracket() reads session.Checks.Age; Method() returns
			// session.VerificationType ("full" | "age_check" | "xident_id" | "eu_wallet").
			"age_bracket": session.AgeBracket(),
			"method":      session.Method(),
			"terminal":    session.IsTerminal(),
		})
	})

	log.Fatal(r.Run(":8080"))
}
