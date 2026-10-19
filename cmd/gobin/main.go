// Command gobin runs your gobin server.
//
//	go run ./cmd/gobin -addr :8080
//
// Stage 23 asks you to rewrite this file to shut down gracefully.
package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/jamesz3ng/learn_http_go/gobin"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	flag.Parse()

	cfg := gobin.Config{Addr: *addr}
	log.Printf("gobin listening on %s", *addr)

	// ListenAndServe blocks until the server fails; it never returns nil.
	log.Fatal(http.ListenAndServe(*addr, gobin.NewHandler(cfg)))
}
