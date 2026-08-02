package xident

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// failingReader fails every Read with a fixed error.
//
// Real request bodies are always backed by a bytes.Reader and real response
// bodies come from a live connection, so the read-failure branches in do() are
// only reachable by injecting a reader that misbehaves on purpose.
type failingReader struct{ err error }

func (f failingReader) Read([]byte) (int, error) { return 0, f.err }

// roundTripFunc adapts a plain function to http.RoundTripper, so a test can
// hand do() a fabricated response or a transport-level failure without
// standing up a server that can produce one.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// stubResponse builds a minimal 200 response carrying body.
func stubResponse(r *http.Request, status int, body io.Reader) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(body),
		Request:    r,
	}
}

func TestNewRequest_MarshalError(t *testing.T) {
	c := NewClient("sk_test_key")

	// A channel has no JSON representation. Every params type the SDK exports
	// today is plain data, so this branch guards against a future params type
	// growing a field that cannot be marshalled -- without it, the request
	// would go out with an empty body instead of failing.
	_, err := c.newRequest(http.MethodPost, "init", make(chan int))
	if err == nil {
		t.Fatal("expected an error for an unmarshalable body, got nil")
	}
	if !strings.Contains(err.Error(), "xident: failed to marshal request body") {
		t.Errorf("error = %q, want it to report a marshal failure", err)
	}

	var typeErr *json.UnsupportedTypeError
	if !errors.As(err, &typeErr) {
		t.Fatalf("error %v does not wrap *json.UnsupportedTypeError", err)
	}
	if got := typeErr.Type.String(); got != "chan int" {
		t.Errorf("wrapped unsupported type = %q, want %q", got, "chan int")
	}
}

func TestNewRequest_InvalidURL(t *testing.T) {
	// A control character in the host makes url.Parse fail inside
	// http.NewRequest. This is the one way newRequest can fail for the
	// SDK's own (always marshalable) params types.
	c := NewClient("sk_test_key", WithBaseURL("http://local\x7fhost"))

	_, err := c.newRequest(http.MethodGet, "result/abc", nil)
	if err == nil {
		t.Fatal("expected an error for an unparseable base URL, got nil")
	}
	if !strings.Contains(err.Error(), "xident: failed to create request") {
		t.Errorf("error = %q, want it to report request construction failure", err)
	}

	var urlErr *url.Error
	if !errors.As(err, &urlErr) {
		t.Fatalf("error %v does not wrap *url.Error", err)
	}
	if urlErr.Op != "parse" {
		t.Errorf("url.Error.Op = %q, want %q", urlErr.Op, "parse")
	}
}

