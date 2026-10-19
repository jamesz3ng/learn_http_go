package tests

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var content = []byte(strings.Repeat("0123456789", 10)) // 100 bytes

// fileServer serves `content` at /file with http.ServeContent (which supports
// Range requests) and ignores Range at /plain.
func fileServer(t *testing.T) *recordingServer {
	return newRecordingServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/file":
			http.ServeContent(w, r, "data.bin", time.Time{}, bytes.NewReader(content))
		case "/plain":
			w.Write(content)
		}
	})
}

func TestStage22_Output(t *testing.T) {
	srv := fileServer(t)
	out := filepath.Join(t.TempDir(), "out.bin")
	res := curl(t, "-o", out, srv.URL+"/file")
	res.wantCode(t, 0, "gocurl -o")
	if res.Stdout != "" {
		t.Fatalf("with -o, stdout should be empty, got %d bytes", len(res.Stdout))
	}
	if got, _ := os.ReadFile(out); !bytes.Equal(got, content) {
		t.Fatalf("output file has %q, want the body", got)
	}
	// -o truncates an existing, longer file.
	os.WriteFile(out, bytes.Repeat([]byte("Z"), 500), 0o644)
	curl(t, "--output", out, srv.URL+"/file").wantCode(t, 0, "gocurl --output over an existing file")
	if got, _ := os.ReadFile(out); !bytes.Equal(got, content) {
		t.Fatalf("output file wasn't truncated: %d bytes", len(got))
	}
	curl(t, "-o", filepath.Join(t.TempDir(), "no", "such", "dir", "f"), srv.URL+"/file").
		wantCode(t, 23, "gocurl -o into a directory that doesn't exist")
}

func TestStage22_Range(t *testing.T) {
	srv := fileServer(t)
	res := curl(t, "-r", "10-14", srv.URL+"/file")
	res.wantCode(t, 0, "gocurl -r 10-14")
	if got := srv.last(t).Header.Get("Range"); got != "bytes=10-14" {
		t.Fatalf("Range header = %q, want bytes=10-14", got)
	}
	if res.Stdout != "01234" {
		t.Fatalf("stdout = %q, want %q", res.Stdout, "01234")
	}
}

func TestStage22_Resume(t *testing.T) {
	srv := fileServer(t)
	out := filepath.Join(t.TempDir(), "partial.bin")
	os.WriteFile(out, content[:40], 0o644)

	curl(t, "-C", "-", "-o", out, srv.URL+"/file").wantCode(t, 0, "gocurl -C - -o partial")
	if got := srv.last(t).Header.Get("Range"); got != "bytes=40-" {
		t.Fatalf("Range header = %q, want bytes=40- (the size of the partial file)", got)
	}
	if got, _ := os.ReadFile(out); !bytes.Equal(got, content) {
		t.Fatalf("after resuming, the file is %d bytes %q, want the full 100", len(got), got)
	}
}

func TestStage22_ResumeWhenServerIgnoresRange(t *testing.T) {
	srv := fileServer(t)
	out := filepath.Join(t.TempDir(), "partial.bin")
	os.WriteFile(out, content[:40], 0o644)
	curl(t, "-C", "-", "-o", out, srv.URL+"/plain").wantCode(t, 0, "gocurl -C - with a server that ignores Range")
	if got, _ := os.ReadFile(out); !bytes.Equal(got, content) {
		t.Fatalf("the server answered 200 with the whole body, so the file must be overwritten, not appended to; got %d bytes", len(got))
	}
}

func TestStage22_ResumeWhenComplete(t *testing.T) {
	srv := fileServer(t)
	out := filepath.Join(t.TempDir(), "done.bin")
	os.WriteFile(out, content, 0o644)
	curl(t, "-C", "-", "-o", out, srv.URL+"/file").wantCode(t, 0, "gocurl -C - on an already complete file (server says 416)")
	if got, _ := os.ReadFile(out); !bytes.Equal(got, content) {
		t.Fatalf("a complete file must be left alone, but it changed to %d bytes", len(got))
	}
}

func TestStage22_ResumeFresh(t *testing.T) {
	srv := fileServer(t)
	out := filepath.Join(t.TempDir(), "new.bin")
	curl(t, "-C", "-", "-o", out, srv.URL+"/file").wantCode(t, 0, "gocurl -C - with no existing file")
	if got := srv.last(t).Header.Get("Range"); got != "" {
		t.Fatalf("with nothing downloaded yet there should be no Range header, got %q", got)
	}
	if got, _ := os.ReadFile(out); !bytes.Equal(got, content) {
		t.Fatalf("file = %d bytes", len(got))
	}
	curl(t, "-C", "-", srv.URL+"/file").wantCode(t, 2, "gocurl -C - without -o")
}

func TestStage22_Form(t *testing.T) {
	type part struct{ name, filename, ctype, body string }
	got := make(chan []part, 1)
	srv := newRecordingServer(t, func(w http.ResponseWriter, r *http.Request) {
		mr, err := r.MultipartReader()
		if err != nil {
			http.Error(w, err.Error(), 400)
			got <- nil
			return
		}
		var parts []part
		for {
			p, err := mr.NextPart()
			if err != nil {
				break
			}
			b, _ := io.ReadAll(p)
			parts = append(parts, part{p.FormName(), p.FileName(), p.Header.Get("Content-Type"), string(b)})
		}
		got <- parts
	})
	dir := t.TempDir()
	png := filepath.Join(dir, "pic.png")
	os.WriteFile(png, []byte("\x89PNG fake"), 0o644)
	odd := filepath.Join(dir, "data.xyz123")
	os.WriteFile(odd, []byte("raw"), 0o644)

	res := curl(t, "-F", "title=hello world", "-F", "image=@"+png, "--form", "blob=@"+odd, srv.URL+"/up")
	res.wantCode(t, 0, "gocurl -F")
	parts := <-got
	r := srv.last(t)
	if r.Method != "POST" || !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data; boundary=") {
		t.Fatalf("-F should POST multipart/form-data, got %s with Content-Type %q", r.Method, r.Header.Get("Content-Type"))
	}
	want := []part{
		{"title", "", "", "hello world"},
		{"image", "pic.png", "image/png", "\x89PNG fake"},
		{"blob", "data.xyz123", "application/octet-stream", "raw"},
	}
	if len(parts) != len(want) {
		t.Fatalf("server received parts %+v, want %+v", parts, want)
	}
	for i := range want {
		if parts[i] != want[i] {
			t.Fatalf("part %d = %+v, want %+v%s", i, parts[i], want[i],
				hint("CreateFormFile always uses application/octet-stream; use CreatePart with your own header and mime.TypeByExtension"))
		}
	}

	curl(t, "-F", "f=@"+filepath.Join(dir, "missing"), srv.URL).wantCode(t, 26, "gocurl -F with a missing file")
}
