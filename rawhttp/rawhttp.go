// Package rawhttp is an HTTP/1.1 server written directly on TCP, WITHOUT
// net/http. Building it once shows you what net/http does for you.
//
// Stage 1 — read stages/01-raw-http/README.md.
package rawhttp

import (
	"errors"
	"net"
)

// Serve accepts connections on ln and answers each with one HTTP/1.1
// response describing the request it received:
//
//	method=GET
//	target=/hello?x=1
//	host=example.com
//
// It returns a non-nil error when ln.Accept fails (for example because the
// listener was closed).
func Serve(ln net.Listener) error {
	// TODO(stage 1): accept connections in a loop and handle each one.
	return errors.New("rawhttp.Serve: not implemented yet (stage 1)")
}
