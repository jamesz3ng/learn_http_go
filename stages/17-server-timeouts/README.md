# Stage 17: Server timeouts and cancellation

> **Part VI · Time** · **You build:** `gobin` · **Time:** ~75 min

## Why this stage matters

`http.ListenAndServe(addr, h)` runs a server with **no timeouts at all**. One
client that opens connections and dribbles bytes (or just sits there) can tie
up goroutines and file descriptors until your server falls over. On the other
side, a handler that keeps working after its client has gone wastes CPU
and database time on an answer nobody will read. This stage covers both:
the `http.Server` timeouts, and the request **context**.

## Before you read: predict

1. Your handler runs a 10-second query; the client gives up after 1 second.
   Does the query stop?
2. `WriteTimeout` is 5s and a handler takes 8s. What does the client see?
3. Which timeout protects against a client that sends one header byte every 9 seconds?

## Reading

### The `http.Server` timeouts

```
 conn accepted   request headers read   body read        response written    next request
     │◀──── ReadHeaderTimeout ───▶│                                                 │
     │◀──────────────── ReadTimeout ──────────────▶│                               │
                                  │◀───────────── WriteTimeout ─────────▶│          │
                                                                          │◀ IdleTimeout ▶│
```

| Field | Limits | If zero |
|---|---|---|
| `ReadHeaderTimeout` | time to read the request line and headers | falls back to `ReadTimeout` |
| `ReadTimeout` | time to read the **entire** request, body included | no limit |
| `WriteTimeout` | from the end of reading the headers to the end of writing the response | no limit |
| `IdleTimeout` | how long a keep-alive connection may sit between requests | falls back to `ReadTimeout` |
| `MaxHeaderBytes` | size of request line + headers | 1 MB |

`ReadHeaderTimeout` is the key defence against **slowloris** (clients that
send headers very slowly to hold connections open). Set it to a few seconds.

`ReadTimeout` and `WriteTimeout` apply to **every** request on the server, so
they're blunt: a 30s `WriteTimeout` also kills a legitimate 2-minute
download or a long-lived event stream. Since Go 1.20, a handler can adjust
its own deadlines with `http.NewResponseController(w).SetWriteDeadline(t)`
and `SetReadDeadline(t)` (stage 19 does this).

**`WriteTimeout` doesn't stop your handler.** When it expires, the handler
keeps running; its writes start failing; the client sees the connection
close without a response. Use the context to stop the work.

### Request contexts

Every request carries a context: `r.Context()`. The server **cancels it** when:

- the client's connection closes (it disconnected or timed out),
- an HTTP/2 client cancels the request (resets the stream),
- or `ServeHTTP` returns.

So a handler doing slow work should watch it:

```go
select {
case <-time.After(d):           // the "work"
case <-r.Context().Done():
    return // client's gone; r.Context().Err() is context.Canceled
}
```

More often you **pass it on**: `db.QueryContext(r.Context(), ...)`,
`http.NewRequestWithContext(r.Context(), ...)` for outgoing calls. Then
cancellation propagates through your whole call tree automatically. This is
the main reason context exists.

Prefer `time.NewTimer` + `defer timer.Stop()` over `time.After` in code that
runs often. Before Go 1.23, an unfired `time.After` timer stayed alive until
it fired.

### Logging abandoned requests

If a handler returns without writing anything, `net/http` sends a `200` with an
empty body, to nobody. Your access log would say `200`, which is misleading.
nginx's convention is **`499 Client Closed Request`**: in your logging
middleware, if nothing was written and `r.Context().Err() != nil`, log `499`.

### `http.TimeoutHandler`

```go
mux.Handle("GET /report", http.TimeoutHandler(reportHandler, 2*time.Second, "report timed out"))
```

It runs the handler with a context that's cancelled after the timeout, and
answers `503` with the given message if the handler hasn't finished in time.
It buffers the response, so it can't stream (no `Flush`). It's useful per
route, but not for every endpoint.

### A configured server

Building the `*http.Server` in a function keeps `main` small and makes the
configuration testable:

```go
func NewServer(cfg Config) *http.Server {
    return &http.Server{
        Addr:              cfg.Addr,
        Handler:           NewHandler(cfg),
        ReadHeaderTimeout: orDefault(cfg.ReadHeaderTimeout, 5*time.Second),
        ...
    }
}
```

