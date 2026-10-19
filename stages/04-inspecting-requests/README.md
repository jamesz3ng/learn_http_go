# Stage 4: Reading requests

> **Part II · Messages** · **You build:** `gobin` · **Time:** ~60 min

## Why this stage matters

A handler's first job is to understand the request it got: method, path,
query string, headers, who sent it. `*http.Request` has all of that, but a few
fields aren't where you'd expect (the `Host` header isn't in `Header`, and
`r.URL` has no host). In this stage gobin gets httpbin's "tell me what you
saw" endpoints, which you'll use for the rest of the course to see what
clients actually send.

## Before you read: predict

1. A client sends the header `x-request-source: cli`. Under which key will it
   be in `r.Header`?
2. On the server, what is `r.URL.Host` for a request to `http://localhost:8080/get`?
3. How do you get both values of `?tag=a&tag=b`?

## Reading

### `*http.Request` on the server

| Field | Example | Notes |
|---|---|---|
| `r.Method` | `"GET"` | |
| `r.URL` | `*url.URL` | parsed from the request target: usually **only Path and RawQuery** |
| `r.URL.Path` | `"/get"` | percent-**decoded** |
| `r.URL.RawQuery` | `"a=1&a=2"` | still encoded |
| `r.RequestURI` | `"/get?a=1&a=2"` | the raw target, exactly as sent |
| `r.Proto` | `"HTTP/1.1"` | `"HTTP/2.0"` over HTTP/2 |
| `r.Header` | `http.Header` | everything **except** `Host` |
| `r.Host` | `"localhost:8080"` | the `Host` header (or HTTP/2 `:authority`) |
| `r.RemoteAddr` | `"127.0.0.1:52341"` | the peer's IP **and port** |
| `r.TLS` | `*tls.ConnectionState` | non-nil only over HTTPS |
| `r.Body`, `r.ContentLength` | | stage 6 |

> On the **client** side the same `http.Request` type means something else:
> there `r.URL` is the full URL to fetch, and `RequestURI` must be empty. One
> type, two roles, so always read a field's doc comment for the side you're on.

### Query strings: `url.Values`

```go
q := r.URL.Query()     // url.Values, which is a map[string][]string
q.Get("tag")           // "a": the FIRST value, or "" if absent
q["tag"]               // []string{"a", "b"}: all of them
q.Has("debug")         // true for "?debug" and "?debug="
```

Values are decoded for you: `?b=x%20y` and `?b=x+y` both give `"x y"`.
`Query()` parses `RawQuery` each time you call it (and silently drops
malformed pairs), so call it once and keep the result.

### Headers: `http.Header`

```go
type Header map[string][]string
```

Header names are case-insensitive, so Go stores them in **canonical form**:
the first letter and each letter after a hyphen upper-cased, the rest
lower-cased. `x-request-source` becomes `X-Request-Source`. The methods
canonicalise for you:

```go
r.Header.Get("content-type")     // first value ("" if absent): works with any case
r.Header.Values("X-Multi")       // all values
r.Header["Content-Type"]         // direct map access: you must use the canonical key yourself
http.CanonicalHeaderKey("x-foo") // "X-Foo"
```

A header can appear more than once (each occurrence is another element in the
slice). `r.UserAgent()` is a shortcut for `r.Header.Get("User-Agent")`.

**`Host` is special.** The server moves it out of `r.Header` into `r.Host`, so
`r.Header.Get("Host")` is always `""`.

### Who is the client? `RemoteAddr`

`r.RemoteAddr` is `"ip:port"` from the TCP connection, e.g. `"127.0.0.1:52341"`
or, for IPv6, `"[::1]:52341"`. Use `net.SplitHostPort` to split it. It handles
the brackets.

You'll see `X-Forwarded-For` headers set by proxies to pass on the original
client's IP. Only trust them if **your own** proxy sets them; otherwise
any client can claim any IP by sending the header.

### The full URL

Because `r.URL` on the server usually holds only path and query, rebuild the
absolute URL from pieces:

```go
scheme := "http"
if r.TLS != nil {
    scheme = "https"
}
full := scheme + "://" + r.Host + r.URL.RequestURI() // RequestURI = path + "?" + query
```

### Writing JSON

```go
func writeJSON(w http.ResponseWriter, status int, v any) {
    w.Header().Set("Content-Type", "application/json") // 1. headers
    w.WriteHeader(status)                              // 2. status
    json.NewEncoder(w).Encode(v)                       // 3. body
}
```

- Set `Content-Type` **before** `WriteHeader`. JSON isn't content-sniffed
  (Go would send `text/plain`).
