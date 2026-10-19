# Stage 25: Connection pooling and httptrace

> **Part VIII · Production** · **You build:** `loadgen` · **Time:** ~90 min

## Why this stage matters

Opening a connection is expensive: a TCP handshake, maybe a TLS handshake
(more round trips plus crypto), and TCP slow start. Go's `Transport`
keeps a **pool** of open connections and reuses them, but only if you let
it. One default setting, `MaxIdleConnsPerHost = 2`, quietly destroys reuse for
any program making many concurrent requests to one host. You'll build a load
generator like [hey](https://github.com/rakyll/hey), use `net/http/httptrace`
to *see* whether each request reused a connection, and tune the transport
until the numbers look right.

## Before you read: predict

1. 50 goroutines share one `http.Client` with the default transport and hammer
   one host. Roughly how many idle connections does the pool keep between requests?
2. How can a program find out whether a particular request reused a connection?
3. Over HTTP/2, how many connections does a client typically open to one host?

## Reading

### The pool

`*http.Transport` keeps idle connections keyed by
scheme + host + port (+ proxy). When a request finishes **and its body was read
to EOF and closed** (stage 3), the connection goes back into the pool; the
next request to that host takes it instead of dialling.

| Setting | Meaning | `DefaultTransport` |
|---|---|---|
| `MaxIdleConns` | idle connections kept, across all hosts | 100 |
| `MaxIdleConnsPerHost` | idle connections kept **per host** | 0, which means **2** (`DefaultMaxIdleConnsPerHost`) |
| `MaxConnsPerHost` | total (idle + active) per host; extra requests **wait** | 0 = unlimited |
| `IdleConnTimeout` | close idle connections after this long | 90s |
| `DisableKeepAlives` | one connection per request | false |

With 50 concurrent requests, up to 50 connections are open at once. As they
finish, the pool may keep only **2** of them, so 48 are **closed**, and the next
wave dials 48 new connections. Throughput drops, latency rises, CPU goes to
handshakes, and the OS fills up with `TIME_WAIT` sockets. Set
`MaxIdleConnsPerHost` to at least your concurrency to one host.

Over **HTTP/2**, one connection carries many concurrent requests (streams),
so the pool usually holds just one connection per host.

### `net/http/httptrace`

`httptrace` lets you hook into the client's internals for a **single
request**, through its context:

```go
trace := &httptrace.ClientTrace{
    GotConn: func(info httptrace.GotConnInfo) {
        // info.Reused: from the pool? info.WasIdle, info.IdleTime: how long it waited
    },
    ConnectStart:         func(network, addr string) {},
    TLSHandshakeDone:     func(tls.ConnectionState, error) {},
    GotFirstResponseByte: func() {},
}
ctx = httptrace.WithClientTrace(ctx, trace)
req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
```

There are hooks for DNS, dialling, TLS, writing the request, the first response
byte and more. That's how tools like `httpstat` print a timing breakdown. The
docs warn that hooks **may be called from other goroutines**. `GotConn` runs
before `Do` returns, so reading a variable it set after `Do` is fine in practice,
but treat the general case with care.

### A worker pool

The classic Go shape for "N jobs, C at a time":

```go
jobs := make(chan struct{})
var wg sync.WaitGroup
for range c {                      // C workers
    wg.Add(1)
    go func() {
        defer wg.Done()
        for range jobs {           // until jobs is closed
            // one request; record the result (under a mutex)
        }
    }()
}
for range n {                      // N jobs (stop early if ctx is done)
    jobs <- struct{}{}
}
close(jobs)
wg.Wait()
```

Concurrency is bounded by the number of workers, not by how fast you can
create goroutines.

### Latency percentiles

Averages hide the slow requests your users notice. Report percentiles:
sort the latencies; the p-th percentile ("nearest rank") is the element at
index `ceil(p × n) − 1`. p50 is the typical request; p99 is the slowest 1%.

(A closed-loop generator like this one, where each worker waits for a
response before sending the next request, under-reports tail latency when the
server stalls: fewer requests are sent during the stall. That's called
*coordinated omission*. See Explore.)

> **📖 Source dive:** in `$(go env GOROOT)/src/net/http/transport.go`, find
> `func (t *Transport) tryPutIdleConn` and the check against
> `t.maxIdleConnsPerHost()`. What happens to a connection that doesn't fit?

## Your task

Implement [`loadgen.Run`](../../loadgen/loadgen.go):

| Requirement | Detail |
|---|---|
| work | send `cfg.Requests` requests (`cfg.Method`, default GET) to `cfg.URL` with exactly `cfg.Concurrency` workers (never more in flight) |
| reuse | with C workers, about C connections in total, not one per request. Tune your own `*http.Transport` and read every body to EOF |
| `Report.Requests`, `Errors`, `StatusCodes` | requests attempted, requests with no response (transport errors), and responses by status code (a 503 is a response, not an error) |
| `NewConns`, `ReusedConns` | from `httptrace`: for each response, was the connection new or reused? (They add up to the number of responses) |
| latencies | `P50`, `P90`, `P99`, `Max` over requests that got a response, measured until the body was fully read |
| `Elapsed` | wall-clock time of the run |
| timeout | `cfg.Timeout` per request (default 10s) |
| errors | `cfg.URL` that isn't an absolute `http`/`https` URL with a host → error, before sending anything (as in stage 10: `url.Parse("not a url")` succeeds!). Cancelled `ctx` → stop promptly, return the partial report **and** `ctx.Err()` |

Then try it: `go run ./cmd/loadgen -n 2000 -c 50 http://localhost:8080/get`.

```sh
go run ./cmd/course test 25 -race
```

## Hints

<details>
<summary>Hint 1: one client for the whole run</summary>

Create one `*http.Transport` (and `http.Client`) per `Run`, shared by all workers,
with `MaxIdleConnsPerHost` (and `MaxIdleConns`) set to the concurrency.
`defer tr.CloseIdleConnections()` when the run ends.
</details>

<details>
<summary>Hint 2: recording reuse</summary>

Create a fresh `ClientTrace` per request whose `GotConn` stores `info.Reused`
into a local variable, then record it after the body is read.
</details>

<details>
<summary>Hint 3: cancelled requests</summary>

When `ctx` is cancelled, in-flight requests fail with context errors. Don't
count those as server errors: check `ctx.Err() != nil` and stop the worker.
</details>

## Experiments

Run gobin, then:

1. `go run ./cmd/loadgen -n 5000 -c 50 http://localhost:8080/get` with your
   tuned transport. Then temporarily remove `MaxIdleConnsPerHost` and run it
   again. Compare `Connections` and requests/second. While it runs, count
   sockets: `ss -tan | grep -c TIME-WAIT` (Linux) or `netstat -an | grep -c TIME_WAIT`.
2. Stop reading bodies to EOF (just `Close`). **Predict** the connection count.
3. Against `/delay/0.05`: how do p50 and p99 change between `-c 1` and `-c 50`?
4. Against HTTPS gobin (stage 24) with `-k`-style trust: set
   `TLSClientConfig{InsecureSkipVerify: true}` and `ForceAttemptHTTP2: true` in
   loadgen. How many new connections do 2,000 requests at `-c 50` need over HTTP/2?

## Explore further

- **Timing breakdown:** add DNS, connect, TLS and time-to-first-byte averages
  using more `ClientTrace` hooks, like `curl -w` or `httpstat`.
- **Open-loop mode:** `-q QPS` sends requests on a fixed schedule regardless
  of responses, and measures latency from the *scheduled* time, avoiding
  coordinated omission. Compare p99 against the closed loop when gobin stalls.
- Set `MaxConnsPerHost` lower than the concurrency. Where does the extra time
  show up in the latency numbers?
- Print a latency histogram in the terminal.
- Point loadgen at your stage 26 proxy and compare overhead.

## Check your understanding

```sh
go run ./cmd/course quiz 25
```

**Explain it back:** Why does `MaxIdleConnsPerHost = 2` cause connection
churn with 50 concurrent workers, and what two conditions must hold for a
connection to be reused?

## Reference

- [`http.Transport`](https://pkg.go.dev/net/http#Transport) (pool settings), [`http.DefaultMaxIdleConnsPerHost`](https://pkg.go.dev/net/http#DefaultMaxIdleConnsPerHost)
- [`net/http/httptrace`](https://pkg.go.dev/net/http/httptrace), [Introducing HTTP Tracing](https://go.dev/blog/http-tracing) (Go blog)
- [How NOT to measure latency](https://www.youtube.com/watch?v=lJ8ydIuPFeU) (Gil Tene, coordinated omission)

**Next:** [Stage 26: Capstone: a load-balancing reverse proxy](../26-reverse-proxy/README.md)
