# Stage 6: Request bodies on the server

> **Part II · Messages** · **You build:** `gobin` · **Time:** ~75 min

## Why this stage matters

Bodies are where clients send you data, and where many server bugs and
denial-of-service holes live. Go gives you the body as a stream plus helpers
for forms and multipart uploads. You have to pick the right one for the
`Content-Type`, and **always limit the size**. gobin gets httpbin's
`/anything`, `/post`, `/put`, `/patch` and `/delete`, which echo back the
body in every format.

## Before you read: predict

1. Can you read `r.Body` twice?
2. A client sends a 5 GB body to a handler that calls `io.ReadAll(r.Body)`.
   What happens to your server?
3. The query string is `?q=1` and the form body is `q=2`. What does
   `r.FormValue("q")` return?

## Reading

### `r.Body` is a stream

```go
r.Body          // io.ReadCloser, never nil on the server (empty bodies read EOF at once)
r.ContentLength // bytes, or -1 if unknown (chunked)
```

It reads straight off the connection, **once**. After you've read it, it's
gone. The server closes it for you after your handler returns. You don't
need to `defer r.Body.Close()` in handlers (but on the *client* side, you must
close `resp.Body`).

### Always limit it

`io.ReadAll(r.Body)` will read whatever the client sends. A client can send a
5 GB body and your server will try to hold 5 GB in memory. Wrap the body
first:

```go
r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MiB
b, err := io.ReadAll(r.Body)
var tooBig *http.MaxBytesError
if errors.As(err, &tooBig) {
    // answer 413 Request Entity Too Large
}
```

`MaxBytesReader` returns an error once the limit is exceeded, and also tells
the server to close the connection after the response, because the rest of
the oversized body is still in the pipe. (It needs `w` for exactly that.)

### What kind of body is it? `mime.ParseMediaType`

`Content-Type` can carry parameters: `application/json; charset=utf-8`,
`multipart/form-data; boundary=----x`. Don't compare the header with `==`:

```go
mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
// mediaType == "multipart/form-data", params["boundary"] == "----x"
```

### URL-encoded forms

`application/x-www-form-urlencoded` is what an HTML `<form>` sends:
`name=gopher&tag=a&tag=b`, encoded like a query string.

```go
if err := r.ParseForm(); err != nil { ... } // reads and parses the body
r.PostForm // url.Values: body values only (for POST, PUT, PATCH)
r.Form     // url.Values: body values AND query-string values (body first)
```

`r.FormValue("q")` and `r.PostFormValue("q")` are shortcuts that parse
automatically, but they **ignore parse errors**, including "body too large".
Call `ParseForm` yourself when you care.

### Multipart forms

`multipart/form-data` is used for file uploads. The body is split into
**parts** separated by a boundary string, each with its own headers:

```
------boundary123
Content-Disposition: form-data; name="title"

hello
------boundary123
Content-Disposition: form-data; name="upload"; filename="notes.txt"
Content-Type: text/plain

file contents
------boundary123--
```

```go
if err := r.ParseMultipartForm(1 << 20); err != nil { ... } // keep up to 1 MiB in memory
r.MultipartForm.Value // map[string][]string: ordinary fields
r.MultipartForm.File  // map[string][]*multipart.FileHeader: Filename, Size, Header, Open()
```

The argument to `ParseMultipartForm` is how much to hold **in memory**;
larger files spill into temporary files on disk, which the server deletes
after your handler returns. It's not a limit on the body size: that's still
`MaxBytesReader`'s job. (Stage 21 streams multipart bodies part by part
instead, which never buffers whole files.)

### JSON bodies

```go
var v any
if err := json.Unmarshal(b, &v); err != nil {
    // answer 400 with the error
}
```

To decode into a struct, use `json.NewDecoder(r.Body).Decode(&dst)`.
For APIs you'll often want `dec.DisallowUnknownFields()` too, so typos in
field names are rejected instead of silently ignored.

### Error responses

From now on, errors are JSON too. Add a helper next to `writeJSON` from stage 4:

```go
func writeError(w http.ResponseWriter, status int, msg string) {
    writeJSON(w, status, map[string]string{"error": msg})
}
```

### Routes for every method, or one

```go
mux.HandleFunc("/anything", h)           // no method: matches ALL methods
mux.HandleFunc("/anything/{path...}", h) // {name...} matches the rest of the path
mux.HandleFunc("POST /post", h)          // GET /post → 405 with "Allow: POST"
```

A `{path...}` wildcard must be the last segment and matches everything after
it, slashes included (stage 8 covers wildcards in depth).

### Reusing `describe(r)` with struct embedding

Embedding a struct flattens its fields into the JSON object:

