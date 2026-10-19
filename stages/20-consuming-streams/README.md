# Stage 20: Consuming streams

> **Part VII · Streams and files** · **You build:** `kvclient` · **Time:** ~75 min

## Why this stage matters

The other half of stage 19: a client that consumes an endless response,
handling events as they arrive, stopping cleanly when asked, and never
confusing "I cancelled" with "the network broke". `resp.Body` is just an
`io.Reader`, so you'll parse it incrementally. You'll also see why your
client's timeout settings must be different for streams.

## Before you read: predict

1. `io.ReadAll(resp.Body)` on an endless event stream: what happens?
2. `bufio.Scanner` reads lines. What happens when a line is 200 KB long?
3. You want to stop watching after the third event. How do you make the
   server stop sending?

## Reading

### Reading a body as it arrives

`resp.Body.Read` returns whatever has arrived so far, so a line-by-line scanner
naturally processes each event as soon as it's complete:

```go
sc := bufio.NewScanner(resp.Body)
for sc.Scan() {
    line := sc.Text() // without the trailing "\n"
    ...
}
if err := sc.Err(); err != nil { ... } // nil on a clean EOF
```

`io.ReadAll` would wait for the end of the stream, which may never come.

**Scanner's line limit.** By default a token (line) can be at most 64 KiB
(`bufio.MaxScanTokenSize`). Longer lines make `Scan` stop with
`bufio.ErrTooLong`. Raise the limit:

```go
sc.Buffer(make([]byte, 64<<10), 2<<20) // initial buffer, max line size
```

### Parsing SSE

The rules you need (a subset of the HTML spec):

- Lines end with `\n` (or `\r\n`: trim a trailing `\r`).
- A line starting with `:` is a comment: ignore it.
- Otherwise `field:value`; if the value starts with one space, drop that space.
  - `event`: the event type (default `message`)
  - `data`: append to the data buffer; **several `data` lines are joined with `\n`**
  - `id`: the event id
  - anything else (e.g. `retry`): ignore
- A **blank line** dispatches the event (if it has data) and resets the fields.

For gobin's events, `data` is JSON: `{"key": "...", "value": "..."}`, where
`value` is absent for deletes. A `*string` field tells "absent" from "empty":

```go
var payload struct {
    Key   string  `json:"key"`
    Value *string `json:"value"` // nil if absent
}
```

### Callback or channel?

Two common shapes for a streaming API:

```go
// A: callback. Watch blocks until the stream ends.
err := c.Watch(ctx, "user:", func(e kvclient.Event) error { ...; return nil })

// B: channel. Watch returns immediately; a goroutine feeds the channel.
events, errc := c.WatchChan(ctx, "user:")
```

The callback version is simpler to get right: no goroutine to leak, natural
back-pressure (the next event isn't read until `fn` returns), and errors flow
back as return values. A channel API is easy to build **on top of** it.
kvclient uses the callback.

### Stopping a stream

- **The caller cancels `ctx`.** The transport aborts the connection, and the
  blocked `Read` returns an error. Return **`ctx.Err()`**, not that read
  error: the caller asked for this, and should get `context.Canceled`.
- **The callback returns an error.** Stop reading and return it. Your
  `defer resp.Body.Close()` closes the connection, and the server sees the
  disconnect (stage 19's handler stops too).
- **The server ends the stream** (clean EOF). Return `nil`.

### Timeouts and streams

`http.Client.Timeout` covers reading the body (stage 18), so a client with a
10s timeout kills every watch after 10s. Long-lived streams should be bounded
by the **context** the caller controls. If you need to detect a dead stream,
use an *idle* timeout instead: reset a timer on every line, including
keepalive comments.

> **📖 Source dive:** `go doc -src bufio.Scanner.Scan` and look for
> `ErrTooLong`. Then `go doc bufio.ScanLines`: what does it do with `\r`?

## Your task

Implement `Watch` in kvclient:

```go
func (c *Client) Watch(ctx context.Context, prefix string, fn func(Event) error) error
```

| Requirement | Detail |
|---|---|
| request | `GET {base}/watch?prefix=...` (query-encoded), `Accept: text/event-stream` |
| non-200 | return an `*APIError` (as in stage 10) |
| parsing | the SSE subset above: comments, `\r\n`, multi-line `data`, `event`, `id` |
| events | `fn(Event{ID, Type, Key, Value})`, in order, **as they arrive**; `Value` is `nil` for deletes |
| big events | values over 64 KiB (up to 1 MiB) must work |
| `fn` returns an error | stop, close the stream, return that error |
| `ctx` cancelled | return promptly with an error matching `context.Canceled` |
| client timeout | if the `*http.Client` has a `Timeout` (stage 10's default does), it must **not** cut the stream short: send the request with a copy of the client whose `Timeout` is 0 |
| server ends the stream | return `nil` |

```sh
go run ./cmd/course test 20
go run ./cmd/kv watch user:     # with gobin running; PUT keys in another terminal
```

## Hints

<details>
<summary>Hint 1: separate parsing from HTTP</summary>

Write `readSSE(r io.Reader, dispatch func(fields) error) error` that knows
nothing about HTTP or JSON, and test it with `strings.NewReader`. `Watch`
does the request, then calls `readSSE` with a `dispatch` that decodes the JSON
and calls `fn`.
</details>

<details>
<summary>Hint 2: which error to return</summary>

After the read loop ends: `if ctx.Err() != nil { return ctx.Err() }`, then
return the loop's error (nil for EOF).
</details>

<details>
<summary>Hint 3: data split across lines</summary>

Collect `data` values in a `[]string`, then `strings.Join(data, "\n")` when
the blank line arrives. Newlines are valid whitespace inside JSON.
</details>

## Experiments

1. `go run ./cmd/kv watch` against gobin, then stop gobin. What does `Watch`
   return? Is that the right behaviour for a CLI? For a long-running service?
2. Create the client with `&http.Client{Timeout: 5 * time.Second}` and watch.
   **Predict** what happens after 5 seconds.
3. Remove your `sc.Buffer` call and PUT a 100 KB value while watching.

## Explore further

- **Reconnect:** wrap `Watch` in a loop that reconnects with backoff (stage 16)
  and sends `Last-Event-ID` with the last id seen (pair with stage 19's
  resumable-streams exploration).
- **Idle timeout:** abort the stream if nothing (not even a keepalive) arrives
  for 30s: `time.AfterFunc` + a `context.WithCancel` you cancel from it.
- Build `WatchChan(ctx, prefix) (<-chan Event, <-chan error)` on top of `Watch`.
  Who closes the channels, and when?
- Consume gobin's `/stream/{n}` NDJSON with `json.NewDecoder(resp.Body)` in a
  `for dec.More()` loop. The decoder reads one value at a time from the stream.

## Check your understanding

```sh
go run ./cmd/course quiz 20
```

**Explain it back:** Name the three ways `Watch` can end and what it returns
for each, and explain how cancelling `ctx` unblocks a `Read` that's waiting
for data.

## Reference

- [`bufio.Scanner`](https://pkg.go.dev/bufio#Scanner) (`Buffer`, `ErrTooLong`)
- [HTML Standard: interpreting an event stream](https://html.spec.whatwg.org/multipage/server-sent-events.html#event-stream-interpretation)
- [`json.Decoder`](https://pkg.go.dev/encoding/json#Decoder) (`More`)

**Next:** [Stage 21: Files, ranges and uploads](../21-files/README.md)
