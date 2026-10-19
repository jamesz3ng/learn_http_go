// Package gocurl is a small curl clone built on net/http's client.
//
// REFERENCE SOLUTION — comments mark which stage each piece belongs to.
package gocurl

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

// options holds the parsed command line.
type options struct {
	url string

	// Stage 3
	fail bool

	// Stage 5
	method    string
	headers   []string
	userAgent string
	include   bool
	head      bool

	// Stage 7
	data []dataArg
	get  bool

	// Stage 12
	location  bool
	maxRedirs int

	// Stage 14
	user   string
	bearer string
	cookie string

	// Stage 18
	maxTime        float64
	connectTimeout float64

	// Stage 22
	output     string
	forms      []string
	rangeSpec  string
	continueAt string

	// Stage 24
	insecure bool
	cacert   string
	http11   bool
}

// Run is the whole program: main just calls it with the real arguments and
// streams. Taking them as parameters is what makes gocurl testable in-process.
// The return value is the process exit code.
func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	o, err := parseArgs(args, stderr)
	if err != nil {
		return exitUsage
	}
	if err := run(ctx, o, stdin, stdout); err != nil {
		var ce *curlError
		if !errors.As(err, &ce) {
			ce = &curlError{code: exitGeneric, msg: err.Error()}
		}
		fmt.Fprintf(stderr, "gocurl: (%d) %s\n", ce.code, ce.msg)
		return ce.code
	}
	return 0
}

// --- flags

// stringList is a repeatable string flag.
type stringList []string

func (s *stringList) String() string     { return strings.Join(*s, ", ") }
func (s *stringList) Set(v string) error { *s = append(*s, v); return nil }

// dataFlag appends to a shared, ordered list of data arguments, so that
// "-d a --data-binary b -d c" keeps its order.
type dataFlag struct {
	kind dataKind
	dst  *[]dataArg
}

func (d dataFlag) String() string { return "" }
func (d dataFlag) Set(v string) error {
	*d.dst = append(*d.dst, dataArg{kind: d.kind, value: v})
	return nil
}

func parseArgs(args []string, stderr io.Writer) (*options, error) {
	o := &options{}
	fs := flag.NewFlagSet("gocurl", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprintln(stderr, "usage: gocurl [options] URL") }

	// Every option gets a short and a long name bound to the same variable.
	// (The flag package accepts -name and --name interchangeably.)
	boolFlag := func(p *bool, names ...string) {
		for _, n := range names {
			fs.BoolVar(p, n, false, "")
		}
	}
	stringFlag := func(p *string, names ...string) {
		for _, n := range names {
			fs.StringVar(p, n, "", "")
		}
	}
	listFlag := func(p *stringList, names ...string) {
		for _, n := range names {
			fs.Var(p, n, "")
		}
	}

	boolFlag(&o.fail, "f", "fail")

	stringFlag(&o.method, "X", "request")
	var headers, forms stringList
	listFlag(&headers, "H", "header")
	stringFlag(&o.userAgent, "A", "user-agent")
	boolFlag(&o.include, "i", "include")
	boolFlag(&o.head, "I", "head")

	for _, n := range []string{"d", "data", "data-ascii"} {
		fs.Var(dataFlag{dataASCII, &o.data}, n, "")
	}
	fs.Var(dataFlag{dataBinary, &o.data}, "data-binary", "")
	fs.Var(dataFlag{dataURLEncode, &o.data}, "data-urlencode", "")
	fs.Var(dataFlag{dataJSON, &o.data}, "json", "")
	boolFlag(&o.get, "G", "get")

	boolFlag(&o.location, "L", "location")
	fs.IntVar(&o.maxRedirs, "max-redirs", 50, "")

	stringFlag(&o.user, "u", "user")
	stringFlag(&o.bearer, "oauth2-bearer")
	stringFlag(&o.cookie, "b", "cookie")

	fs.Float64Var(&o.maxTime, "m", 0, "")
	fs.Float64Var(&o.maxTime, "max-time", 0, "")
	fs.Float64Var(&o.connectTimeout, "connect-timeout", 0, "")

	stringFlag(&o.output, "o", "output")
	listFlag(&forms, "F", "form")
	stringFlag(&o.rangeSpec, "r", "range")
	stringFlag(&o.continueAt, "C", "continue-at")

	boolFlag(&o.insecure, "k", "insecure")
	stringFlag(&o.cacert, "cacert")
	boolFlag(&o.http11, "http1.1")

	// The flag package stops at the first non-flag argument. Keep going so
	// that "gocurl URL -i" works like "gocurl -i URL".
	var positional []string
	rest := args
	for {
		if err := fs.Parse(rest); err != nil {
			return nil, err
		}
		rest = fs.Args()
		if len(rest) == 0 {
			break
		}
		positional = append(positional, rest[0])
		rest = rest[1:]
	}
	if len(positional) != 1 {
		fmt.Fprintln(stderr, "gocurl: exactly one URL is required")
		fs.Usage()
		return nil, errors.New("usage")
	}
	if o.continueAt != "" && o.output == "" {
		fmt.Fprintln(stderr, "gocurl: -C/--continue-at needs -o/--output")
		return nil, errors.New("usage")
	}
	o.url = positional[0]
	o.headers = headers
	o.forms = forms
	return o, nil
}

