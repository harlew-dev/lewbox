package httpapi

import (
	"context"
	"crypto/rand"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/harlew-dev/lewbox/internal/logsafe"
	"github.com/harlew-dev/lewbox/internal/service"
)

// requestIDHeader is on every API response. Clients quote it to support; the
// server never trusts an incoming value.
const requestIDHeader = "X-Request-ID"

// requestIDKey is the context key holding this request's ID.
type requestIDKey struct{}

// withRequestID gives every request its own ID, before anything can log or
// answer it.
func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := newRequestID()
		w.Header().Set(requestIDHeader, id)
		ctx := context.WithValue(r.Context(), requestIDKey{}, id)
		ctx = service.WithRequestID(ctx, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requestID returns this request's ID, or "" outside the middleware.
func requestID(r *http.Request) string {
	id, _ := r.Context().Value(requestIDKey{}).(string)
	return id
}

// newRequestID returns req_ followed by 14 base62 characters.
func newRequestID() string {
	const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	var b [14]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "req_00000000000000"
	}
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return "req_" + string(b[:])
}

// withRecovery answers a panic with the generic 500 and logs the cause under
// the request ID.
func withRecovery(next http.Handler, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			recovered := recover()
			if recovered == nil {
				return
			}
			// ErrAbortHandler is net/http's own way to cut a response short.
			if recovered == http.ErrAbortHandler {
				panic(recovered)
			}
			id := requestID(r)
			if log != nil {
				log.Error("request panicked", "request_id", id, "panic", logsafe.Scrub(panicText(recovered)))
			}
			writeJSON(w, http.StatusInternalServerError, ErrorBody{Error: ErrorDetail{
				Code:    "internal",
				Message: internalMessage,
				Details: map[string]string{"request_id": id},
			}})
		}()
		next.ServeHTTP(w, r)
	})
}

func panicText(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case error:
		return v.Error()
	default:
		return "panic"
	}
}

// withAccessLog writes one line per request: method, route pattern, status,
// duration and request ID. The query string, body, cookies and Authorization
// header are never logged.
func withAccessLog(next http.Handler, mux *http.ServeMux, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		recorder := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(recorder, r)
		if log == nil {
			return
		}
		route := routeName(mux, r)
		if route == "" {
			route = "unmatched"
		}
		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		log.Info("request",
			"method", r.Method,
			"route", route,
			"status", status,
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", requestID(r),
		)
	})
}

// statusRecorder remembers the status a handler wrote.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (w *statusRecorder) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusRecorder) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(data)
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (w *statusRecorder) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// withNoStore keeps browsers and proxies from caching API responses,
// including the ones that carry session state.
func withNoStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == Prefix || strings.HasPrefix(r.URL.Path, Prefix+"/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

// withClientIP records the connection's address for rate limiting. An
// X-Forwarded-For header is ignored unless the connection comes from a
// trusted proxy; then the right-most entry that is not itself trusted wins.
func withClientIP(next http.Handler, trusted []netip.Prefix) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := remoteHost(r.RemoteAddr)
		if addr := parseHost(host); addr.IsValid() && trustedAddr(addr, trusted) {
			host = forwardedClient(host, r.Header.Values("X-Forwarded-For"), trusted)
		}
		if host != "" {
			r = r.WithContext(service.WithClientIP(r.Context(), host))
		}
		next.ServeHTTP(w, r)
	})
}

// routePattern returns the route pattern the mux would use for r. The router's
// catch-all is "/", so a non-root path that only matches "/" has no route.
func routePattern(mux *http.ServeMux, r *http.Request) string {
	_, pattern := mux.Handler(r)
	if pattern == "/" && r.URL.Path != "/" {
		return ""
	}
	return pattern
}

// routeName is the pattern that names r's path, even when the method is
// wrong, or "" for an unknown path.
func routeName(mux *http.ServeMux, r *http.Request) string {
	if pattern := routePattern(mux, r); pattern != "" {
		return pattern
	}
	for _, method := range []string{
		http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete, http.MethodOptions,
	} {
		probe := r.Clone(r.Context())
		probe.Method = method
		if pattern := routePattern(mux, probe); pattern != "" {
			return pattern
		}
	}
	return ""
}

