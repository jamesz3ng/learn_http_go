# Stage 2: Handlers and ServeMux

> **Part I · Foundations** · **You build:** `gobin` · **Time:** ~45 min

## Why this stage matters

Everything on the server side of `net/http` is built from one tiny interface,
`http.Handler`. Routers, middleware, file servers and proxies are all handlers.
Once you see how small it is, the rest of the package is variations on it. In
this stage you start **gobin**, the server you'll grow for the rest of the course.

## Before you read: predict

1. You register a handler for the pattern `"/"`. Which paths does it handle?
2. A route is registered for `GET` only. What should the server answer to a
   `POST` to the same path?
3. Your handler writes a body but never calls `WriteHeader`. What status code
   does the client get?

## Reading

### The `Handler` interface

```go
type Handler interface {
    ServeHTTP(ResponseWriter, *Request)
}
```

That's it. The server parses a request into a `*http.Request`, gives you an
`http.ResponseWriter` to write the response into, and calls `ServeHTTP`.

Writing a whole type for every handler would be tedious, so there's an
adapter:

```go
type HandlerFunc func(ResponseWriter, *Request)

func (f HandlerFunc) ServeHTTP(w ResponseWriter, r *Request) { f(w, r) }
```

`HandlerFunc` is a *function type with a method*. Converting any function
with the right signature, `http.HandlerFunc(myFunc)`, makes it a `Handler`.
This "adapter" pattern shows up again on the client side (stage 16).

### Writing a response

`http.ResponseWriter` has three methods:

```go
w.Header().Set("Content-Type", "text/plain; charset=utf-8") // 1. headers
w.WriteHeader(http.StatusCreated)                           // 2. status line
w.Write([]byte("hello\n"))                                  // 3. body (or io.WriteString, fmt.Fprintf...)
```

The order matters because that's the order they go on the wire:

- **Headers must be set before** `WriteHeader` (or the first `Write`). Changes
  afterwards are silently ignored: the headers are already sent.
- If you call `Write` without `WriteHeader`, Go sends **`200 OK`** for you.
- Calling `WriteHeader` twice logs `http: superfluous response.WriteHeader call`
  and the second call has no effect.
- If you don't set `Content-Type`, the first `Write` **sniffs** it from the
  first 512 bytes (`http.DetectContentType`). Handy, but set it explicitly
  for anything that isn't HTML or plain text.
- If your handler writes less than a couple of KB and returns, Go computes
  `Content-Length` for you. Otherwise it uses chunked encoding.

### `ServeMux`: the router

An `http.ServeMux` is a `Handler` that looks at the request and dispatches to
another handler. Since Go 1.22, patterns can include a **method** and
**wildcards**:

```go
mux := http.NewServeMux()
mux.HandleFunc("GET /healthz", healthz)      // only GET (and HEAD)
mux.HandleFunc("POST /items", createItem)
mux.HandleFunc("GET /items/{id}", getItem)   // r.PathValue("id"), stage 8
mux.HandleFunc("GET /{$}", home)             // exactly "/", nothing else
```

Three rules you need today:

1. **A pattern ending in `/` matches the whole subtree.** `"/"` matches
   *every* path, and `"/static/"` matches everything under `/static/`. To match
   only the root, use `"/{$}"` (`{$}` means "end of path").
2. **A method in the pattern restricts it.** `GET` also matches `HEAD` (the
   server discards the body for you). If a path matches but the method
   doesn't, the mux answers **405 Method Not Allowed** with an `Allow` header,
   no code needed.
3. **No match → 404.** The mux uses `http.NotFound`, which writes
   `404 page not found`.

### Don't use the global mux

`http.HandleFunc(...)` (the package-level function) registers on
`http.DefaultServeMux`, a global. Globals make code hard to test: registering
the same pattern twice **panics**, so you couldn't create two servers in one
test run. Create your own with `http.NewServeMux()` instead. Any package you
import could also register handlers on the default mux. `net/http/pprof`
does exactly that.

### Starting the server

```go
log.Fatal(http.ListenAndServe(":8080", handler))
```

