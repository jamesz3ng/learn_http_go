# `net/http` cheat sheet

A compact reference to what the course covers. The number in brackets is the
stage that explains it.

## Server

### Handlers and routing

```go
type Handler interface{ ServeHTTP(http.ResponseWriter, *http.Request) }  // [2]
http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { ... })   // adapter [2]

mux := http.NewServeMux()                       // never the global DefaultServeMux [2]
mux.HandleFunc("GET /{$}", home)                // exact root [2]
mux.HandleFunc("GET /items/{id}", getItem)      // r.PathValue("id") [8]
mux.HandleFunc("/files/{path...}", files)       // rest of the path, all methods [6, 8]
mux.Handle("GET /static/", http.StripPrefix("/static", http.FileServerFS(fsys))) // [21]
```

Most specific pattern wins; conflicting patterns panic at registration; `GET`
also matches `HEAD`; wrong method → automatic `405` + `Allow` [2, 8].

### Reading the request

| Need | Code | Stage |
|---|---|---|
| method, path | `r.Method`, `r.URL.Path` | 4 |
| query | `q := r.URL.Query(); q.Get("x"); q["x"]` | 4 |
| header | `r.Header.Get("X-Thing")`, `r.Header.Values(...)`, `r.UserAgent()` | 4 |
| host | `r.Host` (not in `r.Header`) | 4 |
| client IP | `host, _, _ := net.SplitHostPort(r.RemoteAddr)` | 4 |
| over TLS? | `r.TLS != nil` | 4, 24 |
| body (limited!) | `r.Body = http.MaxBytesReader(w, r.Body, n)`, then `io.ReadAll` / `json.NewDecoder` | 6 |
| content type | `mt, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))` | 6 |
| form | `r.ParseForm()` → `r.PostForm` (body), `r.Form` (body + query) | 6 |
| multipart | `r.ParseMultipartForm(mem)` or stream with `r.MultipartReader()` | 6, 21 |
| cookies | `r.Cookie("name")`, `r.Cookies()` | 13 |
| basic auth | `user, pass, ok := r.BasicAuth()` | 13 |
| cancellation | `r.Context()` (done when the client leaves) | 17 |

### Writing the response

```go
w.Header().Set("Content-Type", "application/json") // 1. headers
w.WriteHeader(http.StatusCreated)                  // 2. status (implicit 200 on first Write)
json.NewEncoder(w).Encode(v)                       // 3. body
```

| Need | Code | Stage |
|---|---|---|
| plain-text error | `http.Error(w, "msg", code)` (sets `text/plain`) | 6 |
| redirect | `http.Redirect(w, r, url, http.StatusSeeOther)` | 11 |
| cookie | `http.SetCookie(w, &http.Cookie{...})`, delete with `MaxAge: -1` | 13 |
| content with Range/ETag/304 | set `ETag`, then `http.ServeContent(w, r, name, mod, readSeeker)` | 21 |
| flush / deadlines | `rc := http.NewResponseController(w); rc.Flush(); rc.SetWriteDeadline(t)` | 15, 19 |
| no body | `204`, `304`: don't write one | 9 |

### Middleware

```go
func mw(next http.Handler) http.Handler {                              // [15]
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // before
        next.ServeHTTP(w, r)
        // after
    })
}
```

ResponseWriter wrappers need `Unwrap() http.ResponseWriter` [15]. Context
values use unexported key types [15]. Recover panics inside the logging
middleware [15].

### The server itself

```go
srv := &http.Server{
    Addr: ":8080", Handler: h,
    ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second,  // [17]
    WriteTimeout: 30 * time.Second, IdleTimeout: 2 * time.Minute,
}
go srv.Serve(ln)                                   // ErrServerClosed after Shutdown [23]
srv.Shutdown(ctx)                                  // graceful: waits for requests [23]
srv.RegisterOnShutdown(func() { close(stopStreams) })                  // [23]
srv.ServeTLS(ln, "cert.pem", "key.pem")            // HTTPS + HTTP/2 [24]
```

## Client

### Requests

