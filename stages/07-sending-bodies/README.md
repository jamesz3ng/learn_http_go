# Stage 7: Sending bodies

> **Part II · Messages** · **You build:** `gocurl` · **Time:** ~60 min

## Why this stage matters

Last stage you received bodies; now you send them. The request body is just
an `io.Reader`, but *which* reader you pass changes what goes on the wire:
whether there's a `Content-Length`, and whether the body can be re-sent on a
redirect or retry. You'll also learn the two URL encodings that trip
everyone up, and give gocurl curl's family of `-d` options.

## Before you read: predict

1. You pass an `*os.File` as a request body. Will the request have a
   `Content-Length` header?
2. `url.QueryEscape("a b")` and `url.PathEscape("a b")`: same result?
3. If you send a body but don't set `Content-Type`, what does Go send?

## Reading

### The body is an `io.Reader`, and its type matters

```go
req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader("a=1"))
req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
```

`NewRequest` inspects the reader's concrete type. For `*bytes.Buffer`,
`*bytes.Reader` and `*strings.Reader` it knows the length, so it sets:

- **`req.ContentLength`**, so the request is sent with `Content-Length: N`;
- **`req.GetBody`**, a function returning a fresh copy of the body, used to
  re-send it on 307/308 redirects (stage 12) and retries (stage 16).

For any other reader (an `*os.File`, an `io.Pipe`, a wrapper...) the length
is unknown. Go sends the body with **`Transfer-Encoding: chunked`**, and
`GetBody` is nil, so the body can't be replayed. That's fine for streaming, but some
servers reject chunked requests. If you know the length (e.g. from
`file.Stat()`), set `req.ContentLength` yourself.

Go never guesses `Content-Type` for requests: set it yourself.

### Encoding data: two different escapes

`application/x-www-form-urlencoded` (form bodies and query strings) encodes a
space as `+`; URL paths use `%20`:

```go
url.QueryEscape("a b&c=d")  // "a+b%26c%3Dd": for one query/form VALUE
url.PathEscape("a b/c")     // "a%20b%2Fc": for one path SEGMENT (stage 10)

v := url.Values{}
v.Set("q", "a b&c")
v.Add("tag", "x")
v.Encode()                  // "q=a+b%26c&tag=x": keys sorted
```

Servers decode `+` and `%20` in form data the same way, so either is valid
there, but **only `%20` means a space in a path**.

### Convenience helpers

`http.Post(url, contentType, body)` and `http.PostForm(url, values)` exist and
use `http.DefaultClient`. They're fine in scripts; a real client wants its own
`*http.Client` and a context.

### curl's data options

| curl | sends | Content-Type |
|---|---|---|
| `-d 'a=1'` (repeatable) | `a=1&b=2` (pieces joined with `&`) | `application/x-www-form-urlencoded` |
| `-d @file` | the file, **with CR and LF removed** | same |
| `--data-binary @file` | the file, byte for byte | same |
| `--data-urlencode 'q=a b'` | `q=a+b` (value URL-encoded) | same |
| `--json '{"a":1}'` | the JSON, as given | `application/json` + `Accept: application/json` |
| `-G` / `--get` | moves all data into the **query string** and sends a `GET` | none |

`@-` means "read from stdin". `-d` implies `POST` unless `-X` says otherwise.

### Several flags, one ordered list

`-d a --data-binary b -d c` must produce `a&b&c`, in the order given. If each
flag had its own slice you'd lose the order. Instead, make several
`flag.Value` types that append to **one shared slice**, each tagging its kind:

```go
type dataArg struct{ kind dataKind; value string }

type dataFlag struct {
    kind dataKind
    dst  *[]dataArg
}

func (d dataFlag) String() string     { return "" }
func (d dataFlag) Set(v string) error { *d.dst = append(*d.dst, dataArg{d.kind, v}); return nil }

fs.Var(dataFlag{dataASCII, &data}, "d", "")
fs.Var(dataFlag{dataBinary, &data}, "data-binary", "")
```

> **📖 Source dive:** in `$(go env GOROOT)/src/net/http/request.go`, read
> `NewRequestWithContext`: the `switch v := body.(type)` that sets
> `ContentLength` and `GetBody`. Which three types does it recognise?

## Your task

Add to gocurl:

