// Command rawhttp runs your raw-TCP HTTP server from stage 1.
//
//	go run ./cmd/rawhttp -addr :8081
//	curl -v http://localhost:8081/hello
package main

import (
	"flag"
	"log"
	"net"

	"github.com/jamesz3ng/learn_http_go/rawhttp"
)

func main() {
	addr := flag.String("addr", ":8081", "listen address")
	flag.Parse()

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("rawhttp listening on %s", ln.Addr())
	log.Fatal(rawhttp.Serve(ln))
}
