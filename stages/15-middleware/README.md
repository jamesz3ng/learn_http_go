# Stage 15: Middleware

> **Part V · Composition** · **You build:** `gobin` · **Time:** ~90 min

## Why this stage matters

Logging, panic recovery, request IDs, auth, compression, CORS, rate limiting:
these apply to *every* request, and writing them into every handler would
be painful. Because everything is an `http.Handler`, you can **wrap**
handlers in other handlers. It's a two-line pattern, but doing it well means
understanding how a `ResponseWriter` wrapper can quietly break streaming,
and what Go already does about panics.

## Before you read: predict

1. A handler panics. Without any recovery middleware, what does the client receive?
2. Your logging middleware wants the response status. `ResponseWriter` has no
   `Status()` method. How can it find out?
3. You wrap the `ResponseWriter` in your own struct. What happens to code that
   does `w.(http.Flusher)`?

## Reading

### The pattern

A middleware takes a handler and returns a new one that does something
before and/or after calling it:

```go
func logRequests(logger *slog.Logger, next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        start := time.Now()
        next.ServeHTTP(w, r)                                           // the wrapped handler
        logger.Info("request", "path", r.URL.Path, "duration", time.Since(start))
    })
}

handler := requestID(logRequests(logger, recoverPanics(mux)))
```

Read the chain inside-out: the **outermost** middleware runs first and
finishes last. Order matters, as you'll see.

### Capturing the status: wrap the `ResponseWriter`

`ResponseWriter` is an interface, so you can pass the handler your own
implementation. Embed the real one to get its methods, and override what you
need:

```go
type statusRecorder struct {
    http.ResponseWriter       // embedded: Header() etc. pass straight through
    status      int
    bytes       int64
    wroteHeader bool
}

func (s *statusRecorder) WriteHeader(code int) {
    if !s.wroteHeader {
        s.status, s.wroteHeader = code, true
    }
    s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
    if !s.wroteHeader {
        s.status, s.wroteHeader = http.StatusOK, true // implicit 200, as in net/http
    }
    n, err := s.ResponseWriter.Write(b)
    s.bytes += int64(n)
    return n, err
}
```

### The wrapper problem, and `Unwrap`

The real `ResponseWriter` also implements optional interfaces:
`http.Flusher` (send buffered data now, which streaming needs, stage 19),
`http.Hijacker` (take over the connection, for WebSockets), and
`io.ReaderFrom` (lets `io.Copy` use `sendfile`). Your wrapper embeds the
`http.ResponseWriter` *interface*, so only the interface's three methods are
promoted. **A type assertion `w.(http.Flusher)` on your wrapper fails**, and
streaming handlers silently stop streaming.

Since Go 1.20 the fix is `http.ResponseController`, plus one method on every
wrapper:

```go
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// in handlers, instead of w.(http.Flusher).Flush():
rc := http.NewResponseController(w)
err := rc.Flush() // walks the Unwrap chain until it finds a writer that can flush
```

`ResponseController` also offers `Hijack`, `SetReadDeadline`,
`SetWriteDeadline` (stage 19) and `EnableFullDuplex`. **Always add `Unwrap` to
ResponseWriter wrappers.** Stage 19's tests will catch you if you forget.

### Structured logging with `log/slog`

```go
logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
logger.LogAttrs(r.Context(), slog.LevelInfo, "request",
    slog.String("method", r.Method),
    slog.Int("status", status),
    slog.Duration("duration", d), // JSON handler writes nanoseconds as a number
)
// {"time":"...","level":"INFO","msg":"request","method":"GET","status":200,"duration":153200}
```

Structured (key/value) logs can be searched and aggregated. `LogAttrs` takes
typed `slog.Attr` values directly, skipping the conversion of alternating
`key, value` arguments, so it allocates less on hot paths. Take the logger as a
dependency (`Config.Logger`), never a global, so tests can capture it, as the
stage tests do.

### Request IDs and `context`

A request ID ties together every log line for one request, across services.
The middleware picks an ID, puts it in the response header, and stores it in
the request's **context** for code further down:

```go
type ctxKey int                    // unexported type: no other package can collide
const requestIDKey ctxKey = 0

ctx := context.WithValue(r.Context(), requestIDKey, id)
next.ServeHTTP(w, r.WithContext(ctx)) // r.WithContext returns a shallow copy

id, _ := r.Context().Value(requestIDKey).(string) // later, anywhere downstream
```

If a client (or upstream proxy) sent an `X-Request-Id`, reuse it, but
**validate** it first: it's untrusted input that will end up in your logs.

### Panics: what Go already does

