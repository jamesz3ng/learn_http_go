# Stage 11: Sending redirects

> **Part IV · Protocol semantics** · **You build:** `gobin` · **Time:** ~45 min

## Why this stage matters

Redirects are how the web moves things: renamed pages, HTTP→HTTPS, logins
that bounce you back, "your order was placed, here's the receipt". There
are five redirect codes with different rules about what happens to a POST
body, and redirect endpoints are a classic source of security holes. gobin
gets httpbin's redirect endpoints, which gocurl will follow in stage 12.

## Before you read: predict

1. A form `POST`s to a URL that answers `302 Found`. What method does the
   browser use for the next request?
2. What's the difference between `307` and `308`?
3. `Location: get` (no leading slash), sent in response to `/redirect/1`.
   Which URL does the client go to?

## Reading

### The redirect codes

| Code | Name | Permanent? | Next request's method |
|---|---|---|---|
| `301` | Moved Permanently | yes (cached) | same in theory; **POST→GET in practice** |
| `302` | Found | no | same in theory; **POST→GET in practice** |
| `303` | See Other | no | **always GET** (body dropped) |
| `307` | Temporary Redirect | no | **same method and body** |
| `308` | Permanent Redirect | yes (cached) | **same method and body** |

`301`/`302` were specified to keep the method, but early browsers turned
POST into GET, and so many sites relied on that that the behaviour became
standard. `303`, `307` and `308` were added to say exactly what you mean.

- **303** is the right answer after a successful form `POST`: "done, now GET
  the result page." This is the **Post/Redirect/Get** pattern: refreshing the
  result page doesn't re-submit the form.
- **307/308** when the client must repeat the same request elsewhere (e.g. an
  API endpoint moved), including the body.
- **304 Not Modified** is in the 3xx range but **isn't a redirect** (stage 21).

`301` and `308` responses may be cached by browsers indefinitely. Treat
"permanent" as a promise you can't easily take back.

### `http.Redirect`

```go
http.Redirect(w, r, "/get", http.StatusFound)
```

It:

- sets `Location` (a relative path like `get` is resolved against the
  request's path first: from `/redirect/1` it becomes `/redirect/get`, and the
  result is cleaned);
- writes the status code;
- for `GET`/`HEAD`, writes a tiny HTML body with a link, for clients that
  don't follow redirects.

`Location` may be absolute (`https://example.com/x`) or relative
(`/x`); clients resolve relative ones against the URL they requested.
`http.RedirectHandler(url, code)` returns a `Handler` that always redirects.

### Absolute URLs and the `Host` header

To build an absolute `Location` you need a scheme and a host. `r.Host` is
whatever the **client** sent, so an attacker can put anything there. If a
cache in front of you stores that response, other users can be sent to the
attacker's host ("host header injection"). Real applications build absolute
URLs from a **configured** base URL. gobin echoes `r.Host` because, like
httpbin, its job is to reflect what it receives.

### Open redirects

An endpoint that redirects to a URL taken from the request
(`/redirect-to?url=...`) is an **open redirect**: an attacker can send a link
on *your* domain that lands the victim on theirs, which makes phishing much
more convincing. gobin has one on purpose (it's a testing tool). In real
applications, allow only relative paths or an allowlist of hosts. Check
the parsed URL's `Host`, and remember `//evil.example` is a
"protocol-relative" URL with a host!

> **📖 Source dive:** `go doc -src net/http.Redirect`. Find the code that
> resolves a relative URL against `r.URL.Path`, and the `htmlEscape` call
> that escapes the URL in the HTML body (why is that needed?).

## Your task

| Endpoint | Behaviour |
|---|---|
| `GET /redirect/{n}` | `302` to `/redirect/{n-1}`, and `/redirect/1` → `/get` (relative `Location`). `n` must be 1–50, else `400` |
| `GET /absolute-redirect/{n}` | same, but `Location` is absolute: `http://<host>/absolute-redirect/{n-1}`, finally `http://<host>/get` (`https` over TLS) |
| `/redirect-to?url=U&status_code=C` (**any** method) | redirect to `U` with status `C` (default `302`). `C` must be 301, 302, 303, 307 or 308; `url` is required. Otherwise `400` |

```sh
go run ./cmd/course test 11
```

## Hints

<details>
<summary>Hint 1: shared validation</summary>

Write `redirectCount(w, r) (int, bool)` that parses `{n}`, writes the 400 if
it's invalid, and reports whether to continue. Both redirect handlers use it.
</details>

<details>
<summary>Hint 2: validating status_code</summary>

```go
switch code {
case 301, 302, 303, 307, 308:
default:
    // 400
}
```
A missing `status_code` means 302; a non-numeric one is a 400.
</details>

## Experiments

1. `curl -i localhost:8080/redirect/3` then `curl -iL localhost:8080/redirect/3`.
   Count the responses.
2. **Predict** the method gobin's `/anything` reports for each, then run:
   ```sh
   curl -L -d a=1 'localhost:8080/redirect-to?url=/anything&status_code=302'
   curl -L -d a=1 'localhost:8080/redirect-to?url=/anything&status_code=307'
   curl -L -d a=1 'localhost:8080/redirect-to?url=/anything&status_code=303'
   ```
3. `curl -i 'localhost:8080/redirect-to?url=anything'` (no slash). **Predict**
   the `Location`. Then `curl -i 'localhost:8080/redirect-to?url=//example.com'`.
4. `curl -i -H 'Host: evil.example' localhost:8080/absolute-redirect/1`. Read
   the `Location`, and think about what a shared cache would do with it.

## Explore further

- Add `/relative-redirect/{n}`, and make `/redirect/{n}?absolute=true` use
  absolute URLs (httpbin has both).
- Write a **safe** redirect helper: accepts only paths starting with a single
  `/` (not `//`), and rejects anything with a scheme or host. Test it with
  `//evil.com`, `/\evil.com`, `https:evil.com` and `%2F%2Fevil.com`.
- Write middleware that redirects plain-HTTP requests to HTTPS with `308`
  (you'll have TLS in stage 24). Why 308 and not 301?
- Implement Post/Redirect/Get: a `POST /notes` that stores a note and `303`s
  to `GET /notes/{id}`.

## Check your understanding

```sh
go run ./cmd/course quiz 11
```

**Explain it back:** When would you choose 303, 307 and 308, and what
happens to a POST body under each?

## Reference

- [RFC 9110 §15.4: Redirection 3xx](https://www.rfc-editor.org/rfc/rfc9110#section-15.4)
- [`http.Redirect`](https://pkg.go.dev/net/http#Redirect), [`http.RedirectHandler`](https://pkg.go.dev/net/http#RedirectHandler)
- [OWASP: Unvalidated redirects and forwards](https://cheatsheetseries.owasp.org/cheatsheets/Unvalidated_Redirects_and_Forwards_Cheat_Sheet.html)

**Next:** [Stage 12: Following redirects](../12-following-redirects/README.md)
