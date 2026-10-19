package tests

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jamesz3ng/learn_http_go/gobin"
)

func TestStage19_StreamLines(t *testing.T) {
	srv := startGobin(t, gobin.Config{})
	resp, body := get(t, newClient(t), srv.URL+"/stream/5")
	wantStatus(t, resp, 200, "GET /stream/5")
	wantContentType(t, resp, "application/x-ndjson", "GET /stream/5")
	lines := strings.Split(strings.TrimSpace(body), "\n")
	if len(lines) != 5 {
		t.Fatalf("GET /stream/5 returned %d lines, want 5:\n%s", len(lines), body)
	}
	for i, l := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("line %d is not JSON: %q", i, l)
		}
		if m["id"] != float64(i) {
			t.Fatalf("line %d has id %v, want %d", i, m["id"], i)
		}
	}
	for _, bad := range []string{"/stream/0", "/stream/101", "/stream/x", "/stream/2?interval=nope", "/stream/2?interval=6s"} {
		resp, _ := get(t, newClient(t), srv.URL+bad)
		wantStatus(t, resp, 400, "GET "+bad)
	}
}

func TestStage19_StreamIsFlushed(t *testing.T) {
	srv := startGobin(t, gobin.Config{})
	start := time.Now()
	resp, err := newClient(t).Get(srv.URL + "/stream/3?interval=400ms")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	line, err := bufio.NewReader(resp.Body).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 300*time.Millisecond {
		t.Fatalf("the first line arrived after %v, but it was ready immediately; it sat in a buffer%s", d,
			hint("flush after each line with http.NewResponseController(w).Flush(). If you wrapped the ResponseWriter in middleware, give the wrapper an Unwrap() http.ResponseWriter method"))
	}
	if !strings.Contains(line, `"id":0`) && !strings.Contains(line, `"id": 0`) {
		t.Fatalf("first line = %q", line)
	}
}

// sseEvent is one parsed Server-Sent Event.
type sseEvent struct{ ID, Event, Data string }

// readEvents parses SSE from r in the background.
func readEvents(r io.Reader) <-chan sseEvent {
	ch := make(chan sseEvent, 100)
	go func() {
		defer close(ch)
		sc := bufio.NewScanner(r)
		sc.Buffer(nil, 4<<20)
		var ev sseEvent
		for sc.Scan() {
			line := sc.Text()
			switch {
			case line == "":
				if ev.Data != "" {
					ch <- ev
				}
				ev = sseEvent{}
			case strings.HasPrefix(line, "id:"):
				ev.ID = strings.TrimSpace(line[3:])
			case strings.HasPrefix(line, "event:"):
				ev.Event = strings.TrimSpace(line[6:])
			case strings.HasPrefix(line, "data:"):
				ev.Data += strings.TrimSpace(line[5:])
			}
		}
	}()
	return ch
}

func nextEvent(t *testing.T, ch <-chan sseEvent) sseEvent {
	t.Helper()
	select {
	case ev, ok := <-ch:
		if !ok {
			t.Fatal("the event stream ended unexpectedly")
		}
		return ev
	case <-time.After(3 * time.Second):
		t.Fatalf("no event arrived within 3s%s", hint("flush after writing each event"))
	}
	return sseEvent{}
}

// openWatch starts GET /watch?prefix=... and returns once headers arrive.
func openWatch(t *testing.T, base, prefix string) (<-chan sseEvent, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/watch?prefix="+prefix, nil)
	c := &http.Client{Transport: &http.Transport{}} // no Timeout: this is a long-lived stream
	type result struct {
		resp *http.Response
		err  error
	}
	rc := make(chan result, 1)
	go func() {
		resp, err := c.Do(req)
		rc <- result{resp, err}
	}()
	select {
	case r := <-rc:
		if r.err != nil {
			t.Fatalf("GET /watch: %v", r.err)
		}
		wantStatus(t, r.resp, 200, "GET /watch")
		wantContentType(t, r.resp, "text/event-stream", "GET /watch")
		t.Cleanup(func() { r.resp.Body.Close() })
		return readEvents(r.resp.Body), cancel
	case <-time.After(3 * time.Second):
		t.Fatalf("GET /watch sent no response headers within 3s%s",
			hint("subscribe, then WriteHeader(200) and Flush right away so the client knows the watch is live"))
	}
	return nil, cancel
}

