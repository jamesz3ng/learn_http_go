package tests

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/jamesz3ng/learn_http_go/gobin"
)

func TestStage11_RedirectChain(t *testing.T) {
	srv := startGobin(t, gobin.Config{})
	c := newNoRedirectClient(t)
	cases := map[string]string{
		"/redirect/3": "/redirect/2",
		"/redirect/2": "/redirect/1",
		"/redirect/1": "/get",
	}
	for path, want := range cases {
		resp, _ := get(t, c, srv.URL+path)
		wantStatus(t, resp, 302, "GET "+path, "http.Redirect(w, r, target, http.StatusFound)")
		if loc := resp.Header.Get("Location"); loc != want {
			t.Fatalf("GET %s: Location = %q, want %q", path, loc, want)
		}
	}
}

func TestStage11_FollowedByAClient(t *testing.T) {
	srv := startGobin(t, gobin.Config{})
	var hops []string
	c := newClient(t)
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		hops = append(hops, req.URL.Path)
		return nil
	}
	resp, body := get(t, c, srv.URL+"/redirect/4")
	wantStatus(t, resp, 200, "following /redirect/4 to the end")
	if strings.Join(hops, " ") != "/redirect/3 /redirect/2 /redirect/1 /get" {
		t.Fatalf("redirect hops = %v", hops)
	}
	if decodeObject(t, body)["url"] != srv.URL+"/get" {
		t.Fatalf("final response should be /get's JSON, got %s", body)
	}
}

func TestStage11_RedirectInvalidN(t *testing.T) {
	srv := startGobin(t, gobin.Config{})
	c := newNoRedirectClient(t)
	for _, n := range []string{"0", "51", "x", "-1"} {
		resp, _ := get(t, c, srv.URL+"/redirect/"+n)
		wantStatus(t, resp, 400, "GET /redirect/"+n, "n must be between 1 and 50")
	}
}

func TestStage11_AbsoluteRedirect(t *testing.T) {
	srv := startGobin(t, gobin.Config{})
	c := newNoRedirectClient(t)
	resp, _ := get(t, c, srv.URL+"/absolute-redirect/2")
	wantStatus(t, resp, 302, "GET /absolute-redirect/2")
	if loc := resp.Header.Get("Location"); loc != srv.URL+"/absolute-redirect/1" {
		t.Fatalf("Location = %q, want %q%s", loc, srv.URL+"/absolute-redirect/1", hint("scheme + \"://\" + r.Host + path"))
	}
	resp, _ = get(t, c, srv.URL+"/absolute-redirect/1")
	if loc := resp.Header.Get("Location"); loc != srv.URL+"/get" {
		t.Fatalf("Location = %q, want %q", loc, srv.URL+"/get")
	}
}

func TestStage11_RedirectTo(t *testing.T) {
	srv := startGobin(t, gobin.Config{})
	c := newNoRedirectClient(t)

	q := url.Values{"url": {"https://example.com/somewhere?x=1"}}
	resp, _ := get(t, c, srv.URL+"/redirect-to?"+q.Encode())
	wantStatus(t, resp, 302, "GET /redirect-to (default status)")
	if loc := resp.Header.Get("Location"); loc != "https://example.com/somewhere?x=1" {
		t.Fatalf("Location = %q", loc)
	}

	for _, code := range []int{301, 302, 303, 307, 308} {
		q := url.Values{"url": {"/get"}, "status_code": {itoa(code)}}
		resp, _ := do(t, c, http.MethodPost, srv.URL+"/redirect-to?"+q.Encode(), strings.NewReader("x"))
		wantStatus(t, resp, code, "POST /redirect-to?status_code="+itoa(code),
			`register "/redirect-to" with no method so POST works too`)
		if loc := resp.Header.Get("Location"); loc != "/get" {
			t.Fatalf("Location = %q, want /get", loc)
		}
	}

	for _, bad := range []string{"?status_code=302", "?url=/get&status_code=200", "?url=/get&status_code=304", "?url=/get&status_code=abc"} {
		resp, _ := get(t, c, srv.URL+"/redirect-to"+bad)
		wantStatus(t, resp, 400, "GET /redirect-to"+bad)
	}
}
