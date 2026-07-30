// Example: Echo framework integration with Xident verification.
//
// This demonstrates how to use the Xident Go SDK with the Echo web framework.
//
// Requirements:
//
//	go get github.com/labstack/echo/v4
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

	"github.com/labstack/echo/v4"
	xident "github.com/xident-io/go-sdk"
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

	e := echo.New()

	// Start verification session.
	e.POST("/verify", func(c echo.Context) error {
		result, _, err := client.Verification.Init(c.Request().Context(), &xident.InitParams{
			// The browser is redirected back to CallbackURL when the flow ends.
			CallbackURL: "https://example.com/callback",
			MinAge:      18,
		})
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}

		// result.Token is the init token (xit_) — redirect the browser to VerifyURL.
		return c.JSON(http.StatusOK, map[string]string{
			"token":      result.Token,
			"verify_url": result.VerifyURL,
		})
	})

	// Callback: the widget redirects the browser here with
	//   ?status=success|failed|canceled&token=xtk_...&user_id=...
	// Re-verify server-side; never trust the query params alone.
	e.GET("/callback", func(c echo.Context) error {
		token := c.QueryParam("token") // the RESULT token (xtk_...)

		ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
		defer cancel()

		session, _, err := client.Verification.GetResult(ctx, token)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}

		return c.JSON(http.StatusOK, map[string]any{
			"callback_status": c.QueryParam("status"),
			"verified":        session.IsVerified(),
			"status":          session.Status,
			"age_bracket":     session.AgeBracket(),
			"method":          session.Method(),
			"terminal":        session.IsTerminal(),
		})
	})

	// Webhook handler. OPTIONAL, separate feature -- not part of the core
	// redirect flow above. Enable only if you configured a webhook secret.
	e.POST("/webhook", func(c echo.Context) error {
		body, err := io.ReadAll(c.Request().Body)
		if err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Failed to read body"})
		}

		signature := c.Request().Header.Get("X-Xident-Signature")
		event, err := client.Webhooks.ConstructEvent(body, signature, webhookSecret)
		if err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid signature"})
		}

		switch event.Type {
		// "session.completed" is the pre-July-2026 name; an endpoint
		// registered before then still receives it.
		case "session.success", "session.completed":
			log.Printf("Verification completed: %v", event.Data)
		case "session.failed":
			log.Printf("Verification failed: %v", event.Data)
		}

		return c.NoContent(http.StatusOK)
	})

	// Check verification result.
	e.GET("/result/:token", func(c echo.Context) error {
		token := c.Param("token")

		ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
		defer cancel()

		session, _, err := client.Verification.GetResult(ctx, token)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}

		return c.JSON(http.StatusOK, map[string]any{
			"verified":    session.IsVerified(),
			"status":      session.Status,
			"age_bracket": session.AgeBracket(),
			"method":      session.Method(),
			"terminal":    session.IsTerminal(),
		})
	})

	e.Logger.Fatal(e.Start(":8080"))
}
