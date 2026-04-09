# Xident Go SDK

Server-side Go SDK for [Xident](https://xident.io) age and identity verification. Zero external dependencies beyond the standard library. Works with any Go HTTP framework (net/http, Gin, Echo, Fiber, Chi).

## Requirements

- Go 1.21+

## Installation

```bash
go get github.com/xident-io/xident-go
```

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "log"

    xident "github.com/xident-io/xident-go"
)

func main() {
    client := xident.NewClient("sk_live_xxx")

    // 1. Create a verification session (your backend)
    result, _, err := client.Verification.Init(context.Background(), &xident.InitParams{
        CallbackURL: "https://yoursite.com/webhook",
        MinAge:      18,
    })
    if err != nil {
        log.Fatal(err)
    }
    // Redirect user to result.VerifyURL

    // 2. After user returns, verify server-side (NEVER trust URL params)
    session, _, err := client.Verification.GetResult(context.Background(), result.Token)
    if err != nil {
        log.Fatal(err)
    }

    if session.IsVerified() {
        fmt.Printf("Verified! Age bracket: %d\n", *session.AgeBracket())
    }
}
```

## How It Works

1. Your backend calls `POST /verify/v1/init` with your secret key
2. SDK returns a token + verify URL. You redirect the user there.
3. User completes verification on `verify.xident.io` (liveness + age check)
4. User redirected back to your `success_url` with `?token=xxx`
5. Your backend calls `GET /verify/v1/result/{token}` to get the result
6. You make the authorization decision based on the verified result

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
| `CallbackURL` | string | Yes | HTTPS URL for webhook (localhost OK for dev) |
| `MinAge` | int | No | 12, 15, 18, 21, or 25. Default: rule's configured threshold |
| `SuccessURL` | string | No | Redirect on success |
| `FailedURL` | string | No | Redirect on failure |
| `UserID` | string | No | Your internal user ID |
| `Theme` | string | No | `light`, `dark` |
| `Locale` | string | No | `en`, `de`, `es`, `fr`, `it`, `pt`, `nl`, `pl`, `tr`, `ar`, `ja` |
| `Metadata` | string | No | Custom string (max 500 chars) |

Returns: `result.Token`, `result.VerifyURL`

### Verification.GetResult(ctx, token) -> (*SessionResult, *Response, error)

Helpers: `IsVerified()`, `IsFailed()`, `IsPending()`, `IsTerminal()`, `AgeBracket()`, `Method()`

### Webhooks.ConstructEvent(payload, signature, secret, tolerance...) -> (*WebhookEvent, error)

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
            CallbackURL: "https://example.com/webhook",
            MinAge:      18,
            SuccessURL:  "https://example.com/success",
            UserID:      "user_123",
        })
        if err != nil {
            http.Error(w, fmt.Sprintf("Failed: %v", err), 500)
            return
        }
        http.Redirect(w, r, result.VerifyURL, http.StatusFound)
    })

    mux.HandleFunc("/success", func(w http.ResponseWriter, r *http.Request) {
        token := r.URL.Query().Get("token")
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

See `examples/` for Gin, Echo, and Fiber examples.

## Security

- **Secret key**: Never expose `sk_*` in frontend code
- **TLS 1.2+**: Enforced on all API calls
- **Webhooks**: Always verify signatures (constant-time HMAC comparison)
- **Verification tokens**: Always re-verify server-side. Never trust URL params alone.
- **Context**: Pass `context.Context` for cancellation and deadlines

## Testing

```bash
go test ./...              # 107 tests
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
