package tests

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

// redirectServer serves:
//
//	/hop/N   -> 302 to /hop/N-1, and /hop/0 answers "final"
//	/to?code=C&url=U  -> redirect with status C
//	/echo    -> "METHOD body"
func redirectServer(t *testing.T) *recordingServer {
	return newRecordingServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/hop/"):
			n, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/hop/"))
			if n == 0 {
				io.WriteString(w, "final")
				return
			}
			w.Header().Set("Location", "/hop/"+strconv.Itoa(n-1))
			w.WriteHeader(http.StatusFound)
			io.WriteString(w, "redirecting")
		case r.URL.Path == "/to":
			code, _ := strconv.Atoi(r.URL.Query().Get("code"))
			http.Redirect(w, r, r.URL.Query().Get("url"), code)
		case r.URL.Path == "/echo":
			b, _ := io.ReadAll(r.Body)
			io.WriteString(w, r.Method+" "+string(b))
		}
	})
}

func TestStage12_DoesNotFollowByDefault(t *testing.T) {
	srv := redirectServer(t)
	res := curl(t, srv.URL+"/hop/2")
	res.wantCode(t, 0, "gocurl on a redirect")
	if res.Stdout != "redirecting" {
		t.Fatalf("without -L, stdout = %q, want the 302's own body %q%s", res.Stdout, "redirecting",
			hint("set Client.CheckRedirect to return http.ErrUseLastResponse"))
	}
	if n := len(srv.requests()); n != 1 {
		t.Fatalf("without -L gocurl made %d requests, want 1", n)
	}
}

func TestStage12_Location(t *testing.T) {
	srv := redirectServer(t)
	for _, flag := range []string{"-L", "--location"} {
		res := curl(t, flag, srv.URL+"/hop/3")
		res.wantCode(t, 0, "gocurl "+flag)
		if res.Stdout != "final" {
			t.Fatalf("with %s, stdout = %q, want %q", flag, res.Stdout, "final")
		}
	}
}

func TestStage12_MaxRedirs(t *testing.T) {
	srv := redirectServer(t)
	res := curl(t, "-L", "--max-redirs", "2", srv.URL+"/hop/5")
	res.wantCode(t, 47, "gocurl -L --max-redirs 2 on a chain of 5",
		"return an error from CheckRedirect when len(via) > max; find it again with errors.Is")
	if !strings.Contains(res.Stderr, "redirect") {
		t.Fatalf("stderr %q should say redirects were exhausted", res.Stderr)
	}
	curl(t, "-L", "--max-redirs", "5", srv.URL+"/hop/5").wantCode(t, 0, "gocurl -L --max-redirs 5 on a chain of 5")
	// curl's default is 50, more than Go's default of 10.
	curl(t, "-L", srv.URL+"/hop/20").wantCode(t, 0, "gocurl -L on a chain of 20 (default --max-redirs is 50)")
	curl(t, "-L", "--max-redirs", "0", srv.URL+"/hop/1").wantCode(t, 47, "gocurl -L --max-redirs 0 on one redirect")
}

func TestStage12_IncludeShowsEveryHop(t *testing.T) {
	srv := redirectServer(t)
	res := curl(t, "-i", "-L", srv.URL+"/hop/2")
	res.wantCode(t, 0, "gocurl -i -L")
	out := strings.ReplaceAll(res.Stdout, "\r\n", "\n")
	first := strings.Index(out, "HTTP/1.1 302 Found\nContent-Length")
	second := strings.LastIndex(out, "HTTP/1.1 302 Found\n")
	final := strings.Index(out, "HTTP/1.1 200 OK\n")
	if first < 0 || second <= first || final <= second {
		t.Fatalf("with -i -L, expected two 302 header blocks then the 200 block, got:\n%s%s", res.Stdout,
			hint("inside CheckRedirect, req.Response is the redirect response that led to req"))
	}
	if !strings.Contains(out, "Location: /hop/1\n") || !strings.HasSuffix(out, "\n\nfinal") {
		t.Fatalf("unexpected -i -L output:\n%s", res.Stdout)
	}
	if strings.Contains(out, "redirecting") {
		t.Fatalf("with -L, only the final body is printed, got:\n%s", res.Stdout)
	}
}

func TestStage12_MethodAfterRedirect(t *testing.T) {
	srv := redirectServer(t)

	// 302 (and 301/303): POST becomes GET without a body, like browsers do.
	res := curl(t, "-L", "-d", "a=1", srv.URL+"/to?code=302&url=/echo")
	res.wantCode(t, 0, "gocurl -L -d to a 302")
	if res.Stdout != "GET " {
		t.Fatalf("after a 302 the final request should be a body-less GET, server echoed %q", res.Stdout)
	}

	// 307 and 308 must repeat the same method AND body.
	res = curl(t, "-L", "-d", "a=1", srv.URL+"/to?code=307&url=/echo")
	res.wantCode(t, 0, "gocurl -L -d to a 307")
	if res.Stdout != "POST a=1" {
		t.Fatalf("after a 307 the final request should repeat POST a=1, server echoed %q%s", res.Stdout,
			hint("the client can only resend the body if req.GetBody is set: use a *bytes.Reader or *strings.Reader body"))
	}
}
