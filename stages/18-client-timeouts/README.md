# Stage 18: Client timeouts

> **Part VI · Time** · **You build:** `gocurl` · **Time:** ~60 min

## Why this stage matters

`http.DefaultClient` waits **forever**. A server that accepts your
connection and never answers will hang your program, and in a service that
calls other services, hung calls pile up until you run out of memory or
goroutines. Go's client has several timeouts at different layers, and they
cover different phases of a request. Choosing the right one, and recognising
a timeout error when you get one, is everyday work.

## Before you read: predict

1. `http.Client{Timeout: 5 * time.Second}`. `Do` returns after 1s with
   headers, and the body takes 10s to arrive. Does the timeout apply to
   reading the body?
2. What's the difference between `Client.Timeout` and a context deadline?
3. How do you tell "the request timed out" from "the connection was refused"?

## Reading

### The phases of a request, and what limits each

```
 dial (TCP)    TLS handshake   write request   wait for headers    read body
│◀ Dialer.Timeout ▶│◀ TLSHandshakeTimeout ▶│     │◀ ResponseHeaderTimeout ▶│
│◀──────────────────── Client.Timeout / context deadline ─────────────────────────────▶│
```

| Setting | Where | Covers |
|---|---|---|
| `http.Client.Timeout` | client | **everything**: dial, TLS, redirects, headers **and reading the body** |
| context deadline | per request (`NewRequestWithContext`) | same as above, per call, and it can also cover your own work around the call |
| `net.Dialer.Timeout` | `Transport.DialContext` | establishing the TCP connection |
| `Transport.TLSHandshakeTimeout` | transport | the TLS handshake (10s in `DefaultTransport`) |
| `Transport.ResponseHeaderTimeout` | transport | from finishing writing the request to receiving the response headers. **Not** the body |
| `Transport.IdleConnTimeout` | transport | how long unused pooled connections are kept (90s in `DefaultTransport`) |

The overall limits (`Client.Timeout`, context) keep running **after `Do`
returns**: if the deadline passes while you're reading the body, the next
`Body.Read` fails. That's usually what you want for API calls, and **exactly
what you don't want for streams** (a live event stream must not die after
10 seconds, which is why stage 20 bounds streams with contexts instead).

```go
// One-off deadline for a single call:
ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
defer cancel()
req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
resp, err := client.Do(req)
```

A custom dial timeout means building a transport. Clone the default one so
you keep its other sensible settings:

```go
tr := http.DefaultTransport.(*http.Transport).Clone()
tr.DialContext = (&net.Dialer{Timeout: 3 * time.Second, KeepAlive: 30 * time.Second}).DialContext
client := &http.Client{Transport: tr}
```

### Recognising timeouts

Timeout errors come in several shapes depending on the layer:

```
Get "http://...": context deadline exceeded (Client.Timeout exceeded while awaiting headers)
context deadline exceeded (Client.Timeout or context cancellation while reading body)
dial tcp 10.255.255.1:80: i/o timeout
```

Two checks catch them all:

```go
func isTimeout(err error) bool {
    var ne net.Error
    return errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout())
}
```

`net.Error` is an interface with a `Timeout() bool` method; `*url.Error`,
`*net.OpError` and the client's internal errors implement it.

### Timeout vs cancellation

A context can end two ways: its **deadline** passes (`context.DeadlineExceeded`),
or someone calls **cancel** (`context.Canceled`). For a CLI, cancellation
usually means Ctrl-C: [`cmd/gocurl/main.go`](../../cmd/gocurl/main.go) uses
`signal.NotifyContext(ctx, os.Interrupt)`, which cancels `ctx` on Ctrl-C. Check
for timeouts **first**; the two are different events and deserve different
messages and exit codes. (By convention a program killed by Ctrl-C, SIGINT, exits 130 = 128 + 2.)

### curl's options

- `-m`/`--max-time SECONDS` (fractions allowed): the whole operation.
  Timeout → exit **28** ("Operation timed out").
- `--connect-timeout SECONDS`: only the connection phase.
- No overall limit by default: neither curl nor Go's client limits the whole
  transfer (though `DefaultTransport` caps dialing at 30s and the TLS
  handshake at 10s).

