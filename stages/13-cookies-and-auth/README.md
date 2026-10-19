# Stage 13: Cookies and authentication

> **Part IV · Protocol semantics** · **You build:** `gobin` · **Time:** ~60 min

## Why this stage matters

HTTP is stateless: each request stands alone. **Cookies** let a server
hand the client a bit of state to send back with every later request; that's
how logins, carts and preferences work. **Authorization headers** let a
client prove who it is on each request. Both have subtle rules (deleting a
cookie, the right status codes, comparing secrets in constant time) that are
easy to get wrong. gobin gets httpbin's cookie and auth endpoints.

## Before you read: predict

1. How does a server delete a cookie from the client?
2. A request has no credentials. 401 or 403?
3. Why not compare a password with `==`?

## Reading

### Cookies on the wire

```
HTTP/1.1 302 Found                                        ← server sets cookies
Set-Cookie: session=abc123; Path=/; HttpOnly; Secure; SameSite=Lax
Set-Cookie: theme=dark; Path=/; Max-Age=31536000

GET /cookies HTTP/1.1                                     ← client sends them back
Cookie: session=abc123; theme=dark
```

- **One `Set-Cookie` header per cookie** (it's one of the few headers that can't
  be combined with commas), each with optional attributes.
- The client sends **one `Cookie` header**: just `name=value` pairs. The
  attributes stay on the client and decide *whether* to send the cookie.

### `http.Cookie`

```go
http.SetCookie(w, &http.Cookie{
    Name:     "session",
    Value:    "abc123",
    Path:     "/",                  // which paths it's sent to (default: the current "directory")
    MaxAge:   3600,                 // seconds; see below
    HttpOnly: true,                 // JavaScript can't read it (limits XSS damage)
    Secure:   true,                 // only sent over HTTPS
    SameSite: http.SameSiteLaxMode, // not sent on most cross-site requests (CSRF defence)
})
```

`MaxAge` has a Go-specific trap:

| `MaxAge` | On the wire | Meaning |
|---|---|---|
| `0` | (nothing) | unspecified: a **session cookie**, gone when the browser closes |
| `> 0` | `Max-Age=N` | expires in N seconds |
| `< 0` | `Max-Age=0` | **delete it now** |

To **delete** a cookie, set one with the same `Name` and `Path` (and
`Domain`, if it had one) and `MaxAge: -1`. Adding `Expires: time.Unix(0, 0)`
helps very old clients.

Reading cookies on the server:

```go
c, err := r.Cookie("session")   // err == http.ErrNoCookie if absent
for _, c := range r.Cookies() { // all of them
    fmt.Println(c.Name, c.Value)
}
```

