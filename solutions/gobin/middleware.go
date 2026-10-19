package gobin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"
)

// Stage 15 --------------------------------------------------------------------
//
// A middleware is a function from http.Handler to http.Handler: it returns a
// new handler that does something before and/or after calling next.

// statusRecorder wraps a ResponseWriter to remember the status code and the
// number of body bytes written.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	bytes       int64
	wroteHeader bool
}

func (s *statusRecorder) WriteHeader(code int) {
	// 1xx informational responses may precede the real one; only the first
	// final (>= 200) status counts. Later calls are ignored by net/http too.
	if !s.wroteHeader && code >= 200 {
		s.status = code
		s.wroteHeader = true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if !s.wroteHeader {
		// A Write without WriteHeader implies 200, exactly as in net/http.
		s.status = http.StatusOK
		s.wroteHeader = true
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += int64(n)
	return n, err
}

// Unwrap lets http.ResponseController reach the underlying writer, so that
// Flush, SetWriteDeadline, etc. still work through this wrapper (stage 19).
// Without it, streaming responses would silently stop streaming.
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// --- request IDs

// An unexported key type means no other package can collide with our key.
type ctxKey int

const requestIDKey ctxKey = 0

// RequestIDFrom returns the request ID stored in ctx by the middleware.
func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

func validRequestID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, c := range id {
		switch {
		case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z', '0' <= c && c <= '9', c == '-', c == '_', c == '.':
		default:
			return false
		}
	}
	return true
}

func newRequestID() string {
	var b [16]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func (a *app) requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if !validRequestID(id) {
			id = newRequestID()
		}
		// Set before calling next: once the handler writes, it's too late.
		w.Header().Set("X-Request-Id", id)
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// --- access logging

func (a *app) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		// Log in a deferred call so that a request aborted with
		// panic(http.ErrAbortHandler) is still logged on its way out.
		defer func() { a.logRequest(r, rec, start) }()
		next.ServeHTTP(rec, r)
	})
}

func (a *app) logRequest(r *http.Request, rec *statusRecorder, start time.Time) {
	status := rec.status
	if !rec.wroteHeader {
		status = http.StatusOK
		// Stage 17: the handler gave up because the client went away.
		// 499 is nginx's convention for "client closed request".
		if r.Context().Err() != nil {
			status = 499
		}
	}
	level := slog.LevelInfo
	if status >= 500 {
		level = slog.LevelError
	}
	a.log.LogAttrs(r.Context(), level, "request",
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
		slog.Int("status", status),
		slog.Int64("bytes", rec.bytes),
		slog.Duration("duration", time.Since(start)),
		slog.String("request_id", RequestIDFrom(r.Context())),
	)
}

// --- panic recovery

func (a *app) recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w}
		defer func() {
			v := recover()
			if v == nil {
				return
			}
			// http.ErrAbortHandler is the sanctioned way to abort a response;
			// let net/http handle it (it closes the connection quietly).
			if v == http.ErrAbortHandler {
				panic(v)
			}
			a.log.LogAttrs(r.Context(), slog.LevelError, "panic recovered",
				slog.String("panic", fmt.Sprint(v)),
				slog.String("request_id", RequestIDFrom(r.Context())),
				slog.String("stack", string(debug.Stack())),
			)
			if rec.wroteHeader {
				// Too late for a 500: the status line is already on the wire.
				// Abort so the client sees a broken response, not a "successful"
				// truncated one.
				panic(http.ErrAbortHandler)
			}
			writeError(rec, http.StatusInternalServerError, "internal server error")
		}()
		next.ServeHTTP(rec, r)
	})
}

// boom is a deliberately broken endpoint for exercising recoverPanics.
func (a *app) boom(w http.ResponseWriter, r *http.Request) {
	panic("boom")
}