func TestDo_ContextCanceledDuringRetryBackoff(t *testing.T) {
	var attempts int32

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"success":false,"error":{"code":"SERVER_ERROR","message":"Down"}}`)
		if n == 1 {
			// Cancel once the first attempt has been answered, so do() is
			// certain to be inside the backoff sleep rather than in flight.
			go func() {
				time.Sleep(20 * time.Millisecond)
				cancel()
			}()
		}
	}))
	defer server.Close()

	client := NewClient("sk_test_key", WithBaseURL(server.URL), WithMaxRetries(3))

	req, err := client.newRequest(http.MethodGet, "test", nil)
	if err != nil {
		t.Fatalf("newRequest() error: %v", err)
	}

	start := time.Now()
	resp, err := client.do(ctx, req, nil)
	elapsed := time.Since(start)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want it to be context.Canceled", err)
	}
	if resp != nil {
		t.Errorf("resp = %v, want nil when the backoff is abandoned", resp)
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Errorf("attempts = %d, want 1 -- cancellation must stop the retry, not just the request", got)
	}
	// retryDelay(1) is never below one second. Returning well inside that
	// proves do() woke on ctx.Done() instead of sleeping the backoff out.
	if elapsed >= time.Second {
		t.Errorf("do() returned after %v, want < 1s", elapsed)
	}
}

func TestDo_RetryResendsRequestBody(t *testing.T) {
	var (
		mu     sync.Mutex
		bodies []string
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("server failed to read request body: %v", err)
		}
		mu.Lock()
		bodies = append(bodies, string(b))
		n := len(bodies)
		mu.Unlock()

		if n == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, `{"success":false,"error":{"code":"SERVER_ERROR","message":"Down"}}`)
			return
		}
		fmt.Fprint(w, `{"success":true,"data":{"ok":true}}`)
	}))
	defer server.Close()

	client := NewClient("sk_test_key", WithBaseURL(server.URL), WithMaxRetries(1))

	req, err := client.newRequest(http.MethodPost, "init", &InitParams{
		CallbackURL: "https://example.com/cb",
		MinAge:      18,
	})
	if err != nil {
		t.Fatalf("newRequest() error: %v", err)
	}

	var result struct {
		OK bool `json:"ok"`
	}
	if _, err := client.do(context.Background(), req, &result); err != nil {
		t.Fatalf("do() error: %v", err)
	}
	if !result.OK {
		t.Error("result.OK = false, want true after a successful retry")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 2 {
		t.Fatalf("server saw %d requests, want 2", len(bodies))
	}
	// The retry must re-send the payload. A body consumed on the first attempt
	// and not buffered would arrive empty the second time -- and the server
	// would reject it, not the SDK.
	if !strings.Contains(bodies[0], "https://example.com/cb") {
		t.Errorf("first attempt body = %q, want it to carry the callback URL", bodies[0])
	}
	if bodies[1] != bodies[0] {
		t.Errorf("retry body = %q, want it byte-identical to the first attempt %q", bodies[1], bodies[0])
	}
}

func TestDo_RequestBodyReadError(t *testing.T) {
	client, mux, teardown := setup()
	defer teardown()

	mux.HandleFunc("/"+apiVersion+"/test", func(w http.ResponseWriter, r *http.Request) {
		t.Error("server was reached, but a request whose body cannot be read must never be sent")
	})

	sentinel := errors.New("disk went away")
	req, err := http.NewRequest(http.MethodPost,
		client.baseURL+"/"+apiVersion+"/test", failingReader{err: sentinel})
	if err != nil {
		t.Fatalf("http.NewRequest() error: %v", err)
	}

	resp, err := client.do(context.Background(), req, nil)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if resp != nil {
		t.Errorf("resp = %v, want nil -- nothing was sent", resp)
	}
	if !strings.Contains(err.Error(), "xident: failed to read request body") {
		t.Errorf("error = %q, want it to report a request body read failure", err)
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("error %v does not wrap the reader's error", err)
	}
}

func TestDo_RetriesTransportError(t *testing.T) {
	var attempts int32

	client := NewClient("sk_test_key",
		WithMaxRetries(1),
		WithHTTPClient(&http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if atomic.AddInt32(&attempts, 1) == 1 {
					return nil, errors.New("connection refused")
				}
				return stubResponse(r, http.StatusOK,
					strings.NewReader(`{"success":true,"data":{"ok":true}}`)), nil
			}),
		}),
	)

	req, err := client.newRequest(http.MethodGet, "test", nil)
	if err != nil {
		t.Fatalf("newRequest() error: %v", err)
	}

	var result struct {
		OK bool `json:"ok"`
	}
	if _, err := client.do(context.Background(), req, &result); err != nil {
		t.Fatalf("do() error: a transport failure with retries left must be retried, got %v", err)
	}
	if !result.OK {
		t.Error("result.OK = false, want true")
	}
	if got := atomic.LoadInt32(&attempts); got != 2 {
		t.Errorf("attempts = %d, want 2 (one failure, one success)", got)
	}
}

func TestDo_TransportErrorExhaustsRetries(t *testing.T) {
	var attempts int32
	sentinel := errors.New("no route to host")

	client := NewClient("sk_test_key",
		WithMaxRetries(0),
		WithHTTPClient(&http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				atomic.AddInt32(&attempts, 1)
				return nil, sentinel
			}),
		}),
	)

	req, err := client.newRequest(http.MethodGet, "test", nil)
	if err != nil {
		t.Fatalf("newRequest() error: %v", err)
	}

	resp, err := client.do(context.Background(), req, nil)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if resp != nil {
		t.Errorf("resp = %v, want nil", resp)
	}
	if !strings.Contains(err.Error(), "xident: request failed") {
		t.Errorf("error = %q, want it to report the request failure", err)
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("error %v does not wrap the transport error", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Errorf("attempts = %d, want 1 with retries disabled", got)
	}
}

func TestDo_NoResponseReceived(t *testing.T) {
	// do() loops `for attempt := 0; attempt <= c.maxRetries`, so a negative
	// maxRetries skips the body entirely and falls through to the last-resort
	// guard. WithMaxRetries clamps negatives to zero, so reaching this state
	// means the field was written directly -- which is exactly the corruption
	// the guard exists to turn into an error instead of a nil-pointer panic
	// further down.
	c := NewClient("sk_test_key")
	c.maxRetries = -1

	req, err := c.newRequest(http.MethodGet, "test", nil)
	if err != nil {
		t.Fatalf("newRequest() error: %v", err)
	}

	resp, err := c.do(context.Background(), req, nil)
	if resp != nil {
		t.Errorf("resp = %v, want nil", resp)
	}
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if err.Error() != "xident: no response received" {
		t.Errorf("error = %q, want %q", err, "xident: no response received")
	}
}

func TestDo_ResponseBodyReadError(t *testing.T) {
	sentinel := errors.New("connection reset by peer")

	client := NewClient("sk_test_key",
		WithMaxRetries(0),
		WithHTTPClient(&http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				return stubResponse(r, http.StatusOK, failingReader{err: sentinel}), nil
			}),
		}),
	)

	req, err := client.newRequest(http.MethodGet, "test", nil)
	if err != nil {
		t.Fatalf("newRequest() error: %v", err)
	}

	resp, err := client.do(context.Background(), req, nil)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	// The headers arrived even though the body did not, so do() still hands
	// back the Response for the caller to inspect.
	if resp == nil {
		t.Fatal("resp = nil, want the response whose body failed mid-read")
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("resp.StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if !strings.Contains(err.Error(), "xident: failed to read response body") {
		t.Errorf("error = %q, want it to report a response body read failure", err)
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("error %v does not wrap the reader's error", err)
	}
}

func TestDo_MalformedJSONOnErrorStatus(t *testing.T) {
	client, mux, teardown := setup()
	defer teardown()

	// A proxy or load balancer returning an HTML error page is the realistic
	// case: the status says 4xx but the body is not our envelope at all.
	mux.HandleFunc("/"+apiVersion+"/test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `<html><body>502 Bad Gateway</body></html>`)
	})

	req, err := client.newRequest(http.MethodGet, "test", nil)
	if err != nil {
		t.Fatalf("newRequest() error: %v", err)
	}

	resp, err := client.do(context.Background(), req, nil)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if resp == nil {
		t.Fatal("resp = nil, want the response carrying the status")
	}

	// The status is a 4xx, so the caller gets a typed API error rather than a
	// bare decode error -- the failure is the server's, not the payload's.
	var vErr *ValidationError
	if !errors.As(err, &vErr) {
		t.Fatalf("error %v is not a *ValidationError", err)
	}
	if vErr.Code != "PARSE_ERROR" {
		t.Errorf("Code = %q, want %q", vErr.Code, "PARSE_ERROR")
	}
	if vErr.Message != "Failed to parse API response" {
		t.Errorf("Message = %q, want %q", vErr.Message, "Failed to parse API response")
	}
	if vErr.Response.StatusCode != http.StatusBadRequest {
		t.Errorf("Response.StatusCode = %d, want %d", vErr.Response.StatusCode, http.StatusBadRequest)
	}
}

func TestDo_MalformedJSONOnSuccessStatus(t *testing.T) {
	client, mux, teardown := setup()
	defer teardown()

	mux.HandleFunc("/"+apiVersion+"/test", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"success":true,"data":`)
	})

	req, err := client.newRequest(http.MethodGet, "test", nil)
	if err != nil {
		t.Fatalf("newRequest() error: %v", err)
	}

	resp, err := client.do(context.Background(), req, nil)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if resp == nil {
		t.Fatal("resp = nil, want the response")
	}
	// 2xx with an undecodable body: there is no API error to report, so the
	// decode failure itself is what surfaces.
	if !strings.Contains(err.Error(), "xident: failed to parse response") {
		t.Errorf("error = %q, want it to report a parse failure", err)
	}
	var syntaxErr *json.SyntaxError
	if !errors.As(err, &syntaxErr) {
		t.Errorf("error %v does not wrap *json.SyntaxError", err)
	}

	// The status check must gate the PARSE_ERROR path: a 2xx with a broken
	// body is a decode problem, not an API rejection, and must not be dressed
	// up as one.
	var vErr *ValidationError
	if errors.As(err, &vErr) {
		t.Errorf("a 2xx with an undecodable body was reported as a typed API error (%v)", vErr)
	}
}

