# Stage 5: Building requests

> **Part II · Messages** · **You build:** `gocurl` · **Time:** ~60 min

## Why this stage matters

`http.Get` covers one case. Real clients choose methods, set headers, and
look at response headers. You need `http.NewRequestWithContext` and
`Client.Do`. You'll also find out that Go quietly adds headers you never
asked for, and how to control them.

## Before you read: predict

1. You don't set a `User-Agent`. Does Go send one? Which?
2. `req.Header.Set("Host", "example.test")`: will the server see that host?
3. You set no `Accept-Encoding` header. Does Go send one?

## Reading

### `NewRequestWithContext` + `Do`

```go
req, err := http.NewRequestWithContext(ctx, "PATCH", "https://api.example.com/items/1", body)
if err != nil {
    return err // invalid method or unparsable URL
}
req.Header.Set("Accept", "application/json")
resp, err := client.Do(req)
```

The context lets the caller cancel or time-limit the request (stage 18).
Plain `http.NewRequest` uses `context.Background()`, so always prefer the
`WithContext` version in code that has a context to hand.

### Setting headers

```go
req.Header.Set("X-Thing", "a")   // replace all values of X-Thing
req.Header.Add("X-Thing", "b")   // append: two X-Thing headers are sent
req.Header.Del("X-Thing")        // remove
req.Header.Get("x-thing")        // read the first value (names are canonicalised)
```

### What Go adds for you

When the Transport writes a request, it fills in some headers that you never see in
`req.Header`:

| Header | When | How to control it |
|---|---|---|
| `Host` | always | from the URL, **or `req.Host` if set**. `req.Header["Host"]` is ignored |
| `User-Agent` | if `req.Header` has no `User-Agent` | sends `Go-http-client/1.1`. Set your own, or set it to `""` to send none |
| `Accept-Encoding: gzip` | if you didn't set `Accept-Encoding` (or `Range`) | Go then **transparently decompresses** the body and removes `Content-Length`/`Content-Encoding` from the response. Set the header yourself and you get the raw compressed bytes |
| `Content-Length` / `Transfer-Encoding` | for bodies | stage 7 |

The `User-Agent` rule is in the doc comment of `Request.Write`: an empty
value is **present** but empty, and that's what suppresses the default.

### Reading the response's status and headers

```go
fmt.Fprintf(out, "%s %s\r\n", resp.Proto, resp.Status) // "HTTP/1.1 202 Accepted"
for _, name := range slices.Sorted(maps.Keys(resp.Header)) {
    for _, v := range resp.Header[name] {
        fmt.Fprintf(out, "%s: %s\r\n", name, v)
    }
}
```

Go maps have no order; iterating `resp.Header` directly gives a different
order every run. `maps.Keys` returns an iterator (Go 1.23) and
`slices.Sorted` collects and sorts it.

### `HEAD`

A `HEAD` request gets the same status and headers as `GET` but **no body**.
Useful for "does this exist / how big is it / did it change?" without
downloading it. On the server, Go discards whatever a handler writes for a `HEAD`.

### Repeatable flags: `flag.Value`

`-H` can be given many times. The `flag` package supports that through an
interface:

```go
type headerList []string

func (h *headerList) String() string     { return strings.Join(*h, ", ") }
func (h *headerList) Set(v string) error { *h = append(*h, v); return nil }

var headers headerList
fs.Var(&headers, "H", "add a header (repeatable)")
fs.Var(&headers, "header", "add a header (repeatable)")
```

### Flags after the URL

`flag` stops at the first non-flag argument: in `gocurl URL -X PUT`, `-X` is
never parsed. curl allows flags anywhere, so after parsing, take the first
remaining argument as a positional and parse the rest again:

```go
rest := args
for {
    if err := fs.Parse(rest); err != nil { ... }
    rest = fs.Args()
    if len(rest) == 0 { break }
    positional = append(positional, rest[0])
    rest = rest[1:]
}
```

