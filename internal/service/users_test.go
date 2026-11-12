package service

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/harlew-dev/lewbox/internal/db"
)

func TestMain(m *testing.M) {
	// Keep tests fast; the encoding is the same at any iteration count.
	passwordIterations = 1000
	os.Exit(m.Run())
}

const testPassword = "correct-horse-battery"

func newTestService(t *testing.T) *Service {
	t.Helper()
	return newTestServiceWith(t)
}

func newTestServiceWith(t *testing.T, opts ...Option) *Service {
	t.Helper()
	conn, err := db.Open(context.Background(), t.TempDir()+"/lewbox.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return New(db.New(conn), opts...)
}

func localCtx() context.Context {
	return WithPrincipal(context.Background(), LocalPrincipal())
}

func createUser(t *testing.T, s *Service, email string, admin bool) User {
	t.Helper()
	u, err := s.CreateUser(localCtx(), CreateUserRequest{
		Email:         email,
		Password:      testPassword,
		InstanceAdmin: admin,
	})
	if err != nil {
		t.Fatalf("create user %s: %v", email, err)
	}
	return u
}

func loginUser(t *testing.T, s *Service, email, password string) (Principal, string) {
	t.Helper()
	resp, err := s.Login(context.Background(), LoginRequest{Email: email, Password: password})
	if err != nil {
		t.Fatalf("login %s: %v", email, err)
	}
	p, err := s.Authenticate(context.Background(), resp.Token)
	if err != nil {
		t.Fatalf("authenticate %s: %v", email, err)
	}
	return p, resp.Token
}

func wantCode(t *testing.T, err error, code Code) *Error {
	t.Helper()
	se, ok := errors.AsType[*Error](err)
	if !ok || se.Code != code {
		t.Fatalf("error = %v, want code %q", err, code)
	}
	return se
}

func TestPasswordEncoding(t *testing.T) {
	hash, err := hashPassword(testPassword)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(hash, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" || parts[1] != "1000" {
		t.Fatalf("hash = %q", hash)
	}
	ok, err := verifyPassword(hash, testPassword)
	if err != nil || !ok {
		t.Errorf("verify right password = %v, %v", ok, err)
	}
	ok, err = verifyPassword(hash, "not-"+testPassword)
	if err != nil || ok {
		t.Errorf("verify wrong password = %v, %v", ok, err)
	}
	for _, bad := range []string{"", "plain", "pbkdf2-sha256$0$AA$AA", "pbkdf2-sha256$x$AA$AA", "md5$1$AA$AA"} {
		if _, err := verifyPassword(bad, testPassword); !errors.Is(err, errBadPasswordHash) {
			t.Errorf("verifyPassword(%q) err = %v", bad, err)
		}
	}
}

func TestPasswordRules(t *testing.T) {
	cases := []struct {
		name     string
		password string
		wantErr  bool
	}{
		{"short", strings.Repeat("a", 11), true},
		{"minimum", strings.Repeat("a", 12), false},
		{"maximum", strings.Repeat("a", 256), false},
		{"too long", strings.Repeat("a", 257), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validatePassword(tc.password)
			if (err != nil) != tc.wantErr {
				t.Errorf("validatePassword(len %d) = %v, wantErr %v", len(tc.password), err, tc.wantErr)
			}
		})
	}
}

func TestSessionToken(t *testing.T) {
	token, digest, err := newSessionToken()
	if err != nil {
		t.Fatal(err)
	}
	if !validSessionToken(token) {
		t.Errorf("token %q is not valid", token)
	}
	if digest != tokenDigest(token) || len(digest) != 64 {
		t.Errorf("digest = %q", digest)
	}
	if strings.Contains(token, digest) {
		t.Errorf("token %q contains its digest", token)
	}
	for _, bad := range []string{
		"",
		"lbs_",
		"nope",
		"lbs_" + strings.Repeat("a", 63),
		"lbs_" + strings.Repeat("a", 65),
		"lbs_" + strings.Repeat("A", 64),
		"lbs_" + strings.Repeat("g", 64),
	} {
		if validSessionToken(bad) {
			t.Errorf("validSessionToken(%q) = true", bad)
		}
	}
}

