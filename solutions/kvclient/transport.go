package kvclient

import (
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"time"
)

// Stage 16 --------------------------------------------------------------------
//
// An http.RoundTripper turns one request into one response. Wrapping one in
// another is client-side middleware: auth, retries, logging, caching...
//
// The RoundTripper contract (see its doc comment) says, among other things:
//   - RoundTrip must not modify the request (clone it first);
//   - it should not try to interpret the response (that's the Client's job);
//   - it must always close the request body, even on errors.

// roundTripperFunc adapts a function to http.RoundTripper, just as
// http.HandlerFunc adapts a function to http.Handler.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func orDefaultTransport(rt http.RoundTripper) http.RoundTripper {
	if rt == nil {
		return http.DefaultTransport
	}
	return rt
}

// NewBearerTransport adds "Authorization: Bearer <token>" to every request.
func NewBearerTransport(base http.RoundTripper, token string) http.RoundTripper {
	base = orDefaultTransport(base)
	return roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		// Clone: the caller still owns req and may reuse or inspect it.
		r2 := req.Clone(req.Context())
		r2.Header.Set("Authorization", "Bearer "+token)
		return base.RoundTrip(r2)
	})
}

// RetryPolicy configures NewRetryTransport.
type RetryPolicy struct {
	MaxRetries int           // retries after the first attempt
	BaseDelay  time.Duration // delay before the first retry; doubles each time
	MaxDelay   time.Duration // cap for any single delay, Retry-After included
}

// NewRetryTransport retries idempotent requests that fail with a network
// error or a 429/502/503/504 response, with exponential backoff and jitter.
func NewRetryTransport(base http.RoundTripper, p RetryPolicy) http.RoundTripper {
	base = orDefaultTransport(base)
	if p.MaxDelay <= 0 {
		p.MaxDelay = 30 * time.Second
	}
	return roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		if !retryable(req) {
			return base.RoundTrip(req)
		}
		for attempt := 0; ; attempt++ {
			r := req
			if attempt > 0 {
				// The previous attempt consumed (and closed) the body: get a
				// fresh copy. NewRequest sets GetBody for bytes/strings
				// readers; streaming bodies can't be replayed.
				r = req.Clone(req.Context())
				if req.Body != nil && req.Body != http.NoBody {
					body, err := req.GetBody()
					if err != nil {
						return nil, err
					}
					r.Body = body
				}
			}

			resp, err := base.RoundTrip(r)
			if attempt >= p.MaxRetries || !shouldRetry(resp, err) || req.Context().Err() != nil {
				return resp, err
			}

			delay := backoff(p, attempt)
			if resp != nil {
				if d, ok := retryAfter(resp); ok {
					delay = min(d, p.MaxDelay)
				}
				// We're discarding this response: drain and close it so its
				// connection goes back to the pool instead of leaking.
				io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
				resp.Body.Close()
			}

			timer := time.NewTimer(delay)
			select {
			case <-timer.C:
			case <-req.Context().Done():
				timer.Stop()
				return nil, req.Context().Err()
			}
		}
	})
}

// retryable reports whether req may safely be sent more than once.
func retryable(req *http.Request) bool {
	if req.Body != nil && req.Body != http.NoBody && req.GetBody == nil {
		return false // can't replay the body
	}
	switch req.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodPut, http.MethodDelete:
		return true
	}
	// Non-idempotent methods are safe to retry only when the server can
	// deduplicate them.
	return req.Header.Get("Idempotency-Key") != ""
}

func shouldRetry(resp *http.Response, err error) bool {
	if err != nil {
		return true
	}
	switch resp.StatusCode {
	case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

// backoff returns BaseDelay * 2^attempt, capped, with "equal jitter": a
// random value between half and all of it, so that many clients that failed
// together don't all retry at the same instant.
func backoff(p RetryPolicy, attempt int) time.Duration {
	d := p.BaseDelay << attempt
	if d <= 0 || d > p.MaxDelay { // d <= 0 catches overflow
		d = p.MaxDelay
	}
	half := d / 2
	if half <= 0 {
		return d
	}
	return half + rand.N(half+1)
}

// retryAfter parses a Retry-After header given in seconds. (It may also be
// an HTTP date; supporting that is left as an exploration.)
func retryAfter(resp *http.Response) (time.Duration, bool) {
	s := resp.Header.Get("Retry-After")
	if s == "" {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0, false
	}
	return time.Duration(n) * time.Second, true
}
