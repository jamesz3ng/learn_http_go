package gobin

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
)

// Stage 9 ---------------------------------------------------------------------

const (
	maxKeyBytes   = 256
	maxValueBytes = 1 << 20 // 1 MiB
)

// event describes a change to the store (stage 19).
type event struct {
	ID    uint64
	Type  string // "put" or "delete"
	Key   string
	Value []byte
}

// subscriber is one /watch stream (stage 19).
type subscriber struct {
	prefix string
	ch     chan event
}

// store is an in-memory key-value map that is safe for concurrent use.
//
// Every request runs in its own goroutine, so handlers touching shared state
// MUST synchronise. Without the mutex, two concurrent PUTs are a data race and
// the Go runtime may crash the whole process with
// "fatal error: concurrent map writes".
type store struct {
	mu   sync.RWMutex
	data map[string][]byte

	// Stage 19: watchers. Guarded by mu as well, so that events are published
	// in exactly the order the changes happened.
	seq  uint64
	subs map[*subscriber]struct{}
}

func newStore() *store {
	return &store{data: map[string][]byte{}, subs: map[*subscriber]struct{}{}}
}

// put stores value and reports whether the key is new.
func (s *store) put(key string, value []byte) (created bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, existed := s.data[key]
	s.data[key] = value
	s.publishLocked(event{Type: "put", Key: key, Value: value})
	return !existed
}

func (s *store) get(key string) ([]byte, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.data[key]
	return v, ok
}

func (s *store) delete(key string) (existed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data[key]; !ok {
		return false
	}
	delete(s.data, key)
	s.publishLocked(event{Type: "delete", Key: key})
	return true
}

func (s *store) keys(prefix string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	keys := []string{} // not nil: a nil slice encodes as JSON null, not []
	for k := range s.data {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	return keys
}

func validKey(key string) bool {
	return key != "" && len(key) <= maxKeyBytes
}

func (a *app) kvPut(w http.ResponseWriter, r *http.Request) {
	// PathValue is percent-decoded: /kv/a%2Fb gives "a/b".
	key := r.PathValue("key")
	if !validKey(key) {
		writeError(w, http.StatusBadRequest, "key must be 1-256 bytes")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxValueBytes)
	value, err := io.ReadAll(r.Body)
	if err != nil {
		writeBodyError(w, err)
		return
	}
	if a.kv.put(key, value) {
		w.Header().Set("Location", "/kv/"+url.PathEscape(key))
		w.WriteHeader(http.StatusCreated)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *app) kvGet(w http.ResponseWriter, r *http.Request) {
	value, ok := a.kv.get(r.PathValue("key"))
	if !ok {
		writeError(w, http.StatusNotFound, "key not found")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")

	// Stage 21: with an ETag set, ServeContent answers If-None-Match with
	// 304 Not Modified, and handles Range requests and HEAD for us.
	// (Before stage 21 this was simply w.Write(value).)
	w.Header().Set("ETag", etag(value))
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(value))
}

func (a *app) kvDelete(w http.ResponseWriter, r *http.Request) {
	if !a.kv.delete(r.PathValue("key")) {
		writeError(w, http.StatusNotFound, "key not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *app) kvList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"keys": a.kv.keys(r.URL.Query().Get("prefix"))})
}

// etag is a strong validator for value: it changes whenever the bytes change.
func etag(value []byte) string {
	sum := sha256.Sum256(value)
	return `"` + hex.EncodeToString(sum[:16]) + `"`
}
