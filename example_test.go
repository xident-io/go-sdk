// Package xident_test holds the runnable examples for the Xident Go SDK.
//
// These live in an EXTERNAL test package (xident_test) on purpose: they can
// only reach the exported surface, so they read exactly like the code a
// consumer writes, and they cannot quietly lean on an unexported helper that
// a consumer has no way to call.
//
// Most examples stand up an httptest server and point the client at it with
// WithBaseURL. That keeps them genuinely EXECUTED -- `go test` runs them and
// compares stdout -- rather than merely compiled, so they cannot drift away
// from the API without the build going red. In your own code, delete the
// server and the WithBaseURL option; the client then talks to
// https://api.xident.io.
package xident_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"

	xident "github.com/xident-io/go-sdk/v3"
)

// Example shows the whole verification round trip: start a session, send the
// user to the widget, then re-check the outcome server-side when they return.
func Example() {
	// Stand-in for the Xident API so this example runs offline. Delete this
	// server and the WithBaseURL option below in your own code.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/verify/v1/init":
			fmt.Fprint(w, `{"success":true,
				"data":{"token":"xit_9f2a","verify_url":"https://verify.xident.io/s/xit_9f2a"},
				"meta":{"request_id":"req_01"}}`)
		case "/verify/v1/result/xtk_7c31":
			fmt.Fprint(w, `{"success":true,
				"data":{"token":"xtk_7c31","status":"success","verified":true,
					"verification_type":"full",
					"checks":{
						"liveness":{"performed":true,"passed":true},
						"age":{"performed":true,"passed":true,"gate":18},
						"document":{"performed":false,"passed":false},
						"face_match":{"performed":false,"passed":false}
					},
					"created_at":"2026-08-02T10:00:00Z"},
				"meta":{"request_id":"req_02"}}`)
		}
	}))
	defer srv.Close()

	client := xident.NewClient("sk_test_example", xident.WithBaseURL(srv.URL))
	ctx := context.Background()

	// 1. Start a session and send the browser to the widget.
	init, _, err := client.Verification.Init(ctx, &xident.InitParams{
		CallbackURL: "https://example.com/xident/callback",
		MinAge:      18,
	})
	if err != nil {
		fmt.Println("init failed:", err)
		return
	}
	fmt.Println("redirect the user to:", init.VerifyURL)

	// 2. The widget redirects back to CallbackURL with ?status=&token=.
	//    Those query params are unsigned, so they are a hint and not proof --
	//    the trustworthy step is looking the xtk_ token up from your server.
	session, _, err := client.Verification.GetResult(ctx, "xtk_7c31")
	if err != nil {
		fmt.Println("result lookup failed:", err)
		return
	}

	// 3. Gate on IsVerified, never on "the flow finished".
	if session.IsVerified() {
		fmt.Println("access granted, verified via", session.Method())
	} else {
		fmt.Println("access denied:", session.Reason)
	}

	// Output:
	// redirect the user to: https://verify.xident.io/s/xit_9f2a
	// access granted, verified via full
}

