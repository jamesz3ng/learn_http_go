package tests

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jamesz3ng/learn_http_go/gocurl"
)

func TestStage03_PrintsBody(t *testing.T) {
	srv := newRecordingServer(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "hello from the server\n")
	})
	res := curl(t, srv.URL+"/greeting")
	res.wantCode(t, 0, "gocurl URL")
	if res.Stdout != "hello from the server\n" {
		t.Fatalf("stdout = %q, want the response body %q", res.Stdout, "hello from the server\n")
	}
	if got := srv.last(t); got.Method != "GET" || got.Path != "/greeting" {
		t.Fatalf("server saw %s %s, want GET /greeting", got.Method, got.Path)
	}
}

func TestStage03_ErrorStatusIsNotAnError(t *testing.T) {
	srv := newRecordingServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nothing here", http.StatusNotFound)
	})
	res := curl(t, srv.URL+"/missing")
	res.wantCode(t, 0, "gocurl on a 404",
		"an HTTP error status is still a successful HTTP exchange; http.Get only returns an error when there's no response at all")
	if res.Stdout != "nothing here\n" {
		t.Fatalf("stdout = %q, want the 404 body %q", res.Stdout, "nothing here\n")
	}
}

func TestStage03_FailFlag(t *testing.T) {
	srv := newRecordingServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nothing here", http.StatusNotFound)
	})
	for _, flag := range []string{"-f", "--fail"} {
		res := curl(t, flag, srv.URL+"/missing")
		res.wantCode(t, 22, "gocurl "+flag+" on a 404", "with -f, a status >= 400 means exit code 22")
		if res.Stdout != "" {
			t.Fatalf("with %s, nothing should be written to stdout on failure, got %q", flag, res.Stdout)
		}
		if !strings.Contains(res.Stderr, "404") {
			t.Fatalf("stderr %q should mention the status code 404", res.Stderr)
		}
	}
	ok := newRecordingServer(t, nil)
	curl(t, "-f", ok.URL).wantCode(t, 0, "gocurl -f on a 200")
}

func TestStage03_ConnectionRefused(t *testing.T) {
	res := curl(t, closedPortURL(t)+"/")
	res.wantCode(t, 7, "gocurl to a port nobody listens on",
		"use errors.As to find a *net.OpError with Op == \"dial\" in the error chain")
	if !strings.HasPrefix(res.Stderr, "gocurl: (7)") {
		t.Fatalf("stderr = %q, want it to start with %q", res.Stderr, "gocurl: (7)")
	}
}

func TestStage03_Usage(t *testing.T) {
	curl(t).wantCode(t, 2, "gocurl with no URL")
	curl(t, "--no-such-flag", "http://example.com").wantCode(t, 2, "gocurl with an unknown flag")
}

func TestStage03_BadURL(t *testing.T) {
	curl(t, "http://[::1").wantCode(t, 3, "gocurl with a malformed URL")
	curl(t, "ftp://example.com/file").wantCode(t, 1, "gocurl with an unsupported scheme")
}

func TestStage03_URLWithoutScheme(t *testing.T) {
	srv := newRecordingServer(t, nil)
	hostPort := strings.TrimPrefix(srv.URL, "http://")
	res := curl(t, hostPort+"/x")
	res.wantCode(t, 0, "gocurl "+hostPort+"/x",
		`like curl, assume http:// when the URL has no "://". Careful: url.Parse("127.0.0.1:8080/x") does not do what you expect`)
	if srv.last(t).Path != "/x" {
		t.Fatalf("server saw path %q, want /x", srv.last(t).Path)
	}
}

func TestStage03_StreamsTheBody(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	finish := func() { once.Do(func() { close(release) }) }
	defer finish()
	srv := newRecordingServer(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "first line\n")
		http.NewResponseController(w).Flush()
		select { // hold the response open until the test is done
		case <-release:
		case <-r.Context().Done():
		}
		io.WriteString(w, "second line\n")
	})

	out := &syncBuffer{}
	done := make(chan int, 1)
	go func() {
		done <- gocurl.Run(context.Background(), []string{srv.URL}, strings.NewReader(""), out, io.Discard)
	}()
	if !eventually(3*time.Second, func() bool { return strings.Contains(out.String(), "first line") }) {
		t.Fatalf("the first line of the body was not written to stdout while the response was still open%s",
			hint("copy the body to stdout as it arrives (io.Copy) instead of reading it all first (io.ReadAll)"))
	}
	finish()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("gocurl did not finish after the response completed")
	}
	if out.String() != "first line\nsecond line\n" {
		t.Fatalf("stdout = %q, want both lines", out.String())
	}
}