> **📖 Source dive:** in `$(go env GOROOT)/src/net/http/request.go`, read
> `func (r *Request) write(...)`. Find where `Host` and `User-Agent` are
> chosen. Then in `transport.go`, search for `requestedGzip` to see the
> transparent decompression decision.

## Your task

Add these options to gocurl. Keep stage 3 working.

| Option | Behaviour |
|---|---|
| `-X`, `--request METHOD` | use this method |
| `-H`, `--header "Name: value"` | add a header; repeatable. The **first** `-H` for a name replaces any default; later ones for the same name add more values. Trim spaces around the value |
| `-H "Name:"` (nothing after the colon) | remove that header. For `User-Agent` this means **no User-Agent at all** |
| `-H "Host: x"` | send `x` as the Host |
| `-A`, `--user-agent UA` | set the User-Agent. The **default** User-Agent is `gocurl/1.0` |
| `-i`, `--include` | before the body, print `<resp.Proto> <resp.Status>`, then each header as `Name: value` (sorted by name, one line per value), then an empty line. Use `\r\n` line endings |
| `-I`, `--head` | send `HEAD` and print the status line and headers as for `-i` |
| flags after the URL | must work: `gocurl URL -X PUT` |

```sh
go run ./cmd/course test 5
```

## Hints

<details>
<summary>Hint 1: order of operations</summary>

Build the request, set the defaults (`User-Agent: gocurl/1.0`, `-A`), then
apply `-H` options last so they can override or remove anything.
</details>

<details>
<summary>Hint 2: first -H replaces, later -H adds</summary>

Keep a `seen := map[string]bool{}` keyed by the canonical name
(`textproto.CanonicalMIMEHeaderKey`). If you haven't seen it: `Set`. If you
have: `Add`.
</details>

<details>
<summary>Hint 3: removing User-Agent doesn't work</summary>

`req.Header.Del("User-Agent")` makes Go add its own default.
`req.Header.Set("User-Agent", "")` keeps the key present with an empty value,
which tells Go to send nothing.
</details>

## Experiments

With `go run ./cmd/gobin` running:

1. **Predict** every header gobin will report, then run
   `go run ./cmd/gocurl localhost:8080/headers`. Where did `Accept-Encoding` come from?
2. Compare with `curl localhost:8080/headers`. What does curl send that
   gocurl doesn't, and vice versa?
3. `go run ./cmd/gocurl -I localhost:8080/` vs `-i`. Is `Content-Length` the
   same for both? Why does a `HEAD` response have a `Content-Length` but no body?
4. `go run ./cmd/gocurl -i localhost:8080/bytes/5000` (after stage 8) with
   and without `-H 'Accept-Encoding: gzip'`. gobin doesn't compress, so try
   `https://httpbin.org/gzip` instead: **predict** what you'll see in each case.

## Explore further

- Add `-v`/`--verbose`: print the request (`> GET / HTTP/1.1`, `> Header: ...`) and
  response (`< ...`) to stderr like curl. `httputil.DumpRequestOut` shows the
  request as the Transport would write it, including the headers Go adds.
- Add `--compressed`: request gzip explicitly (and decompress with
  `compress/gzip` yourself, since setting the header turns off Go's automatic
  decompression).
- Add `-D FILE`/`--dump-header`: write the response headers to a file.
- Add `--url-query name=value` (curl 7.87+) using `url.Values`.

## Check your understanding

```sh
go run ./cmd/course quiz 5
```

**Explain it back:** List the headers Go's client adds to a request that
aren't in `req.Header`, and how to override each one.

## Reference

- [`http.NewRequestWithContext`](https://pkg.go.dev/net/http#NewRequestWithContext), [`http.Request.Write`](https://pkg.go.dev/net/http#Request.Write) (User-Agent rule), [`Transport.DisableCompression`](https://pkg.go.dev/net/http#Transport)
- [`flag.Value`](https://pkg.go.dev/flag#Value)
- [curl man page](https://curl.se/docs/manpage.html): `-X`, `-H`, `-A`, `-i`, `-I`

**Next:** [Stage 6: Request bodies on the server](../06-reading-bodies/README.md)
