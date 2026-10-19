package gocurl

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const defaultUserAgent = "gocurl/1.0"

type dataKind int

const (
	dataASCII     dataKind = iota // -d: @file has CR/LF stripped
	dataBinary                    // --data-binary: @file sent as-is
	dataURLEncode                 // --data-urlencode
	dataJSON                      // --json
)

type dataArg struct {
	kind  dataKind
	value string
}

// buildRequest turns options into an *http.Request.
func buildRequest(ctx context.Context, o *options, stdin io.Reader) (*http.Request, error) {
	u, err := parseURL(o.url)
	if err != nil {
		return nil, err
	}

	// Stage 7: request bodies.
	var (
		body        io.Reader
		contentType string
		isJSON      bool
	)
	if len(o.data) > 0 {
		data, j, err := buildData(o.data, stdin)
		if err != nil {
			return nil, err
		}
		isJSON = j
		if o.get {
			// -G: the data goes into the query string instead.
			if u.RawQuery != "" {
				u.RawQuery += "&"
			}
			u.RawQuery += string(data)
		} else {
			// bytes.Reader lets NewRequest set ContentLength and GetBody,
			// so the body has a Content-Length (no chunked encoding) and
			// can be replayed on 307/308 redirects.
			body = bytes.NewReader(data)
			contentType = "application/x-www-form-urlencoded"
			if isJSON {
				contentType = "application/json"
			}
		}
	}

	// Stage 22: multipart forms.
	if len(o.forms) > 0 {
		body, contentType, err = buildForm(o.forms)
		if err != nil {
			return nil, err
		}
	}

	req, err := http.NewRequestWithContext(ctx, chooseMethod(o, body != nil), u.String(), body)
	if err != nil {
		return nil, &curlError{exitURLMalformed, err.Error()}
	}

	// Defaults first, so that -H can override or remove them.
	req.Header.Set("User-Agent", defaultUserAgent)
	if o.userAgent != "" {
		req.Header.Set("User-Agent", o.userAgent)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if isJSON {
		req.Header.Set("Accept", "application/json")
	}

	// Stage 14: credentials and cookies.
	if o.user != "" {
		user, pass, _ := strings.Cut(o.user, ":")
		req.SetBasicAuth(user, pass)
	}
	if o.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+o.bearer)
	}
	if o.cookie != "" {
		req.Header.Set("Cookie", o.cookie)
	}

	// Stage 22: byte ranges.
	if o.rangeSpec != "" {
		req.Header.Set("Range", "bytes="+o.rangeSpec)
	}

	// Stage 5: -H. The first -H for a name replaces any default; repeats add.
	seen := map[string]bool{}
	for _, h := range o.headers {
		name, value, ok := strings.Cut(h, ":")
		if !ok {
			return nil, &curlError{exitUsage, fmt.Sprintf("invalid header %q (want \"Name: value\")", h)}
		}
		name = textproto.CanonicalMIMEHeaderKey(strings.TrimSpace(name))
		value = strings.TrimSpace(value)

		if name == "Host" {
			// The Host header is special: Go sends req.Host and ignores
			// req.Header["Host"].
			req.Host = value
			continue
		}
		if value == "" {
			if name == "User-Agent" {
				// An empty (but present) User-Agent stops Go adding its own
				// "Go-http-client/1.1".
				req.Header.Set("User-Agent", "")
			} else {
				req.Header.Del(name)
			}
			continue
		}
		if seen[name] {
			req.Header.Add(name, value)
		} else {
			req.Header.Set(name, value)
			seen[name] = true
		}
	}
	return req, nil
}

// parseURL validates the URL like curl does, adding http:// if there's no scheme.
func parseURL(raw string) (*url.URL, error) {
	// Without this, url.Parse("localhost:8080/x") "succeeds" with the
	// scheme "localhost".
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, &curlError{exitURLMalformed, "URL rejected: " + err.Error()}
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, &curlError{exitUnsupportedProtocol, fmt.Sprintf("Protocol %q not supported", u.Scheme)}
	}
	if u.Host == "" {
		return nil, &curlError{exitURLMalformed, "URL rejected: no host"}
	}
	return u, nil
}

func chooseMethod(o *options, hasBody bool) string {
	switch {
	case o.method != "":
		return o.method
	case o.head:
		return http.MethodHead
	case o.get:
		return http.MethodGet
	case hasBody:
		return http.MethodPost
	default:
		return http.MethodGet
	}
}

