package gobin

import (
	"net"
	"net/http"
)

// Stage 4 ---------------------------------------------------------------------

// requestInfo is what /get (and, extended, /anything) reports about a request.
type requestInfo struct {
	Method  string              `json:"method"`
	Args    map[string][]string `json:"args"`
	Headers map[string][]string `json:"headers"`
	Origin  string              `json:"origin"`
	URL     string              `json:"url"`
}

func describe(r *http.Request) requestInfo {
	return requestInfo{
		Method:  r.Method,
		Args:    r.URL.Query(), // url.Values is a map[string][]string
		Headers: headersOf(r),
		Origin:  origin(r),
		URL:     fullURL(r),
	}
}

// headersOf returns the request headers including Host. The server removes
// Host from r.Header and puts it in r.Host instead, so add it back.
func headersOf(r *http.Request) map[string][]string {
	h := r.Header.Clone()
	if h == nil {
		h = http.Header{}
	}
	h["Host"] = []string{r.Host}
	return h
}

// origin is the client's IP address. RemoteAddr is "ip:port" (or "[ipv6]:port").
func origin(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// fullURL rebuilds the absolute URL the client asked for. On the server,
// r.URL usually holds only the path and query (it came from the request
// line), so the scheme and host must come from elsewhere.
func fullURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host + r.URL.RequestURI()
}

func (a *app) get(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, describe(r))
}

func (a *app) headers(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"headers": headersOf(r)})
}

func (a *app) ip(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"origin": origin(r)})
}

func (a *app) userAgent(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"user-agent": r.UserAgent()})
}
