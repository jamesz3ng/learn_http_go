# Stage 10: An API client SDK

> **Part III · Resources** · **You build:** `kvclient` · **Time:** ~75 min

## Why this stage matters

Most HTTP clients in Go codebases are not curl clones. They're **SDKs**:
a typed Go API (`client.Get(ctx, key)`) that hides URLs, encoding, status
codes and error bodies. A good SDK escapes inputs correctly, returns errors
callers can inspect, never leaks connections, and is easy to test. You'll
build one for gobin's key-value API.

## Before you read: predict

1. Your SDK builds `base + "/kv/" + key`. What goes wrong for the key `a/b`?
   For `100%`? For `x?y`?
2. How should a caller check "was it just not found?" without parsing error strings?
3. Should your SDK use `http.DefaultClient`?

## Reading

### Shape of an SDK

```go
type Client struct {
    base *url.URL      // parsed once, in New
    hc   *http.Client  // injected: callers choose timeouts, transports, test fakes
}

func New(baseURL string, hc *http.Client) (*Client, error)

func (c *Client) Get(ctx context.Context, key string) ([]byte, error)
func (c *Client) Put(ctx context.Context, key string, value []byte) (created bool, err error)
```

- **Validate in the constructor.** Parse the base URL once and reject
  anything that isn't an absolute `http(s)` URL with a host (`url.Parse`
  accepts nearly anything, as you saw in stage 3).
- **Accept an `*http.Client`.** Callers may need a custom timeout, a proxy, or
  the retrying transport you'll write in stage 16, and tests need to point it
  at fakes. If they pass `nil`, create a client **with a timeout**, not
  `http.DefaultClient`, which has none and is shared global state.
- **`ctx` first** on every method that does I/O: it carries cancellation and
  deadlines.
- A `Client` must be **safe for concurrent use**. `http.Client` is, so a
  `Client` that never mutates itself after `New` is too.

### Building URLs safely

String concatenation is a bug waiting to happen. Escape each piece for where
it goes:

```go
base, _ := url.Parse("http://localhost:8080/api/")   // may have a prefix and/or trailing slash

u := base.JoinPath("kv", url.PathEscape(key))         // http://localhost:8080/api/kv/a%2Fb%20c
q := base.JoinPath("kv")
q.RawQuery = url.Values{"prefix": {prefix}}.Encode()  // http://localhost:8080/api/kv?prefix=a%26b
```

- `url.PathEscape` encodes one **path segment**: `/` → `%2F`, space → `%20`,
  `?` → `%3F`, `#` → `%23`, `%` → `%25`. Unescaped, those characters would
  start new segments, a query, or a fragment, or be misread as escapes.
- `JoinPath` (Go 1.19+) joins segments onto the existing path, with exactly
  one slash between them. It treats its arguments as **already-escaped** path
  text, which is why you escape the key first. It also cleans `.` and `..`
  segments, so keys like `..` can't be addressed by path at all.
- Build queries with `url.Values.Encode()`.

### One `do` helper for every request

Every method does the same things: build a request with the context, set
`User-Agent`, send it, and turn non-2xx statuses into errors. Write that once:

```go
func (c *Client) do(ctx context.Context, method, url string, body io.Reader) (*http.Response, error) {
    req, err := http.NewRequestWithContext(ctx, method, url, body)
    if err != nil {
        return nil, err
    }
    req.Header.Set("User-Agent", "kvclient/1.0")
    resp, err := c.hc.Do(req)
    if err != nil {
        return nil, err
    }
    if resp.StatusCode < 200 || resp.StatusCode > 299 {
        defer resp.Body.Close()
        return nil, readAPIError(resp) // reads the body for the message
    }
    return resp, nil // caller reads and closes the body
}
```

### Errors callers can inspect

Two complementary tools from the `errors` package:

```go
// A sentinel: a single, comparable value.
var ErrNotFound = errors.New("kvclient: key not found")

// A type carrying details.
type APIError struct {
    StatusCode int
    Message    string
}
func (e *APIError) Error() string { ... }
```

Callers then write:

```go
if errors.Is(err, kvclient.ErrNotFound) { ... }   // "is it a not-found?"
var apiErr *kvclient.APIError
if errors.As(err, &apiErr) { apiErr.StatusCode }   // "give me the details"
```

How can a 404 `*APIError` be *both*? `errors.Is` checks whether any error
in the chain has an `Is(target error) bool` method that says yes:

```go
func (e *APIError) Is(target error) bool {
    return target == ErrNotFound && e.StatusCode == http.StatusNotFound
}
```

Now one error value answers both questions, and callers never parse strings.

### Read error bodies (a little) and always close

A good error message includes the server's explanation. gobin sends
`{"error": "..."}`, but proxies and load balancers in front of a real API
send HTML or plain text. Handle both, and cap how much you read:

