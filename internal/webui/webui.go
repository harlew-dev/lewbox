// Package webui is the top-level HTTP handler: the /v0 API, the built web
// console, and its client-side routes all share one origin.
package webui

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// contentSecurityPolicy is sent with every HTML response. The console is
// served from this origin, so nothing but 'self' is needed.
const contentSecurityPolicy = "default-src 'self'; script-src 'self'; " +
	"style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:; " +
	"connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; " +
	"form-action 'self'"

// notBuiltPage is served when web/dist has no index.html, which is the case
// until someone builds the React app.
const notBuiltPage = `<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>Lewbox</title></head>
<body>
<h1>The web console isn't built</h1>
<p>Run <code>npm --prefix web ci &amp;&amp; npm --prefix web run build</code>, then restart <code>lewbox serve</code>.</p>
<p>The API and the CLI still work.</p>
</body>
</html>
`

// Handler serves the API under /v0, files from dist, and index.html for every
// other GET so the single-page app can own its routes.
func Handler(api http.Handler, dist fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := w.Header()
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("Referrer-Policy", "same-origin")
		header.Set("X-Frame-Options", "DENY")

		if p := r.URL.Path; p == "/v0" || strings.HasPrefix(p, "/v0/") {
			api.ServeHTTP(w, r)
			return
		}

		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			header.Set("Allow", "GET, HEAD")
			writeError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
			return
		}

		// Cleaning the path first means a request can never climb out of dist.
		rel := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if rel != "" {
			if info, err := fs.Stat(dist, rel); err == nil && info.Mode().IsRegular() {
				if strings.HasPrefix(rel, "assets/") {
					header.Set("Cache-Control", "public, max-age=31536000, immutable")
				} else {
					header.Set("Cache-Control", "no-cache")
				}
				if strings.HasSuffix(rel, ".html") || strings.HasSuffix(rel, ".htm") {
					header.Set("Content-Security-Policy", contentSecurityPolicy)
				}
				// ServeFileFS refuses a URL path containing "..", so hand it
				// the sanitized path we already resolved.
				clean := *r
				cleanURL := *r.URL
				cleanURL.Path = "/" + rel
				cleanURL.RawPath = ""
				clean.URL = &cleanURL
				http.ServeFileFS(w, &clean, dist, rel)
				return
			}
		}
		// A missing build asset (say, an old hashed bundle after an upgrade)
		// must not turn into index.html, which the browser would then refuse
		// to run as a script with a confusing MIME error.
		if strings.HasPrefix(rel, "assets/") {
			writeError(w, http.StatusNotFound, "not_found", "no such asset")
			return
		}
		serveIndex(w, r, dist)
	})
}

// serveIndex serves the single-page app's entry point.
func serveIndex(w http.ResponseWriter, r *http.Request, dist fs.FS) {
	header := w.Header()
	header.Set("Content-Type", "text/html; charset=utf-8")
	header.Set("Cache-Control", "no-cache")
	header.Set("Content-Security-Policy", contentSecurityPolicy)

	index, err := fs.ReadFile(dist, "index.html")
	if err != nil {
		index = []byte(notBuiltPage)
	}
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(index)
}

// errorBody is the API's standard error shape.
type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorBody{Error: errorDetail{Code: code, Message: message}})
}