```go
req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body)) // [5, 7]
req.Header.Set("Content-Type", "application/json")
req.Host = "example.test"                          // not req.Header["Host"] [5]
req.SetBasicAuth(user, pass)                       // [14]
resp, err := client.Do(req)
if err != nil { return err }                       // no response at all [3]
defer resp.Body.Close()                            // always [3]
if resp.StatusCode != http.StatusOK { ... }        // statuses are not errors [3]
io.Copy(dst, resp.Body)                            // stream; read to EOF for reuse [3, 25]
```

| Need | Code | Stage |
|---|---|---|
| query string | `url.Values{...}.Encode()` | 7, 10 |
| path segment | `url.PathEscape(s)`; `base.JoinPath("kv", url.PathEscape(k))` | 10 |
| replayable body | `*bytes.Reader`, `*bytes.Buffer`, `*strings.Reader` set `ContentLength` + `GetBody` | 7 |
| multipart | `multipart.NewWriter(w)`, `CreatePart`, `FormDataContentType()`, `Close()` | 22 |
| streaming upload | `io.Pipe` + goroutine + `CloseWithError` | 22 |

### The client and its transport

```go
tr := http.DefaultTransport.(*http.Transport).Clone()                     // keep the defaults [18]
tr.DialContext = (&net.Dialer{Timeout: 3 * time.Second}).DialContext      // connect timeout [18]
tr.MaxIdleConnsPerHost = 64                                               // default 2! [25]
tr.TLSClientConfig = &tls.Config{RootCAs: pool}                           // custom CA [24]
tr.ForceAttemptHTTP2 = true                                               // already true after Clone; needed on a hand-built Transport [24]
jar, _ := cookiejar.New(nil)                                              // [14]
client := &http.Client{
    Transport: tr,
    Timeout:   10 * time.Second,                   // whole exchange incl. body; not for streams [18]
    Jar:       jar,
    CheckRedirect: func(req *http.Request, via []*http.Request) error {   // [12]
        return http.ErrUseLastResponse             // don't follow
    },
}
```

### Errors

```go
errors.Is(err, context.DeadlineExceeded)           // timeout [18]
var ne net.Error; errors.As(err, &ne) && ne.Timeout()
errors.Is(err, context.Canceled)                   // caller cancelled [18]
var op *net.OpError; errors.As(err, &op) && op.Op == "dial"      // couldn't connect [3]
var dns *net.DNSError; errors.As(err, &dns)        // name didn't resolve [3]
var cert *tls.CertificateVerificationError; errors.As(err, &cert) // bad certificate [24]
```

### RoundTripper middleware

```go
type roundTripperFunc func(*http.Request) (*http.Response, error)
func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
// clone before modifying; don't interpret statuses; close the request body;
// drain+close responses you discard [16]
```

### Tracing

```go
trace := &httptrace.ClientTrace{GotConn: func(i httptrace.GotConnInfo) { _ = i.Reused }} // [25]
ctx = httptrace.WithClientTrace(ctx, trace)
```

## Testing

```go
rec := httptest.NewRecorder()                             // call a handler directly [2]
h.ServeHTTP(rec, httptest.NewRequest("GET", "/x", nil))   // rec.Code, rec.Body
srv := httptest.NewServer(h); defer srv.Close()           // real server, random port [2]
srv := httptest.NewUnstartedServer(h)                     // configure first:
srv.Config.ConnState = ...; srv.EnableHTTP2 = true; srv.StartTLS() // [24, 25]
srv.Client()                                              // trusts srv's certificate [24]
```

Run tests with `-race` whenever handlers share state [9].

## Status codes used in the course

| Code | When | Stage |
|---|---|---|
| 200 / 201 + `Location` / 204 | OK / created / done, no body | 2, 9 |
| 206 / 416 | partial content / range not satisfiable | 21, 22 |
| 301 / 302 / 303 / 307 / 308 | redirects (303: then GET; 307/308: keep method and body) | 11, 12 |
| 304 | not modified (conditional GET) | 21 |
| 400 / 401 + `WWW-Authenticate` / 403 / 404 / 405 + `Allow` | bad input / unauthenticated / forbidden / missing / wrong method | 2, 8, 13 |
| 412 / 413 / 415 / 429 + `Retry-After` | precondition failed / too large / wrong media type / slow down | 6, 16, 21 |
| 499 (log only) | client closed request | 17 |
| 500 / 502 / 503 / 504 | bug / bad gateway / unavailable / gateway timeout | 15, 16, 26 |
