package tests

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jamesz3ng/learn_http_go/kvclient"
)

// sseServer answers /watch with the given raw stream, then optionally holds
// the connection open until the client leaves.
func sseServer(t *testing.T, raw string, hold bool) *recordingServer {
	return newRecordingServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, raw)
		http.NewResponseController(w).Flush()
		if hold {
			<-r.Context().Done()
		}
	})
}

func collect(t *testing.T, base string, ctx context.Context) ([]kvclient.Event, error) {
	t.Helper()
	kv := newKV(t, base)
	var got []kvclient.Event
	err := kv.Watch(ctx, "us er:", func(e kvclient.Event) error {
		got = append(got, e)
		return nil
	})
	return got, err
}

func TestStage20_Basic(t *testing.T) {
	srv := sseServer(t,
		"id: 1\nevent: put\ndata: {\"key\":\"user:1\",\"value\":\"alice\"}\n\n"+
			"id: 2\nevent: delete\ndata: {\"key\":\"user:1\"}\n\n", false)
	got, err := collect(t, srv.URL, context.Background())
	if err != nil {
		t.Fatalf("Watch returned %v when the server ended the stream normally; want nil", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d events, want 2: %+v", len(got), got)
	}
	if got[0].ID != "1" || got[0].Type != "put" || got[0].Key != "user:1" || string(got[0].Value) != "alice" {
		t.Fatalf("event 0 = %+v", got[0])
	}
	if got[1].ID != "2" || got[1].Type != "delete" || got[1].Key != "user:1" || got[1].Value != nil {
		t.Fatalf("event 1 = %+v, want a delete with nil Value", got[1])
	}

	r := srv.last(t)
	if r.Path != "/watch" || r.RawQuery != "prefix=us+er%3A" {
		t.Fatalf("request was %s?%s, want /watch?prefix=us+er%%3A", r.Path, r.RawQuery)
	}
	if a := r.Header.Get("Accept"); a != "text/event-stream" {
		t.Fatalf("Accept = %q, want text/event-stream", a)
	}
}

func TestStage20_FormatDetails(t *testing.T) {
	srv := sseServer(t,
		": this is a comment\r\n"+
			"retry: 1000\r\n"+
			"event:put\r\n"+ // no space after the colon is allowed
			"data: {\"key\":\"a\",\r\n"+ // data split over two lines
			"data: \"value\":\"x y\"}\r\n"+
			"\r\n"+
			": keepalive\n\n"+
			"event: put\ndata: {\"key\":\"b\",\"value\":\"2\"}\n\n", false)
	got, err := collect(t, srv.URL, context.Background())
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d events, want 2 (comments and keepalives are not events): %+v%s", len(got), got,
			hint("lines starting with ':' are comments; CRLF line endings are allowed; multiple data: lines are joined with \\n"))
	}
	if got[0].Key != "a" || string(got[0].Value) != "x y" || got[0].Type != "put" {
		t.Fatalf("event 0 = %+v, want key a, value \"x y\"", got[0])
	}
	if got[1].Key != "b" {
		t.Fatalf("event 1 = %+v", got[1])
	}
}

func TestStage20_LargeEvent(t *testing.T) {
	big := strings.Repeat("z", 200<<10)
	srv := sseServer(t, "event: put\ndata: {\"key\":\"big\",\"value\":\""+big+"\"}\n\n", false)
	got, err := collect(t, srv.URL, context.Background())
	if err != nil {
		t.Fatalf("Watch with a 200 KiB event: %v%s", err, hint("bufio.Scanner refuses lines over 64 KiB unless you call Scanner.Buffer"))
	}
	if len(got) != 1 || len(got[0].Value) != len(big) {
		t.Fatalf("did not receive the 200 KiB value intact")
	}
}

func TestStage20_EventsArriveLive(t *testing.T) {
	srv := sseServer(t, "event: put\ndata: {\"key\":\"live\",\"value\":\"1\"}\n\n", true)
	kv := newKV(t, srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	gotOne := make(chan kvclient.Event, 1)
	done := make(chan error, 1)
	go func() {
		done <- kv.Watch(ctx, "", func(e kvclient.Event) error {
			gotOne <- e
			return nil
		})
	}()
	select {
	case e := <-gotOne:
		if e.Key != "live" {
			t.Fatalf("event = %+v", e)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("the event wasn't delivered while the stream was still open%s",
			hint("parse the body as it arrives (bufio.Scanner over resp.Body), don't io.ReadAll it"))
	}

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("after cancelling the context Watch returned %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Watch didn't return after its context was cancelled")
	}
}

func TestStage20_CallbackErrorStopsTheStream(t *testing.T) {
	var mu sync.Mutex
	serverDone := false
	srv := newRecordingServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		rc := http.NewResponseController(w)
		defer func() { mu.Lock(); serverDone = true; mu.Unlock() }()
		for {
			if _, err := io.WriteString(w, "event: put\ndata: {\"key\":\"k\",\"value\":\"v\"}\n\n"); err != nil {
				return
			}
			if rc.Flush() != nil {
				return
			}
			select {
			case <-r.Context().Done():
				return
			case <-time.After(20 * time.Millisecond):
			}
		}
	})
	kv := newKV(t, srv.URL)
	errStop := errors.New("seen enough")
	n := 0
	err := kv.Watch(context.Background(), "", func(kvclient.Event) error {
		n++
		if n == 3 {
			return errStop
		}
		return nil
	})
	if !errors.Is(err, errStop) {
		t.Fatalf("Watch returned %v, want the callback's error", err)
	}
	if !eventually(2*time.Second, func() bool { mu.Lock(); defer mu.Unlock(); return serverDone }) {
		t.Fatalf("the server is still streaming: Watch must close the response body when it stops")
	}
}

func TestStage20_ErrorStatus(t *testing.T) {
	srv := newRecordingServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"watching is disabled"}`, http.StatusServiceUnavailable)
	})
	_, err := collect(t, srv.URL, context.Background())
	var apiErr *kvclient.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 503 || apiErr.Message != "watching is disabled" {
		t.Fatalf("Watch on a 503 returned %v, want an *APIError with status 503 and the JSON message", err)
	}
}
