# Stage 19: Streaming responses and SSE

> **Part VII · Streams and files** · **You build:** `gobin` · **Time:** ~120 min

## Why this stage matters

Not every response is "compute, then send". Progress updates, logs, live
dashboards and chat send data **as it happens**, over a response that stays
open. Go's `ResponseWriter` buffers by default, so streaming means flushing
deliberately, noticing when the client leaves, and getting out of the way
of the server's `WriteTimeout`. You'll build two streams: newline-delimited
JSON, and **Server-Sent Events** that push every change in gobin's key-value
store to any number of watchers, which also needs a small pub/sub system with
careful concurrency.

## Before you read: predict

1. A handler writes one line, sleeps 5s, writes another. When does the client
   see the first line?
2. A response is still being generated, so its length is unknown. How does the
   client know where the body ends?
3. One watcher's network is very slow. Should that slow down `PUT` requests
   from other clients?

## Reading

### Buffering and flushing

`ResponseWriter` collects your writes in a buffer (a few KB) and sends them
when the buffer fills or the handler returns. That's efficient for normal
responses, and it means a slow trickle of small writes reaches the client
**all at once at the end**. To push data now:

```go
rc := http.NewResponseController(w)
fmt.Fprintf(w, "line %d\n", i)
if err := rc.Flush(); err != nil {
    return // client gone, or flushing unsupported
}
```

Flushing before the handler returns means the length isn't known in advance,
so on HTTP/1.1 Go switches to `Transfer-Encoding: chunked` automatically (on
HTTP/2, data frames). The older spelling `w.(http.Flusher).Flush()` fails
through middleware wrappers that lack `Unwrap`; `ResponseController` handles
them (stage 15).

### Newline-delimited JSON

A simple streaming format: one JSON value per line (`application/x-ndjson`).
`json.Encoder.Encode` writes a newline after each value, so:

```go
enc := json.NewEncoder(w)
for i := range n {
    enc.Encode(map[string]any{"id": i})
    rc.Flush()
}
```

### Noticing the client left

A stream can outlive its client. Stop when:

- `r.Context().Done()` fires (the server noticed the disconnect), or
- a `Write` or `Flush` returns an error.

Put waits in a `select` with `r.Context().Done()`, as in stage 17.

### Streams vs `WriteTimeout`

The server's `WriteTimeout` (stage 17) is a deadline for the **whole
response**. A watch stream must live for hours. Lift the deadline for this
response only:

```go
rc.SetWriteDeadline(time.Time{}) // zero time = no deadline
```

(It returns an error for writers that don't support deadlines, like
`httptest.ResponseRecorder`, so ignore or log it.)

### Server-Sent Events

SSE is a standard text format for server→client event streams over plain
HTTP, with a browser API (`EventSource`) that reconnects automatically:

```
HTTP/1.1 200 OK
Content-Type: text/event-stream
Cache-Control: no-cache

id: 1
event: put
data: {"key":"user:1","value":"alice"}

id: 2
event: delete
data: {"key":"user:1"}

: lines starting with a colon are comments (handy as keepalives)

```

- Each event is a set of `field: value` lines ended by a **blank line**.
- `data:` is the payload (several `data:` lines are joined with `\n`);
  `event:` names the type (default `message`); `id:` lets a reconnecting
  client say where it left off (`Last-Event-ID` request header).
- Proxies often close idle connections, so send a comment every so often.

### Fan-out: a tiny pub/sub

Every `PUT`/`DELETE` must reach every watcher whose prefix matches. Give
each watcher a **buffered channel**, and keep the set in the store:

```go
type subscriber struct {
    prefix string
    ch     chan event // buffered, e.g. 64
}
```

Four rules make it correct:

1. **Publish in order.** Publish while holding the store's lock, in the same
   critical section as the change, so events can't be reordered.
2. **Never block a writer on a watcher.** A `PUT` handler must not wait for a
   slow client's network. Send without blocking, and if a watcher's buffer is
   full, **drop that watcher** (close its channel; its handler then ends the
   stream). The watcher can reconnect.
   ```go
   select {
   case sub.ch <- ev:
   default: // too slow
       delete(s.subs, sub)
       close(sub.ch)
   }
   ```
3. **Subscribe before sending headers.** Once the client sees `200`, it
   assumes it won't miss events. Subscribe first, then `WriteHeader` + `Flush`.
4. **Always unsubscribe** when the handler returns (`defer`), and make
   unsubscribe safe after a drop: only close a channel that's still in the
   set, so it's never closed twice.

The handler's loop then waits on everything that can happen:

