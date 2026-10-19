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

type runningServer struct {
	url    string
	cancel context.CancelFunc
	done   chan error
}

// startRun runs gobin.Run on a random port.
func startRun(t *testing.T, cfg gobin.Config, scheme string) *runningServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	rs := &runningServer{url: scheme + "://" + ln.Addr().String(), cancel: cancel, done: make(chan error, 1)}
	go func() { rs.done <- gobin.Run(ctx, ln, cfg) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-rs.done:
		case <-time.After(15 * time.Second):
		}
	})
	select {
	case err := <-rs.done:
		rs.done <- err
		t.Fatalf("gobin.Run returned immediately (%v); it should serve until its context is cancelled", err)
	case <-time.After(50 * time.Millisecond):
	}
	return rs
}

// wait returns Run's result, failing if it takes longer than d.
func (rs *runningServer) wait(t *testing.T, d time.Duration, what string) error {
	t.Helper()
	select {
	case err := <-rs.done:
		rs.done <- err // keep it for the cleanup
		return err
	case <-time.After(d):
		t.Fatalf("%s: Run did not return within %v", what, d)
		return nil
	}
}

func TestStage23_Serves(t *testing.T) {
	rs := startRun(t, gobin.Config{}, "http")
	resp, body := get(t, newClient(t), rs.url+"/healthz")
	wantStatus(t, resp, 200, "GET /healthz on Run")
	if body != "ok\n" {
		t.Fatalf("body = %q", body)
	}
	rs.cancel()
	if err := rs.wait(t, 3*time.Second, "after cancelling an idle server"); err != nil {
		t.Fatalf("Run returned %v after a clean shutdown, want nil%s", err,
			hint("Serve returns http.ErrServerClosed once Shutdown starts; that is success, not an error"))
	}
}

func TestStage23_FinishesInFlightRequests(t *testing.T) {
	rs := startRun(t, gobin.Config{ShutdownTimeout: 5 * time.Second}, "http")
	c := newClient(t)
	type result struct {
		code int
		err  error
	}
	res := make(chan result, 1)
	go func() {
		resp, err := c.Get(rs.url + "/delay/0.6")
		if err != nil {
			res <- result{0, err}
			return
		}
		io.ReadAll(resp.Body)
		resp.Body.Close()
		res <- result{resp.StatusCode, nil}
	}()
	time.Sleep(200 * time.Millisecond)
	rs.cancel()

	r := <-res
	if r.err != nil || r.code != 200 {
		t.Fatalf("an in-flight request was cut off by shutdown (status %d, err %v)%s", r.code, r.err,
			hint("use srv.Shutdown(ctx), which waits for active requests, not srv.Close()"))
	}
	if err := rs.wait(t, 3*time.Second, "after the in-flight request finished"); err != nil {
		t.Fatalf("Run returned %v, want nil", err)
	}
	if _, err := net.DialTimeout("tcp", rs.url[len("http://"):], time.Second); err == nil {
		t.Fatalf("the server still accepts connections after Run returned")
	}
}

func TestStage23_StreamsEndOnShutdown(t *testing.T) {
	rs := startRun(t, gobin.Config{ShutdownTimeout: 10 * time.Second}, "http")
	events, _ := openWatch(t, rs.url, "")
	time.Sleep(100 * time.Millisecond)
	start := time.Now()
	rs.cancel()
	if err := rs.wait(t, 5*time.Second, "with an open /watch stream"); err != nil {
		t.Fatalf("Run returned %v, want nil", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("shutdown took %v because the /watch stream kept it waiting%s", d,
			hint("Shutdown does NOT cancel request contexts. Use srv.RegisterOnShutdown to tell streaming handlers to return"))
	}
	deadline := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-events:
			if !ok {
				return // the stream ended, as it should
			}
		case <-deadline:
			t.Fatal("the watcher's stream was not closed")
		}
	}
}

func TestStage23_ShutdownTimeout(t *testing.T) {
	rs := startRun(t, gobin.Config{ShutdownTimeout: 300 * time.Millisecond}, "http")
	go func() {
		c := &http.Client{}
		if resp, err := c.Get(rs.url + "/delay/5"); err == nil {
			resp.Body.Close()
		}
	}()
	time.Sleep(200 * time.Millisecond)
	start := time.Now()
	rs.cancel()
	err := rs.wait(t, 3*time.Second, "with a request that outlives ShutdownTimeout")
	if err == nil {
		t.Fatalf("Run returned nil although a request was still running when ShutdownTimeout expired; report that as an error")
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("Run took %v; it should give up after ShutdownTimeout (300ms)", d)
	}
}

func TestStage23_ListenerError(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	ln.Close() // Serve will fail immediately
	done := make(chan error, 1)
	go func() { done <- gobin.Run(context.Background(), ln, gobin.Config{}) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Run on a closed listener returned nil; it should return Serve's error")
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("Run on a closed listener didn't return%s", hint("select on both ctx.Done() and the error from Serve"))
	}
}
