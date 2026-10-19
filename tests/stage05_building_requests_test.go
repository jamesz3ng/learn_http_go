package tests

import (
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestStage05_Method(t *testing.T) {
	srv := newRecordingServer(t, nil)
	for _, args := range [][]string{{"-X", "DELETE"}, {"--request", "PATCH"}} {
		curl(t, args[0], args[1], srv.URL+"/thing").wantCode(t, 0, "gocurl "+strings.Join(args, " "))
		if got := srv.last(t).Method; got != args[1] {
			t.Fatalf("with %s %s the server saw method %q", args[0], args[1], got)
		}
	}
}

func TestStage05_Headers(t *testing.T) {
	srv := newRecordingServer(t, nil)
	curl(t,
		"-H", "X-One: 1",
		"--header", "x-two:   two  ",
		"-H", "X-Rep: a",
		"-H", "X-Rep: b",
		srv.URL,
	).wantCode(t, 0, "gocurl with -H")
	h := srv.last(t).Header
	if h.Get("X-One") != "1" {
		t.Fatalf("X-One = %q, want %q", h.Get("X-One"), "1")
	}
	if h.Get("X-Two") != "two" {
		t.Fatalf("X-Two = %q, want %q (trim surrounding spaces)", h.Get("X-Two"), "two")
	}
	if got := h.Values("X-Rep"); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("X-Rep values = %q, want [a b]%s", got,
			hint("Header.Set replaces existing values; Header.Add appends"))
	}
}

func TestStage05_UserAgent(t *testing.T) {
	srv := newRecordingServer(t, nil)

	curl(t, srv.URL).wantCode(t, 0, "gocurl")
	if ua := srv.last(t).Header.Get("User-Agent"); ua != "gocurl/1.0" {
		t.Fatalf("default User-Agent = %q, want %q", ua, "gocurl/1.0")
	}

	curl(t, "-A", "agent/007", srv.URL).wantCode(t, 0, "gocurl -A")
	if ua := srv.last(t).Header.Get("User-Agent"); ua != "agent/007" {
		t.Fatalf("with -A, User-Agent = %q, want %q", ua, "agent/007")
	}

	curl(t, "-H", "User-Agent: via-header", srv.URL).wantCode(t, 0, "gocurl -H User-Agent")
	if ua := srv.last(t).Header.Get("User-Agent"); ua != "via-header" {
		t.Fatalf("with -H 'User-Agent: via-header', User-Agent = %q", ua)
	}
}

func TestStage05_RemoveHeaderWithEmptyValue(t *testing.T) {
	srv := newRecordingServer(t, nil)
	curl(t, "-H", "User-Agent:", srv.URL).wantCode(t, 0, "gocurl -H 'User-Agent:'")
	if vals, ok := srv.last(t).Header["User-Agent"]; ok {
		t.Fatalf("-H 'User-Agent:' should send no User-Agent at all, server saw %q%s", vals,
			hint("if req.Header has no User-Agent, Go adds its own default; set it to the empty string to send none (see the comment in (*Request).write in net/http/request.go)"))
	}
}

func TestStage05_HostHeader(t *testing.T) {
	srv := newRecordingServer(t, nil)
	curl(t, "-H", "Host: example.test", srv.URL).wantCode(t, 0, "gocurl -H 'Host: ...'")
	if got := srv.last(t).Host; got != "example.test" {
		t.Fatalf("server saw Host %q, want %q%s", got, "example.test",
			hint("Go ignores req.Header[\"Host\"] when sending; set req.Host"))
	}
}

func TestStage05_Include(t *testing.T) {
	srv := newRecordingServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Zebra", "z")
		w.Header().Add("X-Apple", "a1")
		w.Header().Add("X-Apple", "a2")
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusAccepted)
		io.WriteString(w, "the body")
	})
	for _, flag := range []string{"-i", "--include"} {
		res := curl(t, flag, srv.URL)
		res.wantCode(t, 0, "gocurl "+flag)
		out := strings.ReplaceAll(res.Stdout, "\r\n", "\n")
		head, body, ok := strings.Cut(out, "\n\n")
		if !ok {
			t.Fatalf("with %s, expected headers, a blank line, then the body; got %q", flag, res.Stdout)
		}
		if body != "the body" {
			t.Fatalf("body after the blank line = %q, want %q", body, "the body")
		}
		lines := strings.Split(head, "\n")
		if lines[0] != "HTTP/1.1 202 Accepted" {
			t.Fatalf("first line = %q, want %q%s", lines[0], "HTTP/1.1 202 Accepted",
				hint("print resp.Proto, a space, then resp.Status"))
		}
		// Headers sorted by name; repeated headers on separate lines.
		want := []string{"X-Apple: a1", "X-Apple: a2", "X-Zebra: z"}
		var got []string
		for _, l := range lines[1:] {
			if strings.HasPrefix(l, "X-") {
				got = append(got, l)
			}
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("X- header lines = %q, want %q (sorted by name, one line per value)", got, want)
		}
		if !strings.Contains(head, "\nContent-Type: text/plain") {
			t.Fatalf("headers should include Content-Type: text/plain, got:\n%s", head)
		}
	}
}

func TestStage05_Head(t *testing.T) {
	srv := newRecordingServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Seen", "yes")
		io.WriteString(w, "body that HEAD must not print")
	})
	res := curl(t, "-I", srv.URL)
	res.wantCode(t, 0, "gocurl -I")
	if m := srv.last(t).Method; m != "HEAD" {
		t.Fatalf("-I should send a HEAD request, server saw %s", m)
	}
	out := strings.ReplaceAll(res.Stdout, "\r\n", "\n")
	if !strings.HasPrefix(out, "HTTP/1.1 200 OK\n") || !strings.Contains(out, "X-Seen: yes\n") {
		t.Fatalf("-I should print the status line and headers, got %q", res.Stdout)
	}
	if strings.Contains(out, "must not print") {
		t.Fatalf("-I printed a body: %q", res.Stdout)
	}
}

func TestStage05_FlagsAfterURL(t *testing.T) {
	srv := newRecordingServer(t, nil)
	res := curl(t, srv.URL, "-X", "PUT")
	res.wantCode(t, 0, "gocurl URL -X PUT",
		"the flag package stops parsing at the first non-flag argument; parse the remaining arguments again")
	if m := srv.last(t).Method; m != "PUT" {
		t.Fatalf("gocurl URL -X PUT sent %s", m)
	}
}
