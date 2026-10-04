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
	"crypto/rand"
	"encoding/hex"
	"log"
	"os"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
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

func (s *sessionStore) userID(c *fiber.Ctx) (string, bool) {
	sessionID := c.Cookies(sessionCookie)
	if sessionID == "" {
		return "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.users[sessionID]
	return id, ok
}

// signIn creates a demo user and a session for it. Your app does this at
// login; the example has no login, so every new visitor gets a user.
func (s *sessionStore) signIn(c *fiber.Ctx) string {
	sessionID, userID := randomHex(32), "user_"+randomHex(8)
	s.mu.Lock()
	s.users[sessionID] = userID
	s.mu.Unlock()
	c.Cookie(&fiber.Cookie{
		Name: sessionCookie, Value: sessionID, Path: "/",
		HTTPOnly: true, Secure: c.Protocol() == "https", SameSite: fiber.CookieSameSiteLaxMode,
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
	webhookSecret := os.Getenv("XIDENT_WEBHOOK_SECRET")

	if apiKey == "" || webhookSecret == "" {
		log.Fatal("XIDENT_SECRET_KEY and XIDENT_WEBHOOK_SECRET are required")
	}

	client := xident.NewClient(apiKey,
		xident.WithTimeout(15*time.Second),
	)
	sessions := &sessionStore{users: map[string]string{}}

	app := fiber.New()

	// Start verification session for the signed-in user.
	app.Post("/verify", func(c *fiber.Ctx) error {
		userID, ok := sessions.userID(c)
		if !ok {
			userID = sessions.signIn(c)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		result, _, err := client.Verification.Init(ctx, &xident.InitParams{
			// The browser is redirected back to CallbackURL when the flow ends.
			CallbackURL: "https://example.com/callback",
			// UserID is required: the signed-in user's id, from the server.
			// It comes back in the result as ExternalUserID.
			UserID: userID,
			// The age this site needs, from the server. Xident rounds it up
			// to the next of 12, 15, 18, 21 or 25 (19 is enforced as 21).
			MinAge: requiredMinAge,
		})
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": err.Error(),
			})
		}

		// Send the page only the link. It opens it unchanged, for example
		// with the browser SDK: Xident.start({ verifyUrl }).
		return c.JSON(fiber.Map{
			"verify_url": result.VerifyURL,
		})
	})

	// Callback: the widget redirects the browser here with
	//   ?status=success|failed|canceled&token=xtk_...&user_id=...
	// Decide server-side from the result -- never from the query params.
	app.Get("/callback", func(c *fiber.Ctx) error {
		userID, ok := sessions.userID(c)
		if !ok {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Sign in first"})
		}
		token := c.Query("token") // the RESULT token (xtk_...)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		session, _, err := client.Verification.GetResult(ctx, token)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": err.Error(),
			})
		}

		// Grant access only when the result proves the age THIS site needs
		// (an ID verification has no age gate and proves no age) AND belongs
		// to THIS signed-in user.
		verified := session.ProvesAge(requiredMinAge) && session.ExternalUserID == userID

		return c.JSON(fiber.Map{"verified": verified})
	})

	// Webhook handler. OPTIONAL, separate feature -- not part of the core
	// redirect flow above. Enable only if you configured a webhook secret.
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

		// event.Data is the same tenant result GetResult returns. To grant
		// access from a webhook, decode it into xident.SessionResult and apply
		// the same checks as the callback.
		switch event.Type {
		// "session.completed" is the pre-July-2026 name; an endpoint
		// registered before then still receives it.
		case "session.success", "session.completed":
			log.Printf("Verification completed: %v", event.Data)
		case "session.failed":
			log.Printf("Verification failed: %v", event.Data)
		}

		return c.SendStatus(fiber.StatusOK)
	})

	log.Fatal(app.Listen(":8080"))
}
