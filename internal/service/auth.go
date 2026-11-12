package service

import (
	"context"
	"strings"
)

// Principal is who is calling.
type Principal struct {
	Kind          string // "local", "user" or "key"
	UserID        string
	Email         string
	InstanceAdmin bool
	SessionID     string // set for "user" principals that came from a session token

	// Set for "key" principals: the admin key and the one organization it
	// acts for.
	KeyID string
	OrgID string
}

// LocalPrincipal is the caller for local mode: whoever can open the database
// already owns the instance, so local mode has no sign-in.
func LocalPrincipal() Principal {
	return Principal{Kind: "local", InstanceAdmin: true}
}

type clientIPKey struct{}

// WithClientIP records the caller's address for sign-in rate limiting. The
// HTTP layer takes it from the connection, never from a candidate-supplied
// header.
func WithClientIP(ctx context.Context, ip string) context.Context {
	return context.WithValue(ctx, clientIPKey{}, ip)
}

// clientIPFrom returns the address stored by WithClientIP, if any.
func clientIPFrom(ctx context.Context) string {
	ip, _ := ctx.Value(clientIPKey{}).(string)
	return ip
}

type principalKey struct{}

// WithPrincipal stores p in ctx for the service methods to check.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// PrincipalFrom returns the principal stored in ctx, if any.
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}

// selfRef reports whether ref names the principal's own account, by ID or
// email.
func (p Principal) selfRef(ref string) bool {
	return ref != "" && (ref == p.UserID || strings.EqualFold(ref, p.Email))
}

// requirePrincipal returns an error unless ctx carries a principal of any
// kind, admin keys included. Only methods that serve admin keys use it.
func requirePrincipal(ctx context.Context) (Principal, error) {
	p, ok := PrincipalFrom(ctx)
	if !ok {
		return Principal{}, unauthorized("sign in first: lewbox login")
	}
	return p, nil
}

// requireUser returns an error unless ctx carries a signed-in account or the
// local principal. An admin key acts for one organization, so it is refused
// here: every instance-level method (clusters, nodes, users) starts with
// requireUser or requireAdmin.
func requireUser(ctx context.Context) (Principal, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return Principal{}, err
	}
	if p.Kind == "key" {
		return Principal{}, forbidden("an admin key can only use its organization's routes")
	}
	return p, nil
}

// requireAdmin returns an error unless ctx carries an instance admin.
func requireAdmin(ctx context.Context) (Principal, error) {
	p, err := requireUser(ctx)
	if err != nil {
		return Principal{}, err
	}
	if !p.InstanceAdmin {
		return Principal{}, forbidden("only an instance admin can do this")
	}
	return p, nil
}
