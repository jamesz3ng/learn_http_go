package tests

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/jamesz3ng/learn_http_go/gobin"
)

func TestStage06_FormBody(t *testing.T) {
	srv := startGobin(t, gobin.Config{})
	form := url.Values{"name": {"gopher"}, "tag": {"a", "b"}}
	resp, body := do(t, newClient(t), http.MethodPost, srv.URL+"/post?q=1", strings.NewReader(form.Encode()),
		"Content-Type", "application/x-www-form-urlencoded")
	wantStatus(t, resp, 200, "POST /post with a form")
	wantContentType(t, resp, "application/json", "POST /post")
	m := decodeObject(t, body)

	f := object(t, m["form"], `"form"`)
	if got := stringSlice(t, f["tag"], `form["tag"]`); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf(`form["tag"] = %q, want ["a" "b"]`, got)
	}
	if got := stringSlice(t, f["name"], `form["name"]`); !reflect.DeepEqual(got, []string{"gopher"}) {
		t.Fatalf(`form["name"] = %q, want ["gopher"]`, got)
	}
	if _, ok := f["q"]; ok {
		t.Fatalf(`"form" must contain only body values, but it has the query parameter "q"%s`,
			hint("r.Form merges the query string and the body; r.PostForm has only the body"))
	}
	args := object(t, m["args"], `"args"`)
	if got := stringSlice(t, args["q"], `args["q"]`); !reflect.DeepEqual(got, []string{"1"}) {
		t.Fatalf(`args["q"] = %q, want ["1"] (the query string still belongs in "args")`, got)
	}
	if m["data"] != "" {
		t.Fatalf(`for form bodies "data" should be "", got %v`, m["data"])
	}
}

func TestStage06_JSONBody(t *testing.T) {
	srv := startGobin(t, gobin.Config{})
	raw := `{"name":"gopher","age":13,"tags":["go","http"]}`
	resp, body := do(t, newClient(t), http.MethodPut, srv.URL+"/put", strings.NewReader(raw),
		"Content-Type", "application/json; charset=utf-8")
	wantStatus(t, resp, 200, "PUT /put with JSON")
	m := decodeObject(t, body)
	j := object(t, m["json"], `"json"`)
	if j["name"] != "gopher" || j["age"] != float64(13) {
		t.Fatalf(`"json" = %v, want the decoded request body%s`, j,
			hint("parameters like \"; charset=utf-8\" mean you can't compare Content-Type with ==; use mime.ParseMediaType"))
	}
	if m["data"] != raw {
		t.Fatalf(`"data" = %v, want the raw body %q`, m["data"], raw)
	}
	if m["method"] != "PUT" {
		t.Fatalf(`"method" = %v, want PUT`, m["method"])
	}
}

func TestStage06_InvalidJSON(t *testing.T) {
	srv := startGobin(t, gobin.Config{})
	resp, body := do(t, newClient(t), http.MethodPost, srv.URL+"/post", strings.NewReader(`{"oops":`),
		"Content-Type", "application/json")
	wantStatus(t, resp, 400, "POST /post with invalid JSON")
	m := decodeObject(t, body)
	if s, _ := m["error"].(string); s == "" {
		t.Fatalf(`error responses should be JSON like {"error": "..."}, got %s`, body)
	}
}

func TestStage06_RawBody(t *testing.T) {
	srv := startGobin(t, gobin.Config{})
	resp, body := do(t, newClient(t), http.MethodPatch, srv.URL+"/patch", strings.NewReader("just some text"),
		"Content-Type", "text/plain")
	wantStatus(t, resp, 200, "PATCH /patch")
	m := decodeObject(t, body)
	if m["data"] != "just some text" {
		t.Fatalf(`"data" = %v, want %q`, m["data"], "just some text")
	}
	if m["json"] != nil {
		t.Fatalf(`"json" should be null for non-JSON bodies, got %v`, m["json"])
	}
}