func TestStage19_Watch(t *testing.T) {
	srv := startGobin(t, gobin.Config{})
	events, _ := openWatch(t, srv.URL, "user:")
	c := newClient(t)

	do(t, c, http.MethodPut, srv.URL+"/kv/user:1", strings.NewReader("alice"))
	do(t, c, http.MethodPut, srv.URL+"/kv/post:1", strings.NewReader("ignored: wrong prefix"))
	do(t, c, http.MethodDelete, srv.URL+"/kv/user:1", nil)

	ev := nextEvent(t, events)
	var data map[string]any
	json.Unmarshal([]byte(ev.Data), &data)
	if ev.Event != "put" || data["key"] != "user:1" || data["value"] != "alice" {
		t.Fatalf("first event = %+v, want event: put with data {\"key\":\"user:1\",\"value\":\"alice\"}", ev)
	}
	if ev.ID == "" {
		t.Fatalf("events should carry an id: line, got %+v", ev)
	}
	firstID := ev.ID

	ev = nextEvent(t, events)
	data = nil
	json.Unmarshal([]byte(ev.Data), &data)
	if ev.Event != "delete" || data["key"] != "user:1" {
		t.Fatalf("second event = %+v, want event: delete for user:1 (and nothing for post:1)", ev)
	}
	if ev.ID == firstID {
		t.Fatalf("each event needs a new id, both were %q", ev.ID)
	}
}

func TestStage19_WatchOutlivesWriteTimeout(t *testing.T) {
	base := serveGobin(t, gobin.Config{WriteTimeout: 300 * time.Millisecond})
	events, _ := openWatch(t, base, "")
	time.Sleep(700 * time.Millisecond)
	do(t, newClient(t), http.MethodPut, base+"/kv/late", strings.NewReader("still here"))
	select {
	case ev, ok := <-events:
		if !ok {
			t.Fatalf("the watch stream was cut off by the server's WriteTimeout%s",
				hint("http.NewResponseController(w).SetWriteDeadline(time.Time{}) lifts the deadline for this response"))
		}
		if ev.Event != "put" {
			t.Fatalf("event = %+v", ev)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no event arrived")
	}
}

func TestStage19_SlowWatcherDoesNotBlockWriters(t *testing.T) {
	srv := startGobin(t, gobin.Config{})
	addr := strings.TrimPrefix(srv.URL, "http://")

	// A watcher that subscribes and then never reads.
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.(*net.TCPConn).SetReadBuffer(4096)
	io.WriteString(conn, "GET /watch HTTP/1.1\r\nHost: x\r\n\r\n")
	time.Sleep(100 * time.Millisecond)

	c := newClient(t)
	c.Timeout = 5 * time.Second
	value := strings.Repeat("v", 16<<10)
	start := time.Now()
	for i := range 1000 {
		req, _ := http.NewRequest(http.MethodPut, fmt.Sprintf("%s/kv/k%d", srv.URL, i), strings.NewReader(value))
		resp, err := c.Do(req)
		if err != nil {
			t.Fatalf("PUT #%d failed after %v: %v%s", i, time.Since(start), err,
				hint("never block on sending to a watcher: use select with a default case, and drop watchers whose buffer is full"))
		}
		resp.Body.Close()
	}
}

func TestStage19_WatchEndsWhenClientLeaves(t *testing.T) {
	lc, logger := newLogCapture()
	srv := startGobin(t, gobin.Config{Logger: logger})
	_, cancel := openWatch(t, srv.URL, "")
	cancel()
	if lc.find(isRequest("/watch")) == nil {
		t.Fatalf("the /watch handler didn't return after the client disconnected (no access log within 2s)%s",
			hint("select on r.Context().Done() in the event loop, and unsubscribe when you return"))
	}
}
