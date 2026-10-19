package tests

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jamesz3ng/learn_http_go/rawhttp"
)

// startRaw runs rawhttp.Serve on a random local port and returns its address.
func startRaw(t *testing.T) (addr string, ln net.Listener, served <-chan error) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	errc := make(chan error, 1)
	go func() { errc <- rawhttp.Serve(ln) }()
	t.Cleanup(func() { ln.Close() })
	select {
	case err := <-errc:
		t.Fatalf("rawhttp.Serve returned immediately (%v); it should keep accepting connections until the listener is closed", err)
	case <-time.After(30 * time.Millisecond):
	}
	return ln.Addr().String(), ln, errc
}

// rawExchange writes raw bytes to addr and parses the reply as an HTTP response.
func rawExchange(t *testing.T, addr, request string) (*http.Response, string, string) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dialing %s: %v", addr, err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.WriteString(conn, request); err != nil {
		t.Fatalf("writing request: %v", err)
	}
	raw, err := io.ReadAll(conn)
	if err != nil {
		t.Fatalf("reading response: %v%s", err, hint("did you close the connection after writing the response? The client reads until EOF."))
	}
	resp, err := http.ReadResponse(bufio.NewReader(strings.NewReader(string(raw))), nil)
	if err != nil {
		t.Fatalf("the bytes you sent back are not a valid HTTP response: %v\n    raw: %q%s", err, truncate(string(raw), 300),
			hint("a response starts with a status line like \"HTTP/1.1 200 OK\\r\\n\", then headers, then an empty line \"\\r\\n\""))
	}
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b), string(raw)
}

// rawClient is an HTTP client with a short timeout, so that an unfinished
// server fails fast instead of hanging.
func rawClient(t *testing.T) *http.Client {
	c := newClient(t)
	c.Timeout = 2 * time.Second
	return c
}

func TestStage01_GoClientCanTalkToYou(t *testing.T) {
	addr, _, _ := startRaw(t)
	c := rawClient(t)
	resp, body := get(t, c, "http://"+addr+"/hello?x=1")
	wantStatus(t, resp, 200, "GET /hello?x=1")
	for _, want := range []string{"method=GET\n", "target=/hello?x=1\n", "host=" + addr + "\n"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body %q does not contain %q", body, want)
		}
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("Content-Type is %q, want text/plain; charset=utf-8", ct)
	}
}

func TestStage01_WireFormat(t *testing.T) {
	addr, _, _ := startRaw(t)
	// Header names are case-insensitive: "HOST" is the same header as "Host".
	resp, body, raw := rawExchange(t, addr, "GET /raw HTTP/1.1\r\nHOST: example.test\r\nUser-Agent: test\r\n\r\n")
	if !strings.HasPrefix(raw, "HTTP/1.1 200 OK\r\n") {
		t.Fatalf("response must start with %q, got %q", "HTTP/1.1 200 OK\r\n", truncate(raw, 40))
	}
	wantStatus(t, resp, 200, "GET /raw")
	if resp.ContentLength != int64(len(body)) {
		t.Fatalf("Content-Length is %d but the body has %d bytes%s", resp.ContentLength, len(body),
			hint("Content-Length counts bytes of the body only, not headers"))
	}
	want := "method=GET\ntarget=/raw\nhost=example.test\n"
	if body != want {
		t.Fatalf("body = %q, want %q%s", body, want, hint("match header names case-insensitively (strings.EqualFold), and trim spaces around the value"))
	}
}

func TestStage01_MissingHostIsEmpty(t *testing.T) {
	addr, _, _ := startRaw(t)
	_, body, _ := rawExchange(t, addr, "DELETE /thing HTTP/1.0\r\n\r\n")
	want := "method=DELETE\ntarget=/thing\nhost=\n"
	if body != want {
		t.Fatalf("body = %q, want %q", body, want)
	}
}

func TestStage01_BadRequestLine(t *testing.T) {
	addr, _, _ := startRaw(t)
	resp, _, _ := rawExchange(t, addr, "NONSENSE\r\n\r\n")
	wantStatus(t, resp, 400, "malformed request line", "a valid request line has exactly three parts: METHOD TARGET HTTP/x.y")
	resp, _, _ = rawExchange(t, addr, "GET / FTP/1.0\r\n\r\n")
	wantStatus(t, resp, 400, "request line with a non-HTTP version")
}

func TestStage01_PostWithBody(t *testing.T) {
	addr, _, _ := startRaw(t)
	c := rawClient(t)
	resp, body := do(t, c, http.MethodPost, "http://"+addr+"/submit", strings.NewReader("a=1&b=2"))
	wantStatus(t, resp, 200, "POST /submit")
	if !strings.Contains(body, "method=POST\n") {
		t.Fatalf("body %q should contain method=POST", body)
	}
}

func TestStage01_SlowClientDoesNotBlockOthers(t *testing.T) {
	addr, _, _ := startRaw(t)
	// A client that connects and then goes quiet...
	slow, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer slow.Close()
	io.WriteString(slow, "GET /sl")

	// ...must not stop the next client from being served.
	done := make(chan error, 1)
	go func() {
		c := &http.Client{Timeout: 3 * time.Second}
		resp, err := c.Get("http://" + addr + "/fast")
		if err == nil {
			resp.Body.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("second client failed while the first was idle: %v%s", err,
				hint("handle each connection in its own goroutine: go handle(conn)"))
		}
	case <-time.After(4 * time.Second):
		t.Fatalf("second client was not served while the first was idle%s",
			hint("handle each connection in its own goroutine: go handle(conn)"))
	}
}

func TestStage01_ServeReturnsWhenListenerCloses(t *testing.T) {
	_, ln, served := startRaw(t)
	ln.Close()
	select {
	case err := <-served:
		if err == nil {
			t.Fatalf("Serve returned nil after the listener closed; want the Accept error")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("Serve did not return after the listener was closed%s",
			hint("return the error from ln.Accept() instead of looping forever"))
	}
}
