// Package loadgen is a tiny HTTP load generator in the spirit of hey and ab.
//
// Stage 25 — read stages/25-connection-pooling/README.md.
package loadgen

import (
	"context"
	"errors"
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

	// Latency percentiles over requests that got a response.
	P50, P90, P99, Max time.Duration
}

// Run sends cfg.Requests requests using cfg.Concurrency workers. If ctx is
// cancelled it stops early and returns the partial report with ctx.Err().
func Run(ctx context.Context, cfg Config) (*Report, error) {
	// TODO(stage 25)
	return nil, errors.New("loadgen.Run: not implemented yet (stage 25)")
}
