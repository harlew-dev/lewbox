package httpapi_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/harlew-dev/lewbox/internal/db"
	"github.com/harlew-dev/lewbox/internal/httpapi"
	"github.com/harlew-dev/lewbox/internal/service"
)

// captureLogs keeps every log line the router and the service write.
type captureLogs struct {
	mu    sync.Mutex
	lines []string
}

func (h *captureLogs) Enabled(context.Context, slog.Level) bool { return true }

func (h *captureLogs) Handle(_ context.Context, record slog.Record) error {
	var b strings.Builder
	b.WriteString(record.Message)
	record.Attrs(func(attr slog.Attr) bool {
		b.WriteString(" ")
		b.WriteString(attr.Key)
		b.WriteString("=")
		b.WriteString(attr.Value.String())
		return true
	})
	h.mu.Lock()
	h.lines = append(h.lines, b.String())
	h.mu.Unlock()
	return nil
}

func (h *captureLogs) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *captureLogs) WithGroup(string) slog.Handler      { return h }

func (h *captureLogs) text() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return strings.Join(h.lines, "\n")
}

func newLoggingServer(t *testing.T, logs *captureLogs) (*testServer, *service.Service) {
	t.Helper()
	conn, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "lewbox.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	log := slog.New(logs)
	svc := service.New(conn)
	svc.SetLogger(log)
	ctx := service.WithPrincipal(context.Background(), service.LocalPrincipal())
	if _, err := svc.CreateUser(ctx, service.CreateUserRequest{
		Email:         adminEmail,
		Password:      adminPassword,
		InstanceAdmin: true,
	}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(httpapi.NewRouter(svc, log))
	t.Cleanup(srv.Close)
	return &testServer{t: t, srv: srv, svc: svc, conn: conn}, svc
}

// TestSecretsStayOutOfLogs drives every credential flow through the router
// and asserts no token, password, cookie or query value reached a log line.
func TestSecretsStayOutOfLogs(t *testing.T) {
	logs := &captureLogs{}
	ts, svc := newLoggingServer(t, logs)

	openSignup(t, ts)
	const signupPassword = "signup-password-1"
	res, body := ts.do("POST", "/v0/auth/signup",
		`{"email":"ada@example.com","password":"`+signupPassword+`"}`, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("signup: status %d body %s", res.StatusCode, body)
	}
	signupCookie := cookieNamed(t, res, "lewbox_session")

	// A browser session, then a bearer session.
	res, body = ts.do("POST", "/v0/auth/session", ts.loginJSON(), nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("session: status %d body %s", res.StatusCode, body)
	}
	cookie := cookieNamed(t, res, "lewbox_session")

	res, body = ts.do("POST", "/v0/auth/login", ts.loginJSON(), nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("login: status %d body %s", res.StatusCode, body)
	}
	var login struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal([]byte(body), &login); err != nil {
		t.Fatal(err)
	}
	headers := map[string]string{"Authorization": "Bearer " + login.Token}

	// An access token.
	res, body = ts.do("POST", "/v0/access-tokens",
		`{"name":"laptop","current_password":"`+adminPassword+`"}`, headers)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("access token: status %d body %s", res.StatusCode, body)
	}
	var accessToken struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal([]byte(body), &accessToken); err != nil {
		t.Fatal(err)
	}

	// An admin key for a workspace.
	org, err := svc.CreateOrganization(service.WithPrincipal(context.Background(), service.LocalPrincipal()),
		service.CreateOrganizationRequest{Name: "Acme"})
	if err != nil {
		t.Fatal(err)
	}
	keyHeaders := map[string]string{
		"Authorization":            "Bearer " + login.Token,
		httpapi.OrganizationHeader: org.Slug,
	}
	res, body = ts.do("POST", "/v0/admin-keys",
		`{"name":"ci","current_password":"`+adminPassword+`"}`, keyHeaders)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("admin key: status %d body %s", res.StatusCode, body)
	}
	var adminKey struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal([]byte(body), &adminKey); err != nil {
		t.Fatal(err)
	}

	// A password change, with the old and new passwords both in the body.
	const newPassword = "changed-password-1"
	res, body = ts.do("PATCH", "/v0/users/"+adminEmail+"/password",
		`{"password":"`+newPassword+`","current_password":"`+adminPassword+`"}`,
		map[string]string{"Authorization": "Bearer " + login.Token})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("password change: status %d body %s", res.StatusCode, body)
	}

	// A query value never reaches the log, even when it causes a 400.
	res, _ = ts.do("GET", "/v0/profile?token="+signupCookie.Value, "", nil, cookie)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("query with an unknown parameter: status %d", res.StatusCode)
	}

	text := logs.text()
	secrets := map[string]string{
		"admin password":  adminPassword,
		"signup password": signupPassword,
		"new password":    newPassword,
		"login token":     login.Token,
		"session cookie":  cookie.Value,
		"signup cookie":   signupCookie.Value,
		"access token":    accessToken.Token,
		"admin key":       adminKey.Token,
	}
	for name, secret := range secrets {
		if secret == "" {
			t.Errorf("%s is empty", name)
			continue
		}
		if strings.Contains(text, secret) {
			t.Errorf("log output leaks the %s", name)
		}
	}
	if !strings.Contains(text, "request_id=req_") {
		t.Errorf("no access log line with a request ID: %s", text)
	}
	if !strings.Contains(text, "route=GET /v0/profile") {
		t.Errorf("no access log line for the profile route: %s", text)
	}
	if strings.Contains(text, "token=lbs_") || strings.Contains(text, "token=lbp_") {
		t.Errorf("a token reached the log: %s", text)
	}
}
