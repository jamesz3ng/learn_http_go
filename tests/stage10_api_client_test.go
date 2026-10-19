package tests

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jamesz3ng/learn_http_go/kvclient"
)

// fakeKV is a minimal, independent implementation of gobin's /kv API, so the
// client tests don't depend on your server being finished.
type fakeKV struct {
	*httptest.Server
	conns *atomic.Int64

	mu     sync.Mutex
	data   map[string][]byte
	paths  []string // escaped request paths, in order
	agents []string
}

func newFakeKV(t *testing.T, prefix string) *fakeKV {
	t.Helper()
	f := &fakeKV{data: map[string][]byte{}}
	mux := http.NewServeMux()
	jsonErr := func(w http.ResponseWriter, code int, msg string) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		json.NewEncoder(w).Encode(map[string]string{"error": msg})
	}
	mux.HandleFunc("GET /kv", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		keys := []string{}
		for k := range f.data {
			if strings.HasPrefix(k, r.URL.Query().Get("prefix")) {
				keys = append(keys, k)
			}
		}
		slices.Sort(keys)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"keys": keys})
	})
	mux.HandleFunc("PUT /kv/{key}", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		_, existed := f.data[r.PathValue("key")]
		f.data[r.PathValue("key")] = b
		f.mu.Unlock()
		if existed {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusCreated)
	})
	mux.HandleFunc("GET /kv/{key}", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		v, ok := f.data[r.PathValue("key")]
		f.mu.Unlock()
		if !ok {
			jsonErr(w, http.StatusNotFound, "key not found")
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(v)
	})
	mux.HandleFunc("DELETE /kv/{key}", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		_, ok := f.data[r.PathValue("key")]
		delete(f.data, r.PathValue("key"))
		f.mu.Unlock()
		if !ok {
			jsonErr(w, http.StatusNotFound, "key not found")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	var h http.Handler = mux
	if prefix != "" {
		h = http.StripPrefix(prefix, mux)
	}
	f.Server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.paths = append(f.paths, r.URL.EscapedPath())
		f.agents = append(f.agents, r.UserAgent())
		f.mu.Unlock()
		h.ServeHTTP(w, r)
	}))
	f.conns = countConns(f.Server)
	f.Start()
	t.Cleanup(f.Close)
	return f
}

func (f *fakeKV) lastPath() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.paths) == 0 {
		return ""
	}
	return f.paths[len(f.paths)-1]
}

func newKV(t *testing.T, baseURL string) *kvclient.Client {
	t.Helper()
	c, err := kvclient.New(baseURL, newClient(t))
	if err != nil {
		t.Fatalf("kvclient.New(%q): %v", baseURL, err)
	}
	return c
}

func TestStage10_New(t *testing.T) {
	for _, bad := range []string{"", "::bad", "localhost:8080", "ftp://example.com", "/just/a/path"} {
		if _, err := kvclient.New(bad, nil); err == nil {
			t.Fatalf("kvclient.New(%q) returned no error; it should reject anything that isn't an absolute http(s) URL", bad)
		}
	}
	if c, err := kvclient.New("http://localhost:8080", nil); err != nil || c == nil {
		t.Fatalf("kvclient.New with a nil *http.Client should work, got %v", err)
	}
}

func TestStage10_RoundTrip(t *testing.T) {
	f := newFakeKV(t, "")
	kv := newKV(t, f.URL)
	ctx := context.Background()

	created, err := kv.Put(ctx, "k", []byte("v1"))
	if err != nil || !created {
		t.Fatalf("first Put = (%v, %v), want (true, nil)", created, err)
	}
	created, err = kv.Put(ctx, "k", []byte("v2\x00\xff"))
	if err != nil || created {
		t.Fatalf("second Put = (%v, %v), want (false, nil)", created, err)
	}
	got, err := kv.Get(ctx, "k")
	if err != nil || string(got) != "v2\x00\xff" {
		t.Fatalf("Get = (%q, %v), want the binary value back", got, err)
	}
	kv.Put(ctx, "kz", []byte("x"))
	kv.Put(ctx, "other", []byte("x"))
	keys, err := kv.List(ctx, "k")
	if err != nil || !reflect.DeepEqual(keys, []string{"k", "kz"}) {
		t.Fatalf("List(\"k\") = (%q, %v), want [k kz]", keys, err)
	}
	if err := kv.Delete(ctx, "k"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	f.mu.Lock()
	ua := f.agents[0]
	f.mu.Unlock()
	if ua != "kvclient/1.0" {
		t.Fatalf("User-Agent = %q, want kvclient/1.0", ua)
	}
}

func TestStage10_NotFound(t *testing.T) {
	f := newFakeKV(t, "")
	kv := newKV(t, f.URL)
	_, err := kv.Get(context.Background(), "missing")
	if err == nil {
		t.Fatal("Get of a missing key returned no error")
	}
	if !errors.Is(err, kvclient.ErrNotFound) {
		t.Fatalf("errors.Is(err, kvclient.ErrNotFound) is false for %v%s", err,
			hint("give *APIError an Is(target error) bool method"))
	}
	var apiErr *kvclient.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("errors.As(err, *kvclient.APIError) is false for %v", err)
	}
	if apiErr.StatusCode != 404 || apiErr.Message != "key not found" {
		t.Fatalf("APIError = %+v, want StatusCode 404 and Message from the JSON body", apiErr)
	}
	if err := kv.Delete(context.Background(), "missing"); !errors.Is(err, kvclient.ErrNotFound) {
		t.Fatalf("Delete of a missing key = %v, want an error matching ErrNotFound", err)
	}
}