// --- the transfer

func run(ctx context.Context, o *options, stdin io.Reader, stdout io.Writer) error {
	// Stage 18: -m bounds the whole operation, body included. A context
	// deadline covers connecting, redirects and reading the body.
	if o.maxTime > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, seconds(o.maxTime))
		defer cancel()
	}

	req, err := buildRequest(ctx, o, stdin)
	if err != nil {
		return err
	}

	// Stage 22: resume a partial download.
	var resumeFrom int64
	if o.continueAt != "" {
		resumeFrom, err = resumeOffset(o)
		if err != nil {
			return err
		}
		if resumeFrom > 0 {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", resumeFrom))
		}
	}

	// Intermediate redirect responses are printed (with -i) as they happen.
	var hops strings.Builder
	client, err := newClient(o, &hops)
	if err != nil {
		return err
	}

	resp, err := client.Do(req)
	if err != nil {
		return classify(err)
	}
	// Always close the body: it returns the connection to the pool.
	defer resp.Body.Close()

	// Stage 3: HTTP errors are not transport errors. Only -f makes them fatal.
	if o.fail && resp.StatusCode >= 400 {
		return &curlError{exitHTTPError, fmt.Sprintf("The requested URL returned error: %d", resp.StatusCode)}
	}

	out, closeOut, err := openOutput(o, resp, resumeFrom, stdout)
	if err != nil {
		return err
	}
	defer closeOut()
	if o.continueAt != "" && resp.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		// Nothing left to download: the file is already complete.
		return nil
	}

	if o.include || o.head {
		io.WriteString(out, hops.String())
		writeHead(out, resp)
	}
	if o.head {
		return nil
	}
	// Stage 3: stream the body. io.Copy uses a small buffer, so gocurl's
	// memory use doesn't depend on the response size, and output appears as
	// soon as it arrives.
	if _, err := io.Copy(out, resp.Body); err != nil {
		var we *writeError
		if errors.As(err, &we) {
			return &curlError{exitWriteError, "Failure writing output: " + we.err.Error()}
		}
		return classify(err)
	}
	return nil
}

// writeHead prints the status line and headers, sorted for stable output.
func writeHead(w io.Writer, resp *http.Response) {
	// resp.Status is "200 OK"; resp.Proto is "HTTP/1.1" or "HTTP/2.0".
	fmt.Fprintf(w, "%s %s\r\n", resp.Proto, resp.Status)
	for _, name := range slices.Sorted(maps.Keys(resp.Header)) {
		for _, v := range resp.Header[name] {
			fmt.Fprintf(w, "%s: %s\r\n", name, v)
		}
	}
	io.WriteString(w, "\r\n")
}

// writeError marks errors that came from writing output (as opposed to
// reading the network), so they map to a different exit code.
type writeError struct{ err error }

func (e *writeError) Error() string { return e.err.Error() }

type errWriter struct{ w io.Writer }

func (e errWriter) Write(p []byte) (int, error) {
	n, err := e.w.Write(p)
	if err != nil {
		return n, &writeError{err}
	}
	return n, nil
}

// openOutput decides where the body goes: stdout, or -o FILE (appending when
// resuming and the server sent 206 Partial Content).
func openOutput(o *options, resp *http.Response, resumeFrom int64, stdout io.Writer) (io.Writer, func(), error) {
	if o.output == "" {
		return errWriter{stdout}, func() {}, nil
	}
	flags := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	if resumeFrom > 0 && resp.StatusCode == http.StatusPartialContent {
		flags = os.O_CREATE | os.O_WRONLY | os.O_APPEND
	}
	if resumeFrom > 0 && resp.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		return io.Discard, func() {}, nil // leave the complete file alone
	}
	f, err := os.OpenFile(o.output, flags, 0o644)
	if err != nil {
		return nil, nil, &curlError{exitWriteError, "Failed to open output file: " + err.Error()}
	}
	return errWriter{f}, func() { f.Close() }, nil
}

// resumeOffset works out where -C should continue from.
func resumeOffset(o *options) (int64, error) {
	if o.continueAt != "-" {
		n, err := strconv.ParseInt(o.continueAt, 10, 64)
		if err != nil || n < 0 {
			return 0, &curlError{exitUsage, "invalid -C offset: " + o.continueAt}
		}
		return n, nil
	}
	info, err := os.Stat(o.output)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, &curlError{exitReadError, err.Error()}
	}
	return info.Size(), nil
}

func seconds(f float64) time.Duration { return time.Duration(f * float64(time.Second)) }