Cookie values are restricted: when writing, Go silently drops `;`, `"`, `\`,
control characters and non-ASCII bytes (logging a warning), and wraps values
containing spaces or commas in quotes. Encode arbitrary data (e.g. with
`base64.URLEncoding`) first. And remember the
client can edit cookies freely. Never trust a cookie value you didn't sign
(see Explore).

### Basic authentication

The client sends `Authorization: Basic base64("user:password")`:

```go
user, pass, ok := r.BasicAuth() // decodes the header; ok == false if absent or malformed
```

If the credentials are missing or wrong, answer **`401 Unauthorized`** with a
**`WWW-Authenticate`** header saying how to authenticate. Browsers show a
login dialog when they see it:

```go
w.Header().Set("WWW-Authenticate", `Basic realm="gobin"`)
```

Base64 is encoding, not encryption, so Basic auth is only acceptable over HTTPS.

### Comparing secrets in constant time

`==` on strings stops at the first differing byte. An attacker who can time
responses precisely can, in principle, discover a secret byte by byte. Use:

```go
subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
```

It takes the same time wherever the inputs differ. (It returns at once if
the *lengths* differ, which leaks the length. Comparing SHA-256 hashes of both
avoids even that.) Also compare username **and** password before deciding,
so the timing doesn't reveal which was wrong.

### Bearer tokens

APIs mostly use `Authorization: Bearer <token>`, where the token is an API key,
an OAuth access token or a JWT. Auth scheme names are case-insensitive
(`bearer` is fine). On failure, `401` with `WWW-Authenticate: Bearer`.

### 401 vs 403

- **401 Unauthorized**, despite the name, means **unauthenticated**: "I don't
  know who you are; send credentials." Must include `WWW-Authenticate`.
- **403 Forbidden** means "I know who you are, and you may not do this."

> **📖 Source dive:** `go doc -src net/http.Cookie.String` shows how a cookie
> becomes a `Set-Cookie` line, including `MaxAge` and `sanitizeCookieValue`.
> Then `go doc -src net/http.Request.BasicAuth`.

## Your task

| Endpoint | Behaviour |
|---|---|
| `GET /cookies` | `{"cookies": {"name": "value", ...}}` (`{}` when none) |
| `GET /cookies/set?n1=v1&n2=v2` | set each cookie (with `Path=/`), then `302` to `/cookies` |
| `GET /cookies/delete?n1&n2` | delete each named cookie (`Path=/`, `Max-Age=0`), then `302` to `/cookies` |
| `GET /basic-auth/{user}/{passwd}` | matching Basic credentials → `200` `{"authenticated": true, "user": "..."}`; otherwise `401` with `WWW-Authenticate: Basic realm="gobin"` and `{"authenticated": false}` |
| `GET /bearer` | `Authorization: Bearer <non-empty token>` → `200` `{"authenticated": true, "token": "..."}`; otherwise `401` with `WWW-Authenticate: Bearer` and `{"authenticated": false}` |

Compare credentials in constant time.

```sh
go run ./cmd/course test 13
```

## Hints

<details>
<summary>Hint 1: /cookies/set</summary>

Loop over `r.URL.Query()`; for each name, `http.SetCookie` with the (last)
value and `Path: "/"`; then `http.Redirect(w, r, "/cookies", http.StatusFound)`.
Headers set before the redirect are sent with it.
</details>

<details>
<summary>Hint 2: parsing a Bearer header</summary>

`scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")`, then
`strings.EqualFold(scheme, "Bearer")` and a non-empty `strings.TrimSpace(token)`.
</details>

## Experiments

1. In a browser, open `http://localhost:8080/cookies/set?flavour=choc`, then
   look at the cookie in the developer tools (Application/Storage tab). Now open
   `/cookies/delete?flavour`.
2. curl keeps cookies in a jar file:
   ```sh
   curl -c jar.txt -L 'localhost:8080/cookies/set?a=1&b=2'
   cat jar.txt
   curl -b jar.txt localhost:8080/cookies
   ```
3. Open `http://localhost:8080/basic-auth/me/pw` in a browser. Where does the
   login dialog come from? Remove the `WWW-Authenticate` header and try again.
4. `curl -u alice:s3cret -v localhost:8080/basic-auth/alice/s3cret`. Decode
   the `Authorization` header: `echo <value> | base64 -d`.

## Explore further

- **Signed cookies:** add `/cookies/signed/set` and `/cookies/signed` that
  store `value + "." + base64(HMAC-SHA256(key, value))` and reject tampered
  cookies. Use `crypto/hmac` and `hmac.Equal`.
- Make `/cookies/set` accept `?secure&httponly&samesite=strict` and show what
  browsers do with each.
- **Protect writes:** add `Config.APIToken`; if set, `PUT` and `DELETE` on
  `/kv` require `Authorization: Bearer <token>` (401 without, 403 for a wrong
  one?). Write it as middleware wrapping only those routes. (You'll learn more
  about middleware in stage 15.)
- Read about cookie prefixes: `__Host-` and `__Secure-`. What do they guarantee?

## Check your understanding

```sh
go run ./cmd/course quiz 13
```

**Explain it back:** How do you delete a cookie in Go, and why is
`MaxAge: 0` not the way?

## Reference

- [`http.Cookie`](https://pkg.go.dev/net/http#Cookie), [`http.SetCookie`](https://pkg.go.dev/net/http#SetCookie), [`Request.BasicAuth`](https://pkg.go.dev/net/http#Request.BasicAuth)
- [RFC 6265: HTTP State Management (cookies)](https://www.rfc-editor.org/rfc/rfc6265), [MDN: Using HTTP cookies](https://developer.mozilla.org/en-US/docs/Web/HTTP/Cookies)
- [`crypto/subtle.ConstantTimeCompare`](https://pkg.go.dev/crypto/subtle#ConstantTimeCompare)

**Next:** [Stage 14: Cookie jars and credentials](../14-client-state/README.md)
