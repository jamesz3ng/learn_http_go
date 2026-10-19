# Stage 26: Capstone: a load-balancing reverse proxy

> **Part IX · Capstone** · **You build:** `proxy` · **Time:** ~2–4 hours

## Why this stage matters

A reverse proxy is a server and a client at the same time. It receives a
request (stages 2–9), turns it into an outgoing request (stages 5–7, 16),
streams the response back (19–20), deals with timeouts and failures (17–18),
and must not leak connections (25). That's nearly the whole course in one
program, and the thing in front of most production web services (nginx,
Envoy, cloud load balancers). You'll build one that spreads traffic over
several gobins and survives one of them dying.

This stage gives you more room than earlier ones: the task is precise, but
how you build it is up to you.

## Before you read: predict

1. Your proxy forwards the client's `Connection: keep-alive` header to the
   backend unchanged. Why is that wrong?
2. The proxy uses an `http.Client` to call the backend, and the backend answers
   `302`. What happens, and what should happen?
3. A backend is down. For which requests is it safe to quietly try another backend?

## Reading

### What forwarding involves

Turning an inbound request into an outbound one:

1. **Copy the request**, pointing it at the backend: scheme, host, and path
   (`/base` + `/items/42`), keeping the query. An outbound request must have
   an empty `RequestURI` (that field is server-side only).
2. **Remove hop-by-hop headers.** Some headers describe *this connection*,
   not the request: `Connection`, `Keep-Alive`, `Proxy-Connection`,
   `Proxy-Authenticate`, `Proxy-Authorization`, `TE`, `Trailer`,
   `Transfer-Encoding`, `Upgrade`, **plus any header named in `Connection`**
   (`Connection: X-Secret` makes `X-Secret` hop-by-hop). Forwarding them is a bug
   and sometimes a security hole.
3. **Say who the client was**: the backend only sees the proxy's address.
   `X-Forwarded-For` (client IP, appended to any existing list),
   `X-Forwarded-Host` (the `Host` the client used), `X-Forwarded-Proto`
   (`http`/`https`). RFC 7239's `Forwarded` header is the standardised version.
4. **Send it with a `RoundTripper`, not a `Client`.** A `Client` would
   follow redirects and apply a cookie jar; a proxy must pass `302`s and
   `Set-Cookie`s through untouched.
5. **Copy the response back**: status, headers (minus hop-by-hop), and the
   body, **flushing** as it arrives so streams (stage 19) keep streaming.
6. **Propagate cancellation**: build the outbound request with `r.Context()`,
   so a client that disconnects cancels the backend request too.
