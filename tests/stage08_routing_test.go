package tests

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/jamesz3ng/learn_http_go/gobin"
)

func TestStage08_Status(t *testing.T) {
	srv := startGobin(t, gobin.Config{})
	c := newNoRedirectClient(t)
	for _, code := range []int{200, 201, 204, 302, 400, 404, 418, 500, 503, 599} {
		resp, _ := get(t, c, srv.URL+"/status/"+itoa(code))
		wantStatus(t, resp, code, "GET /status/"+itoa(code), `"GET /status/{code}" and r.PathValue("code")`)
	}
}

func TestStage08_StatusInvalid(t *testing.T) {
	srv := startGobin(t, gobin.Config{})
	c := newClient(t)
	for _, v := range []string{"abc", "99", "600", "-1", "20x"} {
		resp, body := get(t, c, srv.URL+"/status/"+v)
		wantStatus(t, resp, 400, "GET /status/"+v)
		m := decodeObject(t, body)
		if m["error"] == nil {
			t.Fatalf(`400 responses should be JSON {"error": ...}, got %s`, body)
		}
	}
}

func TestStage08_Teapot(t *testing.T) {
	srv := startGobin(t, gobin.Config{})
	resp, body := get(t, newClient(t), srv.URL+"/status/teapot")
	wantStatus(t, resp, 418, "GET /status/teapot",
		`register "GET /status/teapot" too: a literal segment is more specific than a {wildcard}, so it wins`)
	if !strings.Contains(strings.ToLower(body), "teapot") {
		t.Fatalf("body %q should mention a teapot", body)
	}
}

func TestStage08_Bytes(t *testing.T) {
	srv := startGobin(t, gobin.Config{})
	c := newClient(t)
	for _, n := range []int{0, 1, 1024, 102400} {
		resp, body := get(t, c, srv.URL+"/bytes/"+itoa(n))
		wantStatus(t, resp, 200, "GET /bytes/"+itoa(n))
		wantContentType(t, resp, "application/octet-stream", "GET /bytes/n")
		if len(body) != n {
			t.Fatalf("GET /bytes/%d returned %d bytes", n, len(body))
		}
	}
	for _, v := range []string{"102401", "-5", "lots"} {
		resp, _ := get(t, c, srv.URL+"/bytes/"+v)
		wantStatus(t, resp, 400, "GET /bytes/"+v)
	}
}

func TestStage08_Base64(t *testing.T) {
	srv := startGobin(t, gobin.Config{})
	c := newClient(t)
	cases := map[string]string{
		"aGVsbG8gd29ybGQ=": "hello world", // padded
		"aGVsbG8gd29ybGQ":  "hello world", // unpadded
		"Pz8_Pj4-":         "???>>>",      // URL-safe alphabet uses - and _
	}
	for in, want := range cases {
		resp, body := get(t, c, srv.URL+"/base64/"+in)
		wantStatus(t, resp, 200, "GET /base64/"+in)
		if body != want {
			t.Fatalf("GET /base64/%s = %q, want %q", in, body, want)
		}
	}
	resp, _ := get(t, c, srv.URL+"/base64/not*base64!")
	wantStatus(t, resp, 400, "GET /base64/not*base64!")
}

func TestStage08_UUID(t *testing.T) {
	srv := startGobin(t, gobin.Config{})
	c := newClient(t)
	re := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	seen := map[string]bool{}
	for range 3 {
		resp, body := get(t, c, srv.URL+"/uuid")
		wantStatus(t, resp, 200, "GET /uuid")
		u, _ := decodeObject(t, body)["uuid"].(string)
		if !re.MatchString(u) {
			t.Fatalf("uuid %q is not a lower-case version 4 UUID", u)
		}
		if seen[u] {
			t.Fatalf("got the same uuid twice: %s", u)
		}
		seen[u] = true
	}
}

func itoa(n int) string { return strconv.Itoa(n) }
