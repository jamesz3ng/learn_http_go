# Stage 8: Routing in depth

> **Part III · Resources** · **You build:** `gobin` · **Time:** ~60 min

## Why this stage matters

Since Go 1.22 the standard `ServeMux` handles methods and path wildcards,
enough routing for most services without a third-party router. To use it
with confidence you need its rules: what a wildcard matches, which pattern
wins when several match, when it panics at startup, and the redirects it
issues on its own. gobin gains httpbin's parameterised endpoints.

## Before you read: predict

1. Both `GET /status/{code}` and `GET /status/teapot` are registered. Which
   handles `/status/teapot`? Does registration order matter?
2. What does `r.PathValue("key")` return for a request to `/kv/a%2Fb`
   (pattern `/kv/{key}`)?
3. `"GET /"` and `"/index.html"` are both registered. What happens?

## Reading

### Pattern syntax

```
[METHOD ][HOST]/[PATH]
```

| Pattern | Matches |
|---|---|
| `/index.html` | that path, any method, any host |
| `GET /static/` | GET/HEAD for anything **under** `/static/` (trailing slash = subtree) |
| `GET /{$}` | GET/HEAD for exactly `/` |
| `GET /items/{id}` | one segment: `/items/42`, but not `/items/42/x` |
| `/files/{path...}` | the rest of the path, slashes included (only allowed last) |
| `example.com/` | any request whose host is `example.com` |
| `POST /b/{bucket}/o/{object...}` | Google Cloud Storage-style paths |

- Wildcards must be **whole segments**: `/items/{id}` is valid,
  `/items_{id}` panics.
- Literal parts match **case-sensitively**.
- `r.PathValue("id")` returns the matched text; `""` if no such wildcard.

### Escapes inside segments

Matching happens **segment by segment after unescaping**. A `%2F` is an
escaped slash *inside* a segment, not a separator:

```
pattern  /kv/{key}
request  /kv/a%2Fb   → matches, r.PathValue("key") == "a/b"
request  /kv/a/b     → does NOT match (two segments)
```

That's how a key containing `/` can live in a single path segment, as long
as the client escapes it (stage 10).

### Precedence: most specific wins

When several patterns match, the **most specific** one wins. P1 is more
specific than P2 if P1 matches a strict subset of P2's requests:

- `GET /status/teapot` beats `GET /status/{code}` (a literal is more specific
  than a wildcard).
- `GET /items/{id}` beats `/items/{id}` (a method narrows it).
- `/images/thumbnails/` beats `/images/`.

**Registration order never matters.** If neither of two patterns is more
specific than the other, they **conflict**, and `Handle`/`HandleFunc`
**panics** when you register the second. The panic happens at startup, not
when the conflicting request arrives:

```go
mux.HandleFunc("GET /", a)
mux.HandleFunc("/index.html", b) // panic: GET /index.html would match both, neither is more specific
```