func TestDo_DataUnmarshalError(t *testing.T) {
	client, mux, teardown := setup()
	defer teardown()

	mux.HandleFunc("/"+apiVersion+"/test", func(w http.ResponseWriter, r *http.Request) {
		// Envelope is valid; the data field has the wrong shape for the target.
		fmt.Fprint(w, `{"success":true,"data":{"token":123},"meta":{"request_id":"req_shape"}}`)
	})

	req, err := client.newRequest(http.MethodGet, "test", nil)
	if err != nil {
		t.Fatalf("newRequest() error: %v", err)
	}

	var result InitResult
	resp, err := client.do(context.Background(), req, &result)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "xident: failed to unmarshal response data") {
		t.Errorf("error = %q, want it to report a data unmarshal failure", err)
	}

	var typeErr *json.UnmarshalTypeError
	if !errors.As(err, &typeErr) {
		t.Fatalf("error %v does not wrap *json.UnmarshalTypeError", err)
	}
	if typeErr.Field != "token" {
		t.Errorf("UnmarshalTypeError.Field = %q, want %q", typeErr.Field, "token")
	}

	// Envelope metadata is read before the data is decoded, so the request id
	// survives the failure and is still available for a support ticket.
	if resp == nil {
		t.Fatal("resp = nil, want the response")
	}
	if resp.RequestID != "req_shape" {
		t.Errorf("resp.RequestID = %q, want %q", resp.RequestID, "req_shape")
	}
}