7. **Fail well**: if the backend can't be reached, answer `502 Bad Gateway`
   (`504 Gateway Timeout` for a backend that's too slow).

### `httputil.ReverseProxy`

The standard library implements all of that, plus trailers, `1xx` responses
and protocol upgrades (WebSockets):

```go
target, _ := url.Parse("http://10.0.0.1:8080/base")
rp := &httputil.ReverseProxy{
    Rewrite: func(pr *httputil.ProxyRequest) {
        pr.SetURL(target)  // scheme, host, joined path; outbound Host = target host
        pr.SetXForwarded() // X-Forwarded-For/-Host/-Proto
        // pr.In is the inbound request (read-only); pr.Out is yours to modify
    },
    FlushInterval: -1,     // flush after every write (streams are also auto-detected)
    ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
        http.Error(w, "bad gateway", http.StatusBadGateway)
    },
    // Transport: a RoundTripper for reaching backends (default: http.DefaultTransport)
    // ModifyResponse: func(*http.Response) error, to edit the backend's response
}
```

Prefer `Rewrite` (Go 1.20+) over the older `Director`: with `Rewrite`, the
proxy strips client-supplied `Forwarded`/`X-Forwarded-*` headers first, and
hop-by-hop removal can't undo your changes.

### Load balancing and failure

- **Round robin**: an `atomic.Uint64` counter; backend = `backends[n % len]`.
  (Other strategies: least connections, weighted, consistent hashing.)
- **Passive health checking**: when connecting to a backend fails, mark it
  down for a **cooldown** period and skip it. (Active checking probes
  `/healthz` periodically; see Explore.)
- **Retrying another backend is safe only if the request was never sent**
  (the *connection* failed, an `*net.OpError` with `Op == "dial"`) **and** it
  can be re-sent: an inbound request body is a one-shot stream, so only
  body-less requests can be retried transparently.
- If every backend fails: `502`.

One neat design: balancing and failover inside a custom **`RoundTripper`**
(stage 16) used as the `ReverseProxy`'s `Transport`. The `ReverseProxy`
handles the HTTP details; your transport picks backends. (Other designs are
fine.)

### Building it by hand instead

If you want maximum learning, skip `httputil` and write the forwarding
yourself with `r.Clone(r.Context())`, a hop-by-hop header filter, a
`Transport.RoundTrip`, header copying and an `io.Copy` with flushing. Then
read `$(go env GOROOT)/src/net/http/httputil/reverseproxy.go` and compare.
It's about 800 lines, and every one of them handles a real case.

> **📖 Source dive:** in `reverseproxy.go`, find `removeHopByHopHeaders`
> and `func (p *ReverseProxy) flushInterval`. How does it decide a response is
> a stream that must be flushed immediately?

## Your task

Implement [`proxy.New(cfg)`](../../proxy/proxy.go):

| Requirement | Detail |
|---|---|
| validation | error if `Backends` is empty or any entry isn't an absolute `http(s)` URL |
| balancing | round robin across healthy backends: 9 sequential requests over 3 backends → 3 each |
| forwarding | method, body, end-to-end headers, query; path = backend path + request path (`http://b/base/` + `/items/42?x=1` → `/base/items/42?x=1`); response status, headers and body back to the client |
| hop-by-hop | removed, including headers listed in `Connection` |
| forwarded headers | `X-Forwarded-For` = client IP, `X-Forwarded-Host` = the host the client requested, `X-Forwarded-Proto` = `http`/`https` |
| failover | if a backend can't be connected to, mark it down for `cfg.Cooldown` (default 10s) and retry the request on the next backend, for requests without a body |
| all down | `502 Bad Gateway` |
| streaming | responses are flushed as they arrive (an SSE event gets through before the backend finishes) |

`cmd/proxy` is ready to run it:

```sh
go run ./cmd/course test 26
go run ./cmd/gobin -addr :9001 & go run ./cmd/gobin -addr :9002 &
go run ./cmd/proxy -listen :8080 -backend http://localhost:9001 -backend http://localhost:9002
```

## Hints

<details>
<summary>Hint 1: a plan</summary>

1. Parse and validate the backends.
2. Get one backend working with `httputil.ReverseProxy` + `Rewrite` (`SetURL`,
   `SetXForwarded`).
3. Move backend choice into a `RoundTripper` that **clones** the request
   (stage 16's rule), sets the clone's `URL.Scheme`, `URL.Host`, the joined
   path and `Host = ""`, then calls `cfg.Transport` (default
   `http.DefaultTransport`).
4. Add round robin, then failover, then the cooldown.
</details>

<details>
<summary>Hint 2: joining paths without SetURL</summary>

Keep the inbound path *and* its escaped form, then prefix both:
```go
inPath, inRaw := out.URL.Path, out.URL.EscapedPath()
out.URL.Path = strings.TrimSuffix(b.Path, "/") + inPath
out.URL.RawPath = strings.TrimSuffix(b.EscapedPath(), "/") + inRaw
```
</details>

<details>
<summary>Hint 3: telling "couldn't connect" from other errors</summary>

```go
var opErr *net.OpError
connectFailed := errors.As(err, &opErr) && opErr.Op == "dial"
```
Only those are safe to retry on another backend.
</details>

<details>
<summary>Hint 4: ReverseProxy logs errors to stderr</summary>

Set `ErrorHandler` and it'll call yours instead of logging and writing a bare 502.
</details>

## Experiments

With two gobins behind your proxy on `:8080`:

1. `curl localhost:8080/get` a few times. Find `X-Forwarded-For` in the
   `headers`. Which backend answered? (Each gobin's own log shows it.)
2. **Stateful backends:** `curl -X PUT --data-binary v localhost:8080/kv/k`,
   then `curl localhost:8080/kv/k` a few times. **Predict** what you'll see.
   Why? What are three ways real systems fix this?
3. `go run ./cmd/loadgen -n 5000 -c 50 http://localhost:8080/get` while you
   kill one gobin. How many errors?
4. `curl -N localhost:8080/watch` through the proxy, and PUT keys directly to
   the gobin that's serving the watch (check its log).
5. Make a backend slow (`/delay/30`) and `curl` it through the proxy, then
   Ctrl-C curl. Does the backend's access log show `499` (stage 17)?

## Explore further

- **Active health checks:** probe each backend's `/healthz` every few
  seconds and take it out of rotation (and back in) based on the result.
- **Consistent hashing:** route `/kv/{key}` by a hash of the key so each key
  always lands on the same backend, which fixes experiment 2 without shared state.
  What happens to keys when a backend is added?
- **Least connections:** track in-flight requests per backend and pick the
  least busy one.
- **Graceful shutdown and TLS termination** for the proxy: reuse stages 23 and 24.
- **Caching:** store `GET` responses that have `Cache-Control: max-age`, and
  revalidate with `If-None-Match`.
- **By hand:** reimplement forwarding without `httputil` (see Reading), then run
  the stage tests against your version.

## Check your understanding

```sh
go run ./cmd/course quiz 26
```

**Explain it back:** Trace one request through your proxy, naming every
`net/http` type it passes through on the way to the backend and back.

## Where next?

You've built servers, clients, middleware on both sides, streams, files,
TLS, load testing and a proxy, all with `net/http`. Keep your reviews going
(`go run ./cmd/course review`), and see [docs/beyond.md](../../docs/beyond.md)
for ideas to take this further.

## Reference

- [`httputil.ReverseProxy`](https://pkg.go.dev/net/http/httputil#ReverseProxy), [`ProxyRequest.SetURL`](https://pkg.go.dev/net/http/httputil#ProxyRequest.SetURL), [`ProxyRequest.SetXForwarded`](https://pkg.go.dev/net/http/httputil#ProxyRequest.SetXForwarded)
- [RFC 9110 §7.6.1: Connection (hop-by-hop headers)](https://www.rfc-editor.org/rfc/rfc9110#section-7.6.1), [RFC 7239: Forwarded](https://www.rfc-editor.org/rfc/rfc7239)
- [Consistent hashing](https://en.wikipedia.org/wiki/Consistent_hashing)