```go
type bodyInfo struct {
    requestInfo                          // method, args, headers, origin, url
    Data  string              `json:"data"`
    Form  map[string][]string `json:"form"`
    Files map[string][]string `json:"files"`
    JSON  any                 `json:"json"`
}
```

> **📖 Source dive:** `go doc -src net/http.MaxBytesReader` and then look at
> `func (l *maxBytesReader) Read`. How does it tell the server to close the
> connection? (Search `requestTooLarge` in server.go.)

## Your task

Add to gobin:

| Route | Methods |
|---|---|
| `/anything`, `/anything/{path...}` | all |
| `/post` | `POST` only (others get 405) |
| `/put` | `PUT` only |
| `/patch` | `PATCH` only |
| `/delete` | `DELETE` only |

Each responds `200` with JSON: the five fields from `/get` (`method`, `args`,
`headers`, `origin`, `url`) plus:

| Field | Content |
|---|---|
| `data` | the raw body as a string, except for form and multipart bodies, where it's `""` |
| `form` | `application/x-www-form-urlencoded`: the body's fields (**not** the query's). `multipart/form-data`: its non-file fields. Otherwise `{}` |
| `files` | `multipart/form-data`: `{"field": ["filename", ...]}`. Otherwise `{}` |
| `json` | `application/json` (with or without parameters): the parsed body. Otherwise `null` |

Errors (JSON `{"error": "..."}`):

- body larger than **1 MiB** (1,048,576 bytes; exactly 1 MiB is fine) → `413`
- `application/json` that doesn't parse → `400`

```sh
go run ./cmd/course test 6
```

## Hints

<details>
<summary>Hint 1: one handler for all of them</summary>

All six routes can use the same handler function. Wrap the body with
`MaxBytesReader` first, then `switch` on the media type.
</details>

<details>
<summary>Hint 2: a reusable error helper</summary>

```go
func writeBodyError(w http.ResponseWriter, err error) {
    var tooBig *http.MaxBytesError
    if errors.As(err, &tooBig) {
        writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
        return
    }
    writeError(w, http.StatusBadRequest, err.Error())
}
```
Use it for `ParseForm`, `ParseMultipartForm` and `io.ReadAll` errors.
</details>

<details>
<summary>Hint 3: the form contains the query parameter</summary>

You used `r.Form`. Use `r.PostForm`.
</details>

## Experiments

1. `curl -d 'a=1&a=2' 'localhost:8080/post?a=0'`. **Predict** `form` and `args`.
   Then change your handler to use `r.Form` and compare.
2. `curl -F title=hi -F upload=@go.mod localhost:8080/post`. Add `-v`:
   find the boundary in the request's `Content-Type`.
3. `curl --json '{"n": 12345678901234567890}' localhost:8080/post`.
   **Predict** what `json.n` comes back as. (Hint: `any` decodes numbers as
   `float64`. Look up `json.Decoder.UseNumber`.)
4. `head -c 2000000 /dev/zero | curl -v --data-binary @- localhost:8080/post`.
   Look for `Expect: 100-continue` in what curl sends and `HTTP/1.1 100 Continue`
   (or not!) in what comes back. Go's server only sends `100 Continue` when
   your handler starts reading the body.
5. `curl -X GET localhost:8080/post -i`: find the `Allow` header.

## Explore further

- Write `decodeJSON(w, r, &dst) error` for strict API input: 1 MiB limit,
  `DisallowUnknownFields`, reject trailing data (a second `Decode` must
  return `io.EOF`), and turn each `json` error type
  (`*json.SyntaxError`, `*json.UnmarshalTypeError`) into a friendly message.
- Support `Content-Encoding: gzip` request bodies with `compress/gzip`. How
  would you limit the *decompressed* size? (Look up "zip bomb".)
- Return `415 Unsupported Media Type` from `/post` for content types you
  don't understand. Is that better than echoing them as `data`?

## Check your understanding

```sh
go run ./cmd/course quiz 6
```

**Explain it back:** Why must you limit request bodies yourself, and what
does `http.MaxBytesReader` do beyond returning an error?

## Reference

- [`http.MaxBytesReader`](https://pkg.go.dev/net/http#MaxBytesReader), [`Request.ParseForm`](https://pkg.go.dev/net/http#Request.ParseForm), [`Request.ParseMultipartForm`](https://pkg.go.dev/net/http#Request.ParseMultipartForm)
- [`mime.ParseMediaType`](https://pkg.go.dev/mime#ParseMediaType), [`mime/multipart`](https://pkg.go.dev/mime/multipart)
- [How to parse a JSON request body in Go](https://www.alexedwards.net/blog/how-to-properly-parse-a-json-request-body) (Alex Edwards)

**Next:** [Stage 7: Sending bodies](../07-sending-bodies/README.md)
