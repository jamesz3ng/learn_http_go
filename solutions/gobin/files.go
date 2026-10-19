package gobin

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
)

// Stage 21 --------------------------------------------------------------------

// noListingFS hides directories that have no index.html, so http.FileServer
// returns 404 instead of an automatic directory listing.
type noListingFS struct{ fs.FS }

func (n noListingFS) Open(name string) (fs.File, error) {
	f, err := n.FS.Open(name)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if info.IsDir() {
		index, err := n.FS.Open(path.Join(name, "index.html"))
		if err != nil {
			f.Close()
			return nil, fs.ErrNotExist
		}
		index.Close()
	}
	return f, nil
}

const maxUploadBytes = 10 << 20 // 10 MiB

type uploadedFile struct {
	Field       string `json:"field"`
	Filename    string `json:"filename"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
	ContentType string `json:"content_type"`
}

// upload reads a multipart body part by part. Unlike ParseMultipartForm it
// never holds a whole file in memory or on disk: each part is hashed as it
// streams past.
func (a *app) upload(w http.ResponseWriter, r *http.Request) {
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mediaType != "multipart/form-data" {
		writeError(w, http.StatusUnsupportedMediaType, "expected multipart/form-data")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	mr, err := r.MultipartReader()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	files := []uploadedFile{}
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			writeBodyError(w, err)
			return
		}
		if part.FileName() == "" {
			part.Close() // an ordinary form field; skip it
			continue
		}

		// Keep the first 512 bytes for content sniffing, hash everything.
		head := make([]byte, 512)
		n, err := io.ReadFull(part, head)
		if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
			writeBodyError(w, err)
			return
		}
		head = head[:n]
		h := sha256.New()
		h.Write(head)
		rest, err := io.Copy(h, part)
		if err != nil {
			writeBodyError(w, err)
			return
		}
		files = append(files, uploadedFile{
			Field:       part.FormName(),
			Filename:    part.FileName(),
			Size:        int64(n) + rest,
			SHA256:      hex.EncodeToString(h.Sum(nil)),
			ContentType: http.DetectContentType(head),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": files})
}
