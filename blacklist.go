package xident

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// BlacklistService provides methods to manage your face blacklist.
//
// Entries are added by SESSION or IMAGE -- never by raw embedding. The face
// embedding is derived server-side in an async job, so a new entry appears in
// List once processing finishes. Embeddings are never returned.
//
// Access this service through Client.Blacklist:
//
//	client := xident.NewClient("sk_live_xxx")
//	entries, resp, err := client.Blacklist.List(ctx, nil)
type BlacklistService service

// BlacklistEntry is one active entry on your face blacklist. The embedding
// itself never leaves the server.
type BlacklistEntry struct {
	// ID is the entry identifier. Pass it to Remove.
	ID int64 `json:"id"`

	// Reason is the reason you supplied when adding the entry.
	Reason string `json:"reason"`

	// Source records how the entry was created (e.g., "session", "image").
	Source string `json:"source"`

	// SessionID is the numeric id of the verification session the face was
	// lifted from, if the entry was added by session. Nil for image entries.
	SessionID *int64 `json:"session_id,omitempty"`

	// CreatedAt is when the entry was created (RFC 3339).
	CreatedAt string `json:"created_at"`
}

// BlacklistListOptions controls pagination for List. A nil options value
// uses the server defaults (page 1, 20 per page).
type BlacklistListOptions struct {
	// Page is the 1-based page number. Zero means the server default (1).
	Page int

	// PerPage is the number of entries per page, max 100. Zero means the
	// server default (20).
	PerPage int
}

// BlacklistAddResult is the immediate result of AddBySession and AddByImage.
type BlacklistAddResult struct {
	// Status is always "processing" -- the embedding is extracted
	// asynchronously and the entry appears in List once done.
	Status string `json:"status"`
}

// BlacklistRemoveResult is the result of Remove.
type BlacklistRemoveResult struct {
	// Message is a human-readable confirmation.
	Message string `json:"message"`
}

// BlacklistAddSessionParams contains the parameters for AddBySession. Both
// fields are required.
type BlacklistAddSessionParams struct {
	// SessionToken is the token of one of YOUR terminal DOCUMENT
	// verification sessions. Works for 12 months after the session: the
	// face embedding is retained for that period and then permanently
	// deleted.
	//
	// A browser-only (Path A) session never captured a face and is
	// rejected with SESSION_HAS_NO_FACE_DATA — retrying never helps. One
	// past its retention window is rejected with
	// SESSION_FACE_DATA_EXPIRED.
	SessionToken string `json:"session_token"`

	// Reason is why the person is being blacklisted (max 500 chars).
	Reason string `json:"reason"`
}

// BlacklistAddImageParams contains the parameters for AddByImage. Both
// fields are required.
type BlacklistAddImageParams struct {
	// Image is the face image as a base64 string (max ~10MB of base64,
	// roughly 7.5MB decoded).
	Image string `json:"image"`

	// Reason is why the person is being blacklisted (max 500 chars).
	Reason string `json:"reason"`
}

// List retrieves a page of your active blacklist entries.
//
// Pass nil opts for the server defaults (page 1, 20 per page). Pagination
// metadata (total, total pages) is available on the returned Response:
//
//	entries, resp, err := client.Blacklist.List(ctx, &xident.BlacklistListOptions{
//	    Page:    1,
//	    PerPage: 50,
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	if resp.Pagination != nil {
//	    fmt.Printf("%d entries total\n", resp.Pagination.Total)
//	}
func (s *BlacklistService) List(ctx context.Context, opts *BlacklistListOptions) ([]BlacklistEntry, *Response, error) {
	path := "blacklist"
	if opts != nil {
		q := url.Values{}
		if opts.Page > 0 {
			q.Set("page", strconv.Itoa(opts.Page))
		}
		if opts.PerPage > 0 {
			q.Set("per_page", strconv.Itoa(opts.PerPage))
		}
		if len(q) > 0 {
			path += "?" + q.Encode()
		}
	}

	req, err := s.client.newRequest(http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	var entries []BlacklistEntry
	resp, err := s.client.do(ctx, req, &entries)
	if err != nil {
		return nil, resp, err
	}

	return entries, resp, nil
}

// AddBySession blacklists the person from one of your verification sessions.
//
// The server lifts the selfie from the session and derives the embedding
// asynchronously -- the entry appears in List once processed. The session
// must be terminal; a still-running session is rejected with HTTP 409
// (returned as a *ValidationError).
func (s *BlacklistService) AddBySession(ctx context.Context, params *BlacklistAddSessionParams) (*BlacklistAddResult, *Response, error) {
	if params == nil {
		return nil, nil, fmt.Errorf("xident: params cannot be nil")
	}

	req, err := s.client.newRequest(http.MethodPost, "blacklist/session", params)
	if err != nil {
		return nil, nil, err
	}

	result := new(BlacklistAddResult)
	resp, err := s.client.do(ctx, req, result)
	if err != nil {
		return nil, resp, err
	}

	return result, resp, nil
}

// AddByImage blacklists the face in an image.
//
// The server extracts the face embedding asynchronously -- the entry appears
// in List once processed.
func (s *BlacklistService) AddByImage(ctx context.Context, params *BlacklistAddImageParams) (*BlacklistAddResult, *Response, error) {
	if params == nil {
		return nil, nil, fmt.Errorf("xident: params cannot be nil")
	}

	req, err := s.client.newRequest(http.MethodPost, "blacklist/image", params)
	if err != nil {
		return nil, nil, err
	}

	result := new(BlacklistAddResult)
	resp, err := s.client.do(ctx, req, result)
	if err != nil {
		return nil, resp, err
	}

	return result, resp, nil
}

// Remove deactivates a blacklist entry (un-ban).
//
// The audit record survives server-side; retention hard-deletes the
// deactivated biometric data later.
func (s *BlacklistService) Remove(ctx context.Context, id int64) (*BlacklistRemoveResult, *Response, error) {
	if id <= 0 {
		return nil, nil, fmt.Errorf("xident: id must be a positive entry id")
	}

	path := "blacklist/" + strconv.FormatInt(id, 10)
	req, err := s.client.newRequest(http.MethodDelete, path, nil)
	if err != nil {
		return nil, nil, err
	}

	result := new(BlacklistRemoveResult)
	resp, err := s.client.do(ctx, req, result)
	if err != nil {
		return nil, resp, err
	}

	return result, resp, nil
}