func TestStage10_ServerErrorWithTextBody(t *testing.T) {
	srv := newRecordingServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "database on fire", http.StatusInternalServerError)
	})
	kv := newKV(t, srv.URL)
	_, err := kv.Get(context.Background(), "k")
	var apiErr *kvclient.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("want *APIError, got %v", err)
	}
	if apiErr.StatusCode != 500 || apiErr.Message != "database on fire" {
		t.Fatalf("APIError = %+v, want 500 with the (trimmed) body text as Message", apiErr)
	}
	if errors.Is(err, kvclient.ErrNotFound) {
		t.Fatal("a 500 must not match ErrNotFound")
	}
}

func TestStage10_EscapesKeys(t *testing.T) {
	f := newFakeKV(t, "")
	kv := newKV(t, f.URL)
	key := "a/b c?d#e%f"
	if _, err := kv.Put(context.Background(), key, []byte("v")); err != nil {
		t.Fatalf("Put(%q): %v", key, err)
	}
	if got, want := f.lastPath(), "/kv/a%2Fb%20c%3Fd%23e%25f"; got != want {
		t.Fatalf("request path = %q, want %q%s", got, want, hint("url.PathEscape(key) makes the key a single path segment"))
	}
	got, err := kv.Get(context.Background(), key)
	if err != nil || string(got) != "v" {
		t.Fatalf("Get(%q) = (%q, %v)", key, got, err)
	}
}

func TestStage10_ListEscapesPrefix(t *testing.T) {
	f := newFakeKV(t, "")
	kv := newKV(t, f.URL)
	kv.Put(context.Background(), "a&b=c", []byte("1"))
	kv.Put(context.Background(), "a", []byte("1"))
	keys, err := kv.List(context.Background(), "a&b")
	if err != nil || !reflect.DeepEqual(keys, []string{"a&b=c"}) {
		t.Fatalf("List(\"a&b\") = (%q, %v), want [a&b=c]%s", keys, err, hint("build the query with url.Values{...}.Encode()"))
	}
}

func TestStage10_BaseURLWithPath(t *testing.T) {
	f := newFakeKV(t, "/api")
	for _, base := range []string{f.URL + "/api", f.URL + "/api/"} {
		kv := newKV(t, base)
		if _, err := kv.Put(context.Background(), "x", []byte("1")); err != nil {
			t.Fatalf("with base URL %q, Put failed: %v", base, err)
		}
		if got := f.lastPath(); got != "/api/kv/x" {
			t.Fatalf("with base URL %q, request path = %q, want /api/kv/x%s", base, got, hint("url.URL.JoinPath handles slashes for you"))
		}
	}
}

func TestStage10_Context(t *testing.T) {
	srv := newRecordingServer(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	})
	kv := newKV(t, srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := kv.Get(ctx, "slow")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Get with an expired context = %v, want an error wrapping context.DeadlineExceeded%s", err,
			hint("http.NewRequestWithContext(ctx, ...)"))
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("Get took %v to notice the context deadline", time.Since(start))
	}
}

func TestStage10_ReusesConnections(t *testing.T) {
	f := newFakeKV(t, "")
	kv := newKV(t, f.URL)
	ctx := context.Background()
	for i := range 10 {
		kv.Put(ctx, "k", []byte("v"))
		kv.Get(ctx, "k")
		kv.Get(ctx, "missing")
		kv.List(ctx, "")
		if i%2 == 0 {
			kv.Delete(ctx, "k")
			kv.Delete(ctx, "k")
		}
	}
	if n := f.conns.Load(); n != 1 {
		t.Fatalf("50+ sequential requests used %d TCP connections, want 1%s", n,
			hint("read every response body to the end and close it, on error paths too, so the connection can be reused"))
	}
}

func TestStage10_RejectsUnaddressableKeys(t *testing.T) {
	f := newFakeKV(t, "")
	kv := newKV(t, f.URL)
	ctx := context.Background()
	for _, key := range []string{"", ".", "..", strings.Repeat("k", 257)} {
		if _, err := kv.Get(ctx, key); err == nil {
			t.Fatalf("Get(%q) returned no error%s", truncate(key, 20),
				hint(`"" would address /kv itself, and "." and ".." are path segments that get cleaned away; reject them (and keys over 256 bytes) before sending anything`))
		}
		if _, err := kv.Put(ctx, key, []byte("v")); err == nil {
			t.Fatalf("Put(%q) returned no error", truncate(key, 20))
		}
		if err := kv.Delete(ctx, key); err == nil {
			t.Fatalf("Delete(%q) returned no error", truncate(key, 20))
		}
	}
	f.mu.Lock()
	n := len(f.paths)
	f.mu.Unlock()
	if n != 0 {
		t.Fatalf("invalid keys should be rejected without sending a request, but the server saw %d", n)
	}
}
