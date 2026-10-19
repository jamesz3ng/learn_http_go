# Stage 23: Graceful shutdown

> **Part VIII · Production** · **You build:** `gobin` · **Time:** ~75 min

## Why this stage matters

Servers are restarted all the time: deploys, scaling, config changes. Each
restart that kills in-flight requests shows up as errors for your users. A
**graceful** shutdown stops accepting new connections, lets requests already
in progress finish, and only then exits. `http.Server.Shutdown` does most of
it, but long-lived streams can stall it, and the most common way to call it
is subtly wrong.

## Before you read: predict

1. `main` does `log.Fatal(srv.ListenAndServe())`, and another goroutine calls
   `srv.Shutdown(ctx)`. What happens to requests still in progress?
2. Does `Shutdown` cancel the contexts of running requests?
3. Kubernetes stops a pod by sending which signal first?

## Reading

### Signals

Process managers ask a program to stop with a **signal**: `SIGTERM` from
Kubernetes, systemd and Docker (then `SIGKILL` if it's still running after a
grace period, 30s by default in Kubernetes); `SIGINT` from Ctrl-C.
`signal.NotifyContext` turns them into context cancellation:

```go
ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer stop()
```

(On Windows, Ctrl-C/Ctrl-Break arrive as `os.Interrupt`, and closing the
console, logoff or shutdown arrive as `syscall.SIGTERM`.)

### `Shutdown` vs `Close`

| | `srv.Shutdown(ctx)` | `srv.Close()` |
|---|---|---|
| listeners | closed: no new connections | closed |
| idle connections | closed | closed |
| active requests | **waits for them to finish** | **cut off immediately** |
| returns | when all are done, or `ctx.Err()` if `ctx` expires first | immediately |

Two details that bite:

- **`Serve` returns `http.ErrServerClosed` as soon as `Shutdown` starts**, not
  when it finishes. If `main` returns at that point, the process exits and the
  in-flight requests die anyway. You must wait for `Shutdown` itself to return.
- `ErrServerClosed` is the *expected* result after a shutdown, so don't report it
  as a failure.

### The shape of a graceful `Run`

```go
func Run(ctx context.Context, ln net.Listener, cfg Config) error {
    srv := NewServer(cfg)
    serveErr := make(chan error, 1)
    go func() { serveErr <- srv.Serve(ln) }()

    select {
    case err := <-serveErr:
        return err // failed on its own, e.g. the listener broke
    case <-ctx.Done():
    }

    // ctx is already cancelled: use a fresh one for the grace period.
    shutdownCtx, cancel := context.WithTimeout(context.Background(), gracePeriod)
    defer cancel()
    if err := srv.Shutdown(shutdownCtx); err != nil {
        srv.Close() // out of time: force the rest closed
        return err
    }
    return nil
}
```

Taking a `net.Listener` instead of an address makes it testable: tests listen
on port `0` (a random free port) and know the address before `Run` starts.

### Long-lived requests stall `Shutdown`

`Shutdown` waits for active requests to finish and **does not cancel their
contexts**. A `/watch` stream (stage 19) never finishes on its own, so
`Shutdown` waits for the whole grace period and then everything is cut off.

You need to *tell* streaming handlers to stop. Two tools:

- **`srv.RegisterOnShutdown(f)`**: `f` runs (in its own goroutine) when
  `Shutdown` begins. Close a channel there, and have streaming handlers select on it:
  ```go
  shutdown := make(chan struct{})
  srv.RegisterOnShutdown(func() { close(shutdown) }) // guard with sync.Once if Shutdown may run twice
  // in the watch loop:
  case <-shutdown:
      return
  ```
- **`srv.BaseContext`** returns the parent context for every request. If you
  cancel it at shutdown, *every* request's context is cancelled, which stops
  streams but also aborts the short requests you wanted to finish gracefully.
  It suits some servers, but not our requirements.

That raises a design question: `NewServer` (stage 17) calls `NewHandler(cfg)`,
which hides everything inside it, so how can the server reach the handlers'
shutdown channel? Refactor so both can share one `app`:

```go
func newApp(cfg Config) *app { ... }            // store, logger, shutdown channel
func (a *app) handler() http.Handler { ... }    // mux + middleware

func NewHandler(cfg Config) http.Handler { return newApp(cfg).handler() }

func NewServer(cfg Config) *http.Server {
    a := newApp(cfg)
    srv := &http.Server{Handler: a.handler(), /* timeouts... */}
    srv.RegisterOnShutdown(func() { close(a.shutdown) }) // guard with sync.Once
    return srv
}
```

