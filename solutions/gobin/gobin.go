// Package gobin is an HTTP toolbox server: a small clone of httpbin.org plus
// a key-value store, built up over the stages of this course.
//
// REFERENCE SOLUTION — read it after you've passed a stage (or when you're
// truly stuck after using every hint). Comments mark which stage each piece
// belongs to.
package gobin

import (
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"time"
)

// Config holds everything the server needs. Fields are introduced by later
// stages; the zero value is a valid configuration.
type Config struct {
	// Stage 15: Logger receives one structured record per request.
	// nil discards logs.
	Logger *slog.Logger

	// Stage 17: listen address and http.Server timeouts used by NewServer.
	// Zero values mean "use a sensible default".
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

// app holds the dependencies shared by all handlers. Handlers are methods on
// it, so there are no package-level globals and every NewHandler call gets a
// fresh, isolated server (which is what makes the tests independent).
type app struct {
	cfg Config
	log *slog.Logger
	kv  *store

	// Stage 23: closed when the http.Server begins shutting down, so that
	// long-lived streaming responses can finish.
	shutdown chan struct{}
}

func newApp(cfg Config) *app {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &app{
		cfg:      cfg,
		log:      logger,
		kv:       newStore(),
		shutdown: make(chan struct{}),
	}
}

// NewHandler returns the root handler: the router wrapped in middleware.
func NewHandler(cfg Config) http.Handler {
	return newApp(cfg).handler()
}

func (a *app) handler() http.Handler {
	mux := http.NewServeMux()
	a.routes(mux)

	// Stage 15: middleware, outermost first. Order matters:
	//   - requestID runs first so everything after it can log the ID;
	//   - logRequests wraps recoverPanics so a recovered panic is logged as
	//     the 500 it became.
	return a.requestID(a.logRequests(a.recoverPanics(mux)))
}

func (a *app) routes(mux *http.ServeMux) {
	// Stage 2
	mux.HandleFunc("GET /{$}", a.hello)
	mux.HandleFunc("GET /healthz", a.healthz)

	// Stage 4
	mux.HandleFunc("GET /get", a.get)
	mux.HandleFunc("GET /headers", a.headers)
	mux.HandleFunc("GET /ip", a.ip)
	mux.HandleFunc("GET /user-agent", a.userAgent)

	// Stage 6
	mux.HandleFunc("/anything", a.anything)
	mux.HandleFunc("/anything/{path...}", a.anything)
	mux.HandleFunc("POST /post", a.anything)
	mux.HandleFunc("PUT /put", a.anything)
	mux.HandleFunc("PATCH /patch", a.anything)
	mux.HandleFunc("DELETE /delete", a.anything)

	// Stage 8
	mux.HandleFunc("GET /status/{code}", a.status)
	mux.HandleFunc("GET /status/teapot", a.teapot)
	mux.HandleFunc("GET /bytes/{n}", a.bytes)
	mux.HandleFunc("GET /base64/{value}", a.base64)
	mux.HandleFunc("GET /uuid", a.uuid)

	// Stage 9
	mux.HandleFunc("GET /kv", a.kvList)
	mux.HandleFunc("PUT /kv/{key}", a.kvPut)
	mux.HandleFunc("GET /kv/{key}", a.kvGet)
	mux.HandleFunc("DELETE /kv/{key}", a.kvDelete)

	// Stage 11
	mux.HandleFunc("GET /redirect/{n}", a.redirect)
	mux.HandleFunc("GET /absolute-redirect/{n}", a.absoluteRedirect)
	mux.HandleFunc("/redirect-to", a.redirectTo)

	// Stage 13
	mux.HandleFunc("GET /cookies", a.cookies)
	mux.HandleFunc("GET /cookies/set", a.setCookies)
	mux.HandleFunc("GET /cookies/delete", a.deleteCookies)
	mux.HandleFunc("GET /basic-auth/{user}/{passwd}", a.basicAuth)
	mux.HandleFunc("GET /bearer", a.bearer)

	// Stage 15
	mux.HandleFunc("GET /panic", a.boom)

	// Stage 17
	mux.HandleFunc("GET /delay/{seconds}", a.delay)

	// Stage 19
	mux.HandleFunc("GET /stream/{n}", a.stream)
	mux.HandleFunc("GET /watch", a.watch)

	// Stage 21
	if a.cfg.Static != nil {
		mux.Handle("GET /static/", http.StripPrefix("/static", http.FileServerFS(noListingFS{a.cfg.Static})))
	}
	mux.HandleFunc("POST /upload", a.upload)
}

// Stage 2 ---------------------------------------------------------------------

func (a *app) hello(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	io.WriteString(w, "Hello, HTTP!\n")
}

func (a *app) healthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	io.WriteString(w, "ok\n")
}
