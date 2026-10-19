// Package gobin is your HTTP toolbox server: a small clone of httpbin.org
// plus a key-value store. You will grow it over many stages.
//
// Feel free to split it into as many files as you like (inspect.go, kv.go,
// middleware.go, ...). The tests only rely on the exported API in this file:
// Config, NewHandler, NewServer and Run.
package gobin

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// Config holds the server's settings and dependencies.
//
// Most fields belong to later stages; ignore them until you get there.
// The zero value must keep working, so treat a zero field as "use a default".
type Config struct {
	// Stage 15: Logger receives one structured record per request.
	// nil means "discard logs".
	Logger *slog.Logger

	// Stage 17: the listen address and http.Server timeouts used by
	// NewServer. Zero means "use a sensible default".
	Addr              string
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration

	// Stage 21: Static is served under /static/. nil disables it.
	Static fs.FS

	// Stage 23: how long Run waits for in-flight requests when shutting down.
	ShutdownTimeout time.Duration

	// Stage 24: when both are set, Run serves HTTPS (and HTTP/2).
	TLSCertFile string
	TLSKeyFile  string
}

// NewHandler returns gobin's root http.Handler.
//
// Stage 2 starts here. Every call must return an independent server (its own
// key-value store, etc.), so avoid package-level state.
func NewHandler(cfg Config) http.Handler {
	mux := http.NewServeMux()
	// TODO(stage 2): register your routes, e.g.
	//   mux.HandleFunc("GET /healthz", healthz)
	return mux
}

// NewServer returns an *http.Server for gobin, configured with timeouts from
// cfg (or defaults).
//
// Stage 17.
func NewServer(cfg Config) *http.Server {
	// TODO(stage 17): set timeouts.
	return &http.Server{Addr: cfg.Addr, Handler: NewHandler(cfg)}
}

// Run serves gobin on ln until ctx is cancelled, then shuts down gracefully.
// It returns nil after a clean shutdown.
//
// Stage 23 (and stage 24 for TLS).
func Run(ctx context.Context, ln net.Listener, cfg Config) error {
	// TODO(stage 23)
	return errors.New("gobin.Run: not implemented yet (stage 23)")
}
