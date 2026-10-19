package tests

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStage07_Data(t *testing.T) {
	srv := newRecordingServer(t, nil)
	curl(t, "-d", "name=gopher", "-d", "lang=go", srv.URL+"/submit").wantCode(t, 0, "gocurl -d ... -d ...")
	r := srv.last(t)
	if r.Method != "POST" {
		t.Fatalf("-d should send a POST, server saw %s", r.Method)
	}
	if r.Body != "name=gopher&lang=go" {
		t.Fatalf("body = %q, want %q (multiple -d are joined with &)", r.Body, "name=gopher&lang=go")
	}
	if ct := r.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
		t.Fatalf("Content-Type = %q, want application/x-www-form-urlencoded", ct)
	}
	if r.ContentLength != int64(len(r.Body)) || len(r.TransferEnc) != 0 {
		t.Fatalf("the request should have Content-Length %d, got ContentLength=%d TransferEncoding=%v%s",
			len(r.Body), r.ContentLength, r.TransferEnc,
			hint("http.NewRequest knows the length of *bytes.Reader, *bytes.Buffer and *strings.Reader bodies; other readers get chunked encoding"))
	}
}

func TestStage07_DataFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "body.txt")
	os.WriteFile(path, []byte("line1\r\nline2\n"), 0o644)

	srv := newRecordingServer(t, nil)
	curl(t, "-d", "@"+path, srv.URL).wantCode(t, 0, "gocurl -d @file")
	if b := srv.last(t).Body; b != "line1line2" {
		t.Fatalf("-d @file body = %q, want %q (like curl, -d strips CR and LF from files)", b, "line1line2")
	}
	curl(t, "--data-binary", "@"+path, srv.URL).wantCode(t, 0, "gocurl --data-binary @file")
	if b := srv.last(t).Body; b != "line1\r\nline2\n" {
		t.Fatalf("--data-binary @file body = %q, want the exact file bytes", b)
	}
	curl(t, "-d", "@"+filepath.Join(dir, "missing"), srv.URL).wantCode(t, 26, "gocurl -d @missing-file")
}

func TestStage07_DataFromStdin(t *testing.T) {
	srv := newRecordingServer(t, nil)
	curlStdin(t, "from=stdin", "-d", "@-", srv.URL).wantCode(t, 0, "gocurl -d @-")
	if b := srv.last(t).Body; b != "from=stdin" {
		t.Fatalf("-d @- body = %q, want %q", b, "from=stdin")
	}
}

func TestStage07_DataURLEncode(t *testing.T) {
	srv := newRecordingServer(t, func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		w.Write([]byte(r.PostForm.Get("q") + "|" + r.PostForm.Get("plain")))
	})
	res := curl(t, "--data-urlencode", "q=a b&c=d", "-d", "plain=1", srv.URL)
	res.wantCode(t, 0, "gocurl --data-urlencode")
	if res.Stdout != "a b&c=d|1" {
		t.Fatalf("server decoded q|plain = %q, want %q%s", res.Stdout, "a b&c=d|1",
			hint("encode only the part after the first '=' with url.QueryEscape"))
	}
}

func TestStage07_JSON(t *testing.T) {
	srv := newRecordingServer(t, nil)
	curl(t, "--json", `{"a":1}`, srv.URL).wantCode(t, 0, "gocurl --json")
	r := srv.last(t)
	if r.Method != "POST" || r.Body != `{"a":1}` {
		t.Fatalf("--json sent %s %q, want POST %q", r.Method, r.Body, `{"a":1}`)
	}
	if ct := r.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("--json Content-Type = %q, want application/json", ct)
	}
	if a := r.Header.Get("Accept"); a != "application/json" {
		t.Fatalf("--json Accept = %q, want application/json", a)
	}
}

func TestStage07_ContentTypeOverride(t *testing.T) {
	srv := newRecordingServer(t, nil)
	curl(t, "-H", "Content-Type: text/plain", "-d", "hello", srv.URL).wantCode(t, 0, "gocurl -H Content-Type -d")
	if ct := srv.last(t).Header.Get("Content-Type"); ct != "text/plain" {
		t.Fatalf("Content-Type = %q, want the -H value text/plain (user headers win over defaults)", ct)
	}
}

func TestStage07_MethodOverride(t *testing.T) {
	srv := newRecordingServer(t, nil)
	curl(t, "-X", "PUT", "-d", "v=1", srv.URL).wantCode(t, 0, "gocurl -X PUT -d")
	if r := srv.last(t); r.Method != "PUT" || r.Body != "v=1" {
		t.Fatalf("got %s %q, want PUT %q", r.Method, r.Body, "v=1")
	}
}

func TestStage07_Get(t *testing.T) {
	srv := newRecordingServer(t, nil)
	curl(t, "-G", "-d", "q=go", "--data-urlencode", "s=a b", srv.URL+"/search?page=2").wantCode(t, 0, "gocurl -G")
	r := srv.last(t)
	if r.Method != "GET" {
		t.Fatalf("-G should send GET, got %s", r.Method)
	}
	if r.Body != "" {
		t.Fatalf("-G should send no body, got %q", r.Body)
	}
	if got, want := r.RawQuery, "page=2&q=go&s=a+b"; got != want {
		t.Fatalf("query = %q, want %q (append the data to any existing query)", got, want)
	}
	if r.Path != "/search" {
		t.Fatalf("path = %q, want /search", r.Path)
	}
}

func TestStage07_NoBodyMeansNoContentType(t *testing.T) {
	srv := newRecordingServer(t, nil)
	curl(t, srv.URL).wantCode(t, 0, "gocurl")
	if ct := srv.last(t).Header.Get("Content-Type"); ct != "" {
		t.Fatalf("a request without a body should have no Content-Type, got %q", ct)
	}
	if strings.ToUpper(srv.last(t).Method) != "GET" {
		t.Fatalf("no data should mean GET")
	}
}
