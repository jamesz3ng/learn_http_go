// Package proxy is a load-balancing reverse proxy.
//
// Stage 26, the capstone — read stages/26-reverse-proxy/README.md.
package proxy

import (
	"errors"
	"log/slog"
	"net/http"
	"time"
)

// Config configures the proxy.
type Config struct {
	Backends  []string          // backend base URLs, e.g. "http://10.0.0.1:8080"
	Cooldown  time.Duration     // how long to skip a backend after it fails; default 10s
	Transport http.RoundTripper // used to reach backends; default http.DefaultTransport
	Logger    *slog.Logger      // nil discards logs
}

// New validates cfg and returns the proxy handler.
func New(cfg Config) (http.Handler, error) {
	// TODO(stage 26)
	return nil, errors.New("proxy.New: not implemented yet (stage 26)")
}
