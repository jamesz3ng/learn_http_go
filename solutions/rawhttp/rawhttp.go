// Package rawhttp is a tiny HTTP/1.1 server written directly on top of TCP,
// without net/http. It exists to show what net/http does for you.
//
// REFERENCE SOLUTION — Stage 1.
package rawhttp

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

// Serve accepts connections on ln and answers each one with a single
// HTTP/1.1 response describing the request. It returns when Accept fails,
// for example because ln was closed.
func Serve(ln net.Listener) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		// One goroutine per connection: a slow client must never block the
		// others. net/http does exactly the same thing (see conn.serve in
		// net/http/server.go).
		go handle(conn)
	}
}

func handle(conn net.Conn) {
	defer conn.Close()

	// Don't let a client that never finishes its request hold the goroutine
	// forever. net/http calls this ReadHeaderTimeout.
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))

	r := bufio.NewReader(conn)

	// Request line: METHOD SP TARGET SP VERSION CRLF
	line, err := r.ReadString('\n')
	if err != nil {
		return
	}
	parts := strings.Fields(line)
	if len(parts) != 3 || !strings.HasPrefix(parts[2], "HTTP/") {
		writeResponse(conn, 400, "Bad Request", "bad request\n")
		return
	}
	method, target := parts[0], parts[1]

	// Header lines until the empty line. Header names are case-insensitive,
	// so normalise them before comparing.
	var host string
	var bodyLen int64
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			writeResponse(conn, 400, "Bad Request", "bad request\n")
			return
		}
		switch {
		case strings.EqualFold(strings.TrimSpace(name), "Host"):
			host = strings.TrimSpace(value)
		case strings.EqualFold(strings.TrimSpace(name), "Content-Length"):
			bodyLen, _ = strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		}
	}

	// Read (and discard) the request body before answering. If we close the
	// connection with unread bytes still waiting, the kernel resets it, and
	// the client may lose our response. (net/http does the same, bounded.)
	io.CopyN(io.Discard, r, min(bodyLen, 1<<20))

	body := fmt.Sprintf("method=%s\ntarget=%s\nhost=%s\n", method, target, host)
	writeResponse(conn, 200, "OK", body)
}

func writeResponse(conn net.Conn, code int, reason, body string) {
	// Status line, headers, blank line, body. Every line ends in CRLF.
	// Content-Length tells the client where the body ends; Connection: close
	// tells it we will not accept another request on this connection.
	fmt.Fprintf(conn, "HTTP/1.1 %d %s\r\n", code, reason)
	fmt.Fprintf(conn, "Content-Type: text/plain; charset=utf-8\r\n")
	fmt.Fprintf(conn, "Content-Length: %d\r\n", len(body))
	fmt.Fprintf(conn, "Connection: close\r\n")
	fmt.Fprintf(conn, "\r\n")
	fmt.Fprint(conn, body)
}