(For backwards compatibility, if one of two conflicting patterns has a host
and the other doesn't, the one with the host wins instead of panicking.)

### Things the mux does on its own

- **405 Method Not Allowed** with an `Allow` header when the path matches
  but the method doesn't (stage 2).
- **Trailing-slash redirect:** with `/docs/` registered and no `/docs`, a
  request for `/docs` gets **301** to `/docs/`.
- **Path cleaning:** `/a/../get` or `//get` gets **301** to the clean path
  `/get`.
- **404** when nothing matches.

Go 1.21 and earlier had a much simpler mux (no methods, no wildcards).
`GODEBUG=httpmuxgo121=1` restores the old behaviour. Mostly you'll just
notice the old style in older codebases.

### Validating path values

A wildcard matches any text, so `/status/{code}` also matches `/status/abc`.
Parse and validate, and answer **400 Bad Request** when the input is bad:

```go
code, err := strconv.Atoi(r.PathValue("code"))
if err != nil || code < 200 || code > 599 {
    writeError(w, http.StatusBadRequest, "status code must be an integer between 200 and 599")
    return
}
w.WriteHeader(code)
```

### Base64 flavours

`encoding/base64` has four encodings: `StdEncoding` (`+` `/`, padded with
`=`), `URLEncoding` (`-` `_`, padded), and the `Raw` variants of each without
padding. URL-safe base64 is designed for exactly this, data in a URL.

> **📖 Source dive:** `$(go env GOROOT)/src/net/http/pattern.go` has the
> pattern parser and `func (p1 *pattern) comparePaths`, which decides "more
> specific". `routing_tree.go` is the matcher. Both are short and well commented.

## Your task

Add these `GET` endpoints:

| Endpoint | Response |
|---|---|
| `/status/{code}` | status `code` (an integer 200–599), empty body. Anything else → `400` JSON error |
| `/status/teapot` | `418`, body containing `teapot` (e.g. `I'm a teapot`) |
| `/bytes/{n}` | `n` random bytes (0 ≤ n ≤ 102400), `Content-Type: application/octet-stream`. Invalid `n` → `400` |
| `/base64/{value}` | `value` decoded as **URL-safe** base64 (with or without `=` padding), as `text/plain`. Invalid → `400` |
| `/uuid` | `{"uuid": "..."}`: a random **version 4** UUID in lower-case hex, `xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx` where `y` is one of `8`,`9`,`a`,`b` |

```sh
go run ./cmd/course test 8
```

## Hints

<details>
<summary>Hint 1: random bytes</summary>

`crypto/rand.Read(b)` fills a slice with random bytes (it never fails on
supported platforms). For `/bytes/{n}` you can set `Content-Length` yourself
with `strconv.Itoa(n)`.
</details>

<details>
<summary>Hint 2: padded or unpadded base64</summary>

`strings.TrimRight(value, "=")` then `base64.RawURLEncoding.DecodeString`.
</details>

<details>
<summary>Hint 3: version 4 UUIDs</summary>

16 random bytes; then set the version and variant bits:
`b[6] = b[6]&0x0f | 0x40` and `b[8] = b[8]&0x3f | 0x80`. Format with
`fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])`.
</details>

## Experiments

1. Register `"/index.html"` alongside your `"GET /{$}"`. Does it panic? Now
   try `"GET /"` with `"/index.html"`. Read the panic message carefully.
2. **Predict** the status and `Location` for each, then check with `curl -i`:
   `/get/`, `//get`, `/x/../get`, `/status/%32%30%31`.
3. Add a temporary handler `GET /debug/{rest...}` that prints
   `r.PathValue("rest")`. Request `/debug/a%2Fb/c`. Predict the output.
4. `curl -i localhost:8080/status/204` vs `/status/200`. Does either
   response have a `Content-Length`?

## Explore further

- `/status/{codes}` in httpbin accepts `200,404,500` and picks one at random.
  Implement it, and support weights (`200:0.9,500:0.1`).
- Host routing: register `"admin.localhost/"` for an admin page and try
  `curl -H 'Host: admin.localhost' localhost:8080/`.
- Write a JSON 404 handler. With only `ServeMux`, how can you replace its
  plain-text 404 **without** losing the automatic 405s? (Registering `"/"`
  catches everything, including wrong methods. Try it and see.)
- Benchmark `ServeMux` with 1,000 routes using `testing.B`. How does lookup
  time grow with the number of routes?

## Check your understanding

```sh
go run ./cmd/course quiz 8
```

**Explain it back:** How does `ServeMux` choose between two matching patterns,
and why can registering a route panic?

## Reference

- [`http.ServeMux`](https://pkg.go.dev/net/http#ServeMux): the doc comment *is* the specification; read all of it
- [Routing enhancements for Go 1.22](https://go.dev/blog/routing-enhancements)
- [`encoding/base64`](https://pkg.go.dev/encoding/base64), [RFC 9562: UUIDs](https://www.rfc-editor.org/rfc/rfc9562)

**Next:** [Stage 9: A concurrent REST resource](../09-kv-store/README.md)
