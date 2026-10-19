package tests

import (
	"io"
	"net/http"
	"sort"
	"strings"
	"testing"
)

func TestStage14_BasicAuth(t *testing.T) {
	srv := newRecordingServer(t, nil)
	curl(t, "-u", "alice:s3:cret", srv.URL).wantCode(t, 0, "gocurl -u")
	req, _ := http.NewRequest("GET", "/", nil)
	req.Header = srv.last(t).Header
	user, pass, ok := req.BasicAuth()
	if !ok || user != "alice" || pass != "s3:cret" {
		t.Fatalf("server saw basic auth (%q, %q, %v), want (alice, s3:cret)%s", user, pass, ok,
			hint("split on the FIRST colon only; req.SetBasicAuth does the encoding"))
	}
	curl(t, "--user", "bob", srv.URL).wantCode(t, 0, "gocurl --user bob")
	req.Header = srv.last(t).Header
	if user, pass, ok := req.BasicAuth(); !ok || user != "bob" || pass != "" {
		t.Fatalf("--user bob: server saw (%q, %q, %v), want (bob, \"\")", user, pass, ok)
	}
}

func TestStage14_Bearer(t *testing.T) {
	srv := newRecordingServer(t, nil)
	curl(t, "--oauth2-bearer", "tok123", srv.URL).wantCode(t, 0, "gocurl --oauth2-bearer")
	if got := srv.last(t).Header.Get("Authorization"); got != "Bearer tok123" {
		t.Fatalf("Authorization = %q, want %q", got, "Bearer tok123")
	}
}

// cookieServer: /set?k=v sets cookies and redirects to /show, which prints
// the cookies it received as "k=v" lines, sorted.
func cookieServer(t *testing.T) *recordingServer {
	return newRecordingServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/set":
			for k, v := range r.URL.Query() {
				http.SetCookie(w, &http.Cookie{Name: k, Value: v[0], Path: "/"})
			}
			http.Redirect(w, r, "/show", http.StatusFound)
		case "/show":
			var lines []string
			for _, c := range r.Cookies() {
				lines = append(lines, c.Name+"="+c.Value)
			}
			sort.Strings(lines)
			io.WriteString(w, strings.Join(lines, "\n"))
		}
	})
}

func TestStage14_SendCookies(t *testing.T) {
	srv := cookieServer(t)
	res := curl(t, "-b", "a=1; b=2", srv.URL+"/show")
	res.wantCode(t, 0, "gocurl -b")
	if res.Stdout != "a=1\nb=2" {
		t.Fatalf("server received cookies %q, want a=1 and b=2", res.Stdout)
	}
}

func TestStage14_CookiesSurviveRedirects(t *testing.T) {
	srv := cookieServer(t)
	res := curl(t, "-L", srv.URL+"/set?session=xyz")
	res.wantCode(t, 0, "gocurl -L to a cookie-setting redirect")
	if res.Stdout != "session=xyz" {
		t.Fatalf("after /set?session=xyz -> /show, the server saw cookies %q, want session=xyz%s", res.Stdout,
			hint("give the http.Client a Jar (net/http/cookiejar) so Set-Cookie from one hop is sent on the next"))
	}
	res = curl(t, "-L", "-b", "pre=1", srv.URL+"/set?session=xyz")
	res.wantCode(t, 0, "gocurl -L -b")
	if res.Stdout != "pre=1\nsession=xyz" {
		t.Fatalf("with -b pre=1 and a redirect that sets session=xyz, the server saw %q, want both", res.Stdout)
	}
}