// buildData joins all -d style arguments, in order.
func buildData(args []dataArg, stdin io.Reader) (data []byte, isJSON bool, err error) {
	var buf bytes.Buffer
	for i, a := range args {
		piece, err := dataPiece(a, stdin)
		if err != nil {
			return nil, false, err
		}
		// curl joins data pieces with '&', except consecutive --json pieces,
		// which are simply concatenated.
		if i > 0 && !(a.kind == dataJSON && args[i-1].kind == dataJSON) {
			buf.WriteByte('&')
		}
		buf.WriteString(piece)
		if a.kind == dataJSON {
			isJSON = true
		}
	}
	return buf.Bytes(), isJSON, nil
}

func dataPiece(a dataArg, stdin io.Reader) (string, error) {
	switch a.kind {
	case dataASCII:
		if name, ok := strings.CutPrefix(a.value, "@"); ok {
			b, err := readSource(name, stdin)
			if err != nil {
				return "", err
			}
			// -d @file strips carriage returns and newlines, like curl.
			return strings.NewReplacer("\r", "", "\n", "").Replace(string(b)), nil
		}
		return a.value, nil

	case dataBinary, dataJSON:
		if name, ok := strings.CutPrefix(a.value, "@"); ok {
			b, err := readSource(name, stdin)
			return string(b), err
		}
		return a.value, nil

	case dataURLEncode:
		// Forms: "content", "=content", "name=content", "@file", "name@file".
		if name, content, ok := strings.Cut(a.value, "="); ok {
			if name == "" {
				return url.QueryEscape(content), nil
			}
			return name + "=" + url.QueryEscape(content), nil
		}
		if name, file, ok := strings.Cut(a.value, "@"); ok {
			b, err := readSource(file, stdin)
			if err != nil {
				return "", err
			}
			if name == "" {
				return url.QueryEscape(string(b)), nil
			}
			return name + "=" + url.QueryEscape(string(b)), nil
		}
		return url.QueryEscape(a.value), nil
	}
	return "", fmt.Errorf("unknown data kind %d", a.kind)
}

// readSource reads a file, or stdin for "-".
func readSource(name string, stdin io.Reader) ([]byte, error) {
	var (
		b   []byte
		err error
	)
	if name == "-" {
		b, err = io.ReadAll(stdin)
	} else {
		b, err = os.ReadFile(name)
	}
	if err != nil {
		return nil, &curlError{exitReadError, "Failed to read " + name + ": " + err.Error()}
	}
	return b, nil
}

// buildForm streams a multipart/form-data body through an io.Pipe: a
// goroutine writes parts into one end while the HTTP client reads the other,
// so files are never loaded into memory.
func buildForm(fields []string) (io.Reader, string, error) {
	type field struct{ name, value, file string }
	var parsed []field
	for _, f := range fields {
		name, value, ok := strings.Cut(f, "=")
		if !ok || name == "" {
			return nil, "", &curlError{exitUsage, fmt.Sprintf("invalid -F %q (want name=value or name=@file)", f)}
		}
		if file, isFile := strings.CutPrefix(value, "@"); isFile {
			// Fail early (exit 26) rather than halfway through the upload.
			if _, err := os.Stat(file); err != nil {
				return nil, "", &curlError{exitReadError, "Failed to open " + file + ": " + err.Error()}
			}
			parsed = append(parsed, field{name: name, file: file})
			continue
		}
		parsed = append(parsed, field{name: name, value: value})
	}

	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		err := func() error {
			for _, f := range parsed {
				if f.file == "" {
					if err := mw.WriteField(f.name, f.value); err != nil {
						return err
					}
					continue
				}
				if err := writeFilePart(mw, f.name, f.file); err != nil {
					return err
				}
			}
			return mw.Close()
		}()
		// Closing with an error makes the client's read of the body fail,
		// which aborts the request instead of sending a truncated form.
		pw.CloseWithError(err)
	}()
	return pr, mw.FormDataContentType(), nil
}

var quoteEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`)

func writeFilePart(mw *multipart.Writer, name, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	// multipart.Writer.CreateFormFile always says application/octet-stream,
	// so build the part header ourselves to set a better Content-Type.
	ct := mime.TypeByExtension(filepath.Ext(path))
	if ct == "" {
		ct = "application/octet-stream"
	}
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`,
		quoteEscaper.Replace(name), quoteEscaper.Replace(filepath.Base(path))))
	h.Set("Content-Type", ct)
	w, err := mw.CreatePart(h)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, f)
	return err
}
