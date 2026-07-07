# Xident Go SDK

Server-side Go SDK for [Xident](https://xident.io) age and identity verification. Zero external dependencies beyond the standard library. Works with any Go HTTP framework (net/http, Gin, Echo, Fiber, Chi).

## Requirements

- Go 1.21+

## Installation

```bash
go get github.com/xident-io/go-sdk
```

## Quick Start

Verification is a two-step, two-request flow. **Step 1** (`Init`) runs on the
route that starts verification and returns a `verify_url` you redirect the
browser to. **Step 2** (`GetResult`) runs on your `callback_url` route, which
the widget redirects the browser back to with a `token` query param — the
**result** token (`xtk_…`), which is different from the init token (`xit_…`).

```go
package main

import (
    "context"
    "fmt"
    "log"
    "net/http"

    xident "github.com/xident-io/go-sdk"
)

func main() {
    client := xident.NewClient("sk_live_xxx")

    // Step 1: start verification and redirect the browser to the widget.
    http.HandleFunc("/verify", func(w http.ResponseWriter, r *http.Request) {
        result, _, err := client.Verification.Init(r.Context(), &xident.InitParams{
            CallbackURL: "https://yoursite.com/callback", // browser returns here
            MinAge:      18,
        })
        if err != nil {
            http.Error(w, err.Error(), http.StatusInternalServerError)
            return
        }
        // result.Token is the init token (xit_) — do NOT pass it to GetResult.
        http.Redirect(w, r, result.VerifyURL, http.StatusFound)
    })

    // Step 2: the widget redirects the browser back here with query params:
    //   ?status=success|failed|cancelled&token=xtk_...&user_id=...
    // Re-verify server-side (NEVER trust the URL params alone).
    http.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
        token := r.URL.Query().Get("token") // the RESULT token (xtk_...)

        session, _, err := client.Verification.GetResult(r.Context(), token)
        if err != nil {
            http.Error(w, err.Error(), http.StatusInternalServerError)
            return
        }
        if session.IsVerified() {
            if b := session.AgeBracket(); b != nil {
                fmt.Fprintf(w, "Verified! Age bracket: %d\n", *b)
            } else {
                fmt.Fprintln(w, "Verified!")
            }
        }
    })

    log.Fatal(http.ListenAndServe(":8080", nil))
}
```

## How It Works

1. Your backend calls `POST /verify/v1/init` with your secret key (`sk_…`).
2. The SDK returns an **init** token (`xit_…`, one-time use, 10-minute TTL) plus
   a `verify_url`. You redirect the browser to that URL.
3. The user completes verification on `verify.xident.io` (liveness + age check).
4. The widget redirects the browser back to your `callback_url` with query
   params: `?status=success|failed|cancelled`, `token=xtk_…` (the **result**
   token, different from the init token), and `user_id` (only if you supplied
   one). This is a plain browser GET redirect, **not** a signed webhook.
5. Your backend reads `token` from the query string and calls
   `GET /verify/v1/result/{token}` (via `GetResult`) to fetch the outcome.
6. You make the authorization decision based on the verified result.

> Signed server-to-server webhooks are a **separate, optional** feature — see
> [Webhooks (optional server-to-server)](#webhooks-optional-server-to-server)
> below. The primary flow above needs only the browser callback redirect.

## API Reference

### Client

```go
client := xident.NewClient("sk_live_xxx",
    xident.WithBaseURL("https://api.xident.io"),  // Optional (default)
    xident.WithTimeout(30 * time.Second),          // Optional (default: 30s)
    xident.WithMaxRetries(3),                      // Optional (default: 3, retries on 5xx)
    xident.WithHTTPClient(customHTTPClient),        // Optional (full control)
    xident.WithUserAgent("MyApp/1.0"),             // Optional
)
```

The client is safe for concurrent use across goroutines. Create one and reuse it.

### Verification.Init(ctx, params) -> (*InitResult, *Response, error)

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `CallbackURL` | string | Yes | Browser GET redirect target; widget returns here with `?status`, `token` (xtk_), `user_id`. HTTPS required (http OK for localhost) |
| `MinAge` | int | No | 1–99 (0–99 when `Purpose` is `id_verification`). Default: rule's configured threshold. Trained brackets: 12, 15, 18, 21, 25 |
| `SuccessURL` | string | No | Redirect on success |
| `FailedURL` | string | No | Redirect on failure |
| `UserID` | string | No | Your internal user ID; echoed back on the callback as `user_id` |
| `Theme` | string | No | `light`, `dark`, `system` |
| `Locale` | string | No | `en`, `de`, `es`, `fr`, `it`, `pt`, `nl`, `pl`, `tr`, `ar`, `ja` |
| `Purpose` | string | No | `age_verification` (default) or `id_verification` |
| `Metadata` | string | No | Opaque string (max 500 chars), passed through verbatim and returned unchanged on the result (not parsed or base64-encoded) |

Returns: `result.Token` (the init token, `xit_…`), `result.VerifyURL`. The
**result** token (`xtk_…`) you pass to `GetResult` comes from the callback
redirect's `token` query param, not from `Init`.

### Verification.GetResult(ctx, token) -> (*SessionResult, *Response, error)

`token` is the **result** token (`xtk_…`) read from the callback redirect's
`token` query param — not the init token (`xit_…`) returned by `Init`.

Helpers: `IsVerified()`, `IsFailed()`, `IsPending()`, `IsTerminal()`, `AgeBracket()`, `Method()`

### Webhooks (optional server-to-server)

> The core flow (Init -> redirect -> callback -> GetResult) does **not** require
> webhooks. This is a separate, optional feature for backends that also want a
> signed server-to-server notification. If you use it, still call `GetResult`
> to fetch the authoritative outcome.

**`Webhooks.ConstructEvent(payload, signature, secret, tolerance...) -> (*WebhookEvent, error)`**

Verify HMAC-SHA256 webhook signature and parse event. Default tolerance is 5 minutes.

```go
event, err := client.Webhooks.ConstructEvent(body, sig, "whsec_xxx")
if err != nil {
    http.Error(w, "Invalid signature", 400)
    return
}
// event.Type, event.Data
```

## Error Handling

```go
import "errors"

var authErr *xident.AuthenticationError
var notFoundErr *xident.NotFoundError
var rateLimitErr *xident.RateLimitError

session, _, err := client.Verification.GetResult(ctx, token)
if err != nil {
    switch {
    case errors.As(err, &authErr):
        // 401/403 - Invalid API key
    case errors.As(err, &notFoundErr):
        // 404 - Session not found
    case errors.As(err, &rateLimitErr):
        // 429 - Rate limited, retry after rateLimitErr.RetryAfter seconds
    default:
        var apiErr *xident.ErrorResponse
        if errors.As(err, &apiErr) {
            fmt.Println(apiErr.Code)      // API error code
            fmt.Println(apiErr.RequestID) // For support tickets
        }
    }
}
```

Error types: `AuthenticationError` (401/403), `ValidationError` (400/4xx), `NotFoundError` (404), `RateLimitError` (429), `ServerError` (5xx).

All error types embed `ErrorResponse` which provides `Code`, `Message`, and `RequestID` fields.

## Session Result Helpers

```go
session, _, _ := client.Verification.GetResult(ctx, token)

session.IsVerified()  // true if status == "completed"
session.IsFailed()    // true if status == "failed"
session.IsPending()   // true if status == "pending" or "in_progress"
session.IsTerminal()  // true if completed, failed, canceled, or claimed

session.AgeBracket()  // *int: 12, 15, 18, 21, or 25 (nil if not yet determined)
session.Method()      // string: "ml_fast", "ocr", "self_declaration", etc.
```

Session statuses: `pending`, `in_progress`, `completed`, `failed`, `canceled`, `claimed`.

## Retry Behavior

Automatic retry with exponential backoff + jitter (1s, 2s, 4s) on 5xx server errors and network failures only. Never retries 4xx. Configurable via `WithMaxRetries(n)` (set to 0 to disable).

## net/http Example

```go
func main() {
    client := xident.NewClient(os.Getenv("XIDENT_SECRET_KEY"),
        xident.WithTimeout(15 * time.Second),
    )

    mux := http.NewServeMux()

    mux.HandleFunc("/verify", func(w http.ResponseWriter, r *http.Request) {
        result, _, err := client.Verification.Init(r.Context(), &xident.InitParams{
            CallbackURL: "https://example.com/callback",
            MinAge:      18,
            UserID:      "user_123",
        })
        if err != nil {
            http.Error(w, fmt.Sprintf("Failed: %v", err), 500)
            return
        }
        http.Redirect(w, r, result.VerifyURL, http.StatusFound)
    })

    // callback_url: the widget redirects the browser back here with
    //   ?status=success|failed|cancelled&token=xtk_...&user_id=...
    mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
        q := r.URL.Query()
        status := q.Get("status") // "success", "failed", or "cancelled"
        token := q.Get("token")   // the xtk_ result token
        if status != "success" || token == "" {
            fmt.Fprintf(w, "Verification not completed (status=%q)\n", status)
            return
        }

        // Re-verify server-side; never trust the query params alone.
        session, _, err := client.Verification.GetResult(r.Context(), token)
        if err != nil {
            http.Error(w, fmt.Sprintf("Failed: %v", err), 500)
            return
        }
        if session.IsVerified() {
            fmt.Fprintln(w, "Age verified!")
        }
    })

    log.Fatal(http.ListenAndServe(":8080", mux))
}
```

See `examples/` for Gin, Echo, and Fiber examples. Each is its own Go module
(so the SDK stays dependency-free); `cd examples/gin && go run .` to try one.

## Security

- **Secret key**: Never expose `sk_*` in frontend code
- **TLS 1.2+**: Enforced on all API calls
- **Webhooks**: Always verify signatures (constant-time HMAC comparison)
- **Verification tokens**: Always re-verify server-side. Never trust URL params alone.
- **Context**: Pass `context.Context` for cancellation and deadlines

## Testing

```bash
go test ./...              # run the test suite
go test -race ./...        # With race detector
go test -cover ./...       # With coverage report
```

## Links

- [Try it live](https://demo.xident.io)
- [Documentation](https://docs.xident.io/sdks/go)
- [API Reference](https://docs.xident.io/api-reference)
- [JavaScript SDK](https://docs.xident.io/sdks/javascript) (client-side counterpart)
- [Dashboard](https://dashboard.xident.io) (get your API key)

## License

MIT
