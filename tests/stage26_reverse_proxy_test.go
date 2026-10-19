package tests

import (
	"bufio"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
		counts[strings.Fields(body)[0]]++
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
			hint("ReverseProxy.FlushInterval = -1 flushes after every write"))
	}
}
