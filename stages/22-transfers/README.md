# Stage 22: Downloads, uploads and resuming

> **Part VII · Streams and files** · **You build:** `gocurl` · **Time:** ~90 min

## Why this stage matters

The client side of stage 21: saving downloads to files, uploading files as
multipart forms without loading them into memory, asking for byte ranges,
and **resuming** a broken download where it stopped. Each one has a decision
that's easy to get wrong: append or truncate? Stream or buffer? What if the
server ignores your `Range`?

## Before you read: predict

1. You resume a download by sending `Range: bytes=40-`, and the server answers
   `200 OK` with the whole file. If you append it to your partial file, what do you get?
2. `multipart.Writer.CreateFormFile` sets which `Content-Type` on the part?
3. What's the cost of streaming an upload through an `io.Pipe` instead of
   building it in a `bytes.Buffer`?

## Reading

### Writing the body to a file

```go
f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644) // like os.Create
// or, to add to the end of an existing file:
f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
defer f.Close()
_, err = io.Copy(f, resp.Body)
```

When `io.Copy` fails, was it the network (read) or the disk (write)? curl
reports them differently (`56` vs `23`). A tiny wrapper that tags write
errors lets you tell them apart afterwards with `errors.As`:

```go
type writeError struct{ err error }
func (e *writeError) Error() string { return e.err.Error() }

type errWriter struct{ w io.Writer }
func (e errWriter) Write(p []byte) (int, error) {
    n, err := e.w.Write(p)
    if err != nil {
        return n, &writeError{err}
    }
    return n, nil
}
```

### Building multipart forms

```go
var buf bytes.Buffer
mw := multipart.NewWriter(&buf)
mw.WriteField("title", "hello")         // a plain field
fw, _ := mw.CreateFormFile("image", "pic.png")
io.Copy(fw, file)                       // the file's bytes
mw.Close()                              // writes the final boundary: don't forget it!

req, _ := http.NewRequestWithContext(ctx, "POST", url, &buf)
req.Header.Set("Content-Type", mw.FormDataContentType()) // includes the boundary
```

`CreateFormFile` always labels the part `application/octet-stream`. To send a
real type, build the part header yourself:

```go
h := textproto.MIMEHeader{}
h.Set("Content-Disposition", `form-data; name="image"; filename="pic.png"`) // escape quotes in names!
h.Set("Content-Type", mime.TypeByExtension(".png"))                         // "image/png"
w, _ := mw.CreatePart(h)
```

`mime.TypeByExtension` knows a small built-in table plus your OS's MIME
database, and returns `""` for unknown extensions. Fall back to
`application/octet-stream`.

### Streaming an upload with `io.Pipe`

Building the form in a `bytes.Buffer` holds every file in memory. An
`io.Pipe` connects a writer goroutine to the reader the HTTP client
consumes, so data flows straight from disk to network:

```go
pr, pw := io.Pipe()
mw := multipart.NewWriter(pw)
go func() {
    err := writeParts(mw) // WriteField, CreatePart, io.Copy from files...
    if err == nil {
        err = mw.Close()
    }
    pw.CloseWithError(err) // nil → reader sees EOF; non-nil → the request fails
}()
req, _ := http.NewRequestWithContext(ctx, "POST", url, pr)
```