func TestCreateUserValidation(t *testing.T) {
	s := newTestService(t)

	emailCases := []string{
		"",
		"no-at",
		"a@b@c",
		"@example.com",
		"ada@",
		strings.Repeat("a", 243) + "@example.com", // 255 characters
	}
	for _, email := range emailCases {
		if _, err := s.CreateUser(localCtx(), CreateUserRequest{Email: email, Password: testPassword}); err == nil {
			t.Errorf("CreateUser(%q) succeeded", email)
		} else {
			wantCode(t, err, CodeInvalid)
		}
	}

	u, err := s.CreateUser(localCtx(), CreateUserRequest{
		Email:    "  Ada@Example.COM ",
		Password: testPassword,
	})
	if err != nil {
		t.Fatal(err)
	}
	if u.Email != "ada@example.com" || u.InstanceAdmin || u.Disabled || u.CreatedAt.IsZero() {
		t.Errorf("user = %+v", u)
	}
	if u.CreatedAt.Location() != time.UTC {
		t.Errorf("created_at location = %v", u.CreatedAt.Location())
	}

	_, err = s.CreateUser(localCtx(), CreateUserRequest{Email: "ADA@example.com", Password: testPassword})
	wantCode(t, err, CodeConflict)

	admin := createUser(t, s, "root@example.com", true)
	if !admin.InstanceAdmin {
		t.Errorf("admin = %+v", admin)
	}
}

func TestListAndGetUsers(t *testing.T) {
	s := newTestService(t)
	createUser(t, s, "zoe@example.com", false)
	createUser(t, s, "ada@example.com", false)

	list, err := s.ListUsers(localCtx())
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Data) != 2 || list.Data[0].Email != "ada@example.com" || list.Data[1].Email != "zoe@example.com" {
		t.Errorf("users = %+v", list.Data)
	}

	byEmail, err := s.GetUser(localCtx(), "ADA@EXAMPLE.COM")
	if err != nil || byEmail.Email != "ada@example.com" {
		t.Errorf("get by email = %+v, %v", byEmail, err)
	}
	byID, err := s.GetUser(localCtx(), byEmail.ID)
	if err != nil || byID.ID != byEmail.ID {
		t.Errorf("get by id = %+v, %v", byID, err)
	}
	if _, err := s.GetUser(localCtx(), "nobody@example.com"); err != nil {
		wantCode(t, err, CodeNotFound)
	}
	if _, err := s.GetUser(localCtx(), ""); err != nil {
		wantCode(t, err, CodeInvalid)
	}
}

func TestLastAdminProtection(t *testing.T) {
	s := newTestService(t)
	a := createUser(t, s, "a@example.com", true)

	_, err := s.RemoveUser(localCtx(), a.ID)
	wantCode(t, err, CodeConflict)
	_, err = s.SetUserDisabled(localCtx(), a.ID, true)
	wantCode(t, err, CodeConflict)

	b := createUser(t, s, "b@example.com", true)
	if _, err := s.SetUserDisabled(localCtx(), b.ID, true); err != nil {
		t.Fatalf("disable second admin: %v", err)
	}
	// b is disabled, so a is still the last enabled admin.
	_, err = s.RemoveUser(localCtx(), a.ID)
	wantCode(t, err, CodeConflict)
	// Removing a disabled admin is fine.
	if _, err := s.RemoveUser(localCtx(), b.ID); err != nil {
		t.Fatalf("remove disabled admin: %v", err)
	}

	c := createUser(t, s, "c@example.com", true)
	if _, err := s.RemoveUser(localCtx(), a.ID); err != nil {
		t.Fatalf("remove admin with a peer: %v", err)
	}
	_, err = s.RemoveUser(localCtx(), c.ID)
	wantCode(t, err, CodeConflict)
}

