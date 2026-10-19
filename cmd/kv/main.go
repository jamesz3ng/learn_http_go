// Command kv is a tiny command-line front end for your kvclient package.
//
//	go run ./cmd/gobin &
//	go run ./cmd/kv put greeting hello
//	go run ./cmd/kv get greeting
//	go run ./cmd/kv list
//	go run ./cmd/kv delete greeting
//	go run ./cmd/kv watch [prefix]      # stage 20
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/jamesz3ng/learn_http_go/kvclient"
)

func main() {
	server := flag.String("server", "http://localhost:8080", "gobin base URL")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: kv [-server URL] put KEY VALUE | get KEY | delete KEY | list [PREFIX] | watch [PREFIX]")
	}
	flag.Parse()
	args := flag.Args()
	if len(args) == 0 {
		flag.Usage()
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	c, err := kvclient.New(*server, nil)
	if err != nil {
		fail(err)
	}

	arg := func(i int) string {
		if i < len(args) {
			return args[i]
		}
		return ""
	}
	need := map[string]int{"put": 3, "get": 2, "delete": 2}
	if n, ok := need[args[0]]; ok && len(args) != n {
		flag.Usage()
		os.Exit(2)
	}
	switch args[0] {
	case "put":
		created, err := c.Put(ctx, arg(1), []byte(arg(2)))
		if err != nil {
			fail(err)
		}
		if created {
			fmt.Println("created")
		} else {
			fmt.Println("updated")
		}
	case "get":
		v, err := c.Get(ctx, arg(1))
		if errors.Is(err, kvclient.ErrNotFound) {
			fmt.Fprintln(os.Stderr, "not found")
			os.Exit(1)
		}
		if err != nil {
			fail(err)
		}
		os.Stdout.Write(v)
		fmt.Println()
	case "delete":
		if err := c.Delete(ctx, arg(1)); err != nil {
			fail(err)
		}
	case "list":
		keys, err := c.List(ctx, arg(1))
		if err != nil {
			fail(err)
		}
		for _, k := range keys {
			fmt.Println(k)
		}
	case "watch":
		err := c.Watch(ctx, arg(1), func(e kvclient.Event) error {
			fmt.Printf("%-6s %s %q\n", e.Type, e.Key, e.Value)
			return nil
		})
		if err != nil && !errors.Is(err, context.Canceled) {
			fail(err)
		}
	default:
		flag.Usage()
		os.Exit(2)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "kv:", err)
	os.Exit(1)
}