// methodAllow returns the methods the mux accepts for r's path, or nil when
// the path has no route.
func methodAllow(mux *http.ServeMux, r *http.Request) []string {
	var allow []string
	for _, method := range []string{
		http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete, http.MethodOptions,
	} {
		if method == r.Method {
			continue
		}
		probe := r.Clone(r.Context())
		probe.Method = method
		if pattern := routePattern(mux, probe); pattern != "" {
			allow = append(allow, method)
		}
	}
	return allow
}

// withMethodNotAllowed answers a known path with the wrong method as JSON 405
// plus Allow, instead of letting the catch-all call it 404.
func withMethodNotAllowed(next http.Handler, mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if routePattern(mux, r) != "" {
			next.ServeHTTP(w, r)
			return
		}
		allow := methodAllow(mux, r)
		if len(allow) == 0 {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Allow", strings.Join(allow, ", "))
		writeJSON(w, http.StatusMethodNotAllowed, ErrorBody{Error: ErrorDetail{
			Code:    "method_not_allowed",
			Message: "method not allowed",
		}})
	})
}

// queryAllowLists names the parameters each route accepts. A route absent from
// the map accepts none. Anything not listed, or listed twice, is a 400.
var queryAllowLists = map[string][]string{
	"GET " + Prefix + "/access-tokens":                   {"user"},
	"DELETE " + Prefix + "/access-tokens/{access_token}": {"user"},
	"GET " + Prefix + "/archives/{archive}":              {"org"},
	"POST " + Prefix + "/archives/{archive}/restore":     {"org"},
	"DELETE " + Prefix + "/archives/{archive}":           {"org"},
	"DELETE " + Prefix + "/clusters/{cluster}":           {"force"},
	"GET " + Prefix + "/clusters/{cluster}/health":       {"days"},
	"GET " + Prefix + "/clusters/{cluster}/metrics":      {"range"},
	"GET " + Prefix + "/clusters/resolve":                {"cluster"},
	"GET " + Prefix + "/events":                          {"before", "actor", "organization", "action", "from", "to", "limit"},
	"GET " + Prefix + "/nodes":                           {"cluster"},
	"DELETE " + Prefix + "/nodes/{node}":                 {"force"},
	"GET " + Prefix + "/organizations":                   {"user", "status"},
	"GET " + Prefix + "/sandboxes":                       {"region", "status", "org"},
	"POST " + Prefix + "/sandboxes/{sandbox}/stop":       {"region", "org"},
	"DELETE " + Prefix + "/sandboxes/{sandbox}":          {"region", "org"},
	"GET " + Prefix + "/snapshots":                       {"region", "org"},
	"DELETE " + Prefix + "/snapshots/{snapshot}":         {"region", "org"},
	"DELETE " + Prefix + "/subscriptions/{region}":       {"force"},
	"GET " + Prefix + "/usage":                           {"from", "to", "granularity", "org"},
}

// withQueryAllowList rejects an unknown, repeated or malformed query
// parameter before the handler reads it.
func withQueryAllowList(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pattern := routePattern(mux, r)
		if pattern != "" {
			if err := checkQuery(r, queryAllowLists[pattern]); err != nil {
				respond(w, nil, 0, nil, err)
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}

// checkQuery validates the raw query string against the route's allow-list.
func checkQuery(r *http.Request, allowed []string) error {
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return badBody("the query string is malformed")
	}
	names := make(map[string]bool, len(allowed))
	for _, name := range allowed {
		names[name] = true
	}
	for name, list := range values {
		if !names[name] {
			return badBody("unknown query parameter " + name)
		}
		if len(list) > 1 {
			return badBody("query parameter " + name + " must appear once")
		}
	}
	return nil
}

// remoteHost strips the port from a RemoteAddr.
func remoteHost(remoteAddr string) string {
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return h
	}
	return remoteAddr
}

// parseHost reads an address, with or without a port.
func parseHost(raw string) netip.Addr {
	raw = strings.TrimSpace(raw)
	if addr, err := netip.ParseAddr(raw); err == nil {
		return addr.Unmap()
	}
	if host, _, err := net.SplitHostPort(raw); err == nil {
		if addr, err := netip.ParseAddr(host); err == nil {
			return addr.Unmap()
		}
	}
	return netip.Addr{}
}

