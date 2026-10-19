// Package kvclient is a Go SDK for gobin's key-value API.
//
// REFERENCE SOLUTION — stages 10, 16 and 20.
package kvclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Stage 10 --------------------------------------------------------------------

// ErrNotFound reports that a key does not exist. Check with errors.Is.
var ErrNotFound = errors.New("kvclient: key not found")

// APIError is returned when the server answers with a non-2xx status.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("kvclient: server returned %d: %s", e.StatusCode, e.Message)
}

// Is makes errors.Is(err, ErrNotFound) true for 404 responses, so callers
// can use the sentinel without knowing about APIError.
func (e *APIError) Is(target error) bool {
	return target == ErrNotFound && e.StatusCode == http.StatusNotFound
}

// Client talks to a gobin server. It is safe for concurrent use.
type Client struct {
	base *url.URL
	hc   *http.Client
}

const userAgent = "kvclient/1.0"

// New returns a client for the server at baseURL (which may include a path
// prefix, e.g. "http://host/api"). If hc is nil, a client with a 10 second
// timeout is used: http.DefaultClient has no timeout at all.
func New(baseURL string, hc *http.Client) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("kvclient: invalid base URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" || u.Host == "" {
		return nil, fmt.Errorf("kvclient: base URL must be absolute http(s), got %q", baseURL)
	}
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{base: u, hc: hc}, nil
}

// keyURL builds base + "/kv/" + key. url.PathEscape matters: without it a
// key like "a/b" would become two path segments, and "a?b" would start a
// query string.
//
// Some keys can't be addressed at all: "" would mean the /kv collection
// itself, and "." and ".." are path segments that JoinPath (and every server)
// cleans away. Reject them rather than silently talking to the wrong URL.
func (c *Client) keyURL(key string) (string, error) {
	if key == "" || key == "." || key == ".." || len(key) > 256 {
		return "", fmt.Errorf("kvclient: invalid key %q (must be 1-256 bytes, not \".\" or \"..\")", key)
	}
	return c.base.JoinPath("kv", url.PathEscape(key)).String(), nil
}

// do sends a request and returns the response if its status is 2xx.
// Otherwise it reads the error body, closes it, and returns an *APIError.
func (c *Client) do(ctx context.Context, method, url string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		defer resp.Body.Close()
		return nil, readAPIError(resp)
	}
	return resp, nil
}

// readAPIError builds an APIError from {"error": "..."} or, failing that,
// from the (truncated) body text.
func readAPIError(resp *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	var payload struct {
		Error string `json:"error"`
	}
	msg := strings.TrimSpace(string(b))
	if json.Unmarshal(b, &payload) == nil && payload.Error != "" {
		msg = payload.Error
	}
	if msg == "" {
		msg = http.StatusText(resp.StatusCode)
	}
	return &APIError{StatusCode: resp.StatusCode, Message: msg}
}

// drainClose reads what's left of a body and closes it, so the connection
// can be reused for the next request.
func drainClose(body io.ReadCloser) {
	io.Copy(io.Discard, io.LimitReader(body, 64<<10))
	body.Close()
}

// Put stores value under key. created reports whether the key was new.
func (c *Client) Put(ctx context.Context, key string, value []byte) (created bool, err error) {
	u, err := c.keyURL(key)
	if err != nil {
		return false, err
	}
	resp, err := c.do(ctx, http.MethodPut, u, bytes.NewReader(value))
	if err != nil {
		return false, err
	}
	drainClose(resp.Body)
	return resp.StatusCode == http.StatusCreated, nil
}

// Get returns the value stored under key.
func (c *Client) Get(ctx context.Context, key string) ([]byte, error) {
	u, err := c.keyURL(key)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// Delete removes key.
func (c *Client) Delete(ctx context.Context, key string) error {
	u, err := c.keyURL(key)
	if err != nil {
		return err
	}
	resp, err := c.do(ctx, http.MethodDelete, u, nil)
	if err != nil {
		return err
	}
	drainClose(resp.Body)
	return nil
}

// List returns the keys that start with prefix, sorted.
func (c *Client) List(ctx context.Context, prefix string) ([]string, error) {
	u := c.base.JoinPath("kv")
	u.RawQuery = url.Values{"prefix": {prefix}}.Encode()
	resp, err := c.do(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	defer drainClose(resp.Body)
	var out struct {
		Keys []string `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("kvclient: decoding list response: %w", err)
	}
	return out.Keys, nil
}
