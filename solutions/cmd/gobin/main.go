// Command gobin, final version (reference solution for cmd/gobin/main.go
// after stages 15, 17, 21, 23 and 24).
//
//	go run ./solutions/cmd/gobin -addr :8080 -static . -json
//	go run ./solutions/cmd/gobin -addr :8443 -cert cert.pem -key key.pem
package main

import (
	"context"
	"flag"
	"log"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jamesz3ng/learn_http_go/gobin"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	static := flag.String("static", "", "directory to serve under /static/ (stage 21)")
	certFile := flag.String("cert", "", "TLS certificate file (stage 24)")
	keyFile := flag.String("key", "", "TLS key file (stage 24)")
	jsonLogs := flag.Bool("json", false, "log as JSON instead of text")
	flag.Parse()

	// Stage 15: structured access logs.
	var handler slog.Handler = slog.NewTextHandler(os.Stderr, nil)
	if *jsonLogs {
		handler = slog.NewJSONHandler(os.Stderr, nil)
	}
	logger := slog.New(handler)

	cfg := gobin.Config{
		Logger:          logger,
		Addr:            *addr,
		ShutdownTimeout: 15 * time.Second,
		TLSCertFile:     *certFile,
		TLSKeyFile:      *keyFile,
	}
	if *static != "" {
		cfg.Static = os.DirFS(*static)
	}

	// Stage 23: Ctrl-C (SIGINT) or SIGTERM starts a graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	logger.Info("gobin listening", "addr", ln.Addr().String(), "tls", *certFile != "")
	if err := gobin.Run(ctx, ln, cfg); err != nil {
		logger.Error("gobin stopped", "err", err)
		os.Exit(1)
	}
	logger.Info("gobin stopped cleanly")
}