The trade-off (stage 7): a pipe has **no known length** (sent chunked) and
**no `GetBody`** (a 307 or a retry can't resend it). Check that the files
exist *before* starting, so a missing file fails cleanly (curl exits `26`)
instead of halfway through the upload.

### Ranges and resuming

`-r 0-99` just sets `Range: bytes=0-99`. Resuming (`-C -`) is a small
protocol of its own:

```
partial file is 40 bytes  →  Range: bytes=40-
   206 Partial Content    →  APPEND the body to the file
   200 OK                 →  server ignored Range: TRUNCATE and write the whole body
   416 Range Not Satisfiable → nothing left: the file is already complete
```

Getting the 200 case wrong silently corrupts files: a common bug in
hand-rolled download resumers. (A careful resumer also sends `If-Range` with
the ETag or date it saw before, so the server sends the whole file if it
changed in between. See Explore.)

> **📖 Source dive:** `go doc -src mime/multipart.Writer.CreateFormFile` and
> `go doc -src io.Pipe`. How does `CloseWithError` reach the reader?

## Your task

| Option | Behaviour |
|---|---|
| `-o`, `--output FILE` | write the body to FILE (create or **truncate**) instead of stdout. Can't open it → exit **23** |
| `-F`, `--form name=value` / `name=@path` | repeatable; `POST` a `multipart/form-data` body: plain fields, and files with `filename` = the path's base name and `Content-Type` from the extension (`application/octet-stream` if unknown). Missing file → exit **26** |
| `-r`, `--range RANGE` | send `Range: bytes=RANGE` |
| `-C -`, `--continue-at -` | with `-o`, resume: if FILE exists with size N > 0, send `Range: bytes=N-`; `206` → append; `200` → overwrite; `416` → leave the file alone, exit `0`. No file → normal download, no `Range`. `-C` without `-o` → exit **2** |

```sh
go run ./cmd/course test 22
```

## Hints

<details>
<summary>Hint 1: open the output after the response</summary>

Whether to append or truncate depends on the status code, so open the file
*after* `Do` returns. Headers printed with `-i` go to the same output.
</details>

<details>
<summary>Hint 2: Content-Disposition escaping</summary>

```go
var quoteEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`)
fmt.Sprintf(`form-data; name="%s"; filename="%s"`, quoteEscaper.Replace(name), quoteEscaper.Replace(filepath.Base(path)))
```
(This mirrors what `mime/multipart` does internally.)
</details>

<details>
<summary>Hint 3: the resume test overwrites instead of appending</summary>

Check `resp.StatusCode == http.StatusPartialContent` *and* that you sent a
`Range` (N > 0) before choosing `O_APPEND`.
</details>

## Experiments

With `go run ./cmd/gobin -static .` (stage 21):

1. `go run ./cmd/gocurl -o /tmp/readme.md localhost:8080/static/README.md && diff /tmp/readme.md README.md`
2. `head -c 100 README.md > /tmp/part.md` then
   `go run ./cmd/gocurl -C - -o /tmp/part.md localhost:8080/static/README.md` and `diff` again.
   Run it once more: **predict** the status code the server answers now.
3. Now resume against an endpoint that doesn't support ranges:
   `printf 0123456789 > /tmp/y; go run ./cmd/gocurl -C - -o /tmp/y localhost:8080/bytes/1000`.
   **Predict** the size of `/tmp/y`, then check it with `wc -c`.
4. `go run ./cmd/gocurl -F file=@go.mod -F note=hi localhost:8080/upload` (gobin
   from stage 21) and compare the sha256 with `sha256sum go.mod`.
5. If you streamed `-F` through `io.Pipe`, upload to `localhost:8080/anything`
   and look at the request `headers` gobin echoes: is there a `Content-Length`?
   Why not? (`Transfer-Encoding` won't show either: Go's server moves it into
   `r.TransferEncoding`.) If you built the form in a `bytes.Buffer`, what changes?

## Explore further

- **Progress meter:** wrap the body in a reader that counts bytes and prints
  a percentage to stderr (using `resp.ContentLength`, which may be `-1`).
- `-O`: save under the last path segment of the URL; `-J`: use the
  `Content-Disposition` filename from the response (`mime.ParseMediaType`).
  How do you make `-J` safe against `filename="../../.bashrc"`?
- **`If-Range`:** on resume, send the ETag (or `Last-Modified`) you saw
  originally so a changed file is downloaded afresh. Where would you store it?
- **Parallel downloads:** `HEAD` for the size, split into N ranges, download
  them concurrently and `WriteAt` each into place. How much faster is it
  against gobin? Against a real server?
- `-T FILE` upload with a known length (`req.ContentLength = size`) vs `-F`.

## Check your understanding

```sh
go run ./cmd/course quiz 22
```

**Explain it back:** Walk through `-C -` for each possible server response,
and explain what goes wrong if you always append.

## Reference

- [`mime/multipart.Writer`](https://pkg.go.dev/mime/multipart#Writer), [`mime.TypeByExtension`](https://pkg.go.dev/mime#TypeByExtension), [`io.Pipe`](https://pkg.go.dev/io#Pipe)
- [`os.OpenFile`](https://pkg.go.dev/os#OpenFile) flags
- [curl: `-o`, `-F`, `-r`, `-C`](https://curl.se/docs/manpage.html)

**Next:** [Stage 23: Graceful shutdown](../23-graceful-shutdown/README.md)
