# Stage 21: Files, ranges and uploads

> **Part VII · Streams and files** · **You build:** `gobin` · **Time:** ~90 min

## Why this stage matters

Serving files looks trivial until you need what browsers and download
managers expect: correct content types, caching with `304 Not Modified`,
`Range` requests for resuming and seeking video, no directory listings, no
path traversal. `net/http` does almost all of it, **if** you use
`FileServer` and `ServeContent` instead of `os.ReadFile` + `w.Write`. Then
the reverse: accepting uploads without loading whole files into memory.

## Before you read: predict

1. A browser already has a copy of `/static/app.js`. How can it check whether
   the copy is current without downloading it again?
2. A video player wants bytes 1,000,000–1,999,999 of a file. Which request
   header asks for that, and which status code means "here's the part you asked for"?
3. A user uploads a file named `../../etc/passwd`. Where would a naive server write it?

## Reading

### `http.FileServerFS`

```go
mux.Handle("GET /static/", http.StripPrefix("/static", http.FileServerFS(fsys)))
```

- `fsys` is an `fs.FS`: `os.DirFS("public")` for a directory, an `embed.FS` to
  compile files into your binary, or `fstest.MapFS` in tests. (`FileServerFS`
  is Go 1.22; earlier code uses `http.FileServer(http.FS(fsys))`.)
- `StripPrefix` removes `/static` from the path before the file server sees it.
- It sets `Content-Type` from the file extension (or by sniffing),
  `Content-Length`, and `Last-Modified` from the file's modification time.
- It handles `Range`, `If-Modified-Since` and `HEAD` for you.
- For a directory it serves `index.html` if there is one; **otherwise it
  generates a listing of the directory's contents**. That's often not what
  you want in production.
- It cleans paths, so `../` can't escape the root, and `fs.FS` implementations
  reject such names anyway.

### Hiding directory listings

Wrap the `fs.FS` so that opening a directory without an `index.html` fails:

```go
type noListingFS struct{ fs.FS }

func (n noListingFS) Open(name string) (fs.File, error) {
    f, err := n.FS.Open(name)
    // if f is a directory and name/index.html doesn't exist:
    //     close f, return nil, fs.ErrNotExist   → FileServer answers 404
}
```

Wrapping an interface to change one behaviour is the same move as middleware
and RoundTrippers.

### Conditional requests and `304 Not Modified`

Two kinds of **validators** let clients revalidate their cached copy:

| Validator (response) | Conditional request header | Match → |
|---|---|---|
| `Last-Modified: <date>` | `If-Modified-Since: <date>` | `304 Not Modified`, no body |
| `ETag: "abc123"` | `If-None-Match: "abc123"` | `304 Not Modified`, no body |

An **ETag** is an opaque version identifier, often a hash of the content.
Strong ETags (`"abc"`) mean byte-for-byte identical; weak ones (`W/"abc"`)
mean "equivalent". `If-Match` works the other way round, on writes: "only
update if it's still this version" (else `412 Precondition Failed`), which
prevents lost updates.

### Range requests

```
GET /video.mp4            Range: bytes=1000000-1999999
206 Partial Content       Content-Range: bytes 1000000-1999999/52428800
```

- `bytes=0-4` (first five), `bytes=100-` (from 100 to the end), `bytes=-500` (last 500).
- **`206 Partial Content`** + `Content-Range` when served; **`416 Range Not
  Satisfiable`** if the range starts past the end; a server may also ignore
  `Range` and send a normal `200` with everything.
- `Accept-Ranges: bytes` advertises support.

### `http.ServeContent`: all of it, for any content

For content that isn't a file on disk, like a value in your key-value store:

```go
w.Header().Set("Content-Type", "application/octet-stream") // else guessed from name/sniffed
w.Header().Set("ETag", etag(value))                        // set BEFORE ServeContent
http.ServeContent(w, r, name, modTime, bytes.NewReader(value))
```

