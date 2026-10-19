// Package kvclient is a Go SDK for gobin's key-value API (stage 9).
//
// Stage 10 builds the client, stage 16 adds RoundTripper middleware
// (transport.go is a good home for it), and stage 20 adds Watch.
package kvclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

var errNotImplemented = errors.New("kvclient: not implemented yet")

// Stage 10 --------------------------------------------------------------------

// ErrNotFound reports that a key does not exist. Callers check it with
// errors.Is(err, kvclient.ErrNotFound).
var ErrNotFound = errors.New("kvclient: key not found")

// APIError is returned when the server answers with a non-2xx status.
type APIError struct {
	StatusCode int
	Message    string // from the {"error": "..."} body, or the body text
}

func (e *APIError) Error() string {
	return fmt.Sprintf("kvclient: server returned %d: %s", e.StatusCode, e.Message)
}

// TODO(stage 10): make errors.Is(err, ErrNotFound) true when err is an
// *APIError with StatusCode 404.

// Client talks to a gobin server. It must be safe for concurrent use.
type Client struct {
	// TODO(stage 10): add fields.
}

// New returns a client for the server at baseURL, which may include a path
// prefix (e.g. "http://host/api"). If hc is nil, use a client with a timeout.
func New(baseURL string, hc *http.Client) (*Client, error) {
	// TODO(stage 10)
	return &Client{}, nil
}

// Put stores value under key. created reports whether the key was new.
func (c *Client) Put(ctx context.Context, key string, value []byte) (created bool, err error) {
	return false, errNotImplemented
}

// Get returns the value stored under key.
func (c *Client) Get(ctx context.Context, key string) ([]byte, error) {
	return nil, errNotImplemented
}

// Delete removes key.
func (c *Client) Delete(ctx context.Context, key string) error {
	return errNotImplemented
}

// List returns the keys that start with prefix, sorted.
func (c *Client) List(ctx context.Context, prefix string) ([]string, error) {
	return nil, errNotImplemented
}

// Stage 16 --------------------------------------------------------------------

// NewBearerTransport returns a RoundTripper that adds
// "Authorization: Bearer <token>" to every request it sends through base
// (http.DefaultTransport if base is nil).
func NewBearerTransport(base http.RoundTripper, token string) http.RoundTripper {
	// TODO(stage 16)
	return base
}

// RetryPolicy configures NewRetryTransport.
type RetryPolicy struct {
	MaxRetries int           // retries after the first attempt
	BaseDelay  time.Duration // delay before the first retry; doubles each time
	MaxDelay   time.Duration // cap for any single delay, Retry-After included
}

// NewRetryTransport returns a RoundTripper that retries failed requests
// through base according to p. See stages/16-round-trippers/README.md.
func NewRetryTransport(base http.RoundTripper, p RetryPolicy) http.RoundTripper {
	// TODO(stage 16)
	return base
}

// Stage 20 --------------------------------------------------------------------

// Event is one change reported by Watch.
type Event struct {
	ID    string // the SSE "id:" field
	Type  string // "put" or "delete"
	Key   string
	Value []byte // nil for deletes
}

// Watch streams changes to keys starting with prefix, calling fn for each.
// It returns ctx.Err() when ctx is cancelled, fn's error if fn fails, or nil
// when the server ends the stream.
func (c *Client) Watch(ctx context.Context, prefix string, fn func(Event) error) error {
	return errNotImplemented
}
