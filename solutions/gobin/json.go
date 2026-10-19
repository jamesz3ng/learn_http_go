package gobin

import (
	"encoding/json"
	"errors"
	"net/http"
)

// writeJSON is the one place responses get encoded. Note the order: headers
// first, then the status line (WriteHeader), then the body. Once WriteHeader
// has been called, header changes are ignored.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	// An error here means the client went away mid-response. The status line
	// is already sent, so there is nothing useful left to do.
	_ = enc.Encode(v)
}

// writeError sends {"error": msg} with the given status.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// writeBodyError turns an error from reading a request body into a response:
// 413 if http.MaxBytesReader cut the body off, 400 otherwise.
func writeBodyError(w http.ResponseWriter, err error) {
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
		return
	}
	writeError(w, http.StatusBadRequest, "reading request body: "+err.Error())
}
