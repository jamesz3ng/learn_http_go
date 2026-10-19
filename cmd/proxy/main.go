// Command proxy runs your load-balancing reverse proxy (stage 26).
//
//	go run ./cmd/gobin -addr :9001 &
//	go run ./cmd/gobin -addr :9002 &
//	go run ./cmd/proxy -listen :8080 -backend http://localhost:9001 -backend http://localhost:9002
package main

import (
	"flag"
	"log"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jamesz3ng/learn_http_go/proxy"
)

type list []string

func (l *list) String() string     { return strings.Join(*l, ",") }
func (l *list) Set(v string) error { *l = append(*l, v); return nil }

func main() {
	listen := flag.String("listen", ":8080", "listen address")
	var backends list
	flag.Var(&backends, "backend", "backend base URL (repeatable)")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	h, err := proxy.New(proxy.Config{Backends: backends, Logger: logger})
	if err != nil {
		log.Fatal(err)
	}
	srv := &http.Server{
		Addr:              *listen,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	logger.Info("proxy listening", "addr", *listen, "backends", backends)
	log.Fatal(srv.ListenAndServe())
}
