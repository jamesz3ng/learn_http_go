# Stage 16: RoundTrippers: client middleware

> **Part V · Composition** · **You build:** `kvclient` · **Time:** ~90 min

## Why this stage matters

The server has `Handler` middleware; the client has the same idea, built on
`http.RoundTripper`. Wrapping the transport lets you add authentication,
retries, logging, metrics or caching to **every** request a client makes,
without touching the code that makes them. Retries in particular look simple
and are full of traps: retrying the wrong requests, losing the body, leaking
connections, retrying in lockstep. You'll write two transports and plug them
into your SDK.

## Before you read: predict

1. A `POST /orders` request times out. Is it safe to retry automatically?
2. Your retry loop calls `base.RoundTrip(req)` a second time with the same
   `req`. What body does the server receive?
3. 1,000 clients all get a `503` at the same moment and all retry after exactly
   one second. What happens to the server?

## Reading

### `http.RoundTripper`

```go
type RoundTripper interface {
    RoundTrip(*Request) (*Response, error)
}
```

One request in, one response out: the client-side twin of `Handler`. The
`http.Client` does policy (redirects, cookies, overall timeout) and calls its
`Transport` (a `RoundTripper`) **once per hop**. `*http.Transport` is the
real one (connections, TLS, HTTP/2); you can wrap it.

The interface's doc comment is a contract. The important parts:

- **Don't interpret the response.** Return `err == nil` whenever you got a
  response, even a 500. Turning statuses into errors is the caller's job.
- **Don't modify the request**, except for consuming and closing its body.
  To change headers, **clone** it: `r2 := req.Clone(req.Context())`.
- **Always close the request body**, even when returning an error.

### The adapter and a first transport

```go
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func NewBearerTransport(base http.RoundTripper, token string) http.RoundTripper {
    if base == nil {
        base = http.DefaultTransport
    }
    return roundTripperFunc(func(req *http.Request) (*http.Response, error) {
        r2 := req.Clone(req.Context()) // deep-copies headers; the body is shared
        r2.Header.Set("Authorization", "Bearer "+token)
        return base.RoundTrip(r2)
    })
}

client := &http.Client{Transport: NewBearerTransport(nil, token)}
```

Same pattern as `http.HandlerFunc`, the other way round.

### Retries: what is safe to retry?

**Which failures?** Retry when trying again might help:

| Retry | Don't retry |
|---|---|
| network errors: connection refused or reset | `context.Canceled`, `DeadlineExceeded`: the caller gave up |
| `429 Too Many Requests` | `4xx` in general: the request is wrong; resending won't fix it |
| `502`, `503`, `504`: a gateway or overloaded backend | `500`: usually a bug, and resending may repeat side effects |

**Which requests?** Only those that are safe to send twice:
**idempotent** methods (`GET`, `HEAD`, `OPTIONS`, `PUT`, `DELETE`). A `POST`
that timed out may have succeeded on the server, and retrying it can charge a
card twice. APIs that support it accept an **`Idempotency-Key`** header so the
server can deduplicate. Treat requests carrying one as retryable.

**The body.** The first attempt consumed `req.Body`. For each retry, get a
fresh copy from `req.GetBody()` (set automatically for byte/string readers,
stage 7). No `GetBody` means no retry.

### Backoff and jitter

Waiting a fixed time between attempts makes things worse in an outage: every
client retries at the same instant (a "thundering herd"). Use **exponential
backoff** (each wait doubles), **capped**, with **jitter** (randomness):

```go
d := p.BaseDelay << attempt          // 100ms, 200ms, 400ms, ...
if d <= 0 || d > p.MaxDelay {        // d <= 0 catches overflow
    d = p.MaxDelay
}
d = d/2 + rand.N(d/2 + 1)            // "equal jitter": somewhere in [d/2, d]
```

If the server sends **`Retry-After: N`** (seconds), it's telling you when
to come back: wait that long instead (still capped by `MaxDelay`). It can also
be an HTTP date; seconds are enough for us.

### Waiting politely

Never `time.Sleep` in a retry loop: if the caller cancels the context, you'd
keep sleeping. Wait on both:

```go
timer := time.NewTimer(delay)
select {
case <-timer.C:
case <-req.Context().Done():
    timer.Stop()
    return nil, req.Context().Err()
}
```

### Discarded responses must be drained and closed

When you retry after a `503`, you throw that response away. **Drain and
close its body** first, or its connection leaks out of the pool:

```go
io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
resp.Body.Close()
```

When retries run out, return the **last response as-is** (a 503 is a response,
not an error) and let the caller decide.

### Composition

```go
client := &http.Client{
    Transport: NewBearerTransport(NewRetryTransport(http.DefaultTransport, policy), token),
}
kv, _ := kvclient.New(baseURL, client)
```

