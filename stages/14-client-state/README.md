# Stage 14: Cookie jars and credentials

> **Part IV · Protocol semantics** · **You build:** `gocurl` · **Time:** ~45 min

## Why this stage matters

Now you're on the other side of stage 13: sending credentials and keeping
cookies like a browser. Go's client doesn't remember cookies unless you give
it a **cookie jar**, and redirects make cookies trickier than they look.

## Before you read: predict

1. A server answers `302` with `Set-Cookie: session=x` and redirects to
   `/home`. With a default `http.Client`, does the request to `/home` carry
   the cookie?
2. Does `http.Client` remember cookies between two separate `Do` calls by default?
3. What's in an `http.CookieJar`, structurally?

## Reading

### Credentials

```go
req.SetBasicAuth("alice", "s3:cret")             // Authorization: Basic YWxpY2U6czM6Y3JldA==
req.Header.Set("Authorization", "Bearer "+token) // tokens
```

When parsing curl's `-u user:pass`, split at the **first** colon only: the
username can't contain a colon, but the password can.

### Cookies without a jar

You can always set the `Cookie` header yourself:

```go
req.Header.Set("Cookie", "a=1; b=2")
req.AddCookie(&http.Cookie{Name: "c", Value: "3"}) // appends "; c=3"
```

But an `http.Client` with no jar **ignores `Set-Cookie`** in responses,
including on redirects. A login endpoint that sets a session cookie and
redirects to `/home` will look logged-out.

### `http.CookieJar` and `net/http/cookiejar`

```go
type CookieJar interface {
    SetCookies(u *url.URL, cookies []*Cookie) // store cookies received from u
    Cookies(u *url.URL) []*Cookie             // cookies to send to u
}
```

`net/http/cookiejar` provides an in-memory implementation that follows the
browser rules (domain, path, expiry, Secure):

```go
jar, err := cookiejar.New(nil)
client := &http.Client{Jar: jar}
```

With `Client.Jar` set, the client calls `jar.Cookies(url)` before **every**
request (including each redirect hop) and `jar.SetCookies` after every
response. Share one client (and jar) across requests and cookies persist
for the life of the process. Nothing is saved to disk.

The `nil` option means **no public suffix list**: without one, the jar can't
tell that `co.uk` is a public suffix, so `example.co.uk` could set cookies for
all of `co.uk`. Browsers use the list; Go's lives outside the standard library
in `golang.org/x/net/publicsuffix`. For a CLI that talks to one host at a time, `nil` is fine.

### Cookies you set + a jar + redirects

From the `http.Client` docs: when following redirects, the client forwards
the initial request's headers, but if you set a `Cookie` header yourself
**and** have a jar, "any mutated cookies will be omitted, with the
expectation that the Jar will insert those mutated cookies with the updated
values". In practice: your `-b` cookies keep travelling with each hop (to the
same domain), and the jar adds the ones the server set along the way.

### curl's model

curl has a "cookie engine" switched on by `-b` or `-c`. `-b 'a=1'` sends
cookies; `-b file` reads a cookie file; `-c file` writes all cookies to a file
when done. gocurl simplifies: **the jar is always on** for the duration of a
run, so cookies set during a redirect chain are always sent on later hops.

> **📖 Source dive:** in `$(go env GOROOT)/src/net/http/client.go`, find
> `func (c *Client) send` and see where the jar is consulted (`c.Jar.Cookies`)
> and updated (`c.Jar.SetCookies`). Then skim `net/http/cookiejar/jar.go`'s
> `cookies` method: how does it pick which cookies match a URL?

## Your task

| Option | Behaviour |
|---|---|
| `-u`, `--user USER[:PASSWORD]` | Basic auth; split at the first `:`; no colon → empty password |
| `--oauth2-bearer TOKEN` | `Authorization: Bearer TOKEN` |
| `-b`, `--cookie "a=1; b=2"` | send these cookies (they must still be sent after redirects to the same host) |
| cookie engine | use a cookie jar for every run, so cookies set by one response in a redirect chain are sent on the following requests |

```sh
go run ./cmd/course test 14
```

## Hints

<details>
<summary>Hint 1: where the jar goes</summary>

In the function that builds your `*http.Client` (stage 12), add
`Jar: jar` with `jar, _ := cookiejar.New(nil)`. That alone passes the
redirect test.
</details>

<details>
<summary>Hint 2: -b</summary>

The simplest approach is `req.Header.Set("Cookie", value)`. For a cleaner
alternative, parse the string with `http.ParseCookie` (Go 1.23), set
`Path: "/"` on each (otherwise the jar gives them the request's directory as
their path, and they vanish after a redirect elsewhere on the site), and add
them to the jar for the request URL with `jar.SetCookies`.
</details>

## Experiments

1. **Predict**, then run each against gobin:
   ```sh
   go run ./cmd/gocurl -L 'localhost:8080/cookies/set?a=1'            # jar on
   go run ./cmd/gocurl 'localhost:8080/cookies/set?a=1'               # no -L: what's printed?
   go run ./cmd/gocurl -L -b 'pre=1' 'localhost:8080/cookies/set?a=1'
   ```
2. Temporarily remove the jar and rerun the first command. Where did the cookie go?
3. `go run ./cmd/gocurl -i -u alice:s3cret localhost:8080/basic-auth/alice/s3cret`, then
   with a wrong password. Where's the `WWW-Authenticate` header?
4. Two gobins on `:8080` and `:8081`:
   `go run ./cmd/gocurl -L -b 'a=1' 'localhost:8080/redirect-to?url=http://127.0.0.1:8081/cookies'`.
   **Predict** whether `a` arrives, then explain using stage 12's rules.

## Explore further

- Implement `-c FILE`: write the cookies to a file in curl's Netscape
  format. `jar.Cookies(u)` only returns name and value. How can you get the
  domain, path and expiry? (Wrap the jar: your own `CookieJar` that records
  what `SetCookies` receives before delegating.)
- Implement `-b FILE` (when the argument has no `=`) to load such a file.
- Add `--netrc`: read credentials for the host from `~/.netrc`.
- Digest authentication answers a `401` challenge with a hashed response. Go
  doesn't implement it. Sketch how a `RoundTripper` (stage 16) could retry with
  credentials after a 401.

## Check your understanding

```sh
go run ./cmd/course quiz 14
```

**Explain it back:** Walk through what the client and the jar do, step by step,
for `gocurl -L 'localhost:8080/cookies/set?a=1'`.

## Reference

- [`http.CookieJar`](https://pkg.go.dev/net/http#CookieJar), [`net/http/cookiejar`](https://pkg.go.dev/net/http/cookiejar), [`http.Client`](https://pkg.go.dev/net/http#Client) (the redirect/cookie paragraph)
- [`Request.SetBasicAuth`](https://pkg.go.dev/net/http#Request.SetBasicAuth), [`http.ParseCookie`](https://pkg.go.dev/net/http#ParseCookie)
- [Public Suffix List](https://publicsuffix.org/)

**Next:** [Stage 15: Middleware](../15-middleware/README.md)
