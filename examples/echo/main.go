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
	xident "github.com/xident-io/xident-go"
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
			CallbackURL: "https://example.com/webhook",
			MinAge:      18,
			SuccessURL:  "https://example.com/success",
			FailedURL:   "https://example.com/failed",
		})
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}

		return c.JSON(http.StatusOK, map[string]string{
			"token":      result.Token,
			"verify_url": result.VerifyURL,
		})
	})

	// Webhook handler.
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
		case "session.completed":
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