`ServeContent` needs an `io.ReadSeeker` (to serve ranges) and handles `Range`,
`If-Modified-Since` (if `modTime` isn't zero), and `If-None-Match`/`If-Match`/`If-Range`
**using the `ETag` header you set**. `FileServer` is built on it.

### Uploads: stream them

`ParseMultipartForm` (stage 6) buffers everything, in memory or temp files,
before your handler sees anything. For uploads, read parts as they arrive:

```go
mr, err := r.MultipartReader()        // error if the body isn't multipart
for {
    part, err := mr.NextPart()
    if err == io.EOF {
        break
    }
    if err != nil { ... }
    part.FormName()                   // the field name
    part.FileName()                   // "" for ordinary fields; already passed through filepath.Base
    io.Copy(dst, part)                // stream: to disk, a hash, object storage...
}
```

Rules for uploads:

- **Limit the total size** with `MaxBytesReader` (the stage 6 helper turns
  the error into 413).
- **Never use a client filename as a path.** `FileName()` strips directories,
  but generate your own storage names anyway.
- **Don't trust the client's `Content-Type`** for a part. Sniff the first
  512 bytes with `http.DetectContentType` if the type matters.
- Reject non-multipart bodies with **`415 Unsupported Media Type`**.

> **📖 Source dive:** `$(go env GOROOT)/src/net/http/fs.go`: read
> `serveContent` and `checkPreconditions`. In what order are `If-Match`,
> `If-Unmodified-Since`, `If-None-Match` and `If-Modified-Since` evaluated? Find
> `dirList` too: that's the listing you're hiding.

## Your task

| Feature | Behaviour |
|---|---|
| `GET /static/...` | serve `Config.Static` (an `fs.FS`) under `/static/`. `nil` → nothing registered (404). Directories without `index.html` → **404**, never a listing. Range, `Last-Modified`/`If-Modified-Since` must work (they will, with `FileServerFS`) |
| `POST /upload` | `multipart/form-data` only (else **415**), at most **10 MiB** (else **413**). Respond `{"files": [...]}` with one entry per **file** part (skip plain fields): `{"field", "filename", "size", "sha256" (hex), "content_type" (sniffed from the first 512 bytes)}` |
| `GET /kv/{key}` | now also sends a strong `ETag` that changes when the value changes, answers a matching `If-None-Match` with **304**, and supports `Range` (**206**). Use `http.ServeContent` |

Add a `-static DIR` flag to `cmd/gobin` (`Static: os.DirFS(dir)`) to try it.

```sh
go run ./cmd/course test 21
```

## Hints

<details>
<summary>Hint 1: an ETag for a value</summary>

```go
sum := sha256.Sum256(value)
return `"` + hex.EncodeToString(sum[:16]) + `"` // quotes are part of the ETag syntax
```
</details>

<details>
<summary>Hint 2: noListingFS details</summary>

`f.Stat()` tells you if it's a directory. Check for an index with
`n.FS.Open(path.Join(name, "index.html"))` (use `path`, not `path/filepath`:
`fs.FS` names always use forward slashes). Close what you open.
</details>

<details>
<summary>Hint 3: hashing and sniffing in one pass</summary>

Read up to 512 bytes with `io.ReadFull(part, head)` (it's fine to get
`io.ErrUnexpectedEOF` or `io.EOF` for small files), hash them, then
`io.Copy(hash, part)` for the rest.
</details>

## Experiments

Run `go run ./cmd/gobin -static .` (serve the repo itself):

1. `curl -I localhost:8080/static/go.mod`. Copy the `Last-Modified` value, then
   `curl -i -H 'If-Modified-Since: <value>' localhost:8080/static/go.mod`.
2. `curl -i -r 0-9 localhost:8080/static/go.mod` and `-r 10000-`. **Predict** both statuses.
3. `curl -i localhost:8080/static/stages/`: listing or 404? Then
   `curl --path-as-is -i localhost:8080/static/../go.mod`.
4. ETags with curl (7.68+):
   ```sh
   curl -X PUT --data-binary v1 localhost:8080/kv/doc
   curl --etag-save etag.txt localhost:8080/kv/doc
   curl -i --etag-compare etag.txt localhost:8080/kv/doc   # 304?
   ```
5. `curl -F file=@go.mod -F file=@README.md localhost:8080/upload` and compare
   with `sha256sum go.mod README.md`.

## Explore further

- **`If-Match` on `PUT /kv/{key}`:** return `412 Precondition Failed` when the
  ETag doesn't match, and treat `If-None-Match: *` as "create only".
- **Cache-Control:** serve `/static/` with `Cache-Control: public, max-age=31536000, immutable`
  for files whose names contain a content hash, and `no-cache` for everything else.
- **Embed** a small site into gobin with `//go:embed site` and serve it at `/`.
- Store uploads on disk under random names, writing to a temp file and
  `os.Rename`-ing it into place when complete. What happens to the temp file
  if the client disconnects halfway?
- Hide dotfiles (`.git`!) in `noListingFS`.

## Check your understanding

```sh
go run ./cmd/course quiz 21
```

**Explain it back:** What does `http.ServeContent` do for you, and why must
the `ETag` header be set *before* calling it?

## Reference

- [`http.FileServerFS`](https://pkg.go.dev/net/http#FileServerFS), [`http.ServeContent`](https://pkg.go.dev/net/http#ServeContent), [`Request.MultipartReader`](https://pkg.go.dev/net/http#Request.MultipartReader)
- [RFC 9110 §13 (conditional requests)](https://www.rfc-editor.org/rfc/rfc9110#section-13), [§14 (range requests)](https://www.rfc-editor.org/rfc/rfc9110#section-14)
- [MDN: HTTP caching](https://developer.mozilla.org/en-US/docs/Web/HTTP/Caching)

**Next:** [Stage 22: Downloads, uploads and resuming](../22-transfers/README.md)
