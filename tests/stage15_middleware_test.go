package tests

import (
	"encoding/json"
	"log/slog"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jamesz3ng/learn_http_go/gobin"
)

// logCapture collects JSON log records written by a *slog.Logger.
type logCapture struct{ buf syncBuffer }

func newLogCapture() (*logCapture, *slog.Logger) {
	lc := &logCapture{}
	return lc, slog.New(slog.NewJSONHandler(&lc.buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func (lc *logCapture) records() []map[string]any {
	var out []map[string]any
	for _, line := range strings.Split(lc.buf.String(), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if json.Unmarshal([]byte(line), &m) == nil {
			out = append(out, m)
		}
	}
	return out
}

// find waits up to 2s for a record matching pred. Log lines may be written
// a moment after the client has received the response.
func (lc *logCapture) find(pred func(map[string]any) bool) map[string]any {
	var found map[string]any
	eventually(2*time.Second, func() bool {
		for _, r := range lc.records() {
			if pred(r) {
				found = r
				return true
			}
		}
		return false
	})
	return found
}

func isRequest(path string) func(map[string]any) bool {
	return func(r map[string]any) bool { return r["msg"] == "request" && r["path"] == path }
}

func TestStage15_AccessLog(t *testing.T) {
	lc, logger := newLogCapture()
	srv := startGobin(t, gobin.Config{Logger: logger})
	c := newClient(t)

	resp, body := get(t, c, srv.URL+"/get?x=1")
	wantStatus(t, resp, 200, "GET /get")
	rec := lc.find(isRequest("/get"))
	if rec == nil {
		t.Fatalf("no log record with msg=\"request\" and path=\"/get\"; got records: %v%s", lc.records(),
			hint(`logger.Info("request", "method", r.Method, "path", r.URL.Path, "status", ..., ...)`))
	}
	if rec["method"] != "GET" || rec["status"] != float64(200) || rec["level"] != "INFO" {
		t.Fatalf("log record = %v, want method GET, status 200, level INFO", rec)
	}
	if rec["bytes"] != float64(len(body)) {
		t.Fatalf("log record bytes = %v, want %d (the number of body bytes written)", rec["bytes"], len(body))
	}
	if _, ok := rec["duration"].(float64); !ok {
		t.Fatalf(`log record needs a numeric "duration" (slog.Duration), got %v`, rec["duration"])
	}
	if rec["request_id"] != resp.Header.Get("X-Request-Id") {
		t.Fatalf("log request_id %v doesn't match the X-Request-Id response header %q", rec["request_id"], resp.Header.Get("X-Request-Id"))
	}
}

func TestStage15_LogsStatusCodes(t *testing.T) {
	lc, logger := newLogCapture()
	srv := startGobin(t, gobin.Config{Logger: logger})
	c := newClient(t)

	get(t, c, srv.URL+"/status/418")
	if rec := lc.find(isRequest("/status/418")); rec == nil || rec["status"] != float64(418) {
		t.Fatalf("log for /status/418 = %v, want status 418%s", rec,
			hint("wrap the ResponseWriter in a struct that remembers the code passed to WriteHeader"))
	}
	get(t, c, srv.URL+"/status/503")
	if rec := lc.find(isRequest("/status/503")); rec == nil || rec["status"] != float64(503) || rec["level"] != "ERROR" {
		t.Fatalf("log for /status/503 = %v, want status 503 at level ERROR", rec)
	}
	get(t, c, srv.URL+"/no/such/page")
	if rec := lc.find(isRequest("/no/such/page")); rec == nil || rec["status"] != float64(404) {
		t.Fatalf("log for a 404 = %v, want status 404 (wrap the whole mux, not each handler)", rec)
	}
}

func TestStage15_RequestID(t *testing.T) {
	srv := startGobin(t, gobin.Config{})
	c := newClient(t)
	hex32 := regexp.MustCompile(`^[0-9a-f]{32}$`)

	resp, _ := get(t, c, srv.URL+"/healthz")
	id1 := resp.Header.Get("X-Request-Id")
	if !hex32.MatchString(id1) {
		t.Fatalf("X-Request-Id = %q, want 32 lower-case hex characters (16 random bytes)", id1)
	}
	resp, _ = get(t, c, srv.URL+"/healthz")
	if id2 := resp.Header.Get("X-Request-Id"); id2 == id1 {
		t.Fatalf("two requests got the same X-Request-Id %q", id1)
	}

	resp, _ = get(t, c, srv.URL+"/healthz", "X-Request-Id", "upstream-123.abc_DEF")
	if got := resp.Header.Get("X-Request-Id"); got != "upstream-123.abc_DEF" {
		t.Fatalf("a valid incoming X-Request-Id should be reused, got %q", got)
	}
	for _, bad := range []string{"has spaces", strings.Repeat("a", 129), "semi;colon", "<script>"} {
		resp, _ = get(t, c, srv.URL+"/healthz", "X-Request-Id", bad)
		if got := resp.Header.Get("X-Request-Id"); !hex32.MatchString(got) {
			t.Fatalf("an invalid incoming X-Request-Id %q must be replaced by a fresh one, got %q", bad, got)
		}
	}
}

func TestStage15_RecoversPanics(t *testing.T) {
	lc, logger := newLogCapture()
	srv := startGobin(t, gobin.Config{Logger: logger})
	c := newClient(t)

	resp, body := get(t, c, srv.URL+"/panic")
	wantStatus(t, resp, 500, "GET /panic",
		"defer a function that calls recover() and writes a 500 if the handler panicked")
	if m := decodeObject(t, body); m["error"] != "internal server error" {
		t.Fatalf(`/panic body = %s, want {"error": "internal server error"}`, body)
	}
	if resp.Header.Get("X-Request-Id") == "" {
		t.Fatalf("even a panicking request should get an X-Request-Id header")
	}
	rec := lc.find(func(r map[string]any) bool { return r["msg"] == "panic recovered" })
	panicValue, _ := rec["panic"].(string)
	if rec == nil || rec["level"] != "ERROR" || !strings.Contains(panicValue, "boom") {
		t.Fatalf(`want an ERROR record with msg "panic recovered" and panic "boom", got %v`, rec)
	}
	if rec := lc.find(isRequest("/panic")); rec == nil || rec["status"] != float64(500) {
		t.Fatalf("the access log for /panic = %v, want status 500 (recovery must run inside the logging middleware)", rec)
	}

	resp, _ = get(t, c, srv.URL+"/healthz")
	wantStatus(t, resp, 200, "GET /healthz after a panic")
}

func TestStage15_NilLoggerIsFine(t *testing.T) {
	srv := startGobin(t, gobin.Config{})
	resp, _ := get(t, newClient(t), srv.URL+"/healthz")
	wantStatus(t, resp, 200, "GET /healthz with Config{} (no Logger)", "treat a nil Logger as 'discard logs'")
}
