# Stage 9: A concurrent REST resource

> **Part III · Resources** · **You build:** `gobin` · **Time:** ~75 min

## Why this stage matters

Until now gobin has been stateless. Real services store things, and the
moment two requests touch the same data you hit the most important fact
about Go HTTP servers: **every request runs in its own goroutine, all at the
same time.** In this stage you build a key-value store behind a REST API, get
the status codes right, and make it safe under concurrency. The race
detector will check your work.

## Before you read: predict

1. Two clients `PUT` to the same Go `map` at the same instant, without a lock.
   What's the worst that can happen?
2. What status code should creating something return? And replacing it?
3. A `204 No Content` response: can it have a body?

## Reading

### Designing the resource

| Request | Meaning | Success |
|---|---|---|
| `PUT /kv/{key}` | store the body under `key` (create or replace) | `201 Created` + `Location` if new, `204 No Content` if replaced |
| `GET /kv/{key}` | fetch the value | `200` with the raw bytes |
| `DELETE /kv/{key}` | remove it | `204 No Content` |
| `GET /kv?prefix=p` | list keys | `200` with `{"keys": [...]}` |

**`PUT` vs `POST` for creating.** `PUT` stores a representation *at the URL
the client chose*, and it's **idempotent**: doing it twice leaves the same
state. `POST` asks the server to create something *and choose its URL*,
reported in a `Location` header, and isn't idempotent. A key-value store, where
clients choose keys, is a natural fit for `PUT`.

### Status codes that say exactly what happened

- **`201 Created`**: a new resource exists. Include `Location` pointing at it.
- **`204 No Content`**: success, and there's deliberately **no body**. Don't
  write one: Go returns an error from `Write` after `WriteHeader(204)`.
- **`404 Not Found`**: no such key. Use the same JSON error shape as before:
  `{"error": "key not found"}`.
- **`400 Bad Request`**: an invalid key. **`413`**: an oversized value.

```go
w.Header().Set("Location", "/kv/"+url.PathEscape(key)) // escape: keys can contain "/" or spaces
w.WriteHeader(http.StatusCreated)
```

### Handlers run concurrently

`net/http` serves each connection in its own goroutine (and, with HTTP/2,
each request in its own goroutine). Two requests can run your handler
**at the same moment** on different CPU cores. A plain Go map is not safe for
concurrent writes; the runtime detects some cases and **crashes the whole
process**:

```
fatal error: concurrent map writes
```

That is not a panic you can `recover`. Other races don't crash; they
silently corrupt data. Protect shared state with a mutex:

```go
type store struct {
    mu   sync.RWMutex
    data map[string][]byte
}

func (s *store) get(key string) ([]byte, bool) {
    s.mu.RLock()         // many readers at once...
    defer s.mu.RUnlock()
    v, ok := s.data[key]
    return v, ok
}

func (s *store) put(key string, value []byte) (created bool) {
    s.mu.Lock()          // ...or one writer
    defer s.mu.Unlock()
    _, existed := s.data[key]
    s.data[key] = value
    return !existed
}
```

Two habits:

- **Don't do I/O while holding the lock.** Read the request body *first*,
  then lock, update, unlock. A slow client mustn't block everyone else.
- **Wrap the data in a type with methods**, so the locking lives in one
  place and handlers can't forget it.

### The race detector

Go has a built-in data-race detector. It instruments memory accesses and
reports two goroutines touching the same memory without synchronisation:

```sh
go run ./cmd/course test 9    # -race is on automatically when cgo works; or: go test -race -run TestStage09_ ./tests/
```

It needs cgo (a C compiler). It only finds races that actually happen while it
runs, so it pairs well with tests that hammer your code concurrently, like
`TestStage09_ConcurrentWrites`.

### No globals: state belongs to the handler

Create the store **inside** `NewHandler`, so each server gets its own. A common
structure is an `app` struct holding dependencies, with handlers as
methods:

```go
type app struct {
    kv *store
}

func NewHandler(cfg Config) http.Handler {
    a := &app{kv: newStore()}
    mux := http.NewServeMux()
    mux.HandleFunc("PUT /kv/{key}", a.kvPut)
    ...
}

func (a *app) kvPut(w http.ResponseWriter, r *http.Request) { ... a.kv.put(...) ... }
```

A method value like `a.kvPut` is a `func(http.ResponseWriter, *http.Request)`,
so it can be passed to `HandleFunc` directly. (Closures work too.)

### `null` vs `[]`

