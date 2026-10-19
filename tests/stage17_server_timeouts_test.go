package tests

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/jamesz3ng/learn_http_go/gobin"
)

func TestStage17_Delay(t *testing.T) {
	srv := startGobin(t, gobin.Config{})
	c := newClient(t)
	start := time.Now()
	resp, body := get(t, c, srv.URL+"/delay/0.3?x=1")
	wantStatus(t, resp, 200, "GET /delay/0.3")
	if d := time.Since(start); d < 280*time.Millisecond || d > 2*time.Second {
		t.Fatalf("GET /delay/0.3 took %v, want about 300ms", d)
	}
	if m := decodeObject(t, body); m["url"] != srv.URL+"/delay/0.3?x=1" {
		t.Fatalf("/delay should answer like /get, got %s", body)
	}
	for _, bad := range []string{"-1", "11", "abc", "NaN"} {
		resp, _ := get(t, c, srv.URL+"/delay/"+bad)
		wantStatus(t, resp, 400, "GET /delay/"+bad)
	}
}

func TestStage17_DelayStopsWhenClientLeaves(t *testing.T) {
	lc, logger := newLogCapture()
	srv := startGobin(t, gobin.Config{Logger: logger})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/delay/8", nil)
	if resp, err := newClient(t).Do(req); err == nil {
		resp.Body.Close()
		t.Fatal("expected the client to give up")
	}
	gaveUp := time.Now()

	rec := lc.find(isRequest("/delay/8"))
	if rec == nil {
		t.Fatalf("the /delay/8 handler kept running after the client left (no access log within 2s)%s",
			hint("select on time.After/time.NewTimer AND r.Context().Done()"))
	}
	if time.Since(gaveUp) > 2*time.Second {
		t.Fatalf("handler finished %v after the client left", time.Since(gaveUp))
	}
	if rec["status"] != float64(499) {
		t.Fatalf("log status for an abandoned request = %v, want 499%s", rec["status"],
			hint("in the logging middleware: if nothing was written and r.Context().Err() != nil, log 499 (nginx's 'client closed request')"))
	}
}

func TestStage17_NewServerDefaults(t *testing.T) {
	srv := gobin.NewServer(gobin.Config{})
	if srv.Handler == nil {
		t.Fatal("NewServer must set Handler")
	}
	checks := map[string]time.Duration{
		"ReadHeaderTimeout": srv.ReadHeaderTimeout,
		"ReadTimeout":       srv.ReadTimeout,
		"WriteTimeout":      srv.WriteTimeout,
		"IdleTimeout":       srv.IdleTimeout,
	}
	for name, d := range checks {
		if d <= 0 {
			t.Fatalf("NewServer(Config{}).%s = %v; the zero value means NO timeout. Pick a sensible default", name, d)
		}
	}
	if srv.ReadHeaderTimeout > 30*time.Second {
		t.Fatalf("ReadHeaderTimeout %v is too generous; a few seconds is plenty to send headers", srv.ReadHeaderTimeout)
	}
}

func TestStage17_NewServerUsesConfig(t *testing.T) {
	cfg := gobin.Config{
		Addr:              "127.0.0.1:9999",
		ReadHeaderTimeout: 1 * time.Second,
		ReadTimeout:       2 * time.Second,
		WriteTimeout:      3 * time.Second,
		IdleTimeout:       4 * time.Second,
	}
	srv := gobin.NewServer(cfg)
	if srv.Addr != cfg.Addr || srv.ReadHeaderTimeout != cfg.ReadHeaderTimeout || srv.ReadTimeout != cfg.ReadTimeout ||
		srv.WriteTimeout != cfg.WriteTimeout || srv.IdleTimeout != cfg.IdleTimeout {
		t.Fatalf("NewServer ignored Config: got Addr=%q RH=%v R=%v W=%v I=%v", srv.Addr,
			srv.ReadHeaderTimeout, srv.ReadTimeout, srv.WriteTimeout, srv.IdleTimeout)
	}
}

// serveGobin runs gobin.NewServer(cfg) on a random port and returns its URL.
func serveGobin(t *testing.T, cfg gobin.Config) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := gobin.NewServer(cfg)
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return "http://" + ln.Addr().String()
}

func TestStage17_SlowlorisIsDisconnected(t *testing.T) {
	base := serveGobin(t, gobin.Config{ReadHeaderTimeout: 200 * time.Millisecond})
	conn, err := net.Dial("tcp", base[len("http://"):])
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// Start a request and never finish the headers.
	io.WriteString(conn, "GET / HTTP/1.1\r\nHost: x\r\n")
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	start := time.Now()
	io.ReadAll(conn) // returns when the server hangs up
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("a client that never finished its headers was kept for %v; ReadHeaderTimeout (200ms) should cut it off", d)
	}
	resp, _ := get(t, newClient(t), base+"/healthz")
	wantStatus(t, resp, 200, "GET /healthz on NewServer")
}

func TestStage17_WriteTimeoutApplies(t *testing.T) {
	base := serveGobin(t, gobin.Config{WriteTimeout: 300 * time.Millisecond})
	c := newClient(t)
	resp, err := c.Get(base + "/delay/0.6")
	if err == nil {
		io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("a response that took longer than WriteTimeout still arrived (status %d)", resp.StatusCode)
	}
	// WriteTimeout doesn't stop the handler: it makes its writes fail, so the
	// client sees the connection close without a response.
}