```go
b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
```

On success paths with no body to return (201/204), **still close the body**.
Read it to EOF first if there might be one, so the connection is reused:

```go
io.Copy(io.Discard, resp.Body)
resp.Body.Close()
```

### Testing a client: fake the server

You don't need gobin running to test kvclient. `httptest.NewServer` with a
small fake handler gives full control: return a 500, record the escaped
path, stall forever. Read `fakeKV` in
[`tests/stage10_api_client_test.go`](../../tests/stage10_api_client_test.go):
it's a complete, ~60-line fake of the API.

> **📖 Source dive:** `go doc -src net/url.URL.JoinPath` and
> `go doc -src net/url.PathEscape` (follow it to `shouldEscape`). Which
> characters does a path segment allow unescaped?

## Your task

Implement [`kvclient/kvclient.go`](../../kvclient/kvclient.go) (stage 10 section):

| API | Behaviour |
|---|---|
| `New(baseURL, hc)` | error unless `baseURL` is an absolute `http`/`https` URL with a host. `hc == nil` → a client with a timeout |
| `Put(ctx, key, value)` | `PUT {base}/kv/{escaped key}`; `created` is true on 201, false on 204 |
| `Get(ctx, key)` | the value bytes |
| `Delete(ctx, key)` | |
| `List(ctx, prefix)` | `GET {base}/kv?prefix=...`; decode `{"keys": [...]}` |
| errors | any non-2xx → `*APIError{StatusCode, Message}`; `Message` from the JSON `error` field if the body parses as JSON (whatever the `Content-Type` says: `http.Error` labels JSON bodies `text/plain`), or else the trimmed body text. A 404 also matches `errors.Is(err, ErrNotFound)` |
| headers | `User-Agent: kvclient/1.0` |
| context | every request uses `ctx` (cancellation and deadlines work) |
| connections | sequential calls reuse one connection, error paths included |

The base URL may have a path prefix, with or without a trailing slash:
`http://host/api` and `http://host/api/` both mean `http://host/api/kv/...`.

```sh
go run ./cmd/course test 10
go run ./cmd/gobin &                       # then try your SDK for real:
go run ./cmd/kv put 'user:1' alice && go run ./cmd/kv list
```

## Hints

<details>
<summary>Hint 1: parse once</summary>

Store the parsed `*url.URL` in the `Client`. `JoinPath` returns a **new**
URL, so the stored one is never modified, which keeps the client safe for
concurrent use.
</details>

<details>
<summary>Hint 2: making errors.Is work</summary>

Add the `Is` method from the Reading section to `*APIError`. Return the
`*APIError` itself (not wrapped in a plain string error) from `do`.
</details>

<details>
<summary>Hint 3: the connection-reuse test fails</summary>

Every path must end with the body read to EOF and closed: success with a
body (Get/List), success without one (Put/Delete), and errors. `defer` in
the right places, and drain with `io.Copy(io.Discard, resp.Body)` where
you don't need the content.
</details>

## Experiments

1. With gobin running, `go run ./cmd/kv put 'a/b c' x` then
   `curl 'localhost:8080/kv?prefix=a'`. Was the key stored intact?
2. Where you escape the key for the URL, swap `url.PathEscape` for `url.QueryEscape`. **Predict** what
   happens to the key `a b`, then run the tests.
3. Write a small program that calls `Get` with a context that's
   already cancelled. What error do you get? Is it `errors.Is(err, context.Canceled)`?

## Explore further

- Add `GetJSON[T any](ctx, c, key) (T, error)` and `PutJSON` as generic
  helpers that store values as JSON.
- Switch `New` to **functional options**: `New(url, WithHTTPClient(hc),
  WithUserAgent("x"))`. When is that better than a config struct?
- Add `ErrUnauthorized` for 401 and make `APIError.Is` match it too. How far
  should this go?
- Write an `ExampleClient_Get` test function: it shows up in `go doc` and is
  checked by `go test`.

## Check your understanding

```sh
go run ./cmd/course quiz 10
```

**Explain it back:** How does a 404 `*APIError` satisfy both
`errors.Is(err, ErrNotFound)` and `errors.As(err, &apiErr)`?

## Reference

- [`url.URL.JoinPath`](https://pkg.go.dev/net/url#URL.JoinPath), [`url.PathEscape`](https://pkg.go.dev/net/url#PathEscape)
- [Working with errors in Go 1.13](https://go.dev/blog/go1.13-errors) (Go blog): `Is`, `As`, wrapping
- [`errors.Is`](https://pkg.go.dev/errors#Is): read about the `Is` method

**Next:** [Stage 11: Sending redirects](../11-redirects/README.md)