| Option | Behaviour |
|---|---|
| `-d`, `--data DATA` | add `DATA` to the body; repeatable; pieces joined with `&`. `@FILE` reads a file and strips `\r` and `\n`; `@-` reads stdin |
| `--data-binary DATA` | like `-d`, but `@FILE` is sent exactly |
| `--data-urlencode DATA` | `name=value` → `name=` + URL-encoded value; plain `value` → URL-encoded value (`=value` too) |
| `--json DATA` | body is `DATA` (or `@FILE`), with `Content-Type: application/json` and `Accept: application/json`; several `--json` pieces are concatenated without `&` |
| `-G`, `--get` | put the data in the query string (appending to any existing query with `&`) and send `GET` with no body |

Rules:

- With data (and no `-G`), the method is `POST` unless `-X` is given.
- Default `Content-Type` is `application/x-www-form-urlencoded`; a `-H Content-Type: ...` wins.
- The body must be sent with a **`Content-Length`** (not chunked).
- Unreadable `@file` → exit `26` (curl's "read error").
- No data → no `Content-Type`.

```sh
go run ./cmd/course test 7
```

## Hints

<details>
<summary>Hint 1: build the body as bytes</summary>

Collect all the data into a `[]byte` (a `bytes.Buffer` is handy) and pass
`bytes.NewReader(data)` to `NewRequestWithContext`. You get `Content-Length`
and `GetBody` for free.
</details>

<details>
<summary>Hint 2: defaults vs -H</summary>

Set `Content-Type` (and `Accept` for `--json`) *before* applying `-H`
options, so the user's headers override them, just like `User-Agent` in stage 5.
</details>

<details>
<summary>Hint 3: --data-urlencode</summary>

`strings.Cut(value, "=")`: if there's a name, send `name + "=" + url.QueryEscape(content)`.
If the name is empty (`=value`) or there's no `=`, encode the whole content.
</details>

## Experiments

With gobin running (`/anything` echoes everything):

1. `go run ./cmd/gocurl -d 'a=1' -d 'b=2' localhost:8080/anything`. Find `form`.
2. `go run ./cmd/gocurl --data-urlencode 'q=a b&c' localhost:8080/anything` and
   the same with real curl. **Predict** the raw bytes each sends. (Use
   `--data-binary` with `-H 'Content-Type: text/plain'` to see them in `data`.)
3. **Predict**, then check: in your code, replace `bytes.NewReader(data)` with
   `struct{ io.Reader }{bytes.NewReader(data)}` (which hides the concrete type).
   What do `headers` in gobin's response show now? Is there a `Content-Length`?
   (You won't see `Transfer-Encoding` either: Go's server moves it out of
   `r.Header` into `r.TransferEncoding`.) Run the stage tests and read the
   failure. Put it back.
4. `echo '{"from":"stdin"}' | go run ./cmd/gocurl --json @- localhost:8080/anything`

## Explore further

- Add `-T FILE`/`--upload-file`: `PUT` a file by **streaming** it (pass the
  `*os.File` as the body) and set `req.ContentLength` from `Stat()` so it isn't chunked.
- Add `--data-raw` (like `-d` but `@` is not special).
- curl adds `Expect: 100-continue` for large uploads, so the server can reject them
  *before* the body is sent. Set it on a request in Go, and look up
  `Transport.ExpectContinueTimeout`. Try it against gobin with a 2 MB body.
  Does gobin's 413 arrive before the upload finishes?

## Check your understanding

```sh
go run ./cmd/course quiz 7
```

**Explain it back:** What do `req.ContentLength` and `req.GetBody` do, and
which body types get them set automatically?

## Reference

- [`http.NewRequestWithContext`](https://pkg.go.dev/net/http#NewRequestWithContext) (read the doc on body types), [`http.Request.GetBody`](https://pkg.go.dev/net/http#Request)
- [`url.Values.Encode`](https://pkg.go.dev/net/url#Values.Encode), [`url.QueryEscape`](https://pkg.go.dev/net/url#QueryEscape), [`url.PathEscape`](https://pkg.go.dev/net/url#PathEscape)
- [curl: `-d`, `--data-binary`, `--data-urlencode`, `--json`, `-G`](https://curl.se/docs/manpage.html)

**Next:** [Stage 8: Routing in depth](../08-routing/README.md)
