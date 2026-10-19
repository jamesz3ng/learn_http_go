package tests

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/jamesz3ng/learn_http_go/gobin"
)

func TestStage09_Lifecycle(t *testing.T) {
	srv := startGobin(t, gobin.Config{})
	c := newClient(t)
	u := srv.URL + "/kv/greeting"

	resp, _ := get(t, c, u)
	wantStatus(t, resp, 404, "GET of a missing key")

	resp, _ = do(t, c, http.MethodPut, u, strings.NewReader("hello"))
	wantStatus(t, resp, 201, "first PUT /kv/greeting", "201 Created when the key is new")
	if loc := resp.Header.Get("Location"); loc != "/kv/greeting" {
		t.Fatalf("201 response Location = %q, want %q", loc, "/kv/greeting")
	}

	resp, body := get(t, c, u)
	wantStatus(t, resp, 200, "GET after PUT")
	if body != "hello" {
		t.Fatalf("GET body = %q, want %q", body, "hello")
	}
	wantContentType(t, resp, "application/octet-stream", "GET /kv/greeting")

	resp, _ = do(t, c, http.MethodPut, u, strings.NewReader("hi again"))
	wantStatus(t, resp, 204, "second PUT /kv/greeting", "204 No Content when replacing an existing key")
	if _, body = get(t, c, u); body != "hi again" {
		t.Fatalf("GET after replace = %q, want %q", body, "hi again")
	}

	resp, _ = do(t, c, http.MethodHead, u, nil)
	wantStatus(t, resp, 200, "HEAD /kv/greeting")

	resp, _ = do(t, c, http.MethodDelete, u, nil)
	wantStatus(t, resp, 204, "DELETE /kv/greeting")
	resp, _ = get(t, c, u)
	wantStatus(t, resp, 404, "GET after DELETE")
	resp, body = do(t, c, http.MethodDelete, u, nil)
	wantStatus(t, resp, 404, "DELETE of a missing key")
	if decodeObject(t, body)["error"] == nil {
		t.Fatalf(`404s should be JSON {"error": ...}, got %s`, body)
	}
}

func TestStage09_EmptyValue(t *testing.T) {
	srv := startGobin(t, gobin.Config{})
	c := newClient(t)
	resp, _ := do(t, c, http.MethodPut, srv.URL+"/kv/empty", nil)
	wantStatus(t, resp, 201, "PUT with an empty body")
	resp, body := get(t, c, srv.URL+"/kv/empty")
	wantStatus(t, resp, 200, "GET of an empty value (it exists, it's just empty)")
	if body != "" {
		t.Fatalf("body = %q, want empty", body)
	}
}

func TestStage09_EscapedKeys(t *testing.T) {
	srv := startGobin(t, gobin.Config{})
	c := newClient(t)
	// "a/b c" escaped as one path segment.
	resp, _ := do(t, c, http.MethodPut, srv.URL+"/kv/a%2Fb%20c", strings.NewReader("v"))
	wantStatus(t, resp, 201, "PUT /kv/a%2Fb%20c")
	if loc := resp.Header.Get("Location"); loc != "/kv/a%2Fb%20c" {
		t.Fatalf("Location = %q, want %q%s", loc, "/kv/a%2Fb%20c", hint("escape the key with url.PathEscape"))
	}
	_, body := get(t, c, srv.URL+"/kv?prefix=a")
	keys := stringSlice(t, decodeObject(t, body)["keys"], `"keys"`)
	if !reflect.DeepEqual(keys, []string{"a/b c"}) {
		t.Fatalf("keys = %q, want [\"a/b c\"]%s", keys, hint("r.PathValue returns the decoded value"))
	}
}

func TestStage09_KeyLimits(t *testing.T) {
	srv := startGobin(t, gobin.Config{})
	c := newClient(t)
	long := strings.Repeat("k", 257)
	resp, _ := do(t, c, http.MethodPut, srv.URL+"/kv/"+long, strings.NewReader("v"))
	wantStatus(t, resp, 400, "PUT with a 257-byte key")
	resp, _ = do(t, c, http.MethodPut, srv.URL+"/kv/"+long[:256], strings.NewReader("v"))
	wantStatus(t, resp, 201, "PUT with a 256-byte key")

	resp, _ = do(t, c, http.MethodPut, srv.URL+"/kv/big", strings.NewReader(strings.Repeat("v", 1<<20+1)))
	wantStatus(t, resp, 413, "PUT with a value over 1 MiB")
}

func TestStage09_List(t *testing.T) {
	srv := startGobin(t, gobin.Config{})
	c := newClient(t)

	resp, body := get(t, c, srv.URL+"/kv")
	wantStatus(t, resp, 200, "GET /kv")
	wantContentType(t, resp, "application/json", "GET /kv")
	if _, ok := decodeObject(t, body)["keys"].([]any); !ok {
		t.Fatalf(`an empty store must list as {"keys": []}, got %s%s`, strings.TrimSpace(body),
			hint("a nil slice encodes as null; start from []string{}"))
	}

	for _, k := range []string{"user:2", "user:1", "post:1", "user:10"} {
		resp, _ := do(t, c, http.MethodPut, srv.URL+"/kv/"+k, strings.NewReader("x"))
		wantStatus(t, resp, 201, "PUT /kv/"+k)
	}
	_, body = get(t, c, srv.URL+"/kv")
	if got := stringSlice(t, decodeObject(t, body)["keys"], `"keys"`); !reflect.DeepEqual(got, []string{"post:1", "user:1", "user:10", "user:2"}) {
		t.Fatalf("GET /kv keys = %q, want all keys sorted", got)
	}
	_, body = get(t, c, srv.URL+"/kv?prefix=user:1")
	if got := stringSlice(t, decodeObject(t, body)["keys"], `"keys"`); !reflect.DeepEqual(got, []string{"user:1", "user:10"}) {
		t.Fatalf("GET /kv?prefix=user:1 keys = %q, want [user:1 user:10]", got)
	}
}

func TestStage09_HandlersHaveSeparateStores(t *testing.T) {
	a := startGobin(t, gobin.Config{})
	b := startGobin(t, gobin.Config{})
	c := newClient(t)
	do(t, c, http.MethodPut, a.URL+"/kv/only-in-a", strings.NewReader("x"))
	resp, _ := get(t, c, b.URL+"/kv/only-in-a")
	wantStatus(t, resp, 404, "GET from a second, independent server",
		"create the store inside NewHandler, not as a package-level variable")
}

func TestStage09_ConcurrentWrites(t *testing.T) {
	// Call the handler directly from many goroutines. Without a mutex this
	// races (run `go test -race`) and can crash with "concurrent map writes".
	h := gobin.NewHandler(gobin.Config{})
	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			key := fmt.Sprintf("k%03d", i)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/kv/"+key, strings.NewReader("v")))
			rec = httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/kv/"+key, nil))
			rec = httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/kv", nil))
		}()
	}
	wg.Wait()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/kv", nil))
	keys := stringSlice(t, decodeObject(t, rec.Body.String())["keys"], `"keys"`)
	if len(keys) != 100 {
		t.Fatalf("after 100 concurrent PUTs, GET /kv lists %d keys%s", len(keys),
			hint("handlers run concurrently; protect the map with a sync.Mutex or sync.RWMutex"))
	}
}