> **📖 Source dive:** in `$(go env GOROOT)/src/net/http/client.go`, read
> `func setRequestCancel` and how `Client.Timeout` is turned into a
> deadline; then find the text `"Client.Timeout exceeded while awaiting headers"`.

## Your task

| Option / event | Behaviour |
|---|---|
| `-m`, `--max-time SECONDS` | limit the whole transfer, **body included**; fractions like `0.5` allowed. On timeout: stderr `gocurl: (28) ...`, exit **28**. Whatever part of the body arrived is still printed |
| `--connect-timeout SECONDS` | limit only the TCP connect |
| no `-m` | no overall timeout |
| `ctx` cancelled (Ctrl-C) | stop promptly and exit **130** |

```sh
go run ./cmd/course test 18
```

## Hints

<details>
<summary>Hint 1: -m with a context</summary>

At the start of the transfer:
```go
if maxTime > 0 {
    ctx, cancel = context.WithTimeout(ctx, time.Duration(maxTime*float64(time.Second)))
    defer cancel()
}
```
Build the request with that `ctx` and the deadline also covers `io.Copy` of the body.
(`Client.Timeout` works too.)
</details>

<details>
<summary>Hint 2: classifying errors from io.Copy</summary>

The body-copy error goes through the same classification as the `Do` error
(timeout → 28, cancelled → 130, ...). Check timeout before cancellation.
</details>

<details>
<summary>Hint 3: --connect-timeout</summary>

Clone `http.DefaultTransport`, set `DialContext` to a `net.Dialer` with
`Timeout`, and use the transport in your client.
</details>

## Experiments

With gobin running:

1. `go run ./cmd/gocurl -m 1 localhost:8080/delay/3; echo $?`. (Come back
   after stage 19 and try `go run ./cmd/gocurl -m 1 'localhost:8080/stream/5?interval=400ms'`:
   which lines arrive before the timeout?)
2. Run `go run ./cmd/gocurl localhost:8080/delay/10` and press Ctrl-C after a
   second. What does gobin's access log say (stage 17)?
3. `--connect-timeout 2` against an unroutable address such as
   `http://10.255.255.1/` (on most networks packets there vanish). **Predict**
   the error message. What happens without `--connect-timeout`, and why does it
   still eventually fail? (Look at `DefaultTransport`'s dialer.)
4. Write a program using `http.Client{Timeout: time.Second}` to GET a
   response that arrives slowly: an `httptest.NewServer` whose handler writes
   a line, flushes (`http.NewResponseController(w).Flush()`) and sleeps 300ms,
   ten times (or gobin's `/stream/10?interval=300ms` after stage 19). `Do`
   succeeds. Where does it fail?

## Explore further

- Add curl's `-w '%{time_total}\n'`, then `%{time_connect}` and
  `%{time_starttransfer}` using `net/http/httptrace` (stage 25 goes deeper).
- Add `--retry N`: plug in your stage 16 retry transport. Which curl exit
  codes should count as retryable?
- In kvclient, `New` with `hc == nil` sets a timeout. What's a good value for
  an SDK, and should `Watch` (stage 20) use the same client?
- `Transport.ResponseHeaderTimeout` vs `-m`: when would you want only the former?

## Check your understanding

```sh
go run ./cmd/course quiz 18
```

**Explain it back:** Which client timeouts cover reading the response body,
and which don't? Why does that matter for streaming?

## Reference

- [`http.Client`](https://pkg.go.dev/net/http#Client) (`Timeout`), [`http.Transport`](https://pkg.go.dev/net/http#Transport) (`ResponseHeaderTimeout`, `TLSHandshakeTimeout`, `IdleConnTimeout`), [`net.Dialer`](https://pkg.go.dev/net#Dialer)
- [`net.Error`](https://pkg.go.dev/net#Error), [`context.WithTimeout`](https://pkg.go.dev/context#WithTimeout), [`signal.NotifyContext`](https://pkg.go.dev/os/signal#NotifyContext)
- [The complete guide to Go net/http timeouts](https://blog.cloudflare.com/the-complete-guide-to-golang-net-http-timeouts/) (Cloudflare, the client section)

**Next:** [Stage 19: Streaming responses and SSE](../19-streaming/README.md)