If a handler panics, `net/http` **recovers it for you**, per connection: it
logs the panic and stack trace to `Server.ErrorLog` and **closes the
connection**. The client sees the connection drop ("empty reply from
server"), **not** a 500. One bad request never kills the process.

A recovery middleware improves on that: log it your way, and answer with a
proper `500` JSON error, **if the response hasn't started yet**. Once the
status line is on the wire you can't change it; the honest move then is to
abort the response with `panic(http.ErrAbortHandler)`, a special value that
`net/http` uses to close the connection without logging a stack trace.

```go
defer func() {
    if v := recover(); v != nil {
        if v == http.ErrAbortHandler {
            panic(v) // a deliberate abort: let net/http deal with it
        }
        // log v and debug.Stack(); then 500 if nothing was written yet
    }
}()
```

### Putting the chain in order

```
requestID ─▶ logRequests ─▶ recoverPanics ─▶ mux ─▶ handler
```

- `requestID` first, so everything after it (logs included) can use the ID,
  and the header is set even when something later panics.
- `recoverPanics` **inside** `logRequests`, so the logger sees the `500` it
  produced. Swap them and a panic skips the log line entirely.

> **📖 Source dive:** in `$(go env GOROOT)/src/net/http/server.go`, find the
> `recover()` in `func (c *conn) serve`. What does it do with
> `ErrAbortHandler`? Then read `responsecontroller.go`: it's short, and the
> `Unwrap` loop is right there.

## Your task

Wrap gobin's mux with middleware, configured by `Config.Logger` (nil → discard):

| Middleware | Behaviour |
|---|---|
| **access log** | exactly one record per request, after it finishes: message `request`, attributes `method`, `path`, `status` (int), `bytes` (body bytes written), `duration` (`slog.Duration`), `request_id`. Level **INFO**, or **ERROR** for status ≥ 500 |
| **request ID** | reuse an incoming `X-Request-Id` if it's 1–128 characters of `[A-Za-z0-9._-]`; otherwise generate 16 random bytes as 32 lower-case hex characters. Set it on the **response** header `X-Request-Id`, and log it |
| **recovery** | a panic becomes `500` with `{"error": "internal server error"}` (if nothing was written yet), an **ERROR** record with message `panic recovered` and a `panic` attribute (the value as a string), and the request still gets its access-log record with status 500 |
| `GET /panic` | a deliberately broken endpoint that panics with `"boom"` |

All existing behaviour, 404s and 405s included, must go through the middleware
(wrap the whole mux).

```sh
go run ./cmd/course test 15
```

Then pass a real logger in [`cmd/gobin/main.go`](../../cmd/gobin/main.go)
(`Logger: slog.New(slog.NewTextHandler(os.Stderr, nil))`) and watch the logs.

## Hints

<details>
<summary>Hint 1: methods on an app struct</summary>

Middleware that needs the logger can be a method: `func (a *app) logRequests(next http.Handler) http.Handler`.
Then `return a.requestID(a.logRequests(a.recoverPanics(mux)))`.
</details>

<details>
<summary>Hint 2: knowing whether the response started, in recovery</summary>

Wrap `w` in a `statusRecorder` inside the recovery middleware too, and check
`rec.wroteHeader` in the deferred function. (You can reuse the same type.)
</details>

<details>
<summary>Hint 3: the request_id in the log is empty</summary>

The logging middleware must read the ID from the **request it received**,
which carries the context set by `requestID`. Make sure `requestID` is the
outer middleware and passes `r.WithContext(ctx)` on.
</details>

## Experiments

1. Remove your recovery middleware, run gobin, and `curl -v localhost:8080/panic`.
   What does curl report? Where did the stack trace go?
2. Swap the order of `logRequests` and `recoverPanics`. **Predict** what's
   logged for `/panic`, then check.
3. **Predict:** in a handler, `_, ok := w.(http.Flusher)`. Is `ok` true or false
   with your middleware in place? Check, then remove your `Unwrap` method and
   try `http.NewResponseController(w).Flush()`: what error do you get?
4. `curl -H 'X-Request-Id: hello world' -i localhost:8080/healthz`. Why
   reject that value?

## Explore further

- **Compression:** a gzip middleware that compresses responses when the
  client sends `Accept-Encoding: gzip`, sets `Content-Encoding` and
  `Vary: Accept-Encoding`, removes `Content-Length`, and still supports `Flush`.
- **CORS:** answer preflight `OPTIONS` requests and add
  `Access-Control-Allow-Origin` for configured origins.
- **Rate limiting:** a token bucket per client IP, returning `429` with
  `Retry-After`. (Write the bucket yourself, or use `golang.org/x/time/rate`.)
- **Metrics:** count requests by `r.Pattern` (Go 1.23) and status, and publish
  them with `expvar` on `/debug/vars`.
- Add a `chain(h, mws...)` helper. Does it make the order clearer or hide it?

## Check your understanding

```sh
go run ./cmd/course quiz 15
```

**Explain it back:** Why do `ResponseWriter` wrappers break `w.(http.Flusher)`,
and how do `Unwrap` and `http.ResponseController` fix it?

## Reference

- [`http.ResponseController`](https://pkg.go.dev/net/http#ResponseController), [`http.ErrAbortHandler`](https://pkg.go.dev/net/http#ErrAbortHandler)
- [`log/slog`](https://pkg.go.dev/log/slog), [Structured logging with slog](https://go.dev/blog/slog) (Go blog)
- [`context.WithValue`](https://pkg.go.dev/context#WithValue) (read about key types)

**Next:** [Stage 16: RoundTrippers: client middleware](../16-round-trippers/README.md)
