package gobin

import (
	"crypto/subtle"
	"net/http"
	"strings"
	"time"
)

// Stage 13 --------------------------------------------------------------------

func (a *app) cookies(w http.ResponseWriter, r *http.Request) {
	m := map[string]string{}
	for _, c := range r.Cookies() {
		m[c.Name] = c.Value
	}
	writeJSON(w, http.StatusOK, map[string]any{"cookies": m})
}

// setCookies sets one cookie per query parameter, then redirects to /cookies
// so a browser (or a client with a cookie jar) shows the result.
func (a *app) setCookies(w http.ResponseWriter, r *http.Request) {
	for name, values := range r.URL.Query() {
		http.SetCookie(w, &http.Cookie{
			Name:  name,
			Value: values[len(values)-1],
			Path:  "/",
		})
	}
	http.Redirect(w, r, "/cookies", http.StatusFound)
}

// deleteCookies expires each named cookie. MaxAge < 0 in Go means
// "Max-Age=0" on the wire: delete now. (MaxAge == 0 means "unspecified".)
func (a *app) deleteCookies(w http.ResponseWriter, r *http.Request) {
	for name := range r.URL.Query() {
		http.SetCookie(w, &http.Cookie{
			Name:    name,
			Value:   "",
			Path:    "/",
			MaxAge:  -1,
			Expires: time.Unix(0, 0),
		})
	}
	http.Redirect(w, r, "/cookies", http.StatusFound)
}

func (a *app) basicAuth(w http.ResponseWriter, r *http.Request) {
	wantUser, wantPass := r.PathValue("user"), r.PathValue("passwd")
	user, pass, ok := r.BasicAuth()

	// Compare in constant time so response timing doesn't leak how many
	// leading bytes of a guess were right. Evaluate both comparisons.
	userOK := subtle.ConstantTimeCompare([]byte(user), []byte(wantUser)) == 1
	passOK := subtle.ConstantTimeCompare([]byte(pass), []byte(wantPass)) == 1
	if !ok || !userOK || !passOK {
		// 401 must say how to authenticate.
		w.Header().Set("WWW-Authenticate", `Basic realm="gobin"`)
		writeJSON(w, http.StatusUnauthorized, map[string]any{"authenticated": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": true, "user": user})
}

func (a *app) bearer(w http.ResponseWriter, r *http.Request) {
	// Auth schemes are case-insensitive: "bearer x" is as valid as "Bearer x".
	scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	token = strings.TrimSpace(token)
	if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" {
		w.Header().Set("WWW-Authenticate", "Bearer")
		writeJSON(w, http.StatusUnauthorized, map[string]any{"authenticated": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": true, "token": token})
}
