# Where to go next

You've built servers, clients, middleware, streams, a load tester and a proxy
with nothing but the standard library. Some directions to go deeper. Each
makes a good project of its own.

## Deepen what you built

- **Finish the "Explore further" sections** you skipped. A few with
  outsized payoff: resumable SSE with `Last-Event-ID` (19 + 20), a caching
  `RoundTripper` against gobin's ETags (16 + 21), consistent hashing in the proxy (26).
- **Read `net/http` end to end.** Now that you know what each piece does,
  `server.go`, `client.go`, `transport.go` and `httputil/reverseproxy.go` read
  like a story. Look for the edge cases they handle that the course didn't.
- **Write the proxy without `httputil`**, then run the stage 26 tests against it.
- **Fuzz your parsers.** `go test -fuzz` on rawhttp's request parsing and
  kvclient's SSE parser. What does the fuzzer find?

## Protocols the standard library leaves to others

- **WebSockets:** `http.ResponseController.Hijack` / `EnableFullDuplex` give
  you the raw connection. Implement the handshake (RFC 6455) yourself, then
  compare with `github.com/coder/websocket`.
- **HTTP/3** over QUIC: `github.com/quic-go/quic-go`.
- **gRPC** runs on HTTP/2; `connectrpc.com/connect` is built on plain `net/http`
  handlers, so you'll recognise its internals.

## Production concerns

- **Observability:** OpenTelemetry's `otelhttp` wraps handlers and transports
  (middleware and RoundTrippers, again!) to produce traces and metrics.
- **Rate limiting:** `golang.org/x/time/rate`, per client, as middleware.
- **Security headers and CSRF:** Content-Security-Policy, `Strict-Transport-Security`,
  and Go 1.25's `http.CrossOriginProtection`.
- **Automatic certificates:** `golang.org/x/crypto/acme/autocert`.
- **Profiling:** `net/http/pprof` (register its handlers on a private mux!) and `go tool pprof`.

## Further reading

- *Let's Go* and *Let's Go Further* by Alex Edwards: building web apps and
  APIs with the standard library.
- *HTTP: The Definitive Guide* (Gourley & Totty): old, but the protocol hasn't changed much.
- RFCs [9110](https://www.rfc-editor.org/rfc/rfc9110) (semantics),
  [9111](https://www.rfc-editor.org/rfc/rfc9111) (caching),
  [9112](https://www.rfc-editor.org/rfc/rfc9112) (HTTP/1.1),
  [9113](https://www.rfc-editor.org/rfc/rfc9113) (HTTP/2).
- The Go blog: [HTTP tracing](https://go.dev/blog/http-tracing),
  [routing enhancements](https://go.dev/blog/routing-enhancements),
  [structured logging](https://go.dev/blog/slog).

And keep your knowledge fresh: `go run ./cmd/course review`.
