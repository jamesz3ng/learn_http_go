package tests

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jamesz3ng/learn_http_go/gobin"
)

func TestStage02_RootSaysHello(t *testing.T) {
	srv := startGobin(t, gobin.Config{})
	resp, body := get(t, newClient(t), srv.URL+"/")
	wantStatus(t, resp, 200, "GET /", "register a handler with mux.HandleFunc(\"GET /{$}\", ...)")
	if body != "Hello, HTTP!\n" {
		t.Fatalf("GET / body = %q, want %q", body, "Hello, HTTP!\n")
	}
	wantContentType(t, resp, "text/plain", "GET /")
}

func TestStage02_Healthz(t *testing.T) {
	srv := startGobin(t, gobin.Config{})
	resp, body := get(t, newClient(t), srv.URL+"/healthz")
	wantStatus(t, resp, 200, "GET /healthz")
	if body != "ok\n" {
		t.Fatalf("GET /healthz body = %q, want %q", body, "ok\n")
	}
}

func TestStage02_UnknownPathIs404(t *testing.T) {
	srv := startGobin(t, gobin.Config{})
	c := newClient(t)
	// Make sure the server is actually serving something first.
	resp, _ := get(t, c, srv.URL+"/healthz")
	wantStatus(t, resp, 200, "GET /healthz")

	for _, path := range []string{"/nope", "/hello", "/healthz/extra"} {
		resp, _ := get(t, c, srv.URL+path)
		wantStatus(t, resp, 404, "GET "+path,
			`the pattern "/" matches EVERY path (it is a prefix). Use "GET /{$}" to match only the root`)
	}
}

func TestStage02_MethodNotAllowed(t *testing.T) {
	srv := startGobin(t, gobin.Config{})
	resp, _ := do(t, newClient(t), http.MethodPost, srv.URL+"/", strings.NewReader("x"))
	wantStatus(t, resp, 405, "POST /",
		`register the route with a method, "GET /{$}", and ServeMux answers other methods with 405 for you`)
	if allow := resp.Header.Get("Allow"); !strings.Contains(allow, "GET") {
		t.Fatalf("405 response should list allowed methods in the Allow header, got %q", allow)
	}
}

func TestStage02_HeadWorks(t *testing.T) {
	srv := startGobin(t, gobin.Config{})
	resp, body := do(t, newClient(t), http.MethodHead, srv.URL+"/", nil)
	wantStatus(t, resp, 200, "HEAD /", `a "GET" pattern also matches HEAD requests`)
	if body != "" {
		t.Fatalf("HEAD response must not have a body, got %q", body)
	}
}

// This test uses httptest.NewRecorder, which calls the handler directly
// without any network. It's the fastest way to unit-test a handler.
func TestStage02_WorksWithResponseRecorder(t *testing.T) {
	h := gobin.NewHandler(gobin.Config{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Body.String() != "ok\n" {
		t.Fatalf("recorder got %d %q, want 200 %q", rec.Code, rec.Body.String(), "ok\n")
	}
}

func TestStage02_HandlersAreIndependent(t *testing.T) {
	// Two calls to NewHandler must not panic (registering the same pattern
	// twice on one ServeMux panics) — so don't use http.DefaultServeMux or
	// http.HandleFunc.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("calling NewHandler twice panicked: %v%s", r,
				hint("create a fresh mux with http.NewServeMux() inside NewHandler instead of using the global http.HandleFunc"))
		}
	}()
	gobin.NewHandler(gobin.Config{})
	gobin.NewHandler(gobin.Config{})
}