(If you already built your server around an `app` struct, as stage 9
suggested, this is a small change.)

### Updating `main`

```go
func main() {
    // flags...
    ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
    defer stop()
    ln, err := net.Listen("tcp", *addr)
    if err != nil {
        log.Fatal(err)
    }
    if err := gobin.Run(ctx, ln, cfg); err != nil {
        log.Fatal(err)
    }
}
```

> **📖 Source dive:** `$(go env GOROOT)/src/net/http/server.go`: read
> `func (srv *Server) Shutdown`. How often does it poll for idle connections
> (look for `pollIntervalBase`)? When are the `RegisterOnShutdown` functions called?

## Your task

**`Run(ctx, ln, cfg) error`**:

| Requirement | Detail |
|---|---|
| serve | serve `NewServer(cfg)` on `ln` until `ctx` is cancelled |
| graceful | then stop accepting connections and let in-flight requests **finish** (up to `cfg.ShutdownTimeout`, default of your choice) |
| streams | open `/watch` streams must **end promptly** when shutdown begins (Run returns within ~1s, not after the timeout) |
| result | `nil` after a clean shutdown (**not** `ErrServerClosed`); a **non-nil error** if requests were still running when the timeout expired (force them closed); `Serve`'s error if it fails on its own (e.g. a closed listener) |

Then rewrite [`cmd/gobin/main.go`](../../cmd/gobin/main.go) to use
`signal.NotifyContext`, `net.Listen` and `Run`.

```sh
go run ./cmd/course test 23
```

## Hints

<details>
<summary>Hint 1: the Run test says "returned immediately"</summary>

`Run` must block until `ctx` is cancelled. Start `Serve` in a goroutine and
`select` on `ctx.Done()` and the serve error.
</details>

<details>
<summary>Hint 2: where does the shutdown channel live?</summary>

Make it a field of your `app` struct, created in `newApp` (see the refactor in the Reading). In `NewServer`,
`srv.RegisterOnShutdown(func() { once.Do(func() { close(a.shutdown) }) })`,
and in `watch`'s `select`, `case <-a.shutdown: return`. When the handler
comes from `NewHandler` (no server), nothing ever closes the channel, which is fine.
</details>

<details>
<summary>Hint 3: the timeout test returns nil</summary>

When `Shutdown` returns an error (its context expired), call `srv.Close()`
and return the error (wrapped if you like).
</details>

## Experiments

Run your updated `go run ./cmd/gobin`:

1. `curl localhost:8080/delay/5` and press Ctrl-C in gobin's terminal after a
   second. **Predict** what curl gets. Try again with your old `main` from git
   (`git stash` is handy) to compare.
2. `curl -N localhost:8080/watch`, then Ctrl-C gobin. How long until gobin exits?
   Comment out your `case <-a.shutdown` line and repeat.
3. `kill -TERM <pid>` instead of Ctrl-C. Same behaviour?
4. While gobin is shutting down from experiment 1, try a new request. What
   error does curl report?

## Explore further

- **Readiness:** add `/readyz` that returns `503` as soon as shutdown begins,
  and wait a few seconds *before* calling `Shutdown`, so a load balancer has
  time to notice and stop sending traffic. (Why isn't closing the listener enough?)
- Try `srv.BaseContext` cancelled on shutdown instead of `RegisterOnShutdown`. Which
  stage tests fail now, and why?
- For HTTP/2, `Shutdown` sends a `GOAWAY` frame. Look for it with
  `GODEBUG=http2debug=1` after stage 24.
- Read about zero-downtime restarts that hand the listening socket to a new
  process (systemd socket activation, `SO_REUSEPORT`).

## Check your understanding

```sh
go run ./cmd/course quiz 23
```

**Explain it back:** Why does a correct graceful shutdown wait for
`Shutdown` to return rather than for `Serve`, and why do streaming handlers
need their own shutdown signal?

## Reference

- [`http.Server.Shutdown`](https://pkg.go.dev/net/http#Server.Shutdown), [`Server.RegisterOnShutdown`](https://pkg.go.dev/net/http#Server.RegisterOnShutdown), [`Server.BaseContext`](https://pkg.go.dev/net/http#Server)
- [`signal.NotifyContext`](https://pkg.go.dev/os/signal#NotifyContext)
- [Kubernetes: termination of pods](https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/#pod-termination)

**Next:** [Stage 24: TLS and HTTP/2](../24-tls-and-http2/README.md)
