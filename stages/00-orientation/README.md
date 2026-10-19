# Stage 0: Orientation

> **Part 0 · Orientation** · **You build:** nothing yet · **Time:** ~30 min

Welcome! Over 26 stages you'll learn Go's `net/http` package from both sides,
serving requests and making them, by building six small tools. This stage
sets up your environment and gives you the mental model of HTTP that every
later stage builds on.

## 1. Check your setup

```sh
go version                      # need go1.23 or newer
go run ./cmd/course             # the course runner: progress + next step
go run ./cmd/course test 1      # run stage 1's tests. They fail: you haven't started!
```

Seeing red is the normal starting point of every stage. Red tells you
what to do next.

Now skim [docs/LEARNING.md](../../docs/LEARNING.md). It's short, and it explains
why each stage has predictions, experiments, quizzes and reviews, and how to
use them.

## 2. Before you read: predict

Answer these in your head, or better, on paper. Being wrong is fine; it's
the point (see "pre-testing" in LEARNING.md). The answers are in the reading.

1. When your browser fetches a page, what does the very first line of the
   request it sends look like?
2. How does the receiver of an HTTP message know where the body ends?
3. Is `Content-Type` the same header as `content-type`?

## 3. Reading: HTTP on the wire

### A request is text

HTTP/1.1 is a **text protocol over a TCP connection**. A client opens a
connection and writes something like this (`\r\n` is a carriage return +
line feed, written CRLF):

```
GET /search?q=gopher HTTP/1.1\r\n          ← request line: METHOD SP TARGET SP VERSION
Host: example.com\r\n                      ← header fields: Name: value
User-Agent: curl/8.5.0\r\n
Accept: */*\r\n
\r\n                                       ← empty line: end of headers
                                           ← (no body for this GET)
```

The **request line** has three parts separated by single spaces:

- the **method**: what the client wants to do (`GET`, `POST`, `PUT`, `DELETE`, ...),
- the **request target**: usually the path plus query string, `/search?q=gopher`,
- the **protocol version**: `HTTP/1.1`.

Then come **header fields**, one per line, `Name: value`. Header names are
**case-insensitive** (`Content-Type` and `content-type` are the same header),
and a header may appear more than once. An **empty line** ends the headers.
HTTP/1.1 requires a `Host` header, because one IP address can serve many sites.

### A response is text too

```
HTTP/1.1 200 OK\r\n                        ← status line: VERSION SP CODE SP REASON
Content-Type: text/html; charset=utf-8\r\n
Content-Length: 13\r\n
Date: Mon, 01 Jan 2024 00:00:00 GMT\r\n
\r\n
<h1>Hi!</h1>\n                             ← body: exactly 13 bytes
```

The **status code** says how it went. The first digit is the class:

| Class | Meaning | Examples |
|---|---|---|
| 1xx | informational | `100 Continue`, `103 Early Hints` |
| 2xx | success | `200 OK`, `201 Created`, `204 No Content`, `206 Partial Content` |
| 3xx | redirection | `301 Moved Permanently`, `302 Found`, `304 Not Modified`, `307`, `308` |
| 4xx | client error | `400 Bad Request`, `401 Unauthorized`, `404 Not Found`, `405 Method Not Allowed`, `413`, `429` |
| 5xx | server error | `500 Internal Server Error`, `502 Bad Gateway`, `503 Service Unavailable` |

### Where does the body end?

TCP is just a stream of bytes; it has no "messages". So HTTP has to say where
a body ends. There are three ways:

1. **`Content-Length: N`**: the body is exactly N bytes.
2. **`Transfer-Encoding: chunked`**: the body arrives in chunks, each
   prefixed by its size in hex, ending with a zero-size chunk. This is used when
   the sender doesn't know the length up front (e.g. streaming).
3. **Close the connection** (responses only): with neither header, the body
   runs until the server closes the connection. This is common in HTTP/1.0;
   an HTTP/1.1 server doing it must close and should say `Connection: close`.

Getting this right is what lets **keep-alive** work: in HTTP/1.1 a connection
stays open after a response, and the next request reuses it, saving a TCP
(and TLS) handshake. You'll see how much that matters in stage 25.

### Methods have meanings

| Method | Used for | Safe? | Idempotent? |
|---|---|---|---|
| `GET` | read a resource | yes | yes |
| `HEAD` | like GET, but headers only | yes | yes |
| `POST` | submit data / create / "do something" | no | **no** |
| `PUT` | create or replace at a known URL | no | yes |
| `PATCH` | partial update | no | no (in general) |
| `DELETE` | remove | no | yes |
| `OPTIONS` | what's allowed? (CORS preflight) | yes | yes |

