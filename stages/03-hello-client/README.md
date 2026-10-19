# Stage 3: Your first request

> **Part I · Foundations** · **You build:** `gocurl` · **Time:** ~60 min

## Why this stage matters

Making a request in Go is one line: `http.Get(url)`. Using it *correctly*
takes a few more: knowing what counts as an error (less than you'd think),
why the body must be closed, and how to tell a refused connection from a
broken URL. Here you start **gocurl**, a curl clone you'll extend through
the course. Each new curl flag is an excuse to learn another part of the
client.

## Before you read: predict

1. `http.Get` on a URL that returns `404 Not Found`: is `err` nil?
2. What happens if you never call `resp.Body.Close()`?
3. Is `url.Parse("localhost:8080/x")` an error? If not, what's the scheme?

## Reading

### `http.Get` and `*http.Response`

```go
resp, err := http.Get("http://localhost:8080/healthz")
if err != nil {
    return err // no response at all
}
defer resp.Body.Close()

resp.StatusCode    // 200
resp.Status        // "200 OK"
resp.Proto         // "HTTP/1.1"
resp.Header        // http.Header: map[string][]string
resp.ContentLength // -1 if unknown (e.g. chunked)
resp.Body          // io.ReadCloser: the body, streamed from the network
```

`http.Get` is shorthand for `http.DefaultClient.Get`, which builds a
`GET` request and calls `Do`. `Run` receives a `context.Context`, so prefer
the spelled-out version, which lets the caller cancel the request:

```go
req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
if err != nil { ... }               // e.g. an invalid method or URL
resp, err := http.DefaultClient.Do(req)
```

(Stage 5 is all about building requests.)

### What counts as an error

`err` is non-nil only when **there is no HTTP response**: the URL is invalid,
DNS lookup failed, the connection was refused, TLS failed, a timeout hit, or a
redirect policy stopped things. **A 404 or 500 is not an error.** It's a
perfectly good HTTP response that happens to say "no". Check
`resp.StatusCode` yourself.

### Always close the body

`resp.Body` streams straight from the TCP connection. Until you've **read it
to the end and closed it**, the connection can't go back into the client's
pool for reuse. Forget to close and you leak connections (and goroutines) on
every request; read only half and close, and the connection is thrown away
rather than reused. The idiom:

```go
resp, err := client.Do(req)
if err != nil {
    return err
}
defer resp.Body.Close() // right after the error check, every time
```

(When `err != nil`, `resp` is nil and there's nothing to close. The one
exception, a redirect-policy error, comes back with a body that's already
closed.)

### Stream, don't slurp

`io.ReadAll(resp.Body)` reads the entire body into memory before you can do
anything with it. For a curl clone, that's wrong twice: a 4 GB download needs
4 GB of RAM, and nothing appears until the transfer ends. Instead:

```go
_, err = io.Copy(stdout, resp.Body) // a chunk at a time, as it arrives
```

### Looking inside errors

Client errors are layered. `Do` returns a `*url.Error` (which records the
method and URL), wrapping the real cause. Use `errors.As` to look for a
specific type anywhere in the chain:

```go
var opErr *net.OpError
if errors.As(err, &opErr) && opErr.Op == "dial" {
    // couldn't connect: refused, unreachable, ...
}
var dnsErr *net.DNSError
if errors.As(err, &dnsErr) {
    // host name didn't resolve
}
```

Try printing an error with `fmt.Printf("%#v\n", err)` to see the layers.

### A testable command

gocurl's whole program lives in one function:

```go
func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int
```

`main` (in [`cmd/gocurl`](../../cmd/gocurl/main.go)) passes in `os.Args[1:]`,
`os.Stdout` and friends and calls `os.Exit` with the result. The tests pass
in buffers instead. Taking the outside world as parameters makes any
command-line tool testable in-process.

Parse flags with your own `flag.FlagSet`, never the global `flag.Parse`:

```go
fs := flag.NewFlagSet("gocurl", flag.ContinueOnError) // return errors, don't exit
fs.SetOutput(stderr)
var fail bool
fs.BoolVar(&fail, "f", false, "fail on HTTP errors")
fs.BoolVar(&fail, "fail", false, "fail on HTTP errors") // same variable, long name
if err := fs.Parse(args); err != nil {
    return 2 // the FlagSet already printed the problem
}
url := fs.Arg(0)
```

Go's `flag` package accepts `-fail` and `--fail` alike, so registering both
names gives you curl's `-f`/`--fail`.

### `url.Parse` is lenient

`url.Parse` accepts almost anything. `url.Parse("localhost:8080/x")`
succeeds with scheme `"localhost"`! curl assumes `http://` when you leave the
scheme out; to do the same, check for `"://"` before parsing. Then validate:
the scheme must be `http` or `https`, and `Host` must not be empty.

### Exit codes

Scripts tell failures apart by curl's exit code. We'll borrow curl's codes:

| Code | Meaning |
|---|---|
| 0 | success (any HTTP status, unless `-f`) |
| 1 | unsupported protocol (e.g. `ftp://`) |
| 2 | usage error (gocurl's choice: bad flags, missing URL) |
| 3 | malformed URL |
| 6 | couldn't resolve host (optional; not tested) |
| 7 | couldn't connect |
| 22 | HTTP status >= 400 with `-f` |

On failure, print `gocurl: (CODE) message` to stderr.

## Your task

Implement `Run` in [`gocurl/gocurl.go`](../../gocurl/gocurl.go):

| Invocation | Behaviour |
|---|---|
| `gocurl URL` | `GET` the URL; copy the body to stdout **as it arrives**; exit `0` whatever the status code |
| `gocurl -f URL`, `--fail` | if the status is >= 400: write nothing to stdout, write `gocurl: (22) The requested URL returned error: 404` (with the real code) to stderr, exit `22` |
| `gocurl` (no URL), unknown flag | usage message on stderr, exit `2` |
| `gocurl host:port/path` | no `://` means assume `http://` |
| malformed URL (e.g. `http://[::1`) | exit `3` |
| scheme other than http/https | exit `1` |
| connection refused | stderr starts with `gocurl: (7)`, exit `7` |

Tests: [`tests/stage03_hello_client_test.go`](../../tests/stage03_hello_client_test.go).

```sh
go run ./cmd/course test 3
go run ./cmd/gobin &                     # your server from stage 2
go run ./cmd/gocurl localhost:8080/      # Hello, HTTP!
```

## Hints

<details>
<summary>Hint 1: structure</summary>

Split `Run` into: parse flags → validate URL → build request → `Do` → check
status → copy body. Have a helper return an error carrying an exit code
(`type curlError struct{ code int; msg string }`), and let `Run` print it and
return the code in one place.
</details>

<details>
<summary>Hint 2: the streaming test fails</summary>

You're probably using `io.ReadAll`. Use `io.Copy(stdout, resp.Body)`. The test
holds the response open after the first line and checks that line has
already reached stdout.
</details>

<details>
<summary>Hint 3: exit code 7</summary>

```go
var opErr *net.OpError
if errors.As(err, &opErr) && opErr.Op == "dial" { /* 7 */ }
```
</details>

## Experiments

1. **Predict**, then run: `go run ./cmd/gocurl localhost:9` (nothing listens
   on port 9). Print the error with `%#v`. How many layers does it have?
2. Write a tiny program that does `http.Get` 1,000 times against your gobin
   **without** closing the body, then watch `lsof -p <pid> | grep TCP | wc -l`
   (or `netstat`) grow. Add `resp.Body.Close()` and compare.
3. `gocurl -f localhost:8080/nope; echo $?`. Now try real curl: `curl -f localhost:8080/nope; echo $?`.

## Explore further

- Add `-s`/`--silent` (curl hides its progress meter; for us, hide error
  messages) and `-S`/`--show-error`.
- Add `-w '%{http_code}\n'` (write-out): print variables after the transfer.
  Which other `%{...}` variables could you support from `*http.Response`?
- curl exits `6` when a host doesn't resolve. Add it: try
  `gocurl http://no-such-host.invalid/`. (`.invalid` is reserved and never resolves.)
- What does `http.Get` do with a `file:///etc/hosts` URL? Look up
  `http.NewFileTransport`.

## Check your understanding

```sh
go run ./cmd/course quiz 3
```

**Explain it back:** When does `client.Do` return an error, and why must you
close `resp.Body` even if you don't need it?

## Reference

- [`http.Get`](https://pkg.go.dev/net/http#Get), [`http.Response`](https://pkg.go.dev/net/http#Response), [`http.Client.Do`](https://pkg.go.dev/net/http#Client.Do) (read the doc comment about closing bodies)
- [`errors.As`](https://pkg.go.dev/errors#As), [`url.Error`](https://pkg.go.dev/net/url#Error), [`net.OpError`](https://pkg.go.dev/net#OpError)
- [curl exit codes](https://curl.se/libcurl/c/libcurl-errors.html)

**Next:** [Stage 4: Reading requests](../04-inspecting-requests/README.md)
