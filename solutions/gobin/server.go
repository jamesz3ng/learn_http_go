package gobin

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Stage 17 --------------------------------------------------------------------

// delay waits {seconds} (0-10, fractional allowed) and then answers like /get.
// It stops waiting as soon as the client goes away.
func (a *app) delay(w http.ResponseWriter, r *http.Request) {
	secs, err := strconv.ParseFloat(r.PathValue("seconds"), 64)
	if err != nil || math.IsNaN(secs) || secs < 0 || secs > 10 {
		writeError(w, http.StatusBadRequest, "seconds must be a number between 0 and 10")
		return
	}
	timer := time.NewTimer(time.Duration(secs * float64(time.Second)))
	defer timer.Stop()

	select {
	case <-timer.C:
	case <-r.Context().Done():
		// The client disconnected (or the server is being forcibly closed).
		// Nobody is listening, so stop working.
		return
	}
	writeJSON(w, http.StatusOK, describe(r))
}

func orDefault(d, def time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return def
}

// NewServer returns an *http.Server with production-grade timeouts.
//
// The zero http.Server has NO timeouts: one client that opens connections
// and sends nothing can exhaust your file descriptors.
func NewServer(cfg Config) *http.Server {
	a := newApp(cfg)
	addr := cfg.Addr
	if addr == "" {
		addr = ":8080"
	}
	srv := &http.Server{
		Addr:    addr,
		Handler: a.handler(),

		// Time to read the request line and headers. The main defence
		// against slowloris-style attacks.
		ReadHeaderTimeout: orDefault(cfg.ReadHeaderTimeout, 5*time.Second),
		// Time to read the whole request, body included.
		ReadTimeout: orDefault(cfg.ReadTimeout, 30*time.Second),
		// Time from the end of the request headers to the end of the
		// response write. Streaming handlers lift it per response with
		// http.ResponseController (stage 19).
		WriteTimeout: orDefault(cfg.WriteTimeout, 30*time.Second),
		// How long a keep-alive connection may sit idle between requests.
		IdleTimeout: orDefault(cfg.IdleTimeout, 2*time.Minute),

		MaxHeaderBytes: 64 << 10,
		// Send net/http's own error messages (TLS handshake failures, etc.)
		// to our structured logger instead of the standard log package.
		ErrorLog: slog.NewLogLogger(a.log.Handler(), slog.LevelWarn),

		// Stage 24: a sensible TLS floor. HTTP/2 is still enabled automatically
		// by ServeTLS because TLSNextProto is left nil.
		TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12},
	}

	// Stage 23: Shutdown does not cancel the contexts of in-flight requests,
	// so long-lived streams would hold Shutdown hostage until it times out.
	// Tell them to finish instead.
	var once sync.Once
	srv.RegisterOnShutdown(func() { once.Do(func() { close(a.shutdown) }) })
	return srv
}

// Stage 23 --------------------------------------------------------------------

// Run serves on ln until ctx is cancelled, then shuts down gracefully: it
// stops accepting connections, lets in-flight requests finish (up to
// cfg.ShutdownTimeout), and returns nil if everything finished in time.
func Run(ctx context.Context, ln net.Listener, cfg Config) error {
	srv := NewServer(cfg)

	serveErr := make(chan error, 1)
	go func() {
		// Stage 24: TLS when a certificate is configured.
		if cfg.TLSCertFile != "" || cfg.TLSKeyFile != "" {
			serveErr <- srv.ServeTLS(ln, cfg.TLSCertFile, cfg.TLSKeyFile)
			return
		}
		serveErr <- srv.Serve(ln)
	}()

	select {
	case err := <-serveErr:
		// Serve failed on its own (we never called Shutdown, so this is
		// never http.ErrServerClosed).
		return err
	case <-ctx.Done():
	}

	// Use a fresh context: ctx is already cancelled.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), orDefault(cfg.ShutdownTimeout, 10*time.Second))
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		// Graceful shutdown ran out of time: cut the remaining connections.
		srv.Close()
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	// Serve returns http.ErrServerClosed as soon as Shutdown starts. That is
	// the expected, successful outcome, not an error for our caller.
	if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
