package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/harlew-dev/lewbox/internal/db"
	"github.com/harlew-dev/lewbox/internal/httpapi"
	"github.com/harlew-dev/lewbox/internal/service"
)

const (
	adminEmail    = "admin@example.com"
	adminPassword = "admin-password-1"
)

// testServer is one router over a real SQLite database with an admin account.
type testServer struct {
	t   *testing.T
	srv *httptest.Server
	svc *service.Service
}

func newTestServer(t *testing.T, opts ...service.Option) *testServer {
	t.Helper()
	conn, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "lewbox.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	svc := service.New(db.New(conn), opts...)
	ctx := service.WithPrincipal(context.Background(), service.LocalPrincipal())
	if _, err := svc.CreateUser(ctx, service.CreateUserRequest{
		Email:         adminEmail,
		Password:      adminPassword,
		InstanceAdmin: true,
	}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(httpapi.NewRouter(svc, slog.New(slog.DiscardHandler)))
	t.Cleanup(srv.Close)
	return &testServer{t: t, srv: srv, svc: svc}
}

func newTestServerWithOptions(t *testing.T, routerOpts []httpapi.RouterOption, opts ...service.Option) *testServer {
	t.Helper()
	conn, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "lewbox.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	svc := service.New(db.New(conn), opts...)
	ctx := service.WithPrincipal(context.Background(), service.LocalPrincipal())
	if _, err := svc.CreateUser(ctx, service.CreateUserRequest{
		Email:         adminEmail,
		Password:      adminPassword,
		InstanceAdmin: true,
	}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(httpapi.NewRouter(svc, slog.New(slog.DiscardHandler), routerOpts...))
	t.Cleanup(srv.Close)
	return &testServer{t: t, srv: srv, svc: svc}
}

// do sends a request. A non-empty body defaults to application/json unless
// the caller sets Content-Type explicitly (an empty value means "send no
// header").
func (ts *testServer) do(method, path, body string, headers map[string]string, cookies ...*http.Cookie) (*http.Response, string) {
	ts.t.Helper()
	req, err := http.NewRequest(method, ts.srv.URL+path, strings.NewReader(body))
	if err != nil {
		ts.t.Fatal(err)
	}
	if body != "" {
		if _, set := headers["Content-Type"]; !set {
			req.Header.Set("Content-Type", "application/json")
		}
	}
	for k, v := range headers {
		if v != "" {
			req.Header.Set(k, v)
		}
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	res, err := ts.srv.Client().Do(req)
	if err != nil {
		ts.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res, string(b)
}

func (ts *testServer) loginJSON() string {
	return `{"email":"` + adminEmail + `","password":"` + adminPassword + `"}`
}

func cookieNamed(t *testing.T, res *http.Response, name string) *http.Cookie {
	t.Helper()
	for _, c := range res.Cookies() {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("response has no %q cookie", name)
	return nil
}

func hasCookie(res *http.Response, name string) bool {
	for _, c := range res.Cookies() {
		if c.Name == name {
			return true
		}
	}
	return false
}

func TestSessionCookiePlainHTTP(t *testing.T) {
	ts := newTestServer(t)
	res, body := ts.do("POST", "/v0/auth/session", ts.loginJSON(), nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d body %s", res.StatusCode, body)
	}
	if strings.Contains(body, `"token"`) {
		t.Errorf("session body contains a token field: %s", body)
	}

	c := cookieNamed(t, res, "lewbox_session")
	if c.Value == "" || !strings.HasPrefix(c.Value, "lbs_") {
		t.Errorf("cookie value = %q", c.Value)
	}
	if c.Secure || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Path != "/" {
		t.Errorf("cookie attributes = %+v", c)
	}
	if c.MaxAge < 6*24*3600 || c.MaxAge > 7*24*3600 {
		t.Errorf("cookie MaxAge = %d", c.MaxAge)
	}
	if hasCookie(res, "__Host-lewbox_session") {
		t.Error("plain HTTP response set a __Host- cookie")
	}

	var session service.SessionResponse
	if err := json.Unmarshal([]byte(body), &session); err != nil {
		t.Fatal(err)
	}
	if session.User.Email != adminEmail || session.ExpiresAt.IsZero() {
		t.Errorf("session = %+v", session)
	}

	me, meBody := ts.do("GET", "/v0/me", "", nil, c)
	if me.StatusCode != http.StatusOK || !strings.Contains(meBody, `"via":"session"`) {
		t.Fatalf("GET /v0/me: status %d body %s", me.StatusCode, meBody)
	}
}

func TestSessionCookieSecure(t *testing.T) {
	ts := newTestServer(t)
	res, body := ts.do("POST", "/v0/auth/session", ts.loginJSON(), map[string]string{"X-Forwarded-Proto": "https"})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d body %s", res.StatusCode, body)
	}
	c := cookieNamed(t, res, "__Host-lewbox_session")
	if !c.Secure || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Path != "/" {
		t.Errorf("cookie attributes = %+v", c)
	}
	if hasCookie(res, "lewbox_session") {
		t.Error("secure response also set the plain cookie name")
	}

	// The plain name is not accepted on a secure request.
	plain := &http.Cookie{Name: "lewbox_session", Value: c.Value}
	me, _ := ts.do("GET", "/v0/me", "", map[string]string{"X-Forwarded-Proto": "https"}, plain)
	if me.StatusCode != http.StatusUnauthorized {
		t.Errorf("GET /v0/me with the plain cookie: status %d", me.StatusCode)
	}
	// The __Host- cookie works.
	me, meBody := ts.do("GET", "/v0/me", "", map[string]string{"X-Forwarded-Proto": "https"}, c)
	if me.StatusCode != http.StatusOK || !strings.Contains(meBody, `"via":"session"`) {
		t.Errorf("GET /v0/me with the __Host cookie: status %d body %s", me.StatusCode, meBody)
	}
}

func TestCookieSecureForced(t *testing.T) {
	ts := newTestServerWithOptions(t, []httpapi.RouterOption{httpapi.WithCookieSecure(true)})
	res, body := ts.do("POST", "/v0/auth/session", ts.loginJSON(), nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d body %s", res.StatusCode, body)
	}
	if c := cookieNamed(t, res, "__Host-lewbox_session"); !c.Secure {
		t.Errorf("forced cookie is not Secure: %+v", c)
	}
}

func TestLogoutClearsCookieAndSession(t *testing.T) {
	ts := newTestServer(t)
	res, body := ts.do("POST", "/v0/auth/session", ts.loginJSON(), nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("session: status %d body %s", res.StatusCode, body)
	}
	c := cookieNamed(t, res, "lewbox_session")

	out, outBody := ts.do("POST", "/v0/auth/logout", "", nil, c)
	if out.StatusCode != http.StatusOK {
		t.Fatalf("logout: status %d body %s", out.StatusCode, outBody)
	}
	for _, name := range []string{"lewbox_session", "__Host-lewbox_session"} {
		cleared := cookieNamed(t, out, name)
		if cleared.MaxAge >= 0 {
			t.Errorf("%q clearing cookie MaxAge = %d", name, cleared.MaxAge)
		}
	}

	me, _ := ts.do("GET", "/v0/me", "", nil, c)
	if me.StatusCode != http.StatusUnauthorized {
		t.Errorf("GET /v0/me after logout: status %d", me.StatusCode)
	}
	if _, err := ts.svc.Authenticate(context.Background(), c.Value); err == nil {
		t.Error("logout left the session valid")
	}
}

func TestInvalidCookieIsCleared(t *testing.T) {
	ts := newTestServer(t)
	bad := &http.Cookie{Name: "lewbox_session", Value: "lbs_" + strings.Repeat("a", 64)}

	health, healthBody := ts.do("GET", "/v0/health", "", nil, bad)
	if health.StatusCode != http.StatusOK || healthBody != `{"ok":true}`+"\n" {
		t.Errorf("GET /v0/health: status %d body %s", health.StatusCode, healthBody)
	}
	if !hasCookie(health, "lewbox_session") || cookieNamed(t, health, "lewbox_session").MaxAge >= 0 {
		t.Error("bad cookie was not cleared on /v0/health")
	}

	me, _ := ts.do("GET", "/v0/me", "", nil, bad)
	if me.StatusCode != http.StatusUnauthorized {
		t.Errorf("GET /v0/me: status %d", me.StatusCode)
	}
	if !hasCookie(me, "lewbox_session") {
		t.Error("bad cookie was not cleared on /v0/me")
	}
}

func TestCrossSiteRequestRejected(t *testing.T) {
	ts := newTestServer(t)
	res, body := ts.do("POST", "/v0/auth/session", ts.loginJSON(), map[string]string{"Sec-Fetch-Site": "cross-site"})
	want := `{"error":{"code":"forbidden","message":"cross-origin request rejected"}}` + "\n"
	if res.StatusCode != http.StatusForbidden || body != want {
		t.Errorf("status %d body %q, want 403 %q", res.StatusCode, body, want)
	}
}

func TestCookieRequestRequiresJSONContentType(t *testing.T) {
	ts := newTestServer(t)
	res, body := ts.do("POST", "/v0/auth/session", ts.loginJSON(), nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("session: status %d body %s", res.StatusCode, body)
	}
	c := cookieNamed(t, res, "lewbox_session")

	res, body = ts.do("POST", "/v0/nodes", `{"name":"n1","url":"https://n1.example"}`, map[string]string{"Content-Type": "text/plain"}, c)
	if res.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("status %d body %s", res.StatusCode, body)
	}
	if !strings.Contains(body, `"code":"invalid_request"`) {
		t.Errorf("body = %s", body)
	}
}

func TestBearerRequestDoesNotNeedJSONContentType(t *testing.T) {
	ts := newTestServer(t)
	res, body := ts.do("POST", "/v0/auth/login", ts.loginJSON(), nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("login: status %d body %s", res.StatusCode, body)
	}
	var login service.LoginResponse
	if err := json.Unmarshal([]byte(body), &login); err != nil {
		t.Fatal(err)
	}
	bearer := map[string]string{"Authorization": "Bearer " + login.Token, "Content-Type": ""}
	res, body = ts.do("POST", "/v0/nodes", `{"name":"n1","url":"https://n1.example"}`, bearer)
	if res.StatusCode != http.StatusCreated {
		t.Errorf("bearer POST without a content type: status %d body %s", res.StatusCode, body)
	}
}

func TestRateLimitedResponse(t *testing.T) {
	ts := newTestServer(t, service.WithLoginLimits(0, 2))
	wrong := `{"email":"` + adminEmail + `","password":"wrong-password-1"}`
	for i := 0; i < 2; i++ {
		res, body := ts.do("POST", "/v0/auth/login", wrong, nil)
		if res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status %d body %s", i+1, res.StatusCode, body)
		}
	}
	res, body := ts.do("POST", "/v0/auth/login", wrong, nil)
	if res.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status %d body %s, want 429", res.StatusCode, body)
	}
	if !strings.Contains(body, `"code":"rate_limited"`) || !strings.Contains(body, `"retry_after_seconds"`) {
		t.Errorf("body = %s", body)
	}
	retry, err := strconv.Atoi(res.Header.Get("Retry-After"))
	if err != nil || retry < 1 || retry > 60 {
		t.Errorf("Retry-After = %q", res.Header.Get("Retry-After"))
	}
}
