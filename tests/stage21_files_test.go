package tests

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jamesz3ng/learn_http_go/gobin"
)

var modTime = time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)

func staticFS() fstest.MapFS {
	return fstest.MapFS{
		"hello.txt":       {Data: []byte("hello world"), ModTime: modTime},
		"site/index.html": {Data: []byte("<h1>home</h1>"), ModTime: modTime},
		"docs/a.txt":      {Data: []byte("a"), ModTime: modTime},
	}
}

func TestStage21_ServesStaticFiles(t *testing.T) {
	srv := startGobin(t, gobin.Config{Static: staticFS()})
	c := newClient(t)
	resp, body := get(t, c, srv.URL+"/static/hello.txt")
	wantStatus(t, resp, 200, "GET /static/hello.txt",
		`mux.Handle("GET /static/", http.StripPrefix("/static", http.FileServerFS(cfg.Static)))`)
	if body != "hello world" {
		t.Fatalf("body = %q", body)
	}
	wantContentType(t, resp, "text/plain", "GET /static/hello.txt")
	if lm := resp.Header.Get("Last-Modified"); lm != modTime.Format(http.TimeFormat) {
		t.Fatalf("Last-Modified = %q, want %q", lm, modTime.Format(http.TimeFormat))
	}

	resp, body = get(t, c, srv.URL+"/static/site/")
	wantStatus(t, resp, 200, "GET /static/site/ (has index.html)")
	if body != "<h1>home</h1>" {
		t.Fatalf("GET /static/site/ = %q, want the index.html contents", body)
	}

	resp, _ = get(t, c, srv.URL+"/static/missing.txt")
	wantStatus(t, resp, 404, "GET /static/missing.txt")
}

func TestStage21_NoDirectoryListings(t *testing.T) {
	srv := startGobin(t, gobin.Config{Static: staticFS()})
	resp, body := get(t, newClient(t), srv.URL+"/static/docs/")
	wantStatus(t, resp, 404, "GET /static/docs/ (a directory without index.html)",
		"FileServer lists directory contents by default. Wrap the fs.FS so opening a directory without index.html fails with fs.ErrNotExist")
	if strings.Contains(body, "a.txt") {
		t.Fatalf("the response lists the directory's files: %q", body)
	}
}

func TestStage21_NoStaticConfigured(t *testing.T) {
	srv := startGobin(t, gobin.Config{})
	resp, _ := get(t, newClient(t), srv.URL+"/static/hello.txt")
	wantStatus(t, resp, 404, "GET /static/hello.txt with Config.Static == nil")
}

func TestStage21_RangeAndConditional(t *testing.T) {
	srv := startGobin(t, gobin.Config{Static: staticFS()})
	c := newClient(t)
	resp, body := get(t, c, srv.URL+"/static/hello.txt", "Range", "bytes=0-4")
	wantStatus(t, resp, 206, "GET /static/hello.txt with Range: bytes=0-4")
	if body != "hello" || resp.Header.Get("Content-Range") != "bytes 0-4/11" {
		t.Fatalf("range response = %q with Content-Range %q", body, resp.Header.Get("Content-Range"))
	}
	resp, body = get(t, c, srv.URL+"/static/hello.txt", "If-Modified-Since", modTime.Format(http.TimeFormat))
	wantStatus(t, resp, 304, "GET /static/hello.txt with If-Modified-Since")
	if body != "" {
		t.Fatalf("304 responses have no body, got %q", body)
	}
}

func TestStage21_KVConditionalGet(t *testing.T) {
	srv := startGobin(t, gobin.Config{})
	c := newClient(t)
	do(t, c, http.MethodPut, srv.URL+"/kv/doc", strings.NewReader("version one"))

	resp, _ := get(t, c, srv.URL+"/kv/doc")
	etag := resp.Header.Get("ETag")
	if len(etag) < 3 || etag[0] != '"' || etag[len(etag)-1] != '"' {
		t.Fatalf("GET /kv/doc ETag = %q, want a quoted strong validator like \"abc123\"", etag)
	}
	resp, body := get(t, c, srv.URL+"/kv/doc", "If-None-Match", etag)
	wantStatus(t, resp, 304, "GET /kv/doc with a matching If-None-Match",
		"set the ETag header, then let http.ServeContent handle the conditional request")
	if body != "" {
		t.Fatalf("304 with body %q", body)
	}

	do(t, c, http.MethodPut, srv.URL+"/kv/doc", strings.NewReader("version two"))
	resp, body = get(t, c, srv.URL+"/kv/doc", "If-None-Match", etag)
	wantStatus(t, resp, 200, "GET /kv/doc with a stale If-None-Match")
	if body != "version two" || resp.Header.Get("ETag") == etag {
		t.Fatalf("after a change: body %q, ETag %q (must differ from %q)", body, resp.Header.Get("ETag"), etag)
	}

	resp, body = get(t, c, srv.URL+"/kv/doc", "Range", "bytes=8-")
	wantStatus(t, resp, 206, "GET /kv/doc with Range: bytes=8-")
	if body != "two" {
		t.Fatalf("range body = %q, want %q", body, "two")
	}
}

func uploadBody(t *testing.T, files map[string][]byte) (*bytes.Buffer, string) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	mw.WriteField("note", "not a file")
	for name, data := range files {
		fw, _ := mw.CreateFormFile("file", name)
		fw.Write(data)
	}
	mw.Close()
	return &buf, mw.FormDataContentType()
}

func TestStage21_Upload(t *testing.T) {
	srv := startGobin(t, gobin.Config{})
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 600)...)
	text := []byte("plain old text\n")
	body, ct := uploadBody(t, map[string][]byte{"pic.png": png, "notes.txt": text})

	resp, out := do(t, newClient(t), http.MethodPost, srv.URL+"/upload", body, "Content-Type", ct)
	wantStatus(t, resp, 200, "POST /upload")
	files, ok := decodeObject(t, out)["files"].([]any)
	if !ok || len(files) != 2 {
		t.Fatalf(`want {"files": [two entries]} (form fields that aren't files are skipped), got %s`, out)
	}
	byName := map[string]map[string]any{}
	for _, f := range files {
		m := object(t, f, "file entry")
		byName[m["filename"].(string)] = m
	}
	check := func(name string, data []byte, wantType string) {
		m := byName[name]
		sum := sha256.Sum256(data)
		if m == nil || m["field"] != "file" || m["size"] != float64(len(data)) ||
			m["sha256"] != hex.EncodeToString(sum[:]) || m["content_type"] != wantType {
			t.Fatalf("entry for %s = %v, want field \"file\", size %d, sha256 %x, content_type %q%s",
				name, m, len(data), sum, wantType, hint("http.DetectContentType sniffs the first 512 bytes"))
		}
	}
	check("pic.png", png, "image/png")
	check("notes.txt", text, "text/plain; charset=utf-8")
}

func TestStage21_UploadLimits(t *testing.T) {
	srv := startGobin(t, gobin.Config{})
	c := newClient(t)
	resp, _ := do(t, c, http.MethodPost, srv.URL+"/upload", strings.NewReader(`{"a":1}`), "Content-Type", "application/json")
	wantStatus(t, resp, 415, "POST /upload with a JSON body", "415 Unsupported Media Type")

	body, ct := uploadBody(t, map[string][]byte{"huge.bin": bytes.Repeat([]byte("x"), 10<<20+1)})
	resp, _ = do(t, c, http.MethodPost, srv.URL+"/upload", body, "Content-Type", ct)
	wantStatus(t, resp, 413, "POST /upload with more than 10 MiB")
}
