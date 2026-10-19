package main

import "fmt"

// stage describes one step of the course.
type stage struct {
	Num     int
	Slug    string // directory under stages/
	Title   string
	Project string // what you build in this stage
	Part    string
}

func (s stage) dir() string    { return "stages/" + s.Slug }
func (s stage) readme() string { return s.dir() + "/README.md" }
func (s stage) label() string  { return fmt.Sprintf("Stage %d", s.Num) }

var stages = []stage{
	{0, "00-orientation", "Orientation: how to learn here, HTTP on the wire", "reading", "0 · Orientation"},

	{1, "01-raw-http", "HTTP is just text", "rawhttp", "I · Foundations"},
	{2, "02-hello-server", "Handlers and ServeMux", "gobin", "I · Foundations"},
	{3, "03-hello-client", "Your first request", "gocurl", "I · Foundations"},

	{4, "04-inspecting-requests", "Reading requests", "gobin", "II · Messages"},
	{5, "05-building-requests", "Building requests", "gocurl", "II · Messages"},
	{6, "06-reading-bodies", "Request bodies on the server", "gobin", "II · Messages"},
	{7, "07-sending-bodies", "Sending bodies", "gocurl", "II · Messages"},

	{8, "08-routing", "Routing in depth", "gobin", "III · Resources"},
	{9, "09-kv-store", "A concurrent REST resource", "gobin", "III · Resources"},
	{10, "10-api-client", "An API client SDK", "kvclient", "III · Resources"},

	{11, "11-redirects", "Sending redirects", "gobin", "IV · Protocol semantics"},
	{12, "12-following-redirects", "Following redirects", "gocurl", "IV · Protocol semantics"},
	{13, "13-cookies-and-auth", "Cookies and authentication", "gobin", "IV · Protocol semantics"},
	{14, "14-client-state", "Cookie jars and credentials", "gocurl", "IV · Protocol semantics"},

	{15, "15-middleware", "Middleware", "gobin", "V · Composition"},
	{16, "16-round-trippers", "RoundTrippers: client middleware", "kvclient", "V · Composition"},

	{17, "17-server-timeouts", "Server timeouts and cancellation", "gobin", "VI · Time"},
	{18, "18-client-timeouts", "Client timeouts", "gocurl", "VI · Time"},

	{19, "19-streaming", "Streaming responses and SSE", "gobin", "VII · Streams and files"},
	{20, "20-consuming-streams", "Consuming streams", "kvclient", "VII · Streams and files"},
	{21, "21-files", "Files, ranges and uploads", "gobin", "VII · Streams and files"},
	{22, "22-transfers", "Downloads, uploads and resuming", "gocurl", "VII · Streams and files"},

	{23, "23-graceful-shutdown", "Graceful shutdown", "gobin", "VIII · Production"},
	{24, "24-tls-and-http2", "TLS and HTTP/2", "gobin + gocurl", "VIII · Production"},
	{25, "25-connection-pooling", "Connection pooling and httptrace", "loadgen", "VIII · Production"},

	{26, "26-reverse-proxy", "Capstone: a load-balancing reverse proxy", "proxy", "IX · Capstone"},
}

const lastStage = 26

func stageByNum(n int) (stage, bool) {
	if n < 0 || n >= len(stages) {
		return stage{}, false
	}
	return stages[n], true
}
