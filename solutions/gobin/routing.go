package gobin

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// Stage 8 ---------------------------------------------------------------------

func (a *app) status(w http.ResponseWriter, r *http.Request) {
	code, err := strconv.Atoi(r.PathValue("code"))
	if err != nil || code < 200 || code > 599 {
		writeError(w, http.StatusBadRequest, "status code must be an integer between 200 and 599")
		return
	}
	w.WriteHeader(code)
}

// teapot is registered as "GET /status/teapot". It wins over
// "GET /status/{code}" because a literal segment is more specific than a
// wildcard — registration order doesn't matter.
func (a *app) teapot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusTeapot)
	io.WriteString(w, "I'm a teapot\n")
}

const maxBytes = 100 * 1024

func (a *app) bytes(w http.ResponseWriter, r *http.Request) {
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil || n < 0 || n > maxBytes {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("n must be an integer between 0 and %d", maxBytes))
		return
	}
	b := make([]byte, n)
	rand.Read(b)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.Itoa(n))
	w.Write(b)
}

func (a *app) base64(w http.ResponseWriter, r *http.Request) {
	// PathValue is already percent-decoded. Accept padded or unpadded input.
	value := strings.TrimRight(r.PathValue("value"), "=")
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid URL-safe base64: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write(decoded)
}

func (a *app) uuid(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"uuid": newUUID()})
}

// newUUID returns a random (version 4) UUID.
func newUUID() string {
	var b [16]byte
	rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40 // version 4
	b[8] = b[8]&0x3f | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