```go
for {
    select {
    case ev, ok := <-sub.ch:
        if !ok { return }       // dropped for being slow
        writeEvent(w, ev); rc.Flush()
    case <-r.Context().Done():
        return                  // client left
    }
}
```

> **📖 Source dive:** `$(go env GOROOT)/src/net/http/server.go`: read the
> comment above `bufferBeforeChunkingSize` and `func (cw *chunkWriter) writeHeader`.
> When does Go decide between `Content-Length` and chunked encoding?

## Your task

**`GET /stream/{n}`** (`1 ≤ n ≤ 100`): `n` lines of JSON, `{"id": 0, ...}` to
`{"id": n-1, ...}` (add any other fields you like; only `id` is checked),
`Content-Type: application/x-ndjson`, each line
**flushed** as it's written. Optional `?interval=` (a Go duration, 0–5s) waits
between lines. Invalid input → `400`. Stop early if the client leaves.

**`GET /watch?prefix=P`**: Server-Sent Events for every change to keys
starting with `P` (all keys if empty):

| Requirement | Detail |
|---|---|
| headers | `200`, `Content-Type: text/event-stream`, `Cache-Control: no-cache`; sent and flushed **immediately**, after subscribing |
| put | `id: <n>` / `event: put` / `data: {"key":"...","value":"..."}` + blank line |
| delete | `id: <n>` / `event: delete` / `data: {"key":"..."}` + blank line |
| ids | increase with every change |
| slow watchers | never block `PUT`/`DELETE`; drop watchers that fall behind |
| lifetime | outlives the server's `WriteTimeout`; ends promptly when the client disconnects |

Streaming must work **through your middleware** (stage 15): the tests use
`NewHandler`, logging included.

```sh
go run ./cmd/course test 19 -race
```

## Hints

<details>
<summary>Hint 1: the first line arrives late</summary>

Either you're not flushing, or the flush doesn't reach the real writer: add
`Unwrap() http.ResponseWriter` to your middleware's wrapper and use
`http.NewResponseController(w).Flush()`.
</details>

<details>
<summary>Hint 2: where publishing happens</summary>

Give your store `subscribe(prefix)`, `unsubscribe(sub)` and a
`publishLocked(ev)` that `put` and `delete` call while they still hold the
lock. Sequence numbers for `id:` can be a counter in the store, incremented in
`publishLocked`.
</details>

<details>
<summary>Hint 3: the slow-watcher test times out</summary>

Somewhere a send to a subscriber channel blocks. Every send in
`publishLocked` must be a `select` with a `default` case.
</details>

<details>
<summary>Hint 4: the stream dies after WriteTimeout</summary>

Call `rc.SetWriteDeadline(time.Time{})` at the start of the watch handler.
</details>

## Experiments

1. `curl -N localhost:8080/stream/5?interval=1s` (`-N` turns off curl's own
   buffering). Remove your `Flush` call and try again.
2. Look at the wire: `curl -v --raw -N localhost:8080/stream/3?interval=500ms`.
   Find the chunk sizes (hex numbers on their own lines).
3. In one terminal `curl -N localhost:8080/watch`, in another
   `go run ./cmd/kv put 'user:1' alice` and `go run ./cmd/kv delete 'user:1'`.
4. In a browser on `http://localhost:8080/`, open the dev-tools console and run:
   ```js
   const es = new EventSource('/watch');
   es.addEventListener('put', e => console.log('put', e.data));
   ```
   Then PUT some keys. Restart gobin: watch the browser reconnect on its own.

## Explore further

- **Resumable streams:** keep the last 1,000 events in a ring buffer; when a
  client reconnects with `Last-Event-ID`, replay what it missed first.
- Serve a small HTML page (with `embed`) at `/live` that shows changes as they happen.
- Make the keepalive interval configurable and test that comments arrive.
- What does gzip middleware (stage 15 explore) do to a stream? How can it
  flush properly?
- Browsers allow ~6 HTTP/1.1 connections per host, and each `EventSource` holds
  one. How does HTTP/2 (stage 24) change that?

## Check your understanding

```sh
go run ./cmd/course quiz 19
```

**Explain it back:** Why must publishing never block, and what are all the
ways a watch handler's loop can end?

## Reference

- [`http.ResponseController`](https://pkg.go.dev/net/http#ResponseController) (`Flush`, `SetWriteDeadline`)
- [HTML Standard: Server-sent events](https://html.spec.whatwg.org/multipage/server-sent-events.html) (the format, §9.2.6), [MDN: Using server-sent events](https://developer.mozilla.org/en-US/docs/Web/API/Server-sent_events/Using_server-sent_events)
- [NDJSON](https://github.com/ndjson/ndjson-spec)

**Next:** [Stage 20: Consuming streams](../20-consuming-streams/README.md)