func TestDisableKillsSessions(t *testing.T) {
	s := newTestService(t)
	u := createUser(t, s, "ada@example.com", false)
	_, token := loginUser(t, s, u.Email, testPassword)

	if _, err := s.SetUserDisabled(localCtx(), u.Email, true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(context.Background(), token); err != nil {
		wantCode(t, err, CodeUnauthorized)
	}
	_, err := s.Login(context.Background(), LoginRequest{Email: u.Email, Password: testPassword})
	wantCode(t, err, CodeUnauthorized)

	if _, err := s.SetUserDisabled(localCtx(), u.Email, false); err != nil {
		t.Fatal(err)
	}
	loginUser(t, s, u.Email, testPassword)
}

func TestSetPasswordKeepsOwnSession(t *testing.T) {
	s := newTestService(t)
	u := createUser(t, s, "ada@example.com", false)
	p1, token1 := loginUser(t, s, u.Email, testPassword)
	_, token2 := loginUser(t, s, u.Email, testPassword)

	const newPassword = "another-good-password"
	if _, err := s.SetPassword(WithPrincipal(context.Background(), p1), u.Email, SetPasswordRequest{Password: newPassword}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(context.Background(), token1); err != nil {
		t.Errorf("caller's session ended: %v", err)
	}
	if _, err := s.Authenticate(context.Background(), token2); err != nil {
		wantCode(t, err, CodeUnauthorized)
	}
	_, err := s.Login(context.Background(), LoginRequest{Email: u.Email, Password: testPassword})
	wantCode(t, err, CodeUnauthorized)
	loginUser(t, s, u.Email, newPassword)

	// An admin changing someone else's password ends every session.
	_, token := loginUser(t, s, u.Email, newPassword)
	if _, err := s.SetPassword(localCtx(), u.Email, SetPasswordRequest{Password: "third-good-password"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(context.Background(), token); err != nil {
		wantCode(t, err, CodeUnauthorized)
	}
}

func TestLogin(t *testing.T) {
	s := newTestService(t)
	fixed := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	s.now = func() time.Time { return fixed }
	u := createUser(t, s, "ada@example.com", false)

	resp, err := s.Login(context.Background(), LoginRequest{Email: "  ADA@example.com ", Password: testPassword})
	if err != nil {
		t.Fatal(err)
	}
	if !validSessionToken(resp.Token) || resp.User.ID != u.ID || resp.User.Email != u.Email {
		t.Errorf("login = %+v", resp)
	}
	if d := resp.ExpiresAt.Sub(resp.User.CreatedAt); d != sessionLifetime {
		t.Errorf("session lasts %v, want %v", d, sessionLifetime)
	}

	cases := []struct {
		name string
		req  LoginRequest
	}{
		{"wrong password", LoginRequest{Email: u.Email, Password: testPassword + "x"}},
		{"unknown email", LoginRequest{Email: "nobody@example.com", Password: testPassword}},
		{"not an email", LoginRequest{Email: "nobody", Password: testPassword}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.Login(context.Background(), tc.req)
			se := wantCode(t, err, CodeUnauthorized)
			if se.Message != "email or password is wrong" {
				t.Errorf("message = %q", se.Message)
			}
		})
	}

	if _, err := s.SetUserDisabled(localCtx(), u.ID, true); err != nil {
		t.Fatal(err)
	}
	_, err = s.Login(context.Background(), LoginRequest{Email: u.Email, Password: testPassword})
	se := wantCode(t, err, CodeUnauthorized)
	if se.Message != "email or password is wrong" {
		t.Errorf("disabled message = %q", se.Message)
	}
}

func TestAuthenticate(t *testing.T) {
	s := newTestService(t)
	u := createUser(t, s, "ada@example.com", false)

	for _, bad := range []string{"", "nope", "lbs_" + strings.Repeat("a", 63), strings.Repeat("a", 68)} {
		_, err := s.Authenticate(context.Background(), bad)
		wantCode(t, err, CodeUnauthorized)
	}

	_, err := s.Authenticate(context.Background(), "lbs_"+strings.Repeat("a", 64))
	wantCode(t, err, CodeUnauthorized)

	_, token := loginUser(t, s, u.Email, testPassword)
	session, err := s.q.GetSessionByTokenHash(context.Background(), tokenDigest(token))
	if err != nil {
		t.Fatal(err)
	}
	lastUsed := session.LastUsedAt
	base := s.now()
	s.now = func() time.Time { return base.Add(30 * time.Second) }
	if _, err := s.Authenticate(context.Background(), token); err != nil {
		t.Fatal(err)
	}
	session, err = s.q.GetSessionByTokenHash(context.Background(), tokenDigest(token))
	if err != nil || !session.LastUsedAt.Equal(lastUsed) {
		t.Errorf("last_used_at changed within a minute: %v, %v", session.LastUsedAt, err)
	}
	s.now = func() time.Time { return base.Add(2 * time.Minute) }
	if _, err := s.Authenticate(context.Background(), token); err != nil {
		t.Fatal(err)
	}
	session, err = s.q.GetSessionByTokenHash(context.Background(), tokenDigest(token))
	if err != nil || !session.LastUsedAt.Equal(base.Add(2*time.Minute)) {
		t.Errorf("last_used_at = %v, %v", session.LastUsedAt, err)
	}

	s.now = func() time.Time { return base.Add(sessionLifetime + time.Second) }
	_, err = s.Authenticate(context.Background(), token)
	wantCode(t, err, CodeUnauthorized)
}

func TestMeAndLogout(t *testing.T) {
	s := newTestService(t)
	u := createUser(t, s, "ada@example.com", false)

	me, err := s.Me(localCtx())
	if err != nil || me.User != nil || me.Via != "local" {
		t.Errorf("local me = %+v, %v", me, err)
	}
	if _, err := s.Logout(localCtx()); err != nil {
		wantCode(t, err, CodeInvalid)
	}

	p, token := loginUser(t, s, u.Email, testPassword)
	me, err = s.Me(WithPrincipal(context.Background(), p))
	if err != nil || me.User == nil || me.User.ID != u.ID || me.Via != "session" {
		t.Errorf("session me = %+v, %v", me, err)
	}

	if _, err := s.Logout(WithPrincipal(context.Background(), p)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(context.Background(), token); err != nil {
		wantCode(t, err, CodeUnauthorized)
	}
}

func TestBootstrapAdmin(t *testing.T) {
	s := newTestService(t)

	created, err := s.BootstrapAdmin(context.Background(), "root@example.com", testPassword)
	if err != nil || !created {
		t.Fatalf("bootstrap = %v, %v", created, err)
	}
	created, err = s.BootstrapAdmin(context.Background(), "other@example.com", testPassword)
	if err != nil || created {
		t.Fatalf("second bootstrap = %v, %v", created, err)
	}
	users, err := s.ListUsers(localCtx())
	if err != nil {
		t.Fatal(err)
	}
	if len(users.Data) != 1 || users.Data[0].Email != "root@example.com" || !users.Data[0].InstanceAdmin {
		t.Errorf("users = %+v", users.Data)
	}
}

func TestPermissions(t *testing.T) {
	s := newTestService(t)
	admin := createUser(t, s, "admin@example.com", true)
	plain := createUser(t, s, "ada@example.com", false)
	other := createUser(t, s, "bob@example.com", false)

	anon := context.Background()
	if _, err := s.ListNodes(anon, ListNodesRequest{}); err != nil {
		wantCode(t, err, CodeUnauthorized)
	}
	if _, err := s.AddNode(anon, AddNodeRequest{Name: "n1", URL: "https://n1.example"}); err != nil {
		wantCode(t, err, CodeUnauthorized)
	}
	if _, err := s.ListUsers(anon); err != nil {
		wantCode(t, err, CodeUnauthorized)
	}
	if _, err := s.CreateUser(anon, CreateUserRequest{Email: "x@example.com", Password: testPassword}); err != nil {
		wantCode(t, err, CodeUnauthorized)
	}

	plainPrincipal, _ := loginUser(t, s, plain.Email, testPassword)
	plainCtx := WithPrincipal(anon, plainPrincipal)

	if _, err := s.ListNodes(plainCtx, ListNodesRequest{}); err != nil {
		wantCode(t, err, CodeForbidden)
	}
	if _, err := s.ListRegions(plainCtx, ""); err != nil {
		t.Errorf("plain user list regions: %v", err)
	}
	if _, err := s.AddNode(plainCtx, AddNodeRequest{Name: "n1", URL: "https://n1.example"}); err != nil {
		wantCode(t, err, CodeForbidden)
	}
	if _, err := s.ListUsers(plainCtx); err != nil {
		wantCode(t, err, CodeForbidden)
	}
	if _, err := s.CreateUser(plainCtx, CreateUserRequest{Email: "x@example.com", Password: testPassword}); err != nil {
		wantCode(t, err, CodeForbidden)
	}
	if _, err := s.GetUser(plainCtx, plain.Email); err != nil {
		t.Errorf("plain user get self: %v", err)
	}
	if _, err := s.GetUser(plainCtx, other.Email); err != nil {
		wantCode(t, err, CodeForbidden)
	}
	// An unknown account is a 403 too, not a 404.
	_, lookupErr := s.GetUser(plainCtx, "nobody@example.com")
	wantCode(t, lookupErr, CodeForbidden)
	if _, err := s.SetPassword(plainCtx, other.Email, SetPasswordRequest{Password: "another-good-password"}); err != nil {
		wantCode(t, err, CodeForbidden)
	}
	if _, err := s.RemoveUser(plainCtx, other.Email); err != nil {
		wantCode(t, err, CodeForbidden)
	}
	if _, err := s.SetUserDisabled(plainCtx, other.Email, true); err != nil {
		wantCode(t, err, CodeForbidden)
	}

	adminPrincipal, _ := loginUser(t, s, admin.Email, testPassword)
	adminCtx := WithPrincipal(anon, adminPrincipal)
	if _, err := s.AddNode(adminCtx, AddNodeRequest{Name: "n1", URL: "https://n1.example"}); err != nil {
		t.Errorf("admin add node: %v", err)
	}
	if _, err := s.GetUser(adminCtx, other.Email); err != nil {
		t.Errorf("admin get other: %v", err)
	}
}

// The last-admin rule holds in the SQL itself, not only in the Go check
// before it: two admins removing each other at once must leave one.
func TestLastAdminGuardIsAtomic(t *testing.T) {
	s := newTestService(t)
	ctx := localCtx()
	for _, email := range []string{"a@x.dev", "b@x.dev"} {
		if _, err := s.CreateUser(ctx, CreateUserRequest{Email: email, Password: "password-12345", InstanceAdmin: true}); err != nil {
			t.Fatal(err)
		}
	}
	a, _ := s.GetUser(ctx, "a@x.dev")
	b, _ := s.GetUser(ctx, "b@x.dev")
	// Simulate the race: both pre-checks saw two admins, then both deletes run.
	if n, err := s.q.DeleteUser(context.Background(), a.ID); err != nil || n != 1 {
		t.Fatalf("first delete: n=%d err=%v", n, err)
	}
	if n, err := s.q.DeleteUser(context.Background(), b.ID); err != nil || n != 0 {
		t.Fatalf("second delete removed the last admin: n=%d err=%v", n, err)
	}
	if _, err := s.q.SetUserDisabled(context.Background(), db.SetUserDisabledParams{Disabled: 1, ID: b.ID}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("disabling the last admin in SQL: err=%v, want no rows", err)
	}
	if _, err := s.RemoveUser(ctx, "b@x.dev"); err == nil {
		t.Fatal("RemoveUser removed the last admin")
	}
}
