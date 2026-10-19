// Package proxy is a load-balancing reverse proxy.
//
// REFERENCE SOLUTION — stage 26 (the capstone).
//
// This version uses httputil.ReverseProxy, which already handles the fiddly
// parts of proxying correctly: hop-by-hop headers, X-Forwarded-*, trailers,
// protocol upgrades, and flushing streamed responses. Backend selection and
// failover live in a custom RoundTripper.
package proxy

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

// Config configures the proxy.
type Config struct {
	Backends  []string          // backend base URLs, e.g. "http://10.0.0.1:8080"
	Cooldown  time.Duration     // how long to skip a backend after it fails; default 10s
	Transport http.RoundTripper // used to reach backends; default http.DefaultTransport
	Logger    *slog.Logger      // nil discards logs
}

type backend struct {
	url       *url.URL
	downUntil atomic.Int64 // UnixNano; 0 = healthy
}

func (b *backend) healthy(now time.Time) bool { return now.UnixNano() >= b.downUntil.Load() }

type balancer struct {
	backends  []*backend
	next      atomic.Uint64
	cooldown  time.Duration
	transport http.RoundTripper
	log       *slog.Logger
}

// New validates cfg and returns the proxy handler.
func New(cfg Config) (http.Handler, error) {
	if len(cfg.Backends) == 0 {
		return nil, errors.New("proxy: at least one backend is required")
	}
	lb := &balancer{
		cooldown:  cfg.Cooldown,
		transport: cfg.Transport,
		log:       cfg.Logger,
	}
	if lb.cooldown <= 0 {
		lb.cooldown = 10 * time.Second
	}
	if lb.transport == nil {
		lb.transport = http.DefaultTransport
	}
	if lb.log == nil {
		lb.log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	for _, raw := range cfg.Backends {
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, fmt.Errorf("proxy: invalid backend URL %q", raw)
		}
		lb.backends = append(lb.backends, &backend{url: u})
	}

	return &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			// X-Forwarded-For (appending the client IP), -Host and -Proto.
			pr.SetXForwarded()
		},
		Transport: lb,
		// Flush every write straight away so streams (SSE, NDJSON) aren't
		// held in a buffer.
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			lb.log.Warn("proxy error", "method", r.Method, "path", r.URL.Path, "err", err)
			http.Error(w, "bad gateway", http.StatusBadGateway)
		},
	}, nil
}

// RoundTrip sends the (already rewritten) request to a backend, failing over
// to the next one when a backend can't be reached at all.
func (lb *balancer) RoundTrip(req *http.Request) (*http.Response, error) {
	now := time.Now()
	var healthy, down []*backend
	for _, b := range lb.backends {
		if b.healthy(now) {
			healthy = append(healthy, b)
		} else {
			down = append(down, b)
		}
	}

	// Rotate over the healthy backends only: rotating over all of them and
	// skipping the dead one would hand its turns to its neighbour. Backends
	// cooling down come last, as a last resort (they may have recovered).
	var order []*backend
	if len(healthy) > 0 {
		start := int(lb.next.Add(1)-1) % len(healthy)
		order = append(order, healthy[start:]...)
		order = append(order, healthy[:start]...)
	}
	order = append(order, down...)

	// Only a request without a body can be safely re-sent: the first
	// attempt may have consumed it.
	replayable := req.Body == nil || req.Body == http.NoBody

	var lastErr error
	for _, b := range order {
		out := req.Clone(req.Context())
		target(out, b.url)
		resp, err := lb.transport.RoundTrip(out)
		if err == nil {
			b.downUntil.Store(0)
			return resp, nil
		}
		lastErr = err
		if !isConnectError(err) {
			return nil, err // the backend got the request; don't resend
		}
		lb.log.Warn("backend unreachable", "backend", b.url.String(), "err", err)
		b.downUntil.Store(time.Now().Add(lb.cooldown).UnixNano())
		if !replayable {
			return nil, err
		}
	}
	return nil, fmt.Errorf("proxy: no backend available: %w", lastErr)
}

// target points out at backend base u, joining paths ("/api" + "/x").
func target(out *http.Request, u *url.URL) {
	inPath, inRawPath := out.URL.Path, out.URL.EscapedPath()
	out.URL.Scheme = u.Scheme
	out.URL.Host = u.Host
	out.URL.Path = strings.TrimSuffix(u.Path, "/") + inPath
	out.URL.RawPath = strings.TrimSuffix(u.EscapedPath(), "/") + inRawPath
	if u.RawQuery != "" {
		if out.URL.RawQuery == "" {
			out.URL.RawQuery = u.RawQuery
		} else {
			out.URL.RawQuery = u.RawQuery + "&" + out.URL.RawQuery
		}
	}
	// Send the backend's host name, not the one the client used to reach
	// us (that one travels in X-Forwarded-Host).
	out.Host = ""
}

func isConnectError(err error) bool {
	var opErr *net.OpError
	return errors.As(err, &opErr) && opErr.Op == "dial"
}
