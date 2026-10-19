package gobin

import (
	"encoding/json"
	"io"
	"mime"
	"net/http"
)

// Stage 6 ---------------------------------------------------------------------

const maxBodyBytes = 1 << 20 // 1 MiB

// bodyInfo extends requestInfo with what we found in the body. Embedding a
// struct flattens its fields into the JSON object.
type bodyInfo struct {
	requestInfo
	Data  string              `json:"data"`
	Form  map[string][]string `json:"form"`
	Files map[string][]string `json:"files"`
	JSON  any                 `json:"json"`
}

func (a *app) anything(w http.ResponseWriter, r *http.Request) {
	info := bodyInfo{
		requestInfo: describe(r),
		Form:        map[string][]string{},
		Files:       map[string][]string{},
	}

	// Never trust a client to send a reasonable amount of data.
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)

	// mime.ParseMediaType copes with parameters like "; charset=utf-8".
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))

	switch mediaType {
	case "application/x-www-form-urlencoded":
		if err := r.ParseForm(); err != nil {
			writeBodyError(w, err)
			return
		}
		// r.PostForm holds only body values; r.Form also mixes in the query.
		info.Form = r.PostForm

	case "multipart/form-data":
		if err := r.ParseMultipartForm(maxBodyBytes); err != nil {
			writeBodyError(w, err)
			return
		}
		defer r.MultipartForm.RemoveAll() // delete any temp files
		info.Form = r.MultipartForm.Value
		for field, headers := range r.MultipartForm.File {
			for _, fh := range headers {
				info.Files[field] = append(info.Files[field], fh.Filename)
			}
		}

	default:
		b, err := io.ReadAll(r.Body)
		if err != nil {
			writeBodyError(w, err)
			return
		}
		info.Data = string(b)
		if mediaType == "application/json" {
			if err := json.Unmarshal(b, &info.JSON); err != nil {
				writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
				return
			}
		}
	}

	writeJSON(w, http.StatusOK, info)
}