`Server.ErrorLog` is where `net/http` reports its own problems (TLS handshake
errors, panics). `slog.NewLogLogger(handler, level)` routes them into your
structured logger.

> **📖 Source dive:** in `$(go env GOROOT)/src/net/http/server.go`, find
> `func (c *conn) readRequest`. Where are the read and write deadlines set?
> Then find `startBackgroundRead`. That's how the server notices a client
> disconnecting while your handler runs.

## Your task

**`GET /delay/{seconds}`**: `seconds` is a number from 0 to 10 (fractions
allowed, e.g. `0.5`; otherwise `400`). Wait that long, then respond exactly
like `/get`. If the client goes away first, **stop immediately** without
writing.

**Access log**: an abandoned request (nothing written, context cancelled) is
logged with status **`499`**.

**`NewServer(cfg Config) *http.Server`**:

| Field | Value |
|---|---|
| `Addr` | `cfg.Addr` |
| `Handler` | `NewHandler(cfg)` |
| `ReadHeaderTimeout`, `ReadTimeout`, `WriteTimeout`, `IdleTimeout` | from `cfg`; when zero, **non-zero defaults** of your choice (`ReadHeaderTimeout` at most 30s) |

Then switch [`cmd/gobin/main.go`](../../cmd/gobin/main.go) to
`gobin.NewServer(cfg).ListenAndServe()`.

```sh
go run ./cmd/course test 17
```

## Hints

<details>
<summary>Hint 1: parsing seconds</summary>

`strconv.ParseFloat` accepts `"NaN"` and `"Inf"`, so check `math.IsNaN`
too. Convert with `time.Duration(secs * float64(time.Second))`.
</details>

<details>
<summary>Hint 2: logging 499</summary>

In your `statusRecorder`-based logging middleware, after `next.ServeHTTP`:
`if !rec.wroteHeader && r.Context().Err() != nil { status = 499 }`.
</details>

<details>
<summary>Hint 3: the "handler kept running" test fails</summary>

Is your wait a `time.Sleep`? It can't be interrupted. Use `select` with the
timer and `r.Context().Done()`.
</details>

## Experiments

With `cmd/gobin` using `NewServer`:

1. `curl -m 1 localhost:8080/delay/5`. Check gobin's log: status? duration?
2. **Slowloris by hand:** `nc localhost 8080`, type `GET / HTTP/1.1` and Enter,
   then wait. **Predict** when the server hangs up. Now type one header line
   every 2 seconds instead: does it help?
3. Set `WriteTimeout: time.Second` in main and `curl -v localhost:8080/delay/2`.
   What does curl report? Does gobin's log show the request?
4. Count goroutines: add `GET /debug/goroutines` returning `runtime.NumGoroutine()`.
   Fire 100 `curl -m 0.2 localhost:8080/delay/10 &` requests, wait a second, and
   check the count. Then do it with a `time.Sleep`-based `/delay`.

## Explore further

- Wrap only `/delay/{seconds}` in `http.TimeoutHandler` with 3s and try
  `/delay/5`. What does the client get, and what does your handler see?
- Write a `deadline(d)` middleware that does
  `ctx, cancel := context.WithTimeout(r.Context(), d)`. Handlers that respect
  the context then stop. What should the response be?
- Add `GET /fetch?url=` that fetches another URL with
  `http.NewRequestWithContext(r.Context(), ...)`. Cancel the client and watch
  the outgoing request get cancelled too.
- For an upload endpoint, extend the read deadline per request with
  `ResponseController.SetReadDeadline` instead of raising `ReadTimeout` for everyone.

## Check your understanding

```sh
go run ./cmd/course quiz 17
```

**Explain it back:** What does each of the four `http.Server` timeouts limit,
and why isn't `WriteTimeout` enough to stop a slow handler?

## Reference

- [`http.Server`](https://pkg.go.dev/net/http#Server) (timeout fields), [`http.TimeoutHandler`](https://pkg.go.dev/net/http#TimeoutHandler), [`Request.Context`](https://pkg.go.dev/net/http#Request.Context)
- [The complete guide to Go net/http timeouts](https://blog.cloudflare.com/the-complete-guide-to-golang-net-http-timeouts/) (Cloudflare)
- [Go Concurrency Patterns: Context](https://go.dev/blog/context) (Go blog)

**Next:** [Stage 18: Client timeouts](../18-client-timeouts/README.md)