*Safe* means it shouldn't change anything. *Idempotent* means sending it twice
has the same effect as sending it once. This matters when a client wonders
whether it's OK to **retry** (stage 16).

### URLs

```
https://user:pass@api.example.com:8443/v1/items/42?sort=asc&tag=a&tag=b#reviews
└─┬─┘   └───┬───┘ └──────┬──────┘ └┬─┘└─────┬─────┘└────────┬────────┘ └──┬──┘
scheme  userinfo        host      port     path            query       fragment
```

The **fragment** is never sent to the server. Characters with special meaning
(`/ ? # & = space %`...) must be **percent-encoded** when they're data:
a space is `%20` (or `+` in a query string), `/` inside a path segment is `%2F`.
Go's `net/url` package parses and builds these; you'll use it a lot.

### HTTP/2 and HTTP/3

HTTP/2 (2015) and HTTP/3 (2022) keep the same **semantics**: methods,
status codes, headers, bodies. But they change the **wire format**: binary
frames, many requests multiplexed over one connection, compressed headers.
Go's `net/http` speaks HTTP/1.1 and HTTP/2 transparently (HTTP/2 over TLS is
negotiated automatically, stage 24); your handler code doesn't change.
HTTP/3 (over QUIC/UDP) isn't in the standard library.

### The shape of `net/http`

Keep this map in mind. You'll meet every box.

```
SERVER SIDE                                   CLIENT SIDE

http.Server  (address, timeouts, TLS)         http.Client  (redirects, cookies, overall timeout)
   │ accepts TCP connections,                    │ follows policy, then delegates to
   │ one goroutine per connection                ▼
   │ parses each request into               http.RoundTripper interface
   ▼                                             │ usually *http.Transport: connection pool,
http.Handler interface                           │ TLS, HTTP/2, proxies, dialing
   ServeHTTP(http.ResponseWriter, *http.Request) ▼
   │                                         sends *http.Request, returns *http.Response
   ├─ http.HandlerFunc (adapter)
   ├─ *http.ServeMux  (router)
   └─ your middleware: Handler → Handler      your middleware: RoundTripper → RoundTripper

Shared types: http.Request, http.Response, http.Header, http.Cookie, url.URL
Test helpers: net/http/httptest (NewServer, NewRecorder, NewRequest)
```

Notice the symmetry: a server is a `Handler` (request in, response out), and
the client's engine is a `RoundTripper` (request in, response out). Both can
be wrapped to add behaviour. That's middleware on each side (stages 15 and 16).

### Answers to "predict"

<details>
<summary>Reveal</summary>

1. `GET /path?query HTTP/1.1` (method, target, version), followed by headers
   and an empty line. In HTTP/2 the same information travels as binary
   "pseudo-headers" (`:method`, `:path`, ...).
2. `Content-Length`, chunked transfer encoding, or (for responses only) the
   connection closing.
3. Yes. Header names are case-insensitive. Go normalises them to
   "canonical" form, `Content-Type`, as you'll see in stage 4.

</details>

## 4. Experiments

Predict first, then run. If you have `curl` installed (it ships with macOS and
Windows 10+):

```sh
curl -v http://example.com/ -o /dev/null
```

Lines starting with `>` are what curl sent, `<` what it received, `*` are
curl's notes. **Predict:** which headers will curl send that you didn't ask for?

Talk HTTP by hand with `nc` (netcat), if you have it:

```sh
printf 'GET / HTTP/1.1\r\nHost: example.com\r\nConnection: close\r\n\r\n' | nc example.com 80
```

**Predict:** what happens if you leave out the `Host` line? Or send `get`
in lowercase? (Servers vary: try it.)

## 5. Check your understanding

```sh
go run ./cmd/course quiz 0
```

**Explain it back:** in your journal, describe in 3–5 sentences what happens,
byte by byte, when a client fetches `http://example.com/hello`, from opening
the TCP connection to knowing the response is complete.

## Further reading

- [MDN: An overview of HTTP](https://developer.mozilla.org/en-US/docs/Web/HTTP/Overview)
- [RFC 9110: HTTP Semantics](https://www.rfc-editor.org/rfc/rfc9110) (methods, status codes, headers) and
  [RFC 9112: HTTP/1.1](https://www.rfc-editor.org/rfc/rfc9112) (the wire format). They're surprisingly readable.
- [`net/http` package docs](https://pkg.go.dev/net/http)

**Next:** [Stage 1: HTTP is just text](../01-raw-http/README.md)