// Example_callbackHandler is the http.Handler that receives the browser
// redirect at the end of a verification flow.
//
// The point it exists to make: the ?status= param says "failed" here and the
// handler still ignores it, because anyone can type that URL. Only GetResult
// is authoritative.
func Example_callbackHandler() {
	// Stand-in for the Xident API. Delete it and the WithBaseURL option in
	// your own code.
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"success":true,
			"data":{"token":"xtk_7c31","status":"failed","verified":false,
				"reason":"age_below_threshold",
				"checks":{
					"liveness":{"performed":true,"passed":true},
					"age":{"performed":true,"passed":false,"gate":18},
					"document":{"performed":false,"passed":false},
					"face_match":{"performed":false,"passed":false}
				},
				"created_at":"2026-08-02T10:00:00Z"},
			"meta":{"request_id":"req_03"}}`)
	}))
	defer api.Close()

	client := xident.NewClient("sk_test_example", xident.WithBaseURL(api.URL))

	callback := func(w http.ResponseWriter, r *http.Request) {
		// The widget appends status, token and (if you sent UserID at init)
		// user_id. Read only the token; re-derive everything else.
		token := r.URL.Query().Get("token")
		if token == "" {
			http.Error(w, "missing token", http.StatusBadRequest)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		session, _, err := client.Verification.GetResult(ctx, token)
		if err != nil {
			http.Error(w, "verification lookup failed", http.StatusBadGateway)
			return
		}

		if session.IsVerified() {
			fmt.Fprintln(w, "access granted")
			return
		}
		fmt.Fprintln(w, "access denied:", session.Reason)
	}

	// Drive the handler the way the returning browser would.
	req := httptest.NewRequest(http.MethodGet, "/xident/callback?status=failed&token=xtk_7c31", nil)
	rec := httptest.NewRecorder()
	callback(rec, req)
	fmt.Print(rec.Body.String())

	// Output:
	// access denied: age_below_threshold
}

// Example_errorHandling shows how a caller tells the API error types apart.
//
// Every non-2xx response becomes one of five typed errors, all of which embed
// ErrorResponse and so carry Code, Message and RequestID. Match them with
// errors.As -- the concrete types do not overlap, so the order of the cases
// does not matter.
func Example_errorHandling() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/verify/v1/result/xtk_unknown":
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"success":false,"error":{"code":"NOT_FOUND","message":"session not found"},"meta":{"request_id":"req_404"}}`)
		case "/verify/v1/result/xtk_toomany":
			w.Header().Set("Retry-After", "30")
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(w, `{"success":false,"error":{"code":"RATE_LIMITED","message":"too many requests"},"meta":{"request_id":"req_429"}}`)
		default:
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"success":false,"error":{"code":"UNAUTHORIZED","message":"invalid api key"},"meta":{"request_id":"req_401"}}`)
		}
	}))
	defer srv.Close()

	// WithMaxRetries(0) keeps this example quick. By default the client
	// retries 5xx responses with exponential backoff, sleeping between tries.
	client := xident.NewClient("sk_test_example",
		xident.WithBaseURL(srv.URL),
		xident.WithMaxRetries(0),
	)

	for _, token := range []string{"xtk_unknown", "xtk_toomany", "xtk_expired"} {
		_, _, err := client.Verification.GetResult(context.Background(), token)

		var (
			notFound *xident.NotFoundError
			rateErr  *xident.RateLimitError
			authErr  *xident.AuthenticationError
			validErr *xident.ValidationError
			srvErr   *xident.ServerError
		)

		switch {
		case err == nil:
			fmt.Println("ok")
		case errors.As(err, &notFound):
			// 404: unknown or already-expired token. Do not retry.
			fmt.Println("no such session:", notFound.Code)
		case errors.As(err, &rateErr):
			// 429: RetryAfter comes from the Retry-After header, 0 if absent.
			fmt.Printf("rate limited, retry in %ds\n", rateErr.RetryAfter)
		case errors.As(err, &authErr):
			// 401/403: a configuration problem, not a transient one.
			fmt.Println("check your API key:", authErr.Code)
		case errors.As(err, &validErr):
			// 400 and any other 4xx: your request was wrong.
			fmt.Println("bad request:", validErr.Message)
		case errors.As(err, &srvErr):
			// 5xx after the retries were exhausted. Quote RequestID to support.
			fmt.Println("xident is down, request id:", srvErr.RequestID)
		default:
			// Transport, DNS, TLS or context errors -- no HTTP response exists.
			fmt.Println("request failed:", err)
		}
	}

	// Output:
	// no such session: NOT_FOUND
	// rate limited, retry in 30s
	// check your API key: UNAUTHORIZED
}

// ExampleNewClient shows client construction and the functional options.
//
// There is no Output comment: NewClient performs no I/O and the Client
// exposes no getters, so there is nothing observable to assert. Forcing an
// output here would mean bolting on a fake server and a request, which would
// turn a "how do I build a client" example into something else. The options
// themselves are exercised for real by the executed examples above, and the
// one behaviour NewClient does have at runtime -- the panic -- is pinned by
// ExampleNewClient (invalidKey).
func ExampleNewClient() {
	client := xident.NewClient("sk_live_your_secret_key",
		// Point at another deployment: staging, on-premise, or a local mock.
		// Defaults to DefaultBaseURL (https://api.xident.io).
		xident.WithBaseURL("https://staging-api.xident.io"),

		// Timeout for ONE HTTP attempt, not for the call as a whole: with
		// retries enabled the total wall time can be several times this. Put
		// a deadline on the context to bound the whole operation.
		xident.WithTimeout(15*time.Second),

		// Retries apply to 5xx responses only, with exponential backoff and
		// jitter. Zero disables them.
		xident.WithMaxRetries(5),

		// Identify your integration in Xident's request logs.
		xident.WithUserAgent("acme-storefront/2.1"),
	)

	// One client serves the whole process: it is safe for concurrent use and
	// pools connections. Reach the API through its service fields --
	// client.Verification, client.Webhooks, client.Face2FA, client.Blacklist.
	_ = client
}

// ExampleNewClient_invalidKey shows what NewClient does with a key it does
// not accept: it panics rather than returning an error.
//
// That is deliberate. The API key is a deployment-time constant, so a bad one
// is a configuration bug that should stop the process at startup, not a
// runtime condition to branch on. You should not need this recover in real
// code -- it is here only so the example can show you the message.
func ExampleNewClient_invalidKey() {
	defer func() {
		fmt.Println("recovered:", recover())
	}()

	// A publishable key belongs in the browser SDK, never in a server SDK.
	xident.NewClient("pk_live_abc123")

	// Output:
	// recovered: xident: public keys (pk_*) cannot be used with the server SDK. Use your secret key (sk_live_* or sk_test_*)
}

// ExampleWithHTTPClient supplies a fully custom *http.Client.
//
// Compile-only for the same reason as ExampleNewClient: it builds a client
// and performs no I/O.
func ExampleWithHTTPClient() {
	// Reach for this when you need control over the transport: a proxy,
	// custom TLS, connection pool sizing, or an outbound tracing wrapper.
	hc := &http.Client{
		Timeout: 20 * time.Second,
		Transport: &http.Transport{
			MaxIdleConnsPerHost: 20,
			TLSHandshakeTimeout: 5 * time.Second,
		},
	}

	client := xident.NewClient("sk_live_your_secret_key",
		xident.WithHTTPClient(hc),
	)

	// Set the timeout on hc as above rather than also passing WithTimeout.
	// Options are applied in the order you pass them, so mixing the two makes
	// the result depend on that order.
	_ = client
}

// ExampleVerificationService_Init starts a verification session.
func ExampleVerificationService_Init() {
	// Stand-in for the Xident API. Delete it and the WithBaseURL option in
	// your own code.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"success":true,
			"data":{"token":"xit_9f2a","verify_url":"https://verify.xident.io/s/xit_9f2a"},
			"meta":{"request_id":"req_7b1e"}}`)
	}))
	defer srv.Close()

	client := xident.NewClient("sk_test_example", xident.WithBaseURL(srv.URL))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	init, resp, err := client.Verification.Init(ctx, &xident.InitParams{
		// Required. https only, except http://localhost while developing.
		CallbackURL: "https://example.com/xident/callback",

		// The threshold to prove. Trained models cover 12, 15, 18, 21 and 25;
		// any other value falls back to document verification.
		MinAge: 18,

		// Optional. UserID is echoed back on the callback as ?user_id=.
		UserID:   "usr_1042",
		Locale:   "de",
		Theme:    "system",
		Metadata: "order_88231", // returned verbatim on the session result
	})
	if err != nil {
		fmt.Println("init failed:", err)
		return
	}

	fmt.Println("verify url:", init.VerifyURL)
	fmt.Println("init token:", init.Token) // xit_ prefix, 10 minute TTL

	// Every Response carries the request id. Quote it in support tickets.
	fmt.Println("request id:", resp.RequestID)

	// Output:
	// verify url: https://verify.xident.io/s/xit_9f2a
	// init token: xit_9f2a
	// request id: req_7b1e
}