func TestStage06_MultipartBody(t *testing.T) {
	srv := startGobin(t, gobin.Config{})
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	mw.WriteField("title", "hello")
	fw, _ := mw.CreateFormFile("upload", "notes.txt")
	fw.Write([]byte("file contents"))
	mw.Close()

	resp, body := do(t, newClient(t), http.MethodPost, srv.URL+"/post", &buf, "Content-Type", mw.FormDataContentType())
	wantStatus(t, resp, 200, "POST /post multipart")
	m := decodeObject(t, body)
	f := object(t, m["form"], `"form"`)
	if got := stringSlice(t, f["title"], `form["title"]`); !reflect.DeepEqual(got, []string{"hello"}) {
		t.Fatalf(`form["title"] = %q, want ["hello"]%s`, got, hint("r.ParseMultipartForm, then r.MultipartForm.Value"))
	}
	files := object(t, m["files"], `"files"`)
	if got := stringSlice(t, files["upload"], `files["upload"]`); !reflect.DeepEqual(got, []string{"notes.txt"}) {
		t.Fatalf(`files["upload"] = %q, want ["notes.txt"]`, got)
	}
}

func TestStage06_Anything(t *testing.T) {
	srv := startGobin(t, gobin.Config{})
	c := newClient(t)
	for _, method := range []string{"GET", "POST", "DELETE", "OPTIONS"} {
		resp, body := do(t, c, method, srv.URL+"/anything/deep/path?x=1", nil)
		wantStatus(t, resp, 200, method+" /anything/deep/path", `register "/anything/{path...}" without a method so it matches all of them`)
		m := decodeObject(t, body)
		if m["method"] != method {
			t.Fatalf(`"method" = %v, want %s`, m["method"], method)
		}
		if m["url"] != srv.URL+"/anything/deep/path?x=1" {
			t.Fatalf(`"url" = %v`, m["url"])
		}
	}
	resp, _ := do(t, c, "POST", srv.URL+"/anything", strings.NewReader("x"))
	wantStatus(t, resp, 200, "POST /anything")
}

func TestStage06_MethodSpecificRoutes(t *testing.T) {
	srv := startGobin(t, gobin.Config{})
	c := newClient(t)
	for _, tc := range []struct{ method, path string }{
		{"POST", "/post"}, {"PUT", "/put"}, {"PATCH", "/patch"}, {"DELETE", "/delete"},
	} {
		resp, _ := do(t, c, tc.method, srv.URL+tc.path, strings.NewReader("x"))
		wantStatus(t, resp, 200, tc.method+" "+tc.path)

		resp, _ = get(t, c, srv.URL+tc.path)
		wantStatus(t, resp, 405, "GET "+tc.path, `register "`+tc.method+` `+tc.path+`" and ServeMux returns 405 for other methods`)
		if allow := resp.Header.Get("Allow"); !strings.Contains(allow, tc.method) {
			t.Fatalf("GET %s: Allow header = %q, want it to contain %s", tc.path, allow, tc.method)
		}
	}
}

func TestStage06_BodyTooLarge(t *testing.T) {
	srv := startGobin(t, gobin.Config{})
	c := newClient(t)

	big := strings.Repeat("x", 1<<20+1) // 1 MiB + 1 byte
	resp, _ := do(t, c, http.MethodPost, srv.URL+"/post", strings.NewReader(big), "Content-Type", "text/plain")
	wantStatus(t, resp, 413, "POST /post with a 1 MiB + 1 byte body",
		"wrap r.Body with http.MaxBytesReader and check for *http.MaxBytesError with errors.As")

	form := "a=" + strings.Repeat("x", 1<<20)
	resp, _ = do(t, c, http.MethodPost, srv.URL+"/post", strings.NewReader(form), "Content-Type", "application/x-www-form-urlencoded")
	wantStatus(t, resp, 413, "POST /post with a form body over 1 MiB")

	exactly := strings.Repeat("x", 1<<20)
	resp, _ = do(t, c, http.MethodPost, srv.URL+"/post", strings.NewReader(exactly), "Content-Type", "text/plain")
	wantStatus(t, resp, 200, "POST /post with exactly 1 MiB")
}
