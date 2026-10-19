// Command loadgen load-tests a URL using your loadgen package (stage 25).
//
//	go run ./cmd/loadgen -n 2000 -c 50 http://localhost:8080/get
package main

import (
	"context"
	"flag"
	"fmt"
	"maps"
	"os"
	"os/signal"
	"slices"
	"time"

	"github.com/jamesz3ng/learn_http_go/loadgen"
)

func main() {
	n := flag.Int("n", 200, "total number of requests")
	c := flag.Int("c", 10, "number of concurrent workers")
	method := flag.String("m", "GET", "HTTP method")
	timeout := flag.Duration("t", 10*time.Second, "per-request timeout")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: loadgen [-n N] [-c C] [-m METHOD] [-t TIMEOUT] URL")
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	r, err := loadgen.Run(ctx, loadgen.Config{
		URL:         flag.Arg(0),
		Method:      *method,
		Requests:    *n,
		Concurrency: *c,
		Timeout:     *timeout,
	})
	if r == nil {
		fmt.Fprintln(os.Stderr, "loadgen:", err)
		os.Exit(1)
	}

	fmt.Printf("Requests:     %d in %v (%.1f req/s)\n", r.Requests, r.Elapsed.Round(time.Millisecond),
		float64(r.Requests)/r.Elapsed.Seconds())
	fmt.Printf("Errors:       %d\n", r.Errors)
	fmt.Printf("Connections:  %d new, %d reused\n", r.NewConns, r.ReusedConns)
	fmt.Printf("Latency:      p50 %v  p90 %v  p99 %v  max %v\n", r.P50, r.P90, r.P99, r.Max)
	fmt.Println("Status codes:")
	for _, code := range slices.Sorted(maps.Keys(r.StatusCodes)) {
		fmt.Printf("  %d: %d\n", code, r.StatusCodes[code])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "loadgen: stopped early:", err)
		os.Exit(1)
	}
}