- `json.NewEncoder(w).Encode(v)` writes straight into the response;
  `json.Marshal` gives you a `[]byte` first (useful if you want to report an
  encoding error as a 500 *before* committing to a status).
- If `Encode` fails halfway, the status is already sent. There's no taking it
  back. Encoding your own well-formed structs won't fail; client
  disconnection is the usual cause.
- A Go `nil` map or slice encodes as `null`; an empty one as `{}`/`[]`.
- Struct tags pick JSON names: `UserAgent string \`json:"user-agent"\``.

One helper used everywhere keeps responses consistent. You'll reuse
`writeJSON` (and a `describe(r)` function for the request details) in many
later stages.

> **📖 Source dive:** `go doc -src net/textproto.CanonicalMIMEHeaderKey`.
> What happens to a header name containing a space or other invalid character?

## Your task

Add these `GET` endpoints to gobin. All respond `200` with
`Content-Type: application/json`:

| Endpoint | JSON body |
|---|---|
| `/get` | `{"method", "args", "headers", "origin", "url"}` |
| `/headers` | `{"headers": {...}}` |
| `/ip` | `{"origin": "..."}` |
| `/user-agent` | `{"user-agent": "..."}` |

- **args**: every query parameter → an **array** of all its values, e.g.
  `{"a": ["1", "2"], "b": ["x y"]}`; `{}` when there's no query.
  (Real httpbin collapses single values to strings; we keep arrays for consistency.)
- **headers**: every request header → array of values, canonical names,
  **including `Host`**.
- **origin**: the client's IP from `RemoteAddr`, without the port.
- **url**: the absolute URL, e.g. `http://127.0.0.1:53211/get?a=1`.

```sh
go run ./cmd/course test 4
```

## Hints

<details>
<summary>Hint 1: one struct for /get</summary>

```go
type requestInfo struct {
    Method  string              `json:"method"`
    Args    map[string][]string `json:"args"`
    Headers map[string][]string `json:"headers"`
    Origin  string              `json:"origin"`
    URL     string              `json:"url"`
}
```
`url.Values` and `http.Header` are both `map[string][]string`, so they
convert directly.
</details>

<details>
<summary>Hint 2: adding Host without mutating the request</summary>

`h := r.Header.Clone()` then `h["Host"] = []string{r.Host}`. Avoid modifying
`r.Header` in place: middleware or other handlers may read it later.
</details>

<details>
<summary>Hint 3: "args" is null</summary>

Can't happen with `r.URL.Query()` (it always returns a non-nil map). If you
build the map yourself, start from `map[string][]string{}`, not `nil`.
</details>

## Experiments

Run `go run ./cmd/gobin`, then with curl (or your gocurl):

1. `curl 'localhost:8080/get?a=1&a=2&b=x%20y&c=x+y'`. **Predict** `b` and `c`.
2. `curl -H 'x-lower-case: 1' -H 'X-Dup: 1' -H 'X-Dup: 2' localhost:8080/headers`.
   **Predict** how the names appear.
3. `curl -H 'Host: example.test' localhost:8080/get`. **Predict** `url`.
4. `curl -H 'X-Forwarded-For: 6.6.6.6' localhost:8080/ip`. Does `origin`
   change? Should it?
5. If your machine has IPv6: `curl -g 'http://[::1]:8080/ip'`.
6. Compare with the real thing: `curl https://httpbin.org/get?a=1`.

## Explore further

- Add a `?pretty` flag that indents the JSON (`enc.SetIndent`), or make
  gobin indent always, like httpbin.
- **Content negotiation:** if the request's `Accept` header prefers
  `text/plain`, answer in plain text instead of JSON.
- Since Go 1.23, `r.Pattern` holds the mux pattern that matched. Add it to
  `/get`'s output. What does it contain when a request reaches a 404?
- Add a `TrustedProxies` notion: honour `X-Forwarded-For` only when
  `RemoteAddr` is in a configured list. What about `X-Forwarded-Proto` in `url`?

## Check your understanding

```sh
go run ./cmd/course quiz 4
```

**Explain it back:** Why is the `Host` header not in `r.Header`, and why
doesn't `r.URL` contain the host on the server side?

## Reference

- [`http.Request`](https://pkg.go.dev/net/http#Request): read the comments on `URL`, `Host`, `RequestURI` and `RemoteAddr`
- [`url.Values`](https://pkg.go.dev/net/url#Values), [`http.Header`](https://pkg.go.dev/net/http#Header), [`net.SplitHostPort`](https://pkg.go.dev/net#SplitHostPort)
- [JSON and Go](https://go.dev/blog/json) (Go blog)

**Next:** [Stage 5: Building requests](../05-building-requests/README.md)
