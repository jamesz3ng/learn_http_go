package tests

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jamesz3ng/learn_http_go/kvclient"
)

// flakyServer fails the first `failures` requests with status `code`.
type flakyServer struct {
	*httptest.Server
	conns    *atomic.Int64
	attempts atomic.Int64
	mu       sync.Mutex
	bodies   []string
	auths    []string
}

func newFlakyServer(t *testing.T, failures int64, code int, retryAfter string) *flakyServer {
	t.Helper()
	f := &flakyServer{}
	f.Server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := f.attempts.Add(1)
		b, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.bodies = append(f.bodies, string(b))
		f.auths = append(f.auths, r.Header.Get("Authorization"))
		f.mu.Unlock()
		if n <= failures {
			if retryAfter != "" {
				w.Header().Set("Retry-After", retryAfter)
			}
			http.Error(w, strings.Repeat("unavailable ", 200), code)
			return
		}
		if r.Method == http.MethodPut {
			w.WriteHeader(http.StatusCreated)
			return
		}
		io.WriteString(w, "ok")
	}))
	f.conns = countConns(f.Server)
	f.Start()
	t.Cleanup(f.Close)
	return f
}

func retryClient(t *testing.T, p kvclient.RetryPolicy) *http.Client {
	base := &http.Transport{}
	t.Cleanup(base.CloseIdleConnections)
	return &http.Client{Transport: kvclient.NewRetryTransport(base, p), Timeout: 20 * time.Second}
}

var fastPolicy = kvclient.RetryPolicy{MaxRetries: 3, BaseDelay: 10 * time.Millisecond, MaxDelay: 100 * time.Millisecond}

func TestStage16_RetriesUntilSuccess(t *testing.T) {
	f := newFlakyServer(t, 2, http.StatusServiceUnavailable, "")
	resp, body := get(t, retryClient(t, fastPolicy), f.URL)
	if resp.StatusCode != 200 || body != "ok" {
		t.Fatalf("after 2 failures and 3 allowed retries, got %d %q, want 200 ok", resp.StatusCode, truncate(body, 40))
	}
	if n := f.attempts.Load(); n != 3 {
		t.Fatalf("server saw %d attempts, want 3", n)
	}
	if n := f.conns.Load(); n != 1 {
		t.Fatalf("3 attempts used %d connections, want 1%s", n,
			hint("drain and close the body of every response you throw away, or its connection can't be reused"))
	}
}

func TestStage16_GivesUpAfterMaxRetries(t *testing.T) {
	f := newFlakyServer(t, 100, http.StatusBadGateway, "")
	p := fastPolicy
	p.MaxRetries = 2
	resp, _ := get(t, retryClient(t, p), f.URL)
	if resp.StatusCode != 502 {
		t.Fatalf("when retries run out, return the last response as-is; got status %d, want 502", resp.StatusCode)
	}
	if n := f.attempts.Load(); n != 3 {
		t.Fatalf("MaxRetries=2 made %d attempts, want 3 (1 + 2 retries)", n)
	}
}

func TestStage16_RetryableStatuses(t *testing.T) {
	for _, code := range []int{429, 502, 503, 504} {
		f := newFlakyServer(t, 1, code, "")
		get(t, retryClient(t, fastPolicy), f.URL)
		if n := f.attempts.Load(); n != 2 {
			t.Fatalf("status %d should be retried: %d attempts, want 2", code, n)
		}
	}
	for _, code := range []int{400, 404, 500, 501} {
		f := newFlakyServer(t, 1, code, "")
		get(t, retryClient(t, fastPolicy), f.URL)
		if n := f.attempts.Load(); n != 1 {
			t.Fatalf("status %d should NOT be retried: %d attempts, want 1", code, n)
		}
	}
}

func TestStage16_DoesNotRetryPOST(t *testing.T) {
	f := newFlakyServer(t, 1, http.StatusServiceUnavailable, "")
	c := retryClient(t, fastPolicy)
	resp, _ := do(t, c, http.MethodPost, f.URL, strings.NewReader("order=1"))
	if resp.StatusCode != 503 || f.attempts.Load() != 1 {
		t.Fatalf("a POST was retried (%d attempts); POST isn't idempotent, so retrying could e.g. place an order twice", f.attempts.Load())
	}
	f2 := newFlakyServer(t, 1, http.StatusServiceUnavailable, "")
	resp, _ = do(t, c, http.MethodPost, f2.URL, strings.NewReader("order=1"), "Idempotency-Key", "abc")
	if resp.StatusCode != 200 || f2.attempts.Load() != 2 {
		t.Fatalf("a POST with an Idempotency-Key header may be retried: got %d after %d attempts", resp.StatusCode, f2.attempts.Load())
	}
}

