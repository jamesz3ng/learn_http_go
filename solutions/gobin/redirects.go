package gobin

import (
	"net/http"
	"strconv"
)

// Stage 11 --------------------------------------------------------------------

const maxRedirects = 50

// redirectCount parses the {n} wildcard, writing a 400 if it is invalid.
func redirectCount(w http.ResponseWriter, r *http.Request) (int, bool) {
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil || n < 1 || n > maxRedirects {
		writeError(w, http.StatusBadRequest, "n must be an integer between 1 and 50")
		return 0, false
	}
	return n, true
}

// redirect sends /redirect/n -> /redirect/n-1 -> ... -> /get.
func (a *app) redirect(w http.ResponseWriter, r *http.Request) {
	n, ok := redirectCount(w, r)
	if !ok {
		return
	}
	target := "/get"
	if n > 1 {
		target = "/redirect/" + strconv.Itoa(n-1)
	}
	// http.Redirect sets Location, writes the status and, for GET/HEAD, a
	// tiny HTML body for ancient clients that don't follow redirects.
	http.Redirect(w, r, target, http.StatusFound)
}

// absoluteRedirect is like redirect but uses absolute URLs in Location.
func (a *app) absoluteRedirect(w http.ResponseWriter, r *http.Request) {
	n, ok := redirectCount(w, r)
	if !ok {
		return
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	base := scheme + "://" + r.Host
	target := base + "/get"
	if n > 1 {
		target = base + "/absolute-redirect/" + strconv.Itoa(n-1)
	}
	http.Redirect(w, r, target, http.StatusFound)
}

// redirectTo redirects to ?url= with ?status_code= (default 302). It accepts
// every method so clients can observe how each code treats POST bodies.
//
// In a real application this would be an "open redirect" vulnerability:
// attackers can craft links on your domain that bounce to theirs.
func (a *app) redirectTo(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	target := q.Get("url")
	if target == "" {
		writeError(w, http.StatusBadRequest, "missing url parameter")
		return
	}
	code := http.StatusFound
	if s := q.Get("status_code"); s != "" {
		var err error
		code, err = strconv.Atoi(s)
		if err != nil {
			code = 0
		}
	}
	switch code {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther,
		http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
	default:
		writeError(w, http.StatusBadRequest, "status_code must be one of 301, 302, 303, 307, 308")
		return
	}
	http.Redirect(w, r, target, code)
}
