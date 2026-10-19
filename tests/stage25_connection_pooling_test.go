package tests

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jamesz3ng/learn_http_go/loadgen"
)

// loadServer counts connections and tracks how many requests are in flight.
type loadServer struct {
	*httptest.Server
	conns       *atomic.Int64
	inFlight    atomic.Int64
	maxInFlight atomic.Int64
	requests    atomic.Int64
}

func newLoadServer(t *testing.T, delay time.Duration, bodySize int) *loadServer {
	t.Helper()
	ls := &loadServer{}
	body := strings.Repeat("x", bodySize)
	ls.Server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := ls.requests.Add(1)
		cur := ls.inFlight.Add(1)
		defer ls.inFlight.Add(-1)
		for {
			m := ls.maxInFlight.Load()
			if cur <= m || ls.maxInFlight.CompareAndSwap(m, cur) {
				break
			}
		}
		time.Sleep(delay)
		if n%10 == 0 {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		w.Write([]byte(body))
	}))
	ls.conns = countConns(ls.Server)
	ls.Start()
	t.Cleanup(ls.Close)
	return ls
}

func runLoad(t *testing.T, cfg loadgen.Config) *loadgen.Report {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r, err := loadgen.Run(ctx, cfg)
	if err != nil {
		t.Fatalf("loadgen.Run: %v", err)
	}
	if r == nil {
		t.Fatal("loadgen.Run returned a nil report")
	}
	return r
}

func TestStage25_CountsEverything(t *testing.T) {
	ls := newLoadServer(t, 2*time.Millisecond, 100)
	r := runLoad(t, loadgen.Config{URL: ls.URL, Requests: 100, Concurrency: 4})
	if r.Requests != 100 || ls.requests.Load() != 100 {
		t.Fatalf("Report.Requests = %d and the server saw %d, want 100 each", r.Requests, ls.requests.Load())
	}
	if r.Errors != 0 {
		t.Fatalf("Report.Errors = %d; a 503 is a response, not an error", r.Errors)
	}
	if r.StatusCodes[200] != 90 || r.StatusCodes[503] != 10 {
		t.Fatalf("StatusCodes = %v, want 90 x 200 and 10 x 503", r.StatusCodes)
	}
	if r.Elapsed <= 0 {
		t.Fatalf("Elapsed = %v", r.Elapsed)
	}
}

func TestStage25_RespectsConcurrency(t *testing.T) {
	ls := newLoadServer(t, 20*time.Millisecond, 100)
	runLoad(t, loadgen.Config{URL: ls.URL, Requests: 60, Concurrency: 6})
	m := ls.maxInFlight.Load()
	if m > 6 {
		t.Fatalf("up to %d requests were in flight at once, want at most Concurrency (6)", m)
	}
	if m < 3 {
		t.Fatalf("at most %d request(s) were ever in flight at once; the workers aren't running concurrently", m)
	}
}

func TestStage25_ReusesConnections(t *testing.T) {
	ls := newLoadServer(t, time.Millisecond, 32<<10)
	const c = 8
	r := runLoad(t, loadgen.Config{URL: ls.URL, Requests: 400, Concurrency: c})
	serverConns := ls.conns.Load()
	if serverConns > 2*c {
		t.Fatalf("400 requests with %d workers opened %d TCP connections; with keep-alive it should be about %d%s",
			c, serverConns, c,
			hint("http.Transport.MaxIdleConnsPerHost defaults to 2: idle connections beyond that are closed. Also read every body to EOF before closing it"))
	}
	if r.NewConns+r.ReusedConns != 400 {
		t.Fatalf("NewConns (%d) + ReusedConns (%d) should equal the 400 responses%s", r.NewConns, r.ReusedConns,
			hint("httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) {...}} reports info.Reused for each request"))
	}
	if int64(r.NewConns) > serverConns || r.NewConns < 1 {
		t.Fatalf("Report.NewConns = %d, but the server accepted %d connections", r.NewConns, serverConns)
	}
}

func TestStage25_Latencies(t *testing.T) {
	ls := newLoadServer(t, 10*time.Millisecond, 10)
	r := runLoad(t, loadgen.Config{URL: ls.URL, Requests: 50, Concurrency: 5})
	if !(r.P50 >= 10*time.Millisecond && r.P50 <= r.P90 && r.P90 <= r.P99 && r.P99 <= r.Max) {
		t.Fatalf("latencies p50=%v p90=%v p99=%v max=%v; want 10ms <= p50 <= p90 <= p99 <= max", r.P50, r.P90, r.P99, r.Max)
	}
	if r.Max > 5*time.Second {
		t.Fatalf("max latency %v is implausible", r.Max)
	}
}

func TestStage25_Errors(t *testing.T) {
	r := runLoad(t, loadgen.Config{URL: closedPortURL(t), Requests: 10, Concurrency: 2})
	if r.Errors != 10 || r.Requests != 10 {
		t.Fatalf("against a closed port: Requests=%d Errors=%d, want 10 and 10", r.Requests, r.Errors)
	}
	if _, err := loadgen.Run(context.Background(), loadgen.Config{URL: "not a url", Requests: 1, Concurrency: 1}); err == nil {
		t.Fatal("loadgen.Run with an invalid URL should return an error")
	}
}

func TestStage25_Cancellation(t *testing.T) {
	ls := newLoadServer(t, 50*time.Millisecond, 10)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	r, err := loadgen.Run(ctx, loadgen.Config{URL: ls.URL, Requests: 10000, Concurrency: 4})
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("Run kept going for %v after its context expired", d)
	}
	if err == nil {
		t.Fatal("a cancelled Run should return ctx.Err() along with the partial report")
	}
	if r == nil || r.Requests == 0 || r.Requests >= 10000 {
		t.Fatalf("want a partial report, got %+v", r)
	}
}