// ExampleVerificationService_GetResult reads the outcome of a finished
// session.
//
// The session below ran the flow all the way through and still comes back as
// "failed" -- an underage user is a verdict, not a transport error. err stays
// nil; IsVerified is what tells you.
func ExampleVerificationService_GetResult() {
	// Stand-in for the Xident API. Delete it and the WithBaseURL option in
	// your own code.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"success":true,
			"data":{"token":"xtk_7c31","status":"failed","verified":false,
				"reason":"age_below_threshold","verification_type":"full",
				"checks":{
					"liveness":{"performed":true,"passed":true},
					"age":{"performed":true,"passed":false,"gate":18},
					"document":{"performed":false,"passed":false},
					"face_match":{"performed":false,"passed":false}
				},
				"created_at":"2026-08-02T10:00:00Z",
				"completed_at":"2026-08-02T10:00:41Z"},
			"meta":{"request_id":"req_c40d"}}`)
	}))
	defer srv.Close()

	client := xident.NewClient("sk_test_example", xident.WithBaseURL(srv.URL))

	// The xtk_ token from the callback's ?token= param -- NOT the xit_ token
	// that Init returned.
	session, _, err := client.Verification.GetResult(context.Background(), "xtk_7c31")
	if err != nil {
		fmt.Println("lookup failed:", err)
		return
	}

	fmt.Println("verified:", session.IsVerified())
	fmt.Println("terminal:", session.IsTerminal())

	if !session.IsVerified() {
		switch session.Reason {
		case "age_below_threshold":
			fmt.Println("user is under the required age")
		case "face_mismatch", "docverify_reject":
			fmt.Println("document check did not pass")
		default:
			// The reason set is open -- new values may appear, so always keep
			// a default rather than exhausting the known list.
			fmt.Println("not verified:", session.Reason)
		}
	}

	// Output:
	// verified: false
	// terminal: true
	// user is under the required age
}

// ExampleSessionResult_AgeBracket reads the proven age threshold off a
// result.
func ExampleSessionResult_AgeBracket() {
	// In your own code this value comes from Verification.GetResult; it is
	// built by hand here so the example needs no server.
	session := &xident.SessionResult{
		Status:           xident.SessionStatusSuccess,
		VerificationType: "full",
		Checks: xident.Checks{
			Age: xident.AgeCheck{Performed: true, Passed: true, Gate: 18},
		},
	}

	// The bracket is the threshold the user was proven to be ABOVE (12, 15,
	// 18, 21 or 25), not their age. Xident never hands back a date of birth.
	if bracket := session.AgeBracket(); bracket != nil {
		fmt.Printf("proven over %d, via %s\n", *bracket, session.Method())
	}

	// Nil while the session carries no age result yet.
	pending := &xident.SessionResult{Status: xident.SessionStatusPending}
	fmt.Println("bracket while pending:", pending.AgeBracket())

	// Output:
	// proven over 18, via full
	// bracket while pending: <nil>
}

// ExampleWebhookService_ConstructEvent verifies an incoming webhook and acts
// on it. This is the signed, trustworthy channel -- unlike the browser
// callback, which carries no signature.
func ExampleWebhookService_ConstructEvent() {
	const secret = "whsec_example" // from the dashboard; treat as a credential

	payload := []byte(`{"type":"session.success","id":"evt_5512","data":{"session_id":"ses_42","user_id":"usr_1042"}}`)

	// ---- Xident's side, reproduced only so the example runs offline. ----
	// Your server never builds a signature. It arrives in the
	// X-Xident-Signature header as "t=<unix>,v1=<hex>", where the HMAC-SHA256
	// is taken over "<t>.<raw request body>".
	ts := time.Now().Unix()
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%d.%s", ts, payload)
	signature := fmt.Sprintf("t=%d,v1=%s", ts, hex.EncodeToString(mac.Sum(nil)))
	// ---- end of Xident's side ----

	client := xident.NewClient("sk_test_example")

	event, err := client.Webhooks.ConstructEvent(payload, signature, secret)
	if err != nil {
		// A bad signature is not retryable. Answer 400 and stop.
		fmt.Println("rejected:", err)
		return
	}

	switch event.Type {
	case "session.success", "session.completed":
		// "session.completed" is the pre-July-2026 name for the same event.
		// Endpoints registered before the rename still receive it.
		fmt.Println("grant access to", event.Data["user_id"])
	case "session.failed":
		fmt.Println("deny access")
	default:
		fmt.Println("ignoring event type", event.Type)
	}

	// Output:
	// grant access to usr_1042
}

// ExampleWebhookService_VerifySignature checks a signature without parsing
// the event, and shows the two ways it says no.
func ExampleWebhookService_VerifySignature() {
	const secret = "whsec_example"

	payload := []byte(`{"type":"session.success","data":{"session_id":"ses_42"}}`)

	// Signature as Xident would send it, computed over the ORIGINAL bytes.
	ts := time.Now().Unix()
	mac := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(mac, "%d.%s", ts, payload)
	signature := fmt.Sprintf("t=%d,v1=%s", ts, hex.EncodeToString(mac.Sum(nil)))

	client := xident.NewClient("sk_test_example")

	// Pass the body exactly as received. Decoding the JSON and re-encoding it
	// first changes the bytes and the signature will no longer match.
	ok, err := client.Webhooks.VerifySignature(payload, signature, secret)
	fmt.Println("original body:", ok, err)

	// One flipped character in the body is enough.
	tampered := []byte(`{"type":"session.success","data":{"session_id":"ses_99"}}`)
	ok, err = client.Webhooks.VerifySignature(tampered, signature, secret)
	fmt.Println("tampered body:", ok, err)

	// The trailing tolerance argument caps how old a signature may be, which
	// is what stops a captured request being replayed later. It defaults to
	// DefaultWebhookTolerance (5 minutes); 0 turns the check off entirely.
	ok, err = client.Webhooks.VerifySignature(payload, signature, secret, 30*time.Second)
	fmt.Println("tight tolerance:", ok, err)

	// Output:
	// original body: true <nil>
	// tampered body: false xident: webhook signature verification failed
	// tight tolerance: true <nil>
}

// ExampleBlacklistService_List pages through the tenant's face blacklist.
func ExampleBlacklistService_List() {
	// Stand-in for the Xident API. Delete it and the WithBaseURL option in
	// your own code.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"success":true,
			"data":[
				{"id":11,"reason":"chargeback fraud","source":"session","session_id":9931,"created_at":"2026-07-30T09:12:00Z"},
				{"id":12,"reason":"repeat underage attempt","source":"image","created_at":"2026-07-31T14:03:00Z"}
			],
			"meta":{"request_id":"req_bl01","pagination":{"page":1,"per_page":50,"total":2,"total_pages":1}}}`)
	}))
	defer srv.Close()

	client := xident.NewClient("sk_test_example", xident.WithBaseURL(srv.URL))

	// Pass nil opts for the server defaults (page 1, 20 per page).
	entries, resp, err := client.Blacklist.List(context.Background(), &xident.BlacklistListOptions{
		Page:    1,
		PerPage: 50,
	})
	if err != nil {
		fmt.Println("list failed:", err)
		return
	}

	for _, entry := range entries {
		// Face embeddings are never returned -- only this metadata.
		fmt.Printf("#%d %s (%s)\n", entry.ID, entry.Reason, entry.Source)
	}

	// Pagination metadata rides on the Response, not on the slice.
	if resp.Pagination != nil {
		fmt.Printf("page %d of %d, %d entries total\n",
			resp.Pagination.Page, resp.Pagination.TotalPages, resp.Pagination.Total)
	}

	// Output:
	// #11 chargeback fraud (session)
	// #12 repeat underage attempt (image)
	// page 1 of 1, 2 entries total
}

