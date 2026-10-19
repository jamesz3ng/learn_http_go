package tests

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jamesz3ng/learn_http_go/proxy"
)

// backend is a test backend that identifies itself and echoes what it saw.
func backend(t *testing.T, name string) *recordingServer {
	return newRecordingServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/events":
			w.Header().Set("Content-Type", "text/event-stream")
			io.WriteString(w, "data: first\n\n")
			http.NewResponseController(w).Flush()
			select {
			case <-time.After(3 * time.Second):
			case <-r.Context().Done():
			}
			io.WriteString(w, "data: second\n\n")
		case "/progress":
			// A response with a known length that arrives in two parts.
			first, rest := "first part\n", "second part\n"
			w.Header().Set("Content-Length", itoa(len(first)+len(rest)))
			io.WriteString(w, first)
			http.NewResponseController(w).Flush()
			select {
			case <-time.After(3 * time.Second):
			case <-r.Context().Done():
				return
			}
			io.WriteString(w, rest)
		default:
			w.Header().Set("X-Backend", name)
			io.WriteString(w, name+" "+r.Method+" "+r.URL.RequestURI())
		}
	})
}

func startProxy(t *testing.T, cfg proxy.Config) *httptest.Server {
	t.Helper()
	h, err := proxy.New(cfg)
	if err != nil {
		t.Fatalf("proxy.New: %v", err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func TestStage26_Validation(t *testing.T) {
	if _, err := proxy.New(proxy.Config{}); err == nil {
		t.Fatal("proxy.New with no backends should fail")
	}
	if _, err := proxy.New(proxy.Config{Backends: []string{"::nope"}}); err == nil {
		t.Fatal("proxy.New with an invalid backend URL should fail")
	}
}

func TestStage26_RoundRobin(t *testing.T) {
	a, b, c := backend(t, "a"), backend(t, "b"), backend(t, "c")
	p := startProxy(t, proxy.Config{Backends: []string{a.URL, b.URL, c.URL}})
	client := newClient(t)
	counts := map[string]int{}
	for range 9 {
		resp, body := get(t, client, p.URL+"/x")
		wantStatus(t, resp, 200, "GET through the proxy")
		name, _, _ := strings.Cut(body, " ")
		counts[name]++
	}
	if counts["a"] != 3 || counts["b"] != 3 || counts["c"] != 3 {
		t.Fatalf("9 requests over 3 backends were distributed %v, want 3 each (round robin)", counts)
	}
}

func TestStage26_ForwardsTheRequest(t *testing.T) {
	a := backend(t, "a")
	p := startProxy(t, proxy.Config{Backends: []string{a.URL + "/base/"}})
	resp, body := do(t, newClient(t), http.MethodPost, p.URL+"/items/42?sort=asc", strings.NewReader("payload"),
		"X-Custom", "kept",
		"Connection", "X-Hop",
		"X-Hop", "must not reach the backend",
	)
	wantStatus(t, resp, 200, "POST through the proxy")
	if body != "a POST /base/items/42?sort=asc" {
		t.Fatalf("backend saw %q, want path /base/items/42?sort=asc (backend path + request path)", body)
	}
	if resp.Header.Get("X-Backend") != "a" {
		t.Fatalf("response headers from the backend should reach the client")
	}
	r := a.last(t)
	if r.Body != "payload" {
		t.Fatalf("backend got body %q, want %q", r.Body, "payload")
	}
	if r.Header.Get("X-Custom") != "kept" {
		t.Fatalf("end-to-end headers must be forwarded")
	}
	if r.Header.Get("X-Hop") != "" {
		t.Fatalf("headers named in Connection are hop-by-hop and must be removed%s",
			hint("httputil.ReverseProxy does this for you; if you proxy by hand, see RFC 9110 section 7.6.1"))
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "127.0.0.1" {
		t.Fatalf("X-Forwarded-For = %q, want 127.0.0.1", xff)
	}
	if xfh := r.Header.Get("X-Forwarded-Host"); xfh != strings.TrimPrefix(p.URL, "http://") {
		t.Fatalf("X-Forwarded-Host = %q, want the host the client used (%s)", xfh, strings.TrimPrefix(p.URL, "http://"))
	}
	if xfp := r.Header.Get("X-Forwarded-Proto"); xfp != "http" {
		t.Fatalf("X-Forwarded-Proto = %q, want http", xfp)
	}
}

func TestStage26_Failover(t *testing.T) {
	a, c := backend(t, "a"), backend(t, "c")
	dead := closedPortURL(t)
	p := startProxy(t, proxy.Config{Backends: []string{a.URL, dead, c.URL}, Cooldown: time.Minute})
	client := newClient(t)
	for i := range 6 {
		resp, _ := get(t, client, p.URL+"/x")
		wantStatus(t, resp, 200, "GET with one backend down (request "+itoa(i+1)+")",
			"if a backend can't be reached, mark it down and try the next one (safe here: GET has no body)")
	}
	if n := len(a.requests()) + len(c.requests()); n != 6 {
		t.Fatalf("live backends served %d of 6 requests", n)
	}
}

func TestStage26_AllDown(t *testing.T) {
	p := startProxy(t, proxy.Config{Backends: []string{closedPortURL(t), closedPortURL(t)}})
	resp, _ := get(t, newClient(t), p.URL+"/x")
	wantStatus(t, resp, 502, "GET with every backend down", "answer 502 Bad Gateway when no backend can be reached")
}

func TestStage26_Streaming(t *testing.T) {
	a := backend(t, "a")
	p := startProxy(t, proxy.Config{Backends: []string{a.URL}})
	start := time.Now()
	resp, err := newClient(t).Get(p.URL + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	line, _ := bufio.NewReader(resp.Body).ReadString('\n')
	if line != "data: first\n" {
		t.Fatalf("first line = %q", line)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("the first event took %v to come through the proxy; it was buffered%s", d,
			hint("flush to the client after every write from the backend"))
	}
}

func TestStage26_FlushesEveryResponse(t *testing.T) {
	// Not just event streams: a slow response with a Content-Length must also
	// reach the client as it arrives.
	a := backend(t, "a")
	p := startProxy(t, proxy.Config{Backends: []string{a.URL}})
	start := time.Now()
	resp, err := newClient(t).Get(p.URL + "/progress")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	line, _ := bufio.NewReader(resp.Body).ReadString('\n')
	if line != "first part\n" {
		t.Fatalf("first line = %q", line)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("the first part took %v to come through the proxy; it was buffered%s", d,
			hint("httputil.ReverseProxy only auto-flushes streams of unknown length; set FlushInterval = -1 to flush after every write"))
	}
}

func TestStage26_Cooldown(t *testing.T) {
	a := backend(t, "a")
	dead := closedPortURL(t)
	deadAddr := strings.TrimPrefix(dead, "http://")
	var deadDials atomic.Int64
	tr := &http.Transport{DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr == deadAddr {
			deadDials.Add(1)
		}
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}}
	t.Cleanup(tr.CloseIdleConnections)
	p := startProxy(t, proxy.Config{Backends: []string{a.URL, dead}, Cooldown: time.Minute, Transport: tr})
	client := newClient(t)
	for range 10 {
		resp, _ := get(t, client, p.URL+"/x")
		wantStatus(t, resp, 200, "GET with one backend down")
	}
	if n := deadDials.Load(); n != 1 {
		t.Fatalf("the dead backend was dialled %d times in 10 requests, want once%s", n,
			hint("after a connection failure, skip that backend until Config.Cooldown has passed (use Config.Transport to reach backends)"))
	}
}

func TestStage26_EvenSpreadWithBackendDown(t *testing.T) {
	a, c := backend(t, "a"), backend(t, "c")
	p := startProxy(t, proxy.Config{Backends: []string{a.URL, closedPortURL(t), c.URL}, Cooldown: time.Minute})
	client := newClient(t)
	get(t, client, p.URL+"/x") // discovers the dead backend
	before := len(a.requests()) - len(c.requests())
	for range 20 {
		get(t, client, p.URL+"/x")
	}
	diff := len(a.requests()) - len(c.requests()) - before
	if diff < -1 || diff > 1 {
		t.Fatalf("with one backend down, 20 requests split %d/%d between the two healthy ones; round robin should alternate%s",
			len(a.requests()), len(c.requests()),
			hint("rotate over the healthy backends only, otherwise the one after the dead backend gets its share too"))
	}
}
