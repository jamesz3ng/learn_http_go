// Package gocurl is your curl clone, built on net/http's client. You will
// add options over many stages.
//
// The whole program lives in Run so that tests can call it in-process;
// cmd/gocurl/main.go just passes it the real arguments and streams.
package gocurl

import (
	"context"
	"fmt"
	"io"
)

// Run executes gocurl with the given command-line arguments (without the
// program name) and returns the process exit code.
//
//   - stdin is read for "-d @-" (stage 7).
//   - The response body goes to stdout.
//   - Errors go to stderr as "gocurl: (CODE) message".
//   - ctx is cancelled when the user presses Ctrl-C (stage 18).
//
// Stage 3 starts here.
func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	// TODO(stage 3)
	fmt.Fprintln(stderr, "gocurl: not implemented yet (start at stage 3)")
	return 1
}
