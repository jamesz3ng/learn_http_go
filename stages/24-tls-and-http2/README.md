# Stage 24: TLS and HTTP/2

> **Part VIII · Production** · **You build:** `gobin` + `gocurl` · **Time:** ~90 min

## Why this stage matters

Everything real runs over HTTPS. Go makes TLS easy to switch on, and
**HTTP/2 comes along for free**, unless a well-meaning tweak quietly turns it
off. On the client side, `x509: certificate signed by unknown authority` is one
of the most-searched Go errors. You'll serve gobin over TLS, then teach
gocurl to trust a custom CA, skip verification (for testing!), report
certificate failures properly, and choose the HTTP version.

## Before you read: predict

1. Your Go client sets `Transport{TLSClientConfig: &tls.Config{...}}` to trust a
   custom CA. Does it still speak HTTP/2?
2. A server's certificate is valid but was issued for `example.com`, and you
   connect to `127.0.0.1`. What fails?
3. What does a Go HTTPS server answer to a plain-HTTP request on its port?

## Reading

### TLS in one paragraph

TLS encrypts the connection and **authenticates the server**: the server
presents a **certificate** binding its public key to names (DNS names or
IPs, in the *Subject Alternative Name* field), signed by a **certificate
authority**. The client checks that the signature chains to a CA it
**trusts** (the OS's root store by default), that the certificate is
in date, and that one of its names matches the host it dialled. For local
development you use a **self-signed** certificate (or your own CA) and tell
the client to trust it explicitly.

Go ships a generator:

```sh
certs="$(mktemp -d)"    # keep keys out of your repo
(cd "$certs" && go run "$(go env GOROOT)/src/crypto/tls/generate_cert.go" --host localhost,127.0.0.1 --ca)
# writes $certs/cert.pem and $certs/key.pem; --ca makes the certificate its own CA
```

([mkcert](https://github.com/FiloSottile/mkcert) is a nicer tool that also
installs a local CA into your browsers.)

### Serving HTTPS

```go
srv.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
err := srv.ServeTLS(ln, "cert.pem", "key.pem") // or srv.ListenAndServeTLS(certFile, keyFile)
```

- In handlers, `r.TLS` is non-nil (that's how stage 4's `/get` knows to say
  `https://`), with the negotiated version, cipher suite and protocol.
- Since Go 1.22 both clients and servers default to TLS 1.2 as the minimum;
  setting it explicitly documents the decision.
- A plain-HTTP request to a TLS port gets `400 Client sent an HTTP request
  to an HTTPS server.`.

### HTTP/2 comes with TLS

Browsers (and Go) only use HTTP/2 over TLS, where it's negotiated during the
handshake by **ALPN** (the client offers `h2` and `http/1.1`, and the server
picks). Go's server and client both support HTTP/2 **automatically**:

- **Server:** `ServeTLS` enables HTTP/2 as long as `srv.TLSNextProto` is
  nil. Your handlers don't change; `r.Proto` becomes `"HTTP/2.0"`.
- **Client:** `http.DefaultTransport` has `ForceAttemptHTTP2: true`. **But a
  `Transport` you create yourself with a custom `TLSClientConfig`, `DialContext`
  or `DialTLSContext` silently falls back to HTTP/1.1** unless you set
  `ForceAttemptHTTP2: true`. Cloning `DefaultTransport` (stage 18) keeps it on.
- To **disable** HTTP/2: a non-nil, empty map,
  `TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}`,
  on the client or server. (Go 1.24 adds a clearer `Protocols` field on both.)

HTTP/2 multiplexes many requests over one connection, which is why a
Go client talking HTTP/2 needs far fewer connections (you'll see that in stage 25).

### Client-side trust

```go
pem, err := os.ReadFile("ca.pem")
pool := x509.NewCertPool()
if !pool.AppendCertsFromPEM(pem) { /* no certificates in the file */ }

tr := http.DefaultTransport.(*http.Transport).Clone()
tr.TLSClientConfig = &tls.Config{RootCAs: pool} // trust ONLY these roots
```

`InsecureSkipVerify: true` turns verification off entirely. Then anyone on
the network path can impersonate the server. Only use it for local testing
(curl's `-k` exists for the same reason, and is just as dangerous).

### Recognising certificate errors

```
tls: failed to verify certificate: x509: certificate signed by unknown authority
tls: failed to verify certificate: x509: certificate is valid for example.com, not 127.0.0.1
tls: failed to verify certificate: x509: certificate has expired or is not yet valid
```

Since Go 1.20 they're all wrapped in a `*tls.CertificateVerificationError`
(which wraps `x509.UnknownAuthorityError`, `x509.HostnameError`,
`x509.CertificateInvalidError`...), so one `errors.As` covers them. Other
handshake failures (protocol mismatch, an alert from the server) surface as
`tls.RecordHeaderError` or `tls.AlertError`.

### Testing with TLS

`httptest.NewTLSServer(h)` (or `NewUnstartedServer` + `EnableHTTP2 = true` +
`StartTLS()`) runs an HTTPS test server with a built-in certificate;
`srv.Client()` returns a client that trusts it, and `srv.Certificate()` gives
you the certificate, e.g. to write a PEM file for `--cacert` as the stage tests do.

> **📖 Source dive:** in `$(go env GOROOT)/src/net/http/transport.go`, find
> `func (t *Transport) onceSetNextProtoDefaults`. Read the conditions under
> which HTTP/2 is **not** configured. That's the "custom TLS config" trap, in code.

## Your task

**gobin:** when `cfg.TLSCertFile` and `cfg.TLSKeyFile` are both set, `Run`
serves **HTTPS** with HTTP/2 available. (Setting only one of them is a
mistake: return an error instead of quietly serving plain HTTP.) Add `-cert` and `-key` flags to
`cmd/gobin`.

**gocurl:**

| Option / case | Behaviour |
|---|---|
| `https://` URLs | verified against the system roots by default |
| certificate verification fails | stderr `gocurl: (60) ...`, exit **60** |
| other TLS handshake failure | exit **35** |
| `-k`, `--insecure` | skip verification |
| `--cacert FILE` | trust the certificates in PEM `FILE` (instead of the system roots). Unreadable file, or no certificates in it → exit **77** |
| HTTP/2 | used whenever the server supports it, **even with** `-k` or `--cacert`. `-i` then prints `HTTP/2.0 200 OK` |
| `--http1.1` | force HTTP/1.1 |

```sh
go run ./cmd/course test 24
```

## Hints

<details>
<summary>Hint 1: gocurl's HTTP/2 test fails with -k</summary>

You set `TLSClientConfig` on a transport without `ForceAttemptHTTP2: true`.
Clone `http.DefaultTransport` (it has it) or set it explicitly.
</details>

<details>
<summary>Hint 2: classifying certificate errors</summary>

```go
var certErr *tls.CertificateVerificationError
if errors.As(err, &certErr) { /* 60 */ }
```
Check it before your generic "couldn't connect" (7) classification: a TLS
failure happens after the TCP connection succeeded.
</details>

<details>
<summary>Hint 3: --http1.1</summary>

`tr.ForceAttemptHTTP2 = false` and
`tr.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}`.
</details>

## Experiments

1. Generate a cert (see Reading) and run
   `go run ./cmd/gobin -addr :8443 -cert "$certs/cert.pem" -key "$certs/key.pem"`.
2. `curl -v https://localhost:8443/get`. **Predict** the error. Then `curl -vk`
   and find the ALPN lines (`ALPN: server accepted h2`). Then `--http1.1`.
3. `curl -i http://localhost:8443/get` (plain HTTP to the TLS port).
4. `go run ./cmd/gocurl -i --cacert "$certs/cert.pem" https://localhost:8443/get`, then
   `https://127.0.0.1:8443/get`. **Predict**: does the second one fail? (Which
   `--host` names did you generate?)
5. `GODEBUG=http2debug=1 go run ./cmd/gocurl -k https://localhost:8443/get`:
   the client's HTTP/2 frames, logged.

## Explore further

- **Redirect HTTP to HTTPS:** run a second listener on `:8080` that `308`s
  everything to `https://`, and add `Strict-Transport-Security` to HTTPS responses.
- **Mutual TLS:** require client certificates (`tls.Config{ClientAuth:
  tls.RequireAndVerifyClientCert, ClientCAs: pool}`) and add `--cert`/`--key`
  to gocurl. Show the client's certificate subject in `/get`.
- **h2c** (HTTP/2 without TLS): Go 1.24's `Server.Protocols` and
  `Transport.Protocols` with `SetUnencryptedHTTP2(true)`. Why don't browsers support it?
- Hot-reload certificates without restarting, using `tls.Config.GetCertificate`.
- Read how `golang.org/x/crypto/acme/autocert` obtains Let's Encrypt certificates automatically.

## Check your understanding

```sh
go run ./cmd/course quiz 24
```

**Explain it back:** Why can creating a custom `http.Transport` turn off HTTP/2,
and what are two ways to keep it on?

## Reference

- [`http.Server.ServeTLS`](https://pkg.go.dev/net/http#Server.ServeTLS), [`crypto/tls.Config`](https://pkg.go.dev/crypto/tls#Config), [`tls.CertificateVerificationError`](https://pkg.go.dev/crypto/tls#CertificateVerificationError)
- [`http.Transport.ForceAttemptHTTP2`](https://pkg.go.dev/net/http#Transport), [`net/http` docs on HTTP/2](https://pkg.go.dev/net/http#hdr-HTTP_2)
- [The Illustrated TLS 1.3 Connection](https://tls13.xargs.org/)

**Next:** [Stage 25: Connection pooling and httptrace](../25-connection-pooling/README.md)