func trustedAddr(addr netip.Addr, prefixes []netip.Prefix) bool {
	for _, prefix := range prefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// forwardedClient is the right-most X-Forwarded-For address that is not
// trusted, or remote when there is none.
func forwardedClient(remote string, values []string, trusted []netip.Prefix) string {
	var entries []string
	for _, value := range values {
		entries = append(entries, strings.Split(value, ",")...)
	}
	for i := len(entries) - 1; i >= 0; i-- {
		addr := parseHost(entries[i])
		if addr.IsValid() && !trustedAddr(addr, trusted) {
			return addr.String()
		}
	}
	return remote
}

// withAuth resolves a bearer token or a browser session cookie into a
// principal. An Authorization header always wins, so the CLI's behaviour is
// unchanged. A request with neither continues anonymously; the service method
// decides whether that is allowed.
func withAuth(svc *service.Service, log *slog.Logger, opts RouterOptions, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if header := r.Header.Get("Authorization"); header != "" {
			scheme, token, ok := strings.Cut(header, " ")
			if !ok || !strings.EqualFold(scheme, "Bearer") {
				token = "" // force Authenticate to reject the header
			}
			p, err := svc.Authenticate(r.Context(), token)
			if err != nil {
				respond(w, log, 0, nil, err)
				return
			}
			serveAuthenticated(svc, log, w, r, p, next)
			return
		}

		cookie, err := r.Cookie(sessionCookieName(r, opts))
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		p, err := svc.Authenticate(r.Context(), cookie.Value)
		if err != nil {
			// A bad cookie is not a hard failure on public routes: forget it
			// and let the handler answer as an anonymous caller.
			clearSessionCookie(w, r, opts)
			next.ServeHTTP(w, r)
			return
		}
		if message, rejected := unsafeContentType(r); rejected {
			writeJSON(w, http.StatusUnsupportedMediaType, ErrorBody{Error: ErrorDetail{
				Code:    string(service.CodeBadRequest),
				Message: message,
			}})
			return
		}
		serveAuthenticated(svc, log, w, r, p, next)
	})
}

// serveAuthenticated stores the caller on the request and charges one request
// to its budget (api:per-user) before the handler runs. It sits inside
// withAuth, after the origin and content-type checks, so a refused forged
// request does not spend the caller's budget.
func serveAuthenticated(svc *service.Service, log *slog.Logger, w http.ResponseWriter, r *http.Request, p service.Principal, next http.Handler) {
	r = r.WithContext(service.WithPrincipal(r.Context(), p))
	if !unmeteredRoute(r) {
		if err := svc.AllowRequest(r.Context()); err != nil {
			respond(w, log, 0, nil, err)
			return
		}
	}
	next.ServeHTTP(w, r)
}

// unmeteredRoute names the recovery routes excluded from the API budget.
// Without this, someone holding a stolen session could spend the owner's whole
// budget and stop them signing out or changing the password. Both routes keep
// their own guard: changing the password needs fresh proof, which shares the
// sign-in failure budget.
func unmeteredRoute(r *http.Request) bool {
	if r.Method == http.MethodPost && r.URL.Path == Prefix+"/auth/logout" {
		return true
	}
	return r.Method == http.MethodPatch &&
		strings.HasPrefix(r.URL.Path, Prefix+"/users/") &&
		strings.HasSuffix(r.URL.Path, "/password")
}

// unsafeContentType rejects cookie-authenticated state-changing requests that
// are not JSON. Browsers can send text/plain or form posts cross-site without
// a CORS preflight, so requiring JSON closes that gap. Bearer requests are
// unaffected because they are not sent by browsers.
func unsafeContentType(r *http.Request) (string, bool) {
	switch r.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
	default:
		return "", false
	}
	if r.ContentLength == 0 {
		return "", false
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err == nil && strings.EqualFold(mediaType, "application/json") {
		return "", false
	}
	return "cookie-authenticated requests must send Content-Type: application/json", true
}
