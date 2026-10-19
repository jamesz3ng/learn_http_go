# Stage 1: HTTP is just text

> **Part I · Foundations** · **You build:** `rawhttp` · **Time:** ~60 min

## Why this stage matters

Before using `net/http`, you'll build a (tiny, fragile) HTTP server **without
it**, straight on TCP. It takes about 60 lines. Afterwards you'll know
exactly what `net/http` is doing for you, and things that otherwise feel like
magic (why `Content-Length` matters, why a slow client needs a timeout, why
every connection gets a goroutine) will make sense.

## Before you read: predict

1. A client sends `GET /hello HTTP/1.1` in one `Write` call. Will your
   server's `conn.Read` always receive exactly that line in one call?
2. Your server writes a response but never closes the connection and sends no
   `Content-Length`. What does `curl` do?
3. What happens to other clients if your server handles connections one at a
   time and one client connects but never sends anything?

## Reading

### Listening and accepting

The `net` package gives you raw TCP:

```go
ln, err := net.Listen("tcp", "127.0.0.1:8081") // a net.Listener
for {
    conn, err := ln.Accept() // blocks until a client connects; conn is a net.Conn
    if err != nil {
        return err // e.g. the listener was closed
    }
    // conn is an io.Reader and an io.Writer (plus Close, deadlines, addresses)
}
```

A `net.Conn` is a **byte stream** in each direction. There are no messages:
TCP may split one `Write` from the client into several `Read`s on your side,
or merge several writes into one read. So you can't assume one `Read` = one
line. You need **buffered reading**.

### Reading lines with `bufio`

`bufio.Reader` wraps a reader and lets you read up to a delimiter, no matter
how the bytes arrived:

```go
r := bufio.NewReader(conn)
line, err := r.ReadString('\n') // "GET /hello HTTP/1.1\r\n"
line = strings.TrimRight(line, "\r\n")
```

HTTP lines end in CRLF (`\r\n`). `ReadString('\n')` keeps the delimiter, so
trim both characters.

### Parsing the request

The request line is `METHOD SP TARGET SP VERSION`:

```go
parts := strings.Fields(line) // splits on runs of whitespace
if len(parts) != 3 || !strings.HasPrefix(parts[2], "HTTP/") {
    // not HTTP: answer 400 Bad Request
}
method, target := parts[0], parts[1]
```

Then read header lines until the empty line. Each is `Name: value`, and
names are **case-insensitive**, so compare them with `strings.EqualFold`:

```go
name, value, ok := strings.Cut(line, ":")   // split at the FIRST colon
if ok && strings.EqualFold(strings.TrimSpace(name), "Host") {
    host = strings.TrimSpace(value)          // values usually have a space before them
}
```

(Why the first colon? Because values may contain colons: `Host: localhost:8080`.)

You don't need to *use* the request body. But if you close the connection
while body bytes are still unread, the operating system may reset the
connection, and the client can lose your response. So read any
`Content-Length` header too, and discard that many bytes (capped, say at
1 MiB) with `io.CopyN(io.Discard, r, n)` before you answer.

### Writing the response

Write the status line, headers, an empty line, and the body, each line
ending in `\r\n`:

```go
body := "hello\n"
fmt.Fprintf(conn, "HTTP/1.1 200 OK\r\n")
fmt.Fprintf(conn, "Content-Type: text/plain; charset=utf-8\r\n")
fmt.Fprintf(conn, "Content-Length: %d\r\n", len(body))
fmt.Fprintf(conn, "Connection: close\r\n")
fmt.Fprintf(conn, "\r\n")
fmt.Fprint(conn, body)
conn.Close()
```

- `Content-Length` counts **bytes**, and `len(string)` in Go is bytes, so
  this is right even for non-ASCII text.
- `Connection: close` promises we'll close the connection after this
  response, so the client won't try to send a second request on it
  (keep-alive). Then actually close it.

### One goroutine per connection

If your accept loop handles each connection before accepting the next, one
client that connects and goes quiet blocks **everyone**. The fix is one line:

```go
go handle(conn)
```

Goroutines are cheap (a few KB of stack), so a goroutine per connection is
the normal Go design. `net/http` does exactly this.

> **📖 Source dive:** open `$(go env GOROOT)/src/net/http/server.go` and find
> `func (srv *Server) Serve(l net.Listener) error`. You'll recognise the
> loop: `l.Accept()`, then `go c.serve(connCtx)`. Notice what else it does:
> it backs off when `Accept` fails temporarily and tracks connection states.
> Then skim `func (c *conn) serve`: a loop that reads a request, calls your
> handler, finishes the response, and goes round again for keep-alive.

### What `net/http` adds on top

Your server works, but compare it with everything `net/http` handles: keep-alive
(several requests per connection), request bodies (`Content-Length` and
chunked), size limits on headers, timeouts for slow or malicious clients,
`Date` headers, content-type sniffing, `HEAD` requests, `Expect: 100-continue`,
protection against request smuggling, panics in handlers, TLS, HTTP/2...
That's what you'll be using from stage 2.

