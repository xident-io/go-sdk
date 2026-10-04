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
	"crypto/rand"
	"encoding/hex"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
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

func (s *sessionStore) userID(c *gin.Context) (string, bool) {
	sessionID, err := c.Cookie(sessionCookie)
	if err != nil {
		return "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.users[sessionID]
	return id, ok
}

// signIn creates a demo user and a session for it. Your app does this at
// login; the example has no login, so every new visitor gets a user.
func (s *sessionStore) signIn(c *gin.Context) string {
	sessionID, userID := randomHex(32), "user_"+randomHex(8)
	s.mu.Lock()
	s.users[sessionID] = userID
	s.mu.Unlock()
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(sessionCookie, sessionID, 0, "/", "", c.Request.TLS != nil, true)
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
	webhookSecret := os.Getenv("XIDENT_WEBHOOK_SECRET")

	if apiKey == "" || webhookSecret == "" {
		log.Fatal("XIDENT_SECRET_KEY and XIDENT_WEBHOOK_SECRET are required")
	}

	client := xident.NewClient(apiKey,
		xident.WithTimeout(15*time.Second),
	)
	sessions := &sessionStore{users: map[string]string{}}

	r := gin.Default()

	// Start verification session for the signed-in user.
	r.POST("/verify", func(c *gin.Context) {
		userID, ok := sessions.userID(c)
		if !ok {
			userID = sessions.signIn(c)
		}

		result, _, err := client.Verification.Init(c.Request.Context(), &xident.InitParams{
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
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Send the page only the link. It opens it unchanged, for example
		// with the browser SDK: Xident.start({ verifyUrl }).
		c.JSON(http.StatusOK, gin.H{
			"verify_url": result.VerifyURL,
		})
	})

	// Callback: the widget redirects the browser here with
	//   ?status=success|failed|canceled&token=xtk_...&user_id=...
	// Decide server-side from the result -- never from the query params.
	r.GET("/callback", func(c *gin.Context) {
		userID, ok := sessions.userID(c)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Sign in first"})
			return
		}
		token := c.Query("token") // the RESULT token (xtk_...)

		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()

		session, _, err := client.Verification.GetResult(ctx, token)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Grant access only when the result proves the age THIS site needs
		// (an ID verification has no age gate and proves no age) AND belongs
		// to THIS signed-in user.
		provesAge := session.ProvesAge(requiredMinAge) // refuses a test-key result
		if allowTestResults {
			provesAge = session.ProvesAgeAllowingTest(requiredMinAge)
		}
		verified := provesAge && session.ExternalUserID == userID

		c.JSON(http.StatusOK, gin.H{"verified": verified})
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

		c.Status(http.StatusOK)
	})

	log.Fatal(r.Run(":8080"))
}
