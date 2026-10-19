package tests

import (
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/jamesz3ng/learn_http_go/gobin"
)

func TestStage04_GetReportsArgs(t *testing.T) {
	srv := startGobin(t, gobin.Config{})
	resp, body := get(t, newClient(t), srv.URL+"/get?a=1&a=2&b=x%20y&empty=")
	wantStatus(t, resp, 200, "GET /get")
	wantContentType(t, resp, "application/json", "GET /get")
	m := decodeObject(t, body)

	args := object(t, m["args"], `"args"`)
	want := map[string][]string{"a": {"1", "2"}, "b": {"x y"}, "empty": {""}}
	for k, v := range want {
		got := stringSlice(t, args[k], `args["`+k+`"]`)
		if !reflect.DeepEqual(got, v) {
			t.Fatalf("args[%q] = %q, want %q%s", k, got, v,
				hint("r.URL.Query() decodes the query string into a url.Values (a map[string][]string)"))
		}
	}
	if m["method"] != "GET" {
		t.Fatalf(`"method" = %v, want "GET"`, m["method"])
	}
}

func TestStage04_GetReportsEmptyArgsAsObject(t *testing.T) {
	srv := startGobin(t, gobin.Config{})
	_, body := get(t, newClient(t), srv.URL+"/get")
	m := decodeObject(t, body)
	if _, ok := m["args"].(map[string]any); !ok {
		t.Fatalf(`with no query string, "args" must be an empty object {}, got %v`, m["args"])
	}
}

func TestStage04_GetReportsHeaders(t *testing.T) {
	srv := startGobin(t, gobin.Config{})
	_, body := get(t, newClient(t), srv.URL+"/get",
		"x-custom-thing", "one", // lower-case on purpose
		"X-Multi", "a",
		"X-Multi", "b",
	)
	m := decodeObject(t, body)
	headers := object(t, m["headers"], `"headers"`)

	got := stringSlice(t, headers["X-Custom-Thing"], `headers["X-Custom-Thing"]`)
	if !reflect.DeepEqual(got, []string{"one"}) {
		t.Fatalf(`headers["X-Custom-Thing"] = %q, want ["one"]%s`, got,
			hint("Go canonicalises header names: x-custom-thing becomes X-Custom-Thing"))
	}
	got = stringSlice(t, headers["X-Multi"], `headers["X-Multi"]`)
	if !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf(`headers["X-Multi"] = %q, want ["a" "b"] (a header can repeat)`, got)
	}
	u, _ := url.Parse(srv.URL)
	host, ok := headers["Host"]
	if !ok {
		t.Fatalf(`"headers" has no "Host"%s`, hint("the server moves Host out of r.Header into r.Host; add it back"))
	}
	if got := stringSlice(t, host, `headers["Host"]`); !reflect.DeepEqual(got, []string{u.Host}) {
		t.Fatalf(`headers["Host"] = %q, want [%q]`, got, u.Host)
	}
}

func TestStage04_GetReportsOriginAndURL(t *testing.T) {
	srv := startGobin(t, gobin.Config{})
	_, body := get(t, newClient(t), srv.URL+"/get?q=go")
	m := decodeObject(t, body)
	if m["origin"] != "127.0.0.1" {
		t.Fatalf(`"origin" = %v, want "127.0.0.1"%s`, m["origin"],
			hint("r.RemoteAddr is \"ip:port\"; net.SplitHostPort separates them"))
	}
	want := srv.URL + "/get?q=go"
	if m["url"] != want {
		t.Fatalf(`"url" = %v, want %q%s`, m["url"], want,
			hint("on the server, r.URL usually has no scheme or host: build them from r.TLS and r.Host, plus r.URL.RequestURI()"))
	}
}

func TestStage04_Headers(t *testing.T) {
	srv := startGobin(t, gobin.Config{})
	resp, body := get(t, newClient(t), srv.URL+"/headers", "X-Probe", "42")
	wantStatus(t, resp, 200, "GET /headers")
	wantContentType(t, resp, "application/json", "GET /headers")
	m := decodeObject(t, body)
	headers := object(t, m["headers"], `"headers"`)
	if got := stringSlice(t, headers["X-Probe"], `headers["X-Probe"]`); !reflect.DeepEqual(got, []string{"42"}) {
		t.Fatalf(`headers["X-Probe"] = %q, want ["42"]`, got)
	}
}

func TestStage04_IP(t *testing.T) {
	srv := startGobin(t, gobin.Config{})
	resp, body := get(t, newClient(t), srv.URL+"/ip")
	wantStatus(t, resp, 200, "GET /ip")
	m := decodeObject(t, body)
	if m["origin"] != "127.0.0.1" {
		t.Fatalf(`/ip "origin" = %v, want "127.0.0.1"`, m["origin"])
	}
}

func TestStage04_UserAgent(t *testing.T) {
	srv := startGobin(t, gobin.Config{})
	resp, body := get(t, newClient(t), srv.URL+"/user-agent", "User-Agent", "stage4-test/1.0")
	wantStatus(t, resp, 200, "GET /user-agent")
	m := decodeObject(t, body)
	if m["user-agent"] != "stage4-test/1.0" {
		t.Fatalf(`/user-agent "user-agent" = %v, want "stage4-test/1.0"`, m["user-agent"])
	}
	if !strings.Contains(resp.Header.Get("Content-Type"), "json") {
		t.Fatalf("/user-agent should be JSON")
	}
}
