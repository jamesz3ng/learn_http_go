package tests

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jamesz3ng/learn_http_go/gocurl"
)

// stallServer stalls before sending headers on /slow-headers, and in the
// middle of the body on /slow-body. Stalls end when the client leaves.
func stallServer(t *testing.T) *recordingServer {
	return newRecordingServer(t, func(w http.ResponseWriter, r *http.Request) {
		wait := func(d time.Duration) {
			select {
			case <-time.After(d):
			case <-r.Context().Done():
			}
		}
		switch r.URL.Path {
		case "/slow-headers":
			wait(10 * time.Second)
			io.WriteString(w, "too late")
		case "/slow-body":
			io.WriteString(w, "partial ")
			http.NewResponseController(w).Flush()
			wait(10 * time.Second)
			io.WriteString(w, "rest")
		case "/one-second":
			wait(time.Second)
			io.WriteString(w, "worth the wait")
		default:
			io.WriteString(w, "quick")
		}
	})
}

func TestStage18_MaxTimeBeforeHeaders(t *testing.T) {
	srv := stallServer(t)
	start := time.Now()
	res := curl(t, "-m", "0.3", srv.URL+"/slow-headers")
	res.wantCode(t, 28, "gocurl -m 0.3 against a server that stalls before responding",
		"time out with Client.Timeout or a context deadline; recognise it with errors.Is(err, context.DeadlineExceeded) or a net.Error whose Timeout() is true")
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("gocurl -m 0.3 took %v", d)
	}
	if !strings.HasPrefix(res.Stderr, "gocurl: (28)") {
		t.Fatalf("stderr = %q, want it to start with %q", res.Stderr, "gocurl: (28)")
	}
}

func TestStage18_MaxTimeCoversTheBody(t *testing.T) {
	srv := stallServer(t)
	start := time.Now()
	res := curl(t, "--max-time", "0.5", srv.URL+"/slow-body")
	res.wantCode(t, 28, "gocurl --max-time 0.5 against a server that stalls mid-body",
		"-m limits the WHOLE transfer, including reading the body. Transport.ResponseHeaderTimeout alone is not enough")
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("gocurl --max-time 0.5 took %v", d)
	}
	if res.Stdout != "partial " {
		t.Fatalf("stdout = %q, want the part of the body that arrived before the timeout", res.Stdout)
	}
}

func TestStage18_FastEnough(t *testing.T) {
	srv := stallServer(t)
	res := curl(t, "-m", "5", "--connect-timeout", "2", srv.URL+"/fast")
	res.wantCode(t, 0, "gocurl -m 5 --connect-timeout 2 against a fast server")
	if res.Stdout != "quick" {
		t.Fatalf("stdout = %q", res.Stdout)
	}
}

func TestStage18_NoTimeoutByDefault(t *testing.T) {
	srv := stallServer(t)
	res := curl(t, srv.URL+"/one-second")
	res.wantCode(t, 0, "gocurl without -m against a server that takes 1s",
		"like curl, gocurl has no overall timeout unless -m is given")
	if res.Stdout != "worth the wait" {
		t.Fatalf("stdout = %q", res.Stdout)
	}
}

func TestStage18_ContextCancellation(t *testing.T) {
	srv := stallServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() {
		done <- gocurl.Run(ctx, []string{srv.URL + "/slow-body"}, strings.NewReader(""), io.Discard, io.Discard)
	}()
	time.Sleep(200 * time.Millisecond)
	cancel() // what Ctrl-C does via signal.NotifyContext in cmd/gocurl
	select {
	case code := <-done:
		if code != 130 {
			t.Fatalf("after the context was cancelled gocurl exited %d, want 130%s", code,
				hint("errors.Is(err, context.Canceled) — check it after timeouts, since a deadline is not a cancellation"))
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("gocurl kept running after its context was cancelled%s", hint("build the request with http.NewRequestWithContext(ctx, ...)"))
	}
}