`ListenAndServe` creates an `http.Server`, listens, and runs `Serve`, the
accept loop you wrote in stage 1. It **blocks** forever and only returns an
error. (You'll replace this with a properly configured server in stages 17 and 23.)
[`cmd/gobin/main.go`](../../cmd/gobin/main.go) already does this with your
`gobin.NewHandler`.

### Testing handlers with `httptest`

Two tools you'll see in every server test:

```go
// 1. Call the handler directly: no network, fast, precise.
rec := httptest.NewRecorder()
req := httptest.NewRequest("GET", "/healthz", nil)
handler.ServeHTTP(rec, req)
// rec.Code, rec.Header(), rec.Body.String()

// 2. A real server on a random localhost port, for end-to-end tests.
srv := httptest.NewServer(handler)
defer srv.Close()
resp, err := http.Get(srv.URL + "/healthz")
```

Open [`tests/stage02_hello_server_test.go`](../../tests/stage02_hello_server_test.go)
and find both.

> **📖 Source dive:** run `go doc -src net/http.HandlerFunc.ServeHTTP` (it's
> one line). Then look at `func (mux *ServeMux) ServeHTTP` in
> `$(go env GOROOT)/src/net/http/server.go`: the router finds a handler and
> calls its `ServeHTTP`. It's handlers all the way down.

## Your task

In [`gobin/gobin.go`](../../gobin/gobin.go), make `NewHandler` return a mux with:

| Request | Response |
|---|---|
| `GET /` | `200`, `Content-Type: text/plain; charset=utf-8`, body `Hello, HTTP!\n` |
| `GET /healthz` | `200`, body `ok\n` |
| `HEAD /` | `200`, no body (free with a `GET` pattern) |
| `POST /` (or any other method) | `405`, with `Allow` listing `GET` (free with a `GET` pattern) |
| anything else, e.g. `/nope`, `/healthz/extra` | `404` |

`NewHandler` must build a **new** mux on every call (the tests create many
independent servers).

```sh
go run ./cmd/course test 2
go run ./cmd/gobin            # then: curl -i localhost:8080/
```

## Hints

<details>
<summary>Hint 1: where things go</summary>

Inside `NewHandler`, call `mux.HandleFunc(pattern, handlerFunction)` for each
route. Handler functions can be plain functions:
`func healthz(w http.ResponseWriter, r *http.Request) { ... }`.
</details>

<details>
<summary>Hint 2: /nope returns 200</summary>

You registered `"/"` or `"GET /"`. That's a subtree pattern matching
everything. Use `"GET /{$}"`.
</details>

<details>
<summary>Hint 3: the 405 test fails</summary>

Is the method in the pattern (`"GET /{$}"`)? Without it, the handler runs for
every method.
</details>

## Experiments

Run `go run ./cmd/gobin` and use `curl -i` (shows status line and headers):

1. **Predict** the headers of `curl -i localhost:8080/`. Which did Go add that
   you didn't set? (Look for `Date` and `Content-Length`.)
2. `curl -i -X POST localhost:8080/`: find the `Allow` header.
3. `curl -i localhost:8080/healthz/` (trailing slash). **Predict**: 404 or
   200? Then register `"GET /healthz/"` instead and try `curl -i localhost:8080/healthz`.
   You'll get a redirect. Why? (Stage 8 explains.)
4. Temporarily remove your `Content-Type` line and write `<h1>hi</h1>`.
   **Predict** the `Content-Type` Go sends. Then write `{"a":1}`. JSON isn't
   sniffed. What do you get?
5. Call `w.Header().Set("X-Late", "1")` *after* `Write`. Does `curl -i` show it?

## Explore further

- Register a handler on `http.DefaultServeMux` from two different tests in
  the same package and watch it panic. Read the panic message: Go tells you
  exactly which registrations conflict.
- Write a handler as a **struct with a method** (`type counter struct{ n atomic.Int64 }`
  with `ServeHTTP`) that counts its requests. When is a struct handler better
  than a closure?
- What does `http.NotFoundHandler()` return? What about
  `http.RedirectHandler` and `http.StripPrefix`? Each is a `Handler`
  constructor. Read their source.

## Check your understanding

```sh
go run ./cmd/course quiz 2
```

**Explain it back:** What is `http.HandlerFunc`, and why can a plain function
be used where an `http.Handler` is required?

## Reference

- [`http.Handler`](https://pkg.go.dev/net/http#Handler), [`http.ServeMux`](https://pkg.go.dev/net/http#ServeMux) (the doc comment describes the pattern syntax in full)
- [`net/http/httptest`](https://pkg.go.dev/net/http/httptest)
- [Routing enhancements for Go 1.22](https://go.dev/blog/routing-enhancements) (Go blog)

**Next:** [Stage 3: Your first request](../03-hello-client/README.md)