## Your task

Implement `Serve` in [`rawhttp/rawhttp.go`](../../rawhttp/rawhttp.go):

```go
func Serve(ln net.Listener) error
```

| Requirement | Detail |
|---|---|
| Accept loop | Accept connections until `ln.Accept` fails, then **return that error**. |
| Concurrency | Handle each connection in its own goroutine. |
| Parse | Read the request line and the header lines (up to the empty line). |
| Respond 200 | Status line `HTTP/1.1 200 OK`. Headers `Content-Type: text/plain; charset=utf-8`, a correct `Content-Length`, `Connection: close`. Body: three lines, `method=<METHOD>\ntarget=<TARGET>\nhost=<Host header value>\n`. If there's no `Host` header, use `host=` with nothing after it. |
| Respond 400 | If the request line doesn't have exactly three parts, or the version doesn't start with `HTTP/`: `HTTP/1.1 400 Bad Request` with body `bad request\n` (same headers). |
| Close | Close the connection after responding (ideally after discarding any request body, as described in the Reading). |

Example: for `GET /hello?x=1 HTTP/1.1` with `Host: example.com`, the body is

```
method=GET
target=/hello?x=1
host=example.com
```

Run the tests:

```sh
go run ./cmd/course test 1
```

The tests are in [`tests/stage01_raw_http_test.go`](../../tests/stage01_raw_http_test.go).
Notice that one of them uses Go's own `http.Client` to talk to your server.
If it can parse your response, you've spoken HTTP correctly.

## Hints

<details>
<summary>Hint 1: the shape of Serve</summary>

```go
func Serve(ln net.Listener) error {
    for {
        conn, err := ln.Accept()
        if err != nil {
            return err
        }
        go handle(conn)
    }
}
```
</details>

<details>
<summary>Hint 2: reading headers</summary>

After the request line, keep calling `r.ReadString('\n')`, trim `"\r\n"`,
and stop when the line is empty. Use `strings.Cut(line, ":")` and
`strings.EqualFold` to find `Host`.
</details>

<details>
<summary>Hint 3: a response helper</summary>

Write a `writeResponse(conn net.Conn, code int, reason, body string)` that
writes the status line and the three headers, the blank line and the body.
Use it for both the 200 and the 400.
</details>

<details>
<summary>Hint 4: "the client got EOF / malformed response"</summary>

Check every line ends with `\r\n`, including the empty line after the
headers, and that `Content-Length` equals `len(body)`. Make sure you
`defer conn.Close()` in `handle`.
</details>

## Experiments

Run your server with `go run ./cmd/rawhttp` (it listens on `:8081`).

1. `curl -v http://localhost:8081/hi?there=1`. **Predict** the body before running.
   Which request headers did curl send that you didn't ask for?
2. **Predict**, then try: remove the `Content-Length` header from your
   response. Does curl still work? Why? (Hint: what else marks the end of a
   response?) Now also remove `conn.Close()`. What happens now, and why?
3. Set `Content-Length` to `len(body) - 3`. Then to `len(body) + 10`.
   **Predict** what curl shows for each. (The second is a classic production bug.)
4. Comment out the `go` in `go handle(conn)`, then in one terminal run
   `nc localhost 8081` (type nothing), and in another run `curl localhost:8081`.
   What happens? Restore the `go`.

## Explore further

- **Keep-alive.** Serve several requests per connection: drop
  `Connection: close` and loop in `handle`. You'll have to read each request's
  body (using its `Content-Length`) before you can find the next request.
  When does the loop end?
- **Slowloris.** A client that sends one header byte every few seconds can hold
  your goroutine forever. Use `conn.SetReadDeadline` to cap the time to read
  the headers. (This is `http.Server.ReadHeaderTimeout`, stage 17.)
- **Chunked responses.** Send a body with `Transfer-Encoding: chunked`
  instead of `Content-Length`. The format: `<size in hex>\r\n<bytes>\r\n`
  repeated, then `0\r\n\r\n`.
- **Limits.** What happens if a client sends a 10 GB header line?
  `bufio.Reader` will happily keep reading. How would you cap it?

## Check your understanding

```sh
go run ./cmd/course quiz 1
```

**Explain it back:** Why can't you read an HTTP request with a single
`conn.Read`, and why does your server need a goroutine per connection?

## Reference

- [`net`](https://pkg.go.dev/net): `Listen`, `Listener`, `Conn`
- [`bufio.Reader`](https://pkg.go.dev/bufio#Reader)
- [RFC 9112 §2–3](https://www.rfc-editor.org/rfc/rfc9112#section-2): HTTP/1.1 message format

**Next:** [Stage 2: Handlers and ServeMux](../02-hello-server/README.md)
