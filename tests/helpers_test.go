package tests

// Shared helpers for the stage tests.
//
// The tests are the specification for each stage, and they are also worked
// examples of how to test HTTP code in Go: look for httptest.NewServer,
// httptest.NewRecorder and friends as you read them.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jamesz3ng/learn_http_go/gobin"
	"github.com/jamesz3ng/learn_http_go/gocurl"
)

// hint formats a failure explanation. Failures in this course try to tell you
// what to look at next, not just what went wrong.
func hint(s string) string { return "\n    hint: " + s }

// newClient returns an http.Client with its own connection pool, so tests
// don't share keep-alive connections with each other through
// http.DefaultTransport.
func newClient(t *testing.T) *http.Client {
	t.Helper()
	tr := &http.Transport{}
	t.Cleanup(tr.CloseIdleConnections)
	return &http.Client{Transport: tr, Timeout: 10 * time.Second}
}

// newNoRedirectClient is like newClient but returns 3xx responses instead of
// following them.
func newNoRedirectClient(t *testing.T) *http.Client {
	c := newClient(t)
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return c
}

// startGobin serves gobin.NewHandler(cfg) on a random local port.
func startGobin(t *testing.T, cfg gobin.Config) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(gobin.NewHandler(cfg))
	t.Cleanup(srv.Close)
	return srv
}

// do sends a request and returns the response with its body fully read.
func do(t *testing.T, c *http.Client, method, url string, body io.Reader, headers ...string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Add(headers[i], headers[i+1])
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("%s %s: reading body: %v", method, url, err)
	}
	return resp, string(b)
}

// get is do with GET and no body.
func get(t *testing.T, c *http.Client, url string, headers ...string) (*http.Response, string) {
	t.Helper()
	return do(t, c, http.MethodGet, url, nil, headers...)
}

// decodeObject decodes a JSON object, failing the test with a helpful
// message if body is not one.
func decodeObject(t *testing.T, body string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("response body is not a JSON object: %v\n    body: %q", err, truncate(body, 300))
	}
	return m
}

// wantStatus fails the test if resp has an unexpected status code.
func wantStatus(t *testing.T, resp *http.Response, want int, what string, hints ...string) {
	t.Helper()
	if resp.StatusCode != want {
		msg := "%s: got status %d, want %d"
		for _, h := range hints {
			msg += hint(h)
		}
		t.Fatalf(msg, what, resp.StatusCode, want)
	}
}

// wantContentType fails if the Content-Type's media type isn't want.
func wantContentType(t *testing.T, resp *http.Response, want, what string) {
	t.Helper()
	ct := resp.Header.Get("Content-Type")
	mt, _, _ := strings.Cut(ct, ";")
	if strings.TrimSpace(mt) != want {
		t.Fatalf("%s: Content-Type is %q, want %q%s", what, ct, want,
			hint("set it with w.Header().Set(\"Content-Type\", ...) BEFORE calling WriteHeader or Write"))
	}
}

// stringSlice converts a decoded JSON array of strings to []string.
func stringSlice(t *testing.T, v any, what string) []string {
	t.Helper()
	arr, ok := v.([]any)
	if !ok {
		t.Fatalf("%s: want a JSON array of strings, got %T (%v)", what, v, v)
	}
	out := make([]string, len(arr))
	for i, x := range arr {
		s, ok := x.(string)
		if !ok {
			t.Fatalf("%s: element %d is %T, want string", what, i, x)
		}
		out[i] = s
	}
	return out
}

// object asserts that v is a JSON object.
func object(t *testing.T, v any, what string) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("%s: want a JSON object, got %T (%v)", what, v, v)
	}
	return m
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// ---------------------------------------------------------------------------
// gocurl helpers

type curlResult struct {
	Code   int
	Stdout string
	Stderr string
}

// curl runs gocurl.Run in-process with the given arguments.
func curl(t *testing.T, args ...string) curlResult {
	t.Helper()
	return curlStdin(t, "", args...)
}