Order matters in general: a logging transport *outside* the retry logs one
line per call, and *inside* it logs every attempt. Also beware **retry
amplification**: if each layer of a system retries three times, three layers
make 27 attempts.

> **📖 Source dive:** `go doc net/http.RoundTripper` (the full contract) and
> `go doc -src net/http.Request.Clone`. Which fields does `Clone` deep-copy,
> and which are shared?

## Your task

In `kvclient` (e.g. a new `transport.go`):

**`NewBearerTransport(base, token)`** adds `Authorization: Bearer <token>`
to every request without modifying the caller's request. `nil` base →
`http.DefaultTransport`.

**`NewRetryTransport(base, policy)`** with
`RetryPolicy{MaxRetries, BaseDelay, MaxDelay}`:

| Rule | Detail |
|---|---|
| base | `nil` → `http.DefaultTransport` (as for the bearer transport) |
| when | **any** error from `base` (unless the request's context is cancelled or past its deadline), or status `429`, `502`, `503`, `504` |
| which requests | `GET`, `HEAD`, `OPTIONS`, `PUT`, `DELETE`, or any request with an `Idempotency-Key` header. With a body, only if `GetBody` is set |
| how many | at most `MaxRetries` retries (`MaxRetries + 1` attempts) |
| body | identical on every attempt |
| delay | `BaseDelay * 2^retryIndex`, capped at `MaxDelay`; jitter may reduce it by up to half. `Retry-After: N` (seconds) overrides it, still capped at `MaxDelay` |
| cancellation | stop waiting and return the context's error as soon as it's cancelled |
| cleanup | drain and close every discarded response |
| result | when retries run out, return the last response (or error) unchanged |

```sh
go run ./cmd/course test 16
```

## Hints

<details>
<summary>Hint 1: the loop shape</summary>

```go
for attempt := 0; ; attempt++ {
    r := req
    if attempt > 0 { /* clone req, r.Body = req.GetBody() */ }
    resp, err := base.RoundTrip(r)
    if attempt >= p.MaxRetries || !shouldRetry(resp, err) {
        return resp, err
    }
    // pick the delay; drain+close resp if non-nil; wait (timer vs ctx)
}
```
</details>

<details>
<summary>Hint 2: decide retryability up front</summary>

Check the method and `GetBody` **before** the first attempt. If the request
isn't retryable, just `return base.RoundTrip(req)`.
</details>

<details>
<summary>Hint 3: the connection test fails</summary>

Look at every path that discards a response. Each needs
`io.Copy(io.Discard, resp.Body)` + `resp.Body.Close()` before the next attempt.
</details>

## Experiments

1. With gobin running (and its access log on, from stage 15), write a short
   program that builds an `http.Client` with your retry transport
   (`MaxRetries: 4, BaseDelay: 200ms, MaxDelay: 2s`) and GETs
   `localhost:8080/status/503`. Watch gobin log each attempt, and the gaps
   between them grow. Then try `/status/500`: **predict** how many attempts.
2. Put a logging transport (5 lines: log method, URL, status) **outside**, then
   **inside** the retry transport. **Predict** the logs for each.
3. Change your jitter to none and run the backoff test 5 times, printing the
   delays. Then restore it. Why does production code want the randomness?

## Explore further

- **Logging transport** with `slog`: method, URL, status, duration and attempt number.
- **Caching transport:** remember `ETag`s; send `If-None-Match`; on `304`
  return the cached response. (gobin's `/kv/{key}` will support this in stage 21.)
- **Circuit breaker:** after N consecutive failures, fail fast for a while
  without calling the server at all.
- **Retry budget:** allow retries only while they're under 10% of recent
  requests, so retries can't multiply load during an outage.
- Parse `Retry-After` HTTP-dates with `http.ParseTime`.

## Check your understanding

```sh
go run ./cmd/course quiz 16
```

**Explain it back:** List four things a correct retrying `RoundTripper` must do
that a naive "call it again in a loop" version gets wrong.

## Reference

- [`http.RoundTripper`](https://pkg.go.dev/net/http#RoundTripper), [`Request.Clone`](https://pkg.go.dev/net/http#Request.Clone), [`Request.GetBody`](https://pkg.go.dev/net/http#Request)
- [Exponential backoff and jitter](https://aws.amazon.com/blogs/architecture/exponential-backoff-and-jitter/) (AWS Architecture Blog)
- [The Idempotency-Key HTTP header field](https://datatracker.ietf.org/doc/draft-ietf-httpapi-idempotency-key-header/) (IETF draft)

**Next:** [Stage 17: Server timeouts and cancellation](../17-server-timeouts/README.md)