func TestStage16_ResendsTheBody(t *testing.T) {
	f := newFlakyServer(t, 2, http.StatusServiceUnavailable, "")
	kv, err := kvclient.New(f.URL, retryClient(t, fastPolicy))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := kv.Put(context.Background(), "k", []byte("the value")); err != nil {
		t.Fatalf("Put through the retry transport: %v", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, b := range f.bodies {
		if b != "the value" {
			t.Fatalf("attempt %d sent body %q, want %q every time%s", i+1, b, "the value",
				hint("the first attempt consumes req.Body; call req.GetBody() for a fresh copy"))
		}
	}
}

func TestStage16_RetriesNetworkErrors(t *testing.T) {
	f := newFlakyServer(t, 0, 0, "")
	var calls atomic.Int64
	// A base transport whose first attempt fails before reaching the server.
	flakyNet := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			if r.Body != nil {
				r.Body.Close()
			}
			return nil, errors.New("connection reset by peer (simulated)")
		}
		return http.DefaultTransport.RoundTrip(r)
	})
	c := &http.Client{Transport: kvclient.NewRetryTransport(flakyNet, fastPolicy)}
	resp, body := get(t, c, f.URL)
	if resp.StatusCode != 200 || body != "ok" || calls.Load() != 2 {
		t.Fatalf("a network error should be retried: status %d after %d calls", resp.StatusCode, calls.Load())
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestStage16_Backoff(t *testing.T) {
	f := newFlakyServer(t, 2, http.StatusServiceUnavailable, "")
	p := kvclient.RetryPolicy{MaxRetries: 2, BaseDelay: 100 * time.Millisecond, MaxDelay: time.Second}
	start := time.Now()
	get(t, retryClient(t, p), f.URL)
	// Delays of ~100ms then ~200ms; with up to 50% jitter at least 150ms.
	if d := time.Since(start); d < 150*time.Millisecond {
		t.Fatalf("2 retries with BaseDelay 100ms took only %v; wait BaseDelay * 2^attempt between attempts", d)
	}
}

func TestStage16_RetryAfter(t *testing.T) {
	f := newFlakyServer(t, 1, http.StatusTooManyRequests, "1")
	p := kvclient.RetryPolicy{MaxRetries: 1, BaseDelay: time.Millisecond, MaxDelay: 5 * time.Second}
	start := time.Now()
	get(t, retryClient(t, p), f.URL)
	if d := time.Since(start); d < 900*time.Millisecond {
		t.Fatalf("the server said Retry-After: 1 but the retry came after %v", d)
	}

	f = newFlakyServer(t, 1, http.StatusTooManyRequests, "30")
	p.MaxDelay = 100 * time.Millisecond
	start = time.Now()
	get(t, retryClient(t, p), f.URL)
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("Retry-After: 30 should be capped at MaxDelay (100ms), but waiting took %v", d)
	}
}

func TestStage16_StopsWhenContextIsCancelled(t *testing.T) {
	f := newFlakyServer(t, 100, http.StatusServiceUnavailable, "")
	p := kvclient.RetryPolicy{MaxRetries: 5, BaseDelay: 10 * time.Second, MaxDelay: 10 * time.Second}
	c := retryClient(t, p)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, f.URL, nil)
	start := time.Now()
	resp, err := c.Do(req)
	if err == nil {
		resp.Body.Close()
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("a cancelled context should interrupt the backoff wait; the request took %v%s", d,
			hint("select on a timer and req.Context().Done()"))
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want one wrapping context.DeadlineExceeded", err)
	}
}

func TestStage16_BearerTransport(t *testing.T) {
	f := newFlakyServer(t, 0, 0, "")
	rt := kvclient.NewBearerTransport(nil, "s3cr3t")
	req, _ := http.NewRequest(http.MethodGet, f.URL, nil)
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	f.mu.Lock()
	got := f.auths[0]
	f.mu.Unlock()
	if got != "Bearer s3cr3t" {
		t.Fatalf("server saw Authorization %q, want %q", got, "Bearer s3cr3t")
	}
	if req.Header.Get("Authorization") != "" {
		t.Fatalf("the caller's request was modified (it now has Authorization %q)%s", req.Header.Get("Authorization"),
			hint("a RoundTripper must not modify its request: req.Clone(req.Context()) first"))
	}
}

func TestStage16_Composes(t *testing.T) {
	f := newFlakyServer(t, 1, http.StatusServiceUnavailable, "")
	c := &http.Client{Transport: kvclient.NewBearerTransport(kvclient.NewRetryTransport(nil, fastPolicy), "tok")}
	resp, _ := get(t, c, f.URL)
	wantStatus(t, resp, 200, "bearer(retry(default)) after one 503")
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, a := range f.auths {
		if a != "Bearer tok" {
			t.Fatalf("attempt %d had Authorization %q, want every attempt authenticated", i+1, a)
		}
	}
}
