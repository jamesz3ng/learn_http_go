package tests

import (
	"net/http"
	"net/http/cookiejar"
	"testing"

	"github.com/jamesz3ng/learn_http_go/gobin"
)

func cookiesOf(t *testing.T, body string) map[string]any {
	t.Helper()
	return object(t, decodeObject(t, body)["cookies"], `"cookies"`)
}

func TestStage13_ShowCookies(t *testing.T) {
	srv := startGobin(t, gobin.Config{})
	resp, body := get(t, newClient(t), srv.URL+"/cookies", "Cookie", "a=1; b=two")
	wantStatus(t, resp, 200, "GET /cookies")
	got := cookiesOf(t, body)
	if got["a"] != "1" || got["b"] != "two" || len(got) != 2 {
		t.Fatalf(`/cookies = %v, want {"a":"1","b":"two"}%s`, got, hint("r.Cookies() parses the Cookie header"))
	}
	_, body = get(t, newClient(t), srv.URL+"/cookies")
	if len(cookiesOf(t, body)) != 0 {
		t.Fatalf(`with no cookies, /cookies should be {"cookies": {}}, got %s`, body)
	}
}

func TestStage13_SetCookies(t *testing.T) {
	srv := startGobin(t, gobin.Config{})
	resp, _ := get(t, newNoRedirectClient(t), srv.URL+"/cookies/set?flavour=choc&size=large")
	wantStatus(t, resp, 302, "GET /cookies/set")
	if loc := resp.Header.Get("Location"); loc != "/cookies" {
		t.Fatalf("Location = %q, want /cookies", loc)
	}
	got := map[string]*http.Cookie{}
	for _, c := range resp.Cookies() {
		got[c.Name] = c
	}
	if got["flavour"] == nil || got["flavour"].Value != "choc" || got["size"] == nil || got["size"].Value != "large" {
		t.Fatalf("Set-Cookie headers = %q, want flavour=choc and size=large%s", resp.Header.Values("Set-Cookie"),
			hint("http.SetCookie(w, &http.Cookie{...}) adds one Set-Cookie header per call"))
	}
	if got["flavour"].Path != "/" {
		t.Fatalf("cookies should have Path=/ so they apply to every path, got Path=%q", got["flavour"].Path)
	}
}

func TestStage13_DeleteCookies(t *testing.T) {
	srv := startGobin(t, gobin.Config{})
	resp, _ := get(t, newNoRedirectClient(t), srv.URL+"/cookies/delete?flavour")
	wantStatus(t, resp, 302, "GET /cookies/delete")
	cs := resp.Cookies()
	if len(cs) != 1 || cs[0].Name != "flavour" || cs[0].MaxAge >= 0 {
		t.Fatalf("Set-Cookie headers = %q, want flavour expired with Max-Age=0%s", resp.Header.Values("Set-Cookie"),
			hint("in Go, http.Cookie{MaxAge: -1} is what writes Max-Age=0 (MaxAge: 0 means 'not set')"))
	}
}

func TestStage13_CookieJarRoundTrip(t *testing.T) {
	srv := startGobin(t, gobin.Config{})
	jar, _ := cookiejar.New(nil)
	c := newClient(t)
	c.Jar = jar

	_, body := get(t, c, srv.URL+"/cookies/set?a=1&b=2")
	got := cookiesOf(t, body)
	if got["a"] != "1" || got["b"] != "2" {
		t.Fatalf("after following /cookies/set?a=1&b=2 with a cookie jar, /cookies shows %v", got)
	}
	_, body = get(t, c, srv.URL+"/cookies/delete?a")
	got = cookiesOf(t, body)
	if _, still := got["a"]; still || got["b"] != "2" {
		t.Fatalf("after /cookies/delete?a, /cookies shows %v, want only b", got)
	}
}

func TestStage13_BasicAuth(t *testing.T) {
	srv := startGobin(t, gobin.Config{})
	c := newClient(t)

	req, _ := http.NewRequest("GET", srv.URL+"/basic-auth/alice/s3cret", nil)
	req.SetBasicAuth("alice", "s3cret")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	wantStatus(t, resp, 200, "GET /basic-auth/alice/s3cret with the right credentials", "r.BasicAuth()")

	for _, creds := range [][2]string{{"alice", "wrong"}, {"bob", "s3cret"}, {"", ""}} {
		req, _ := http.NewRequest("GET", srv.URL+"/basic-auth/alice/s3cret", nil)
		if creds[0] != "" {
			req.SetBasicAuth(creds[0], creds[1])
		}
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		wantStatus(t, resp, 401, "GET /basic-auth/alice/s3cret with credentials "+creds[0]+":"+creds[1])
		if wa := resp.Header.Get("WWW-Authenticate"); wa != `Basic realm="gobin"` {
			t.Fatalf(`401 must carry WWW-Authenticate: Basic realm="gobin", got %q`, wa)
		}
	}
}

func TestStage13_Bearer(t *testing.T) {
	srv := startGobin(t, gobin.Config{})
	c := newClient(t)
	resp, body := get(t, c, srv.URL+"/bearer", "Authorization", "Bearer abc.def")
	wantStatus(t, resp, 200, "GET /bearer with a token")
	if m := decodeObject(t, body); m["token"] != "abc.def" || m["authenticated"] != true {
		t.Fatalf(`/bearer = %s, want {"authenticated": true, "token": "abc.def"}`, body)
	}
	for _, auth := range []string{"", "Bearer", "Bearer ", "Basic YTpi"} {
		resp, _ := get(t, c, srv.URL+"/bearer", "Authorization", auth)
		wantStatus(t, resp, 401, "GET /bearer with Authorization: "+auth)
		if wa := resp.Header.Get("WWW-Authenticate"); wa != "Bearer" {
			t.Fatalf("401 must carry WWW-Authenticate: Bearer, got %q", wa)
		}
	}
}
