package httpapi

import (
	"net/http"
	"strings"
	"time"
)

// Session cookie names. Browsers reserve the __Host- prefix for cookies that
// are Secure, Path=/ and have no Domain, so it is only used over TLS.
const (
	sessionCookiePlain = "lewbox_session"
	sessionCookieHost  = "__Host-lewbox_session"
)

// secureRequest reports whether the browser reached us over TLS.
func secureRequest(r *http.Request, opts RouterOptions) bool {
	return opts.CookieSecure ||
		r.TLS != nil ||
		strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
}

// sessionCookieName is the one cookie accepted for this request's scheme.
func sessionCookieName(r *http.Request, opts RouterOptions) string {
	if secureRequest(r, opts) {
		return sessionCookieHost
	}
	return sessionCookiePlain
}

// setSessionCookie stores a browser session token. The cookie's lifetime
// matches the session's expires_at.
func setSessionCookie(w http.ResponseWriter, r *http.Request, opts RouterOptions, token string, expires time.Time) {
	secure := secureRequest(r, opts)
	maxAge := int(time.Until(expires) / time.Second)
	if maxAge < 1 {
		maxAge = 1
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName(r, opts),
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
		Expires:  expires.UTC(),
		MaxAge:   maxAge,
	})
}

// clearSessionCookie expires both cookie names, whatever the current scheme.
func clearSessionCookie(w http.ResponseWriter, r *http.Request, opts RouterOptions) {
	for _, name := range []string{sessionCookiePlain, sessionCookieHost} {
		http.SetCookie(w, &http.Cookie{
			Name:     name,
			Value:    "",
			Path:     "/",
			HttpOnly: true,
			Secure:   secureRequest(r, opts),
			SameSite: http.SameSiteStrictMode,
			Expires:  time.Unix(1, 0).UTC(),
			MaxAge:   -1,
		})
	}
}
