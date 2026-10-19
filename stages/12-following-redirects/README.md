# Stage 12: Following redirects

> **Part IV · Protocol semantics** · **You build:** `gocurl` · **Time:** ~60 min

## Why this stage matters

Go's client follows redirects for you, silently, up to 10 hops, changing
methods and dropping headers along the way by rules you should know. Most of
the time that's what you want. When it isn't (an API that answers `302` to
mean something, a tool like curl that shows you each hop, a crawler that
must not leave the site), you control it with one function, `CheckRedirect`.

## Before you read: predict

1. With the default `http.Client`, you `POST` to a URL that answers `302`.
   What request does Go send next?
2. How do you make Go's client return the `302` response itself instead of
   following it?
3. You set an `Authorization` header and get redirected to another domain.
   Is the header sent there?

## Reading

### The default policy

`http.Client` follows redirects automatically. Its `CheckRedirect` field
decides, before each hop, whether to continue:

```go
type Client struct {
    CheckRedirect func(req *Request, via []*Request) error
    ...
}
```

- `req` is the **next** request about to be sent; `via` holds the requests
  made so far, oldest first. On the first redirect `len(via) == 1`.
- Return `nil` to follow, or an error to stop.
- If `CheckRedirect` is nil, the default stops after **10** redirects with
  the error `stopped after 10 redirects`.

### Stopping: two different ways

```go
// 1. "Don't follow; give me the 3xx response." No error, body still open.
client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
    return http.ErrUseLastResponse
}

// 2. "This is an error."
var errTooMany = errors.New("too many redirects")
client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
    if len(via) > max {
        return errTooMany
    }
    return nil
}
resp, err := client.Do(req)
// err is a *url.Error wrapping errTooMany, so errors.Is(err, errTooMany) is true.
// resp is the last redirect response, with its body ALREADY CLOSED.
```

`http.ErrUseLastResponse` is a special sentinel the client recognises: `Do`
returns the redirect response as a normal, successful response.

### Seeing each hop: `req.Response`

Inside `CheckRedirect`, `req.Response` is **the redirect response that
caused `req`**. This field is only set during redirects. That's how you can
log or print every hop:

```go
func(req *http.Request, via []*http.Request) error {
    fmt.Println(req.Response.Status, "→", req.URL) // "302 Found → http://host/hop/1"
    return nil
}
```

After `Do`, `resp.Request.URL` is the **final** URL after all redirects.

### What happens to the method, body and headers

| Status | Next method | Body |
|---|---|---|
| 301, 302, 303 | `GET` (unless it was `GET` or `HEAD`, which stay) | dropped |
| 307, 308 | same method | **re-sent**, using `req.GetBody` |

For 307/308, if the original request had a body but **no `GetBody`** (e.g. you
passed a plain `io.Reader`), Go can't replay it, so it **doesn't follow**: it
returns the 307 response as-is. That's why stage 7 made you build bodies from
`bytes.Reader`.

Headers from the original request are copied to each hop, except:

- `Authorization`, `WWW-Authenticate` and `Cookie` are **dropped** when
  redirecting to a different domain (a subdomain of the original is OK).
  Otherwise a malicious redirect could steal your credentials.
- A `Referer` header is added (unless going from https to http).
- With a cookie jar, cookies are recomputed for each hop (stage 14).

The previous response's body is drained (a little) and closed for you, so
its connection can be reused.

### curl's policy

curl is more conservative than Go: it **doesn't follow redirects** unless you
pass `-L`, then follows up to `--max-redirs` (default 50) and fails with exit
code `47` beyond that. With `-i -L` it prints the headers of every response in the
chain. Its method rules match Go's (POST becomes GET after 301/302/303).

> **📖 Source dive:** in `$(go env GOROOT)/src/net/http/client.go`, read
> `redirectBehavior` (the table above, in code) and the redirect loop in
> `func (c *Client) do`. Find where `ErrUseLastResponse` is checked and where
> sensitive headers are stripped (`shouldCopyHeaderOnRedirect`).

## Your task

| Option | Behaviour |
|---|---|
| (default) | **don't** follow redirects: print the 3xx response's body (and headers with `-i`), exit `0` |
| `-L`, `--location` | follow redirects; print only the final body |
| `--max-redirs N` | with `-L`, follow at most `N` redirects (default **50**). Going over → stderr `gocurl: (47) ...` mentioning redirects, exit **47** |
| `-i -L` | print the status line and headers of **every** response in the chain, in order, each followed by an empty line, then the final body |

Method handling must follow the table above: after a 302 a `-d` POST becomes a
body-less GET; after a 307 the POST and its body are repeated.

```sh
go run ./cmd/course test 12
```

## Hints

<details>
<summary>Hint 1: a custom client</summary>

Stop using `http.DefaultClient`. Build `&http.Client{CheckRedirect: ...}` in a
function that has access to your parsed options (a closure).
</details>

<details>
<summary>Hint 2: printing hops with -i</summary>

In `CheckRedirect`, when `-i` is set, write `req.Response`'s status line and
headers (reuse your stage 5 function) to a buffer, then print the buffer
before the final response's headers. Printing straight to stdout works too,
until stage 22 adds `-o`.
</details>

<details>
<summary>Hint 3: exit code 47</summary>

Return a sentinel error from `CheckRedirect` when `len(via) > maxRedirs`. After
`Do`, check `errors.Is(err, errTooManyRedirects)` before your other error
classification.
</details>

## Experiments

1. Write a tiny program: `http.Get` on gobin's `/redirect/11`. What's the
   error? Now `/redirect/10`.
2. `go run ./cmd/gocurl -L localhost:8080/redirect/2`. The chain ends at
   `/get`, whose `headers` show the final request. **Predict**: is there a
   `Referer` header, and what's its value? Then add `-i` to see every hop.
3. **Predict**, then check what `/anything` reports:
   `go run ./cmd/gocurl -L -X PUT -d x=1 'localhost:8080/redirect-to?url=/anything&status_code=301'`
4. Credentials across hosts: run two gobins (`-addr :8080` and `-addr :8081`).
   `go run ./cmd/gocurl -L -H 'Authorization: Bearer secret' 'localhost:8080/redirect-to?url=http://127.0.0.1:8081/headers'`.
   Is `Authorization` in the output? Why does `localhost` vs `127.0.0.1`
   count as a different domain?

## Explore further

- Add curl's `--post301`/`--post302`/`--post303` (keep POST after those codes).
  You can't change Go's built-in behaviour from `CheckRedirect`. How else
  could you do it? (Hint: follow redirects yourself in a loop.)
- Add `--location-trusted`: keep `Authorization` across hosts. Again: how,
  given Go strips it?
- Add `-w '%{url_effective} %{num_redirects}\n'`. Where do you get each value?
- Detect redirect **loops** (A → B → A) instead of only counting hops.

## Check your understanding

```sh
go run ./cmd/course quiz 12
```

**Explain it back:** What's the difference between returning
`http.ErrUseLastResponse` and returning another error from `CheckRedirect`?

## Reference

- [`http.Client`](https://pkg.go.dev/net/http#Client): the doc comment explains which headers are forwarded on redirects
- [`http.ErrUseLastResponse`](https://pkg.go.dev/net/http#ErrUseLastResponse), [`Request.Response`](https://pkg.go.dev/net/http#Request)
- [curl `-L`](https://curl.se/docs/manpage.html#-L)

**Next:** [Stage 13: Cookies and authentication](../13-cookies-and-auth/README.md)