`json.Marshal` turns a nil slice into `null`. Clients iterating over
`keys` would then break on an empty store. Start from `keys := []string{}`.

> **📖 Source dive:** in `$(go env GOROOT)/src/net/http/server.go`, find
> `func (w *response) Write` and follow it to `bodyAllowedForStatus`
> (in `transfer.go`). Which status codes forbid a body?

## Your task

| Request | Response |
|---|---|
| `PUT /kv/{key}` | store the body. New key → `201` with `Location: /kv/<url.PathEscape(key)>`. Existing key → `204` |
| `GET /kv/{key}` | `200`, `Content-Type: application/octet-stream`, body = the value. Missing → `404` `{"error": ...}` |
| `HEAD /kv/{key}` | like GET without the body (free) |
| `DELETE /kv/{key}` | `204`; missing → `404` `{"error": ...}` |
| `GET /kv?prefix=P` | `200` `{"keys": [...]}`: keys starting with `P` (all if no prefix), **sorted**; `[]` when empty |

Rules:

- Keys are 1–256 bytes (after unescaping) → otherwise `400`.
- Values may be empty (an empty body is a valid value), up to 1 MiB → otherwise `413`.
- Keys can contain anything, including `/` and spaces, when the client escapes
  them (`/kv/a%2Fb%20c` stores key `a/b c`).
- Each `NewHandler` call has its own, empty store.
- Safe for concurrent use (run with `-race`).

```sh
go run ./cmd/course test 9 -race
```

## Hints

<details>
<summary>Hint 1: the store type</summary>

Start from the `store` in the Reading section and add `delete(key) bool` and
`keys(prefix string) []string`. `slices.Sort(keys)` sorts in place.
</details>

<details>
<summary>Hint 2: 413 for big values</summary>

Same pattern as stage 6: `r.Body = http.MaxBytesReader(w, r.Body, 1<<20)`,
`io.ReadAll`, then the `writeBodyError` helper.
</details>

<details>
<summary>Hint 3: the escaped-key test fails</summary>

`r.PathValue("key")` is already unescaped (`a/b c`). Escape again with
`url.PathEscape` only when building the `Location` header.
</details>

## Experiments

1. Remove the mutex and run `go test -race -run TestStage09_ConcurrentWrites ./tests/`.
   Read the race report: it shows the two goroutines' stacks. Then run without
   `-race` a few times. Do you get `fatal error: concurrent map writes`?
2. Using curl, **predict** each status code before running:
   ```sh
   curl -i -X PUT --data-binary 'hello' localhost:8080/kv/greeting
   curl -i -X PUT --data-binary 'again' localhost:8080/kv/greeting
   curl -i localhost:8080/kv/greeting
   curl -I localhost:8080/kv/greeting
   curl -i -X DELETE localhost:8080/kv/greeting
   curl -i -X DELETE localhost:8080/kv/greeting
   ```
3. `curl -i -X PUT --data-binary x 'localhost:8080/kv/a%2Fb'` then
   `curl -i -X PUT --data-binary x 'localhost:8080/kv/a/b'`. Explain the difference.
4. In a handler, try writing a body after `WriteHeader(204)` and check the
   error `Write` returns.

## Explore further

- **Optimistic concurrency:** return an `ETag` on GET, and make `PUT` with
  `If-Match: <etag>` fail with `412 Precondition Failed` if the value changed
  in between. This prevents the "lost update" problem. (`If-None-Match: *` on
  PUT means "create only".)
- **Expiry:** `PUT /kv/{key}?ttl=30s`. Who deletes expired keys, and when?
- **`POST /kv`**: the server picks the key, answering `201` with `Location`.
- **Pagination:** `GET /kv?limit=100&after=<key>`. Why is a key cursor better
  than `?page=3` for a store that changes?
- Benchmark `sync.RWMutex` vs `sync.Mutex` vs `sync.Map` with `testing.B` and
  a mostly-read workload.

## Check your understanding

```sh
go run ./cmd/course quiz 9
```

**Explain it back:** Why do handlers that share a map need a mutex, and why
shouldn't you hold the mutex while reading the request body?

## Reference

- [RFC 9110 §9.3.4 (PUT)](https://www.rfc-editor.org/rfc/rfc9110#section-9.3.4), [§15 (status codes)](https://www.rfc-editor.org/rfc/rfc9110#section-15)
- [`sync.RWMutex`](https://pkg.go.dev/sync#RWMutex), [Data race detector](https://go.dev/doc/articles/race_detector)

**Next:** [Stage 10: An API client SDK](../10-api-client/README.md)
