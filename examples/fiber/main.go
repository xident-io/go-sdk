// Example: Fiber framework integration with Xident verification.
//
// This demonstrates how to use the Xident Go SDK with the Fiber web framework.
//
// Requirements:
//
//	go get github.com/gofiber/fiber/v2
//
// Run with:
//
//	XIDENT_SECRET_KEY=sk_test_xxx XIDENT_WEBHOOK_SECRET=whsec_xxx go run main.go
package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/gofiber/fiber/v2"
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

	app := fiber.New()

	// Start verification session.
	app.Post("/verify", func(c *fiber.Ctx) error {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		result, _, err := client.Verification.Init(ctx, &xident.InitParams{
			CallbackURL: "https://example.com/webhook",
			MinAge:      18,
			SuccessURL:  "https://example.com/success",
			FailedURL:   "https://example.com/failed",
		})
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": err.Error(),
			})
		}

		return c.JSON(fiber.Map{
			"token":      result.Token,
			"verify_url": result.VerifyURL,
		})
	})

	// Webhook handler.
	// Note: Fiber consumes the body, so use c.Body() directly.
	app.Post("/webhook", func(c *fiber.Ctx) error {
		body := c.Body()
		signature := c.Get("X-Xident-Signature")

		event, err := client.Webhooks.ConstructEvent(body, signature, webhookSecret)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "Invalid signature",
			})
		}

		switch event.Type {
		case "session.completed":
			log.Printf("Verification completed: %v", event.Data)
		case "session.failed":
			log.Printf("Verification failed: %v", event.Data)
		}

		return c.SendStatus(fiber.StatusOK)
	})

	// Check verification result.
	app.Get("/result/:token", func(c *fiber.Ctx) error {
		token := c.Params("token")

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		session, _, err := client.Verification.GetResult(ctx, token)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": err.Error(),
			})
		}

		return c.JSON(fiber.Map{
			"verified":    session.IsVerified(),
			"status":      session.Status,
			"age_bracket": session.AgeBracket(),
			"method":      session.Method(),
			"terminal":    session.IsTerminal(),
		})
	})

	log.Fatal(app.Listen(":8080"))
}