// curlStdin is curl with the given standard input.
func curlStdin(t *testing.T, stdin string, args ...string) curlResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var stdout, stderr bytes.Buffer
	done := make(chan int, 1)
	go func() { done <- gocurl.Run(ctx, args, strings.NewReader(stdin), &stdout, &stderr) }()
	select {
	case code := <-done:
		return curlResult{Code: code, Stdout: stdout.String(), Stderr: stderr.String()}
	case <-time.After(20 * time.Second):
		t.Fatalf("gocurl %v did not return within 20s%s", args,
			hint("is something waiting forever? Check timeouts and that you return after the response is done"))
		return curlResult{}
	}
}

// wantCode fails if gocurl exited with an unexpected status.
func (r curlResult) wantCode(t *testing.T, want int, what string, hints ...string) {
	t.Helper()
	if r.Code != want {
		msg := "%s: gocurl exited with %d, want %d\n    stdout: %q\n    stderr: %q"
		for _, h := range hints {
			msg += hint(h)
		}
		t.Fatalf(msg, what, r.Code, want, truncate(r.Stdout, 300), truncate(r.Stderr, 300))
	}
}

// recorded is a request as seen by a recordingServer.
type recorded struct {
	Method        string
	Path          string // r.URL.Path
	RawPath       string // r.URL.EscapedPath()
	RawQuery      string
	Host          string
	Header        http.Header
	Body          string
	ContentLength int64
	TransferEnc   []string
}

// recordingServer is a test server that remembers every request it receives
// and answers with handler (or 200 "ok" if handler is nil).
type recordingServer struct {
	*httptest.Server
	mu   sync.Mutex
	reqs []recorded
}

func newRecordingServer(t *testing.T, handler http.HandlerFunc) *recordingServer {
	t.Helper()
	rs := &recordingServer{}
	rs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		rs.mu.Lock()
		rs.reqs = append(rs.reqs, recorded{
			Method:        r.Method,
			Path:          r.URL.Path,
			RawPath:       r.URL.EscapedPath(),
			RawQuery:      r.URL.RawQuery,
			Host:          r.Host,
			Header:        r.Header.Clone(),
			Body:          string(b),
			ContentLength: r.ContentLength,
			TransferEnc:   r.TransferEncoding,
		})
		rs.mu.Unlock()
		r.Body = io.NopCloser(bytes.NewReader(b))
		if handler != nil {
			handler(w, r)
			return
		}
		io.WriteString(w, "ok")
	}))
	t.Cleanup(rs.Close)
	return rs
}

// requests returns a copy of the requests received so far.
func (rs *recordingServer) requests() []recorded {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return append([]recorded(nil), rs.reqs...)
}

// last returns the most recent request, failing if there was none.
func (rs *recordingServer) last(t *testing.T) recorded {
	t.Helper()
	reqs := rs.requests()
	if len(reqs) == 0 {
		t.Fatalf("the test server received no request")
	}
	return reqs[len(reqs)-1]
}

// ---------------------------------------------------------------------------
// connection counting

// countConns installs a ConnState hook on an unstarted test server and
// returns a counter of new connections.
func countConns(srv *httptest.Server) *atomic.Int64 {
	var n atomic.Int64
	srv.Config.ConnState = func(_ net.Conn, s http.ConnState) {
		if s == http.StateNew {
			n.Add(1)
		}
	}
	return &n
}

// closedPortURL returns an http URL on 127.0.0.1 where nothing is listening.
func closedPortURL(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	return "http://" + addr
}

// ---------------------------------------------------------------------------
// concurrency-safe buffers

// syncBuffer is a bytes.Buffer that is safe for concurrent use. Loggers and
// streaming writers in these tests write from other goroutines.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// eventually polls cond every 10ms until it returns true or timeout passes.
func eventually(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(10 * time.Millisecond)
	}
}
