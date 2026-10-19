// Package loadgen is a tiny HTTP load generator in the spirit of hey and ab.
//
// REFERENCE SOLUTION — stage 25.
package loadgen

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"slices"
	"sync"
	"time"
)

// Config describes a load test.
type Config struct {
	URL         string
	Method      string        // default GET
	Requests    int           // total requests to send (N)
	Concurrency int           // concurrent workers (C)
	Timeout     time.Duration // per-request timeout; default 10s
}

// Report summarises a load test.
type Report struct {
	Requests    int         // requests attempted
	Errors      int         // requests that got no HTTP response
	StatusCodes map[int]int // responses by status code
	NewConns    int         // requests that had to dial a new connection
	ReusedConns int         // requests that reused a pooled connection
	Elapsed     time.Duration

	// Latency percentiles over requests that got a response, measured from
	// sending the request to reading the last byte of the body.
	P50, P90, P99, Max time.Duration
}

// Run sends cfg.Requests requests using cfg.Concurrency workers. If ctx is
// cancelled it stops early and returns the partial report with ctx.Err().
func Run(ctx context.Context, cfg Config) (*Report, error) {
	u, err := url.Parse(cfg.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("loadgen: invalid URL %q", cfg.URL)
	}
	if cfg.Requests < 1 {
		return nil, errors.New("loadgen: Requests must be at least 1")
	}
	method := cfg.Method
	if method == "" {
		method = http.MethodGet
	}
	workers := max(1, min(cfg.Concurrency, cfg.Requests))
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	// The key setting: MaxIdleConnsPerHost defaults to 2. With more workers
	// than that, every response beyond the second finds the idle pool full,
	// its connection is closed, and the next request dials a new one.
	tr := &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		DialContext:         (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		MaxIdleConns:        workers,
		MaxIdleConnsPerHost: workers,
		IdleConnTimeout:     90 * time.Second,
		ForceAttemptHTTP2:   true,
	}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: timeout}

	var (
		mu        sync.Mutex
		report    = &Report{StatusCodes: map[int]int{}}
		latencies []time.Duration
	)
	record := func(status int, reused bool, lat time.Duration, failed bool) {
		mu.Lock()
		defer mu.Unlock()
		report.Requests++
		if failed {
			report.Errors++
			return
		}
		report.StatusCodes[status]++
		if reused {
			report.ReusedConns++
		} else {
			report.NewConns++
		}
		latencies = append(latencies, lat)
	}

	jobs := make(chan struct{})
	var wg sync.WaitGroup
	start := time.Now()
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range jobs {
				status, reused, lat, err := one(ctx, client, method, cfg.URL)
				if err != nil && ctx.Err() != nil {
					return // cancelled: not the server's fault, don't count it
				}
				record(status, reused, lat, err != nil)
			}
		}()
	}

feed:
	for range cfg.Requests {
		select {
		case jobs <- struct{}{}:
		case <-ctx.Done():
			break feed
		}
	}
	close(jobs)
	wg.Wait()
	report.Elapsed = time.Since(start)

	slices.Sort(latencies)
	report.P50 = percentile(latencies, 0.50)
	report.P90 = percentile(latencies, 0.90)
	report.P99 = percentile(latencies, 0.99)
	if len(latencies) > 0 {
		report.Max = latencies[len(latencies)-1]
	}
	return report, ctx.Err()
}

// one sends a single request and reports whether its connection was reused.
func one(ctx context.Context, client *http.Client, method, url string) (status int, reused bool, lat time.Duration, err error) {
	// httptrace hooks into the client's internals for this request only.
	// GotConn fires once a connection (new or pooled) has been obtained.
	trace := &httptrace.ClientTrace{
		GotConn: func(info httptrace.GotConnInfo) { reused = info.Reused },
	}
	req, err := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, trace), method, url, nil)
	if err != nil {
		return 0, false, 0, err
	}
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return 0, false, 0, err
	}
	// Read the body to EOF, or the connection can't be reused.
	_, err = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if err != nil {
		return 0, false, 0, err
	}
	return resp.StatusCode, reused, time.Since(start), nil
}

// percentile returns the p-th percentile of sorted durations (nearest rank).
func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	rank := int(p*float64(len(sorted)) + 0.999999) // ceil
	rank = max(1, min(rank, len(sorted)))
	return sorted[rank-1]
}
