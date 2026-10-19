// Command gocurl runs your curl clone.
//
//	go run ./cmd/gocurl -i http://localhost:8080/get
//
// All the logic lives in package gocurl; you shouldn't need to edit this file.
package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/jamesz3ng/learn_http_go/gocurl"
)

func main() {
	// Cancel the context on Ctrl-C so an in-flight request is aborted cleanly.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := gocurl.Run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}
