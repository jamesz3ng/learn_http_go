# Learn Go's `net/http` by building

A [CodeCrafters](https://codecrafters.io)-style course: you learn Go's HTTP
library, **both serving and making requests**, by building real tools, one
small, automatically tested stage at a time.

```
                 ┌──────────────┐  requests   ┌───────────────────────────────┐
  you ──────────▶│ gocurl       │────────────▶│ gobin                         │
                 │ a curl clone │             │ an httpbin.org clone          │
                 └──────────────┘             │ + a key-value store           │
                 ┌──────────────┐             │ + live change streams (SSE)   │
  your code ────▶│ kvclient     │────────────▶│ + files, uploads, TLS, ...    │
                 │ an API SDK   │             └───────────────▲───────────────┘
                 └──────────────┘                             │
                 ┌──────────────┐             ┌───────────────┴───────────────┐
                 │ loadgen      │────────────▶│ proxy                         │
                 │ a hey clone  │             │ a load-balancing reverse proxy│
                 └──────────────┘             └───────────────────────────────┘
```

| You build | What it teaches |
|---|---|
| **rawhttp**: an HTTP server on raw TCP | what `net/http` does for you |
| **gobin**: an [httpbin](https://httpbin.org) clone + key-value store | the server side: handlers, routing, bodies, JSON, status codes, redirects, cookies, auth, middleware, timeouts, context, streaming/SSE, files, graceful shutdown, TLS |
| **gocurl**: a [curl](https://curl.se) clone | the client side: requests, headers, bodies, redirects, cookie jars, timeouts, error classification, downloads/uploads, Range, TLS, HTTP/2 |
| **kvclient**: a Go SDK for your key-value API | API client design, typed errors, `RoundTripper` middleware (auth, retries with backoff), consuming streams |
| **loadgen**: a [hey](https://github.com/rakyll/hey) clone | connection pooling, `Transport` tuning, `httptrace` |
| **proxy**: a load-balancing reverse proxy | `httputil.ReverseProxy`, hop-by-hop headers, failover: client and server at once |

There are **26 stages** plus an orientation. Every stage has:

- **Reading** you need for the task, written into the stage itself (with
  links to the docs and to the standard library's source if you want to go deeper)
- **A precise task** and **automated tests** that tell you exactly what's
  wrong, often with a hint
- **Progressive hints**, hidden until you ask for them
- **Experiments**: predict what will happen, then run it
- **Explore further**: open-ended challenges with no tests
- **A quiz** in the terminal, with spaced-repetition review across the course

## Quick start

You need **Go 1.23 or newer** (`go version`). `curl` is handy for experiments
but optional; you'll build your own.

```sh
git clone https://github.com/jamesz3ng/learn_http_go && cd learn_http_go
go run ./cmd/course          # where am I, what's next?
```

Then open [`stages/00-orientation/README.md`](stages/00-orientation/README.md).

## The loop

```
  read the stage ──▶ go run ./cmd/course test ──▶ red? read the failure + hints ─┐
       ▲                       │ green                                          │
       │                       ▼                                                │
       │            experiments + explore ◀─────────────────────────────────────┘
       │                       │
       │                       ▼
       └──── next stage ◀── go run ./cmd/course quiz N ◀── every few days: go run ./cmd/course review
```

| Command | What it does |
|---|---|
| `go run ./cmd/course` | progress overview and your next step |
| `go run ./cmd/course test` | test stage 1 up to your current stage (earlier stages catch regressions) |
| `go run ./cmd/course test 7` / `3-7` / `all` | test specific stages |
| `go run ./cmd/course test -race` | same, with the race detector (needs cgo) |
| `go run ./cmd/course quiz 7` | take stage 7's quiz |
| `go run ./cmd/course review` | spaced-repetition review of questions that are due |

Under the hood the runner calls `go test ./tests/ -run '^TestStage07_'`, and
you can too. The tests are plain Go: **read them**. They're the precise
spec, and they're worked examples of testing HTTP code with `httptest`.

## Stage map

| # | Stage | You build | Key APIs |
|---|---|---|---|
| 0 | [Orientation: how to learn here, HTTP on the wire](stages/00-orientation/README.md) | — | HTTP/1.1 message format |
| **I** | **Foundations** | | |
| 1 | [HTTP is just text](stages/01-raw-http/README.md) | rawhttp | `net.Listen`, `bufio.Reader` |
| 2 | [Handlers and ServeMux](stages/02-hello-server/README.md) | gobin | `http.Handler`, `HandlerFunc`, `ServeMux`, `httptest` |
| 3 | [Your first request](stages/03-hello-client/README.md) | gocurl | `http.Get`, `Response`, `Body.Close`, error inspection |
| **II** | **Messages** | | |
| 4 | [Reading requests](stages/04-inspecting-requests/README.md) | gobin | `Request`, `URL`, `Header`, `encoding/json` |
| 5 | [Building requests](stages/05-building-requests/README.md) | gocurl | `NewRequestWithContext`, `Client.Do`, `Header.Set/Add`, `req.Host` |
| 6 | [Request bodies on the server](stages/06-reading-bodies/README.md) | gobin | `ParseForm`, `ParseMultipartForm`, `MaxBytesReader`, `mime` |
| 7 | [Sending bodies](stages/07-sending-bodies/README.md) | gocurl | `io.Reader` bodies, `ContentLength`, `GetBody`, `url.Values` |
| **III** | **Resources** | | |
| 8 | [Routing in depth](stages/08-routing/README.md) | gobin | patterns, wildcards, precedence, `PathValue` |
| 9 | [A concurrent REST resource](stages/09-kv-store/README.md) | gobin | status codes, `Location`, `sync.RWMutex`, `-race` |
| 10 | [An API client SDK](stages/10-api-client/README.md) | kvclient | `url.PathEscape`, `JoinPath`, `errors.Is/As`, typed errors |
| **IV** | **Protocol semantics** | | |
| 11 | [Sending redirects](stages/11-redirects/README.md) | gobin | `http.Redirect`, 301/302/303/307/308 |
| 12 | [Following redirects](stages/12-following-redirects/README.md) | gocurl | `CheckRedirect`, `ErrUseLastResponse`, `Request.Response` |
| 13 | [Cookies and authentication](stages/13-cookies-and-auth/README.md) | gobin | `http.Cookie`, `SetCookie`, `BasicAuth`, `crypto/subtle` |
| 14 | [Cookie jars and credentials](stages/14-client-state/README.md) | gocurl | `cookiejar`, `SetBasicAuth` |
| **V** | **Composition** | | |
| 15 | [Middleware](stages/15-middleware/README.md) | gobin | wrapping `ResponseWriter`, `log/slog`, `recover`, context values |
| 16 | [RoundTrippers: client middleware](stages/16-round-trippers/README.md) | kvclient | `http.RoundTripper`, `Request.Clone`, retries, backoff |
| **VI** | **Time** | | |
| 17 | [Server timeouts and cancellation](stages/17-server-timeouts/README.md) | gobin | `Server` timeouts, `r.Context()` |
| 18 | [Client timeouts](stages/18-client-timeouts/README.md) | gocurl | `Client.Timeout`, `context`, `net.Dialer`, `net.Error` |
| **VII** | **Streams and files** | | |
| 19 | [Streaming responses and SSE](stages/19-streaming/README.md) | gobin | `ResponseController`, `Flush`, Server-Sent Events, pub/sub |
| 20 | [Consuming streams](stages/20-consuming-streams/README.md) | kvclient | `bufio.Scanner` over a body, cancellation |
| 21 | [Files, ranges and uploads](stages/21-files/README.md) | gobin | `FileServerFS`, `ServeContent`, ETags, `MultipartReader` |
| 22 | [Downloads, uploads and resuming](stages/22-transfers/README.md) | gocurl | `mime/multipart`, `io.Pipe`, Range, 206/416 |
| **VIII** | **Production** | | |
| 23 | [Graceful shutdown](stages/23-graceful-shutdown/README.md) | gobin | `Server.Shutdown`, `RegisterOnShutdown`, signals |
| 24 | [TLS and HTTP/2](stages/24-tls-and-http2/README.md) | gobin + gocurl | `ServeTLS`, `tls.Config`, `x509`, `ForceAttemptHTTP2` |
| 25 | [Connection pooling and httptrace](stages/25-connection-pooling/README.md) | loadgen | `Transport` pool settings, `httptrace` |
| **IX** | **Capstone** | | |
| 26 | [A load-balancing reverse proxy](stages/26-reverse-proxy/README.md) | proxy | `httputil.ReverseProxy`, hop-by-hop headers, failover |

Also see: [how this course is designed to help you learn](docs/LEARNING.md) ·
[`net/http` cheat sheet](docs/cheatsheet.md) · [where to go next](docs/beyond.md)

## Repository layout

```
stages/NN-*/README.md   the course: one directory per stage
rawhttp/ gobin/ gocurl/ kvclient/ loadgen/ proxy/
                        YOUR code. Starter stubs with TODOs; grow them stage by stage
cmd/                    a small main for each tool, to try your code for real:
                          go run ./cmd/gobin        your server (you'll rewrite this main in stage 23)
                          go run ./cmd/gocurl URL   your curl
                          go run ./cmd/kv list      your SDK from the command line
                          go run ./cmd/rawhttp      stage 1's server
                          go run ./cmd/loadgen URL  your load generator
                          go run ./cmd/proxy ...    your proxy
                        plus cmd/course, the runner (quizzes live in cmd/course/quizzes)
tests/                  the stage tests (the spec). Read them, don't edit them
solutions/              reference solutions. Spoilers! See solutions/README.md
docs/                   learning guide, cheat sheet, next steps
scripts/selfcheck.sh    maintainer check: solutions pass, starter fails
```

## FAQ

**Do I have to do the stages in order?** The server stages (gobin) and
client stages (gocurl, kvclient) are interleaved on purpose, so each concept
is seen from both sides of the wire. Later gobin stages build on earlier
gobin stages, so keep the order within a project. `course test N` tests a
single stage if you want to jump around.

**I'm stuck.** In order: re-read the failure message and its `hint:` line;
read the test function that failed; re-read the stage's Reading section;
open the hints one at a time (give each one 10 minutes); print-debug or use
`curl -v`/your own `gocurl` against `go run ./cmd/gobin`. The reference
solution in `solutions/` is the last resort. If you use it, close it, wait a
little, and re-write the code from memory.

**Can I change the tests?** You can, but they're the spec. If a test seems
wrong, read it carefully: it may be teaching something.

**Where's my progress stored?** `.course/progress.json` (git-ignored by
default; edit `.gitignore` to commit it).

**For maintainers:** `scripts/selfcheck.sh` checks that the reference
solutions pass every stage (with `-race`) and the starter code fails every
stage.