// ExampleFace2FAService_Register enrolls a user's face and polls the async
// challenge to its verdict.
func ExampleFace2FAService_Register() {
	// Stand-in for the Xident API: it reports "processing" once, then
	// "completed", so the example exercises a real poll loop. Delete it and
	// the WithBaseURL option in your own code.
	polls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/verify/v1/2fa/register":
			fmt.Fprint(w, `{"success":true,
				"data":{"challenge_id":"chl_88","status":"processing"},
				"meta":{"request_id":"req_2fa1"}}`)
		case "/verify/v1/2fa/status/chl_88":
			polls++
			if polls < 2 {
				fmt.Fprint(w, `{"success":true,
					"data":{"challenge_id":"chl_88","kind":"enroll","status":"processing",
						"passed":null,"expires_at":"2026-08-02T10:05:00Z"},
					"meta":{"request_id":"req_2fa2"}}`)
				return
			}
			fmt.Fprint(w, `{"success":true,
				"data":{"challenge_id":"chl_88","kind":"enroll","status":"completed",
					"passed":true,"expires_at":"2026-08-02T10:05:00Z",
					"completed_at":"2026-08-02T10:00:12Z"},
				"meta":{"request_id":"req_2fa3"}}`)
		}
	}))
	defer srv.Close()

	client := xident.NewClient("sk_test_example", xident.WithBaseURL(srv.URL))
	ctx := context.Background()

	challenge, _, err := client.Face2FA.Register(ctx, &xident.Face2FAParams{
		UserID: "usr_1042",                // your identifier, opaque to Xident
		Image:  "/9j/4AAQSkZJRgABAQAA...", // base64 selfie
	})
	if err != nil {
		fmt.Println("register failed:", err)
		return
	}
	fmt.Println("submitted, status:", challenge.Status)

	// Register is asynchronous: poll GetStatus until the status is terminal.
	enrolled := false
	for attempt := 0; attempt < 10; attempt++ {
		status, _, err := client.Face2FA.GetStatus(ctx, challenge.ChallengeID)
		if err != nil {
			fmt.Println("poll failed:", err)
			return
		}
		if status.Status.IsTerminal() {
			// Passed is nil while processing, so check it before dereferencing.
			enrolled = status.Passed != nil && *status.Passed
			if !enrolled && status.FailureReason != nil {
				fmt.Println("reason:", *status.FailureReason)
			}
			break
		}
		time.Sleep(50 * time.Millisecond) // poll about once a second in production
	}

	fmt.Println("enrolled:", enrolled)

	// Output:
	// submitted, status: processing
	// enrolled: true
}
