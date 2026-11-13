// Package httpapi exposes internal/service over HTTP.
package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/harlew-dev/lewbox/internal/docs"
	"github.com/harlew-dev/lewbox/internal/service"
)

// Prefix is the API version prefix (MVP.md §12).
const Prefix = "/v0"

// maxBodyBytes caps JSON request bodies.
const maxBodyBytes = 1 << 20

// OrganizationHeader names the organization a member or admin-key route acts
// on: its ID or slug. An admin key's own organization is the default.
const OrganizationHeader = "Lewbox-Organization"

// Session cookie names. Browsers reserve the __Host- prefix for cookies that
// are Secure, Path=/ and have no Domain, so it is only used over TLS.
const (
	sessionCookiePlain = "lewbox_session"
	sessionCookieHost  = "__Host-lewbox_session"
)

// RouterOptions configures NewRouter.
type RouterOptions struct {
	// CookieSecure forces the Secure attribute on the session cookie. Set it
	// behind a TLS proxy that does not send X-Forwarded-Proto.
	CookieSecure bool
	// TrustedOrigins are extra origins allowed through the CSRF check, for a
	// proxy that rewrites Host.
	TrustedOrigins []string
	// TrustedProxies are the networks whose X-Forwarded-For headers are
	// believed. Empty means the header is ignored.
	TrustedProxies []netip.Prefix
	// Docs serves the customer documentation routes. A nil site answers 404.
	Docs *docs.Site
	// Platform serves the platform documentation routes. A nil site answers 404.
	Platform *docs.Site
}

// RouterOption changes a router built by NewRouter.
type RouterOption func(*RouterOptions)

// WithCookieSecure forces Secure session cookies.
func WithCookieSecure(secure bool) RouterOption {
	return func(o *RouterOptions) { o.CookieSecure = secure }
}

// WithTrustedOrigins adds origins the CSRF check should accept.
func WithTrustedOrigins(origins []string) RouterOption {
	return func(o *RouterOptions) { o.TrustedOrigins = append(o.TrustedOrigins, origins...) }
}

// WithTrustedProxies trusts X-Forwarded-For from the given networks.
func WithTrustedProxies(prefixes []netip.Prefix) RouterOption {
	return func(o *RouterOptions) { o.TrustedProxies = append(o.TrustedProxies, prefixes...) }
}

// WithDocs serves the customer documentation site at /v0/docs.
func WithDocs(site *docs.Site) RouterOption {
	return func(o *RouterOptions) { o.Docs = site }
}

// WithPlatform serves the platform documentation site at /v0/platform.
func WithPlatform(site *docs.Site) RouterOption {
	return func(o *RouterOptions) { o.Platform = site }
}

// NewRouter returns the HTTP handler for the Lewbox API.
func NewRouter(svc *service.Service, log *slog.Logger, options ...RouterOption) http.Handler {
	var opts RouterOptions
	for _, option := range options {
		option(&opts)
	}
	mux := http.NewServeMux()

	mux.HandleFunc("GET "+Prefix+"/health", func(w http.ResponseWriter, r *http.Request) {
		resp, err := svc.Health(r.Context())
		respond(w, log, http.StatusOK, resp, err)
	})

	mux.HandleFunc("GET "+Prefix+"/instance", func(w http.ResponseWriter, r *http.Request) {
		resp, err := svc.Instance(r.Context())
		respond(w, log, http.StatusOK, resp, err)
	})

	mux.HandleFunc("POST "+Prefix+"/auth/login", func(w http.ResponseWriter, r *http.Request) {
		var req service.LoginRequest
		if err := decodeJSON(w, r, &req); err != nil {
			respond(w, log, 0, nil, err)
			return
		}
		resp, err := svc.Login(r.Context(), req)
		respond(w, log, http.StatusOK, resp, err)
	})

	mux.HandleFunc("POST "+Prefix+"/auth/session", func(w http.ResponseWriter, r *http.Request) {
		var req service.LoginRequest
		if err := decodeJSON(w, r, &req); err != nil {
			respond(w, log, 0, nil, err)
			return
		}
		token, resp, err := svc.WebLogin(r.Context(), req)
		if err != nil {
			respond(w, log, 0, nil, err)
			return
		}
		setSessionCookie(w, r, opts, token, resp.ExpiresAt)
		respond(w, log, http.StatusOK, resp, nil)
	})

	mux.HandleFunc("POST "+Prefix+"/auth/signup", func(w http.ResponseWriter, r *http.Request) {
		var req service.SignupRequest
		if err := decodeJSON(w, r, &req); err != nil {
			respond(w, log, 0, nil, err)
			return
		}
		token, resp, err := svc.Signup(r.Context(), req)
		if err != nil {
			respond(w, log, 0, nil, err)
			return
		}
		setSessionCookie(w, r, opts, token, resp.ExpiresAt)
		respond(w, log, http.StatusOK, resp, nil)
	})

	mux.HandleFunc("POST "+Prefix+"/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		// Clear the cookie even when the session is already gone, so a stale
		// browser cookie never keeps a request looking signed in.
		clearSessionCookie(w, r, opts)
		resp, err := svc.Logout(r.Context())
		respond(w, log, http.StatusOK, resp, err)
	})

	// A setup link's public page is HTTP-only, like sign-up.
	mux.HandleFunc("GET "+Prefix+"/setup/{token}", func(w http.ResponseWriter, r *http.Request) {
		resp, err := svc.SetupLinkInfo(r.Context(), r.PathValue("token"))
		respond(w, log, http.StatusOK, resp, err)
	})

	// Setup links: instance admins only.
	mux.HandleFunc("GET "+Prefix+"/setup-links", func(w http.ResponseWriter, r *http.Request) {
		resp, err := svc.ListSetupLinks(r.Context())
		respond(w, log, http.StatusOK, resp, err)
	})

	mux.HandleFunc("POST "+Prefix+"/setup-links", func(w http.ResponseWriter, r *http.Request) {
		var req service.CreateSetupLinkRequest
		if err := decodeJSON(w, r, &req); err != nil {
			respond(w, log, 0, nil, err)
			return
		}
		resp, err := svc.CreateSetupLink(r.Context(), req)
		respond(w, log, http.StatusCreated, resp, err)
	})

	// {id} is the setup link's ID.
	mux.HandleFunc("DELETE "+Prefix+"/setup-links/{id}", func(w http.ResponseWriter, r *http.Request) {
		resp, err := svc.RevokeSetupLink(r.Context(), r.PathValue("id"))
		respond(w, log, http.StatusOK, resp, err)
	})

	mux.HandleFunc("GET "+Prefix+"/me", func(w http.ResponseWriter, r *http.Request) {
		resp, err := svc.Me(r.Context())
		respond(w, log, http.StatusOK, resp, err)
	})

	mux.HandleFunc("GET "+Prefix+"/users", func(w http.ResponseWriter, r *http.Request) {
		resp, err := svc.ListUsers(r.Context())
		respond(w, log, http.StatusOK, resp, err)
	})

	mux.HandleFunc("POST "+Prefix+"/users", func(w http.ResponseWriter, r *http.Request) {
		var req service.CreateUserRequest
		if err := decodeJSON(w, r, &req); err != nil {
			respond(w, log, 0, nil, err)
			return
		}
		resp, err := svc.CreateUser(r.Context(), req)
		respond(w, log, http.StatusCreated, resp, err)
	})

	// {ref} is a user ID or email.
	mux.HandleFunc("GET "+Prefix+"/users/{ref}", func(w http.ResponseWriter, r *http.Request) {
		resp, err := svc.GetUser(r.Context(), r.PathValue("ref"))
		respond(w, log, http.StatusOK, resp, err)
	})

	mux.HandleFunc("DELETE "+Prefix+"/users/{ref}", func(w http.ResponseWriter, r *http.Request) {
		resp, err := svc.RemoveUser(r.Context(), r.PathValue("ref"))
		respond(w, log, http.StatusOK, resp, err)
	})

	mux.HandleFunc("POST "+Prefix+"/users/{ref}/disable", func(w http.ResponseWriter, r *http.Request) {
		resp, err := svc.SetUserDisabled(r.Context(), r.PathValue("ref"), true)
		respond(w, log, http.StatusOK, resp, err)
	})

	mux.HandleFunc("POST "+Prefix+"/users/{ref}/enable", func(w http.ResponseWriter, r *http.Request) {
		resp, err := svc.SetUserDisabled(r.Context(), r.PathValue("ref"), false)
		respond(w, log, http.StatusOK, resp, err)
	})

	mux.HandleFunc("PUT "+Prefix+"/users/{ref}/password", func(w http.ResponseWriter, r *http.Request) {
		var req service.SetPasswordRequest
		if err := decodeJSON(w, r, &req); err != nil {
			respond(w, log, 0, nil, err)
			return
		}
		resp, err := svc.SetPassword(r.Context(), r.PathValue("ref"), req)
		respond(w, log, http.StatusOK, resp, err)
	})

	mux.HandleFunc("GET "+Prefix+"/clusters", func(w http.ResponseWriter, r *http.Request) {
		resp, err := svc.ListClusters(r.Context())
		respond(w, log, http.StatusOK, resp, err)
	})

	mux.HandleFunc("POST "+Prefix+"/clusters", func(w http.ResponseWriter, r *http.Request) {
		var req service.AddClusterRequest
		if err := decodeJSON(w, r, &req); err != nil {
			respond(w, log, 0, nil, err)
			return
		}
		resp, err := svc.AddCluster(r.Context(), req)
		respond(w, log, http.StatusCreated, resp, err)
	})

	// The ref query parameter may contain "/" for the cluster/<x> and
	// node/<x> forms.
	mux.HandleFunc("GET "+Prefix+"/clusters/resolve", func(w http.ResponseWriter, r *http.Request) {
		resp, err := svc.ResolveCluster(r.Context(), r.URL.Query().Get("ref"))
		respond(w, log, http.StatusOK, resp, err)
	})

	// {ref} is a cluster ID or slug.
	mux.HandleFunc("GET "+Prefix+"/clusters/{ref}", func(w http.ResponseWriter, r *http.Request) {
		resp, err := svc.GetCluster(r.Context(), r.PathValue("ref"))
		respond(w, log, http.StatusOK, resp, err)
	})

	mux.HandleFunc("PATCH "+Prefix+"/clusters/{ref}", func(w http.ResponseWriter, r *http.Request) {
		var req service.UpdateClusterRequest
		if err := decodeJSON(w, r, &req); err != nil {
			respond(w, log, 0, nil, err)
			return
		}
		resp, err := svc.UpdateCluster(r.Context(), r.PathValue("ref"), req)
		respond(w, log, http.StatusOK, resp, err)
	})

	mux.HandleFunc("DELETE "+Prefix+"/clusters/{ref}", func(w http.ResponseWriter, r *http.Request) {
		force, err := queryBool(r, "force")
		if err != nil {
			respond(w, log, 0, nil, err)
			return
		}
		resp, err := svc.RemoveCluster(r.Context(), r.PathValue("ref"), force)
		respond(w, log, http.StatusOK, resp, err)
	})

	mux.HandleFunc("POST "+Prefix+"/clusters/{ref}/check", func(w http.ResponseWriter, r *http.Request) {
		resp, err := svc.CheckCluster(r.Context(), r.PathValue("ref"))
		respond(w, log, http.StatusOK, resp, err)
	})

	mux.HandleFunc("GET "+Prefix+"/clusters/{ref}/health", func(w http.ResponseWriter, r *http.Request) {
		days := 0
		if raw := r.URL.Query().Get("days"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil {
				respond(w, log, 0, nil, &service.Error{Code: service.CodeInvalid, Message: "days must be a number"})
				return
			}
			days = parsed
		}
		resp, err := svc.ClusterHealth(r.Context(), r.PathValue("ref"), days)
		respond(w, log, http.StatusOK, resp, err)
	})

	mux.HandleFunc("GET "+Prefix+"/clusters/{ref}/metrics", func(w http.ResponseWriter, r *http.Request) {
		resp, err := svc.ClusterMetrics(r.Context(), r.PathValue("ref"), r.URL.Query().Get("range"))
		respond(w, log, http.StatusOK, resp, err)
	})

	mux.HandleFunc("GET "+Prefix+"/overview", func(w http.ResponseWriter, r *http.Request) {
		resp, err := svc.Overview(r.Context())
		respond(w, log, http.StatusOK, resp, err)
	})

	mux.HandleFunc("POST "+Prefix+"/clusters/{ref}/join-tokens", func(w http.ResponseWriter, r *http.Request) {
		var req service.JoinTokenRequest
		if err := decodeJSON(w, r, &req); err != nil {
			respond(w, log, 0, nil, err)
			return
		}
		resp, err := svc.CreateJoinToken(r.Context(), r.PathValue("ref"), req)
		respond(w, log, http.StatusCreated, resp, err)
	})

	mux.HandleFunc("POST "+Prefix+"/nodes", func(w http.ResponseWriter, r *http.Request) {
		var req service.AddNodeRequest
		if err := decodeJSON(w, r, &req); err != nil {
			respond(w, log, 0, nil, err)
			return
		}
		resp, err := svc.AddNode(r.Context(), req)
		respond(w, log, http.StatusCreated, resp, err)
	})

	mux.HandleFunc("GET "+Prefix+"/nodes", func(w http.ResponseWriter, r *http.Request) {
		resp, err := svc.ListNodes(r.Context(), service.ListNodesRequest{Cluster: r.URL.Query().Get("cluster")})
		respond(w, log, http.StatusOK, resp, err)
	})

	// {ref} is a node ID or name.
	mux.HandleFunc("GET "+Prefix+"/nodes/{ref}", func(w http.ResponseWriter, r *http.Request) {
		resp, err := svc.GetNode(r.Context(), r.PathValue("ref"))
		respond(w, log, http.StatusOK, resp, err)
	})

	mux.HandleFunc("DELETE "+Prefix+"/nodes/{ref}", func(w http.ResponseWriter, r *http.Request) {
		force, err := queryBool(r, "force")
		if err != nil {
			respond(w, log, 0, nil, err)
			return
		}
		resp, err := svc.RemoveNode(r.Context(), r.PathValue("ref"), service.RemoveNodeRequest{
			Force: force,
		})
		respond(w, log, http.StatusOK, resp, err)
	})

	mux.HandleFunc("GET "+Prefix+"/regions", func(w http.ResponseWriter, r *http.Request) {
		resp, err := svc.ListRegions(r.Context(), r.Header.Get(OrganizationHeader))
		respond(w, log, http.StatusOK, resp, err)
	})

	mux.HandleFunc("GET "+Prefix+"/settings", func(w http.ResponseWriter, r *http.Request) {
		resp, err := svc.GetSettings(r.Context())
		respond(w, log, http.StatusOK, resp, err)
	})

	mux.HandleFunc("PATCH "+Prefix+"/settings", func(w http.ResponseWriter, r *http.Request) {
		var req service.UpdateSettingsRequest
		if err := decodeJSON(w, r, &req); err != nil {
			respond(w, log, 0, nil, err)
			return
		}
		resp, err := svc.UpdateSettings(r.Context(), req)
		respond(w, log, http.StatusOK, resp, err)
	})

	mux.HandleFunc("GET "+Prefix+"/plans", func(w http.ResponseWriter, r *http.Request) {
		resp, err := svc.ListPlans(r.Context())
		respond(w, log, http.StatusOK, resp, err)
	})

	mux.HandleFunc("POST "+Prefix+"/plans", func(w http.ResponseWriter, r *http.Request) {
		var req service.CreatePlanRequest
		if err := decodeJSON(w, r, &req); err != nil {
			respond(w, log, 0, nil, err)
			return
		}
		resp, err := svc.CreatePlan(r.Context(), req)
		respond(w, log, http.StatusCreated, resp, err)
	})

	// {ref} is a plan ID or slug.
	mux.HandleFunc("GET "+Prefix+"/plans/{ref}", func(w http.ResponseWriter, r *http.Request) {
		resp, err := svc.GetPlan(r.Context(), r.PathValue("ref"))
		respond(w, log, http.StatusOK, resp, err)
	})

	mux.HandleFunc("PATCH "+Prefix+"/plans/{ref}", func(w http.ResponseWriter, r *http.Request) {
		var req service.UpdatePlanRequest
		if err := decodeJSON(w, r, &req); err != nil {
			respond(w, log, 0, nil, err)
			return
		}
		resp, err := svc.UpdatePlan(r.Context(), r.PathValue("ref"), req)
		respond(w, log, http.StatusOK, resp, err)
	})

	mux.HandleFunc("DELETE "+Prefix+"/plans/{ref}", func(w http.ResponseWriter, r *http.Request) {
		resp, err := svc.DeletePlan(r.Context(), r.PathValue("ref"))
		respond(w, log, http.StatusOK, resp, err)
	})

	// DNS profiles are saved Cloudflare credentials. {ref} is an ID or name.
	mux.HandleFunc("GET "+Prefix+"/dns-profiles", func(w http.ResponseWriter, r *http.Request) {
		resp, err := svc.ListDNSProfiles(r.Context())
		respond(w, log, http.StatusOK, resp, err)
	})

	mux.HandleFunc("POST "+Prefix+"/dns-profiles", func(w http.ResponseWriter, r *http.Request) {
		var req service.CreateDNSProfileRequest
		if err := decodeJSON(w, r, &req); err != nil {
			respond(w, log, 0, nil, err)
			return
		}
		resp, err := svc.CreateDNSProfile(r.Context(), req)
		respond(w, log, http.StatusCreated, resp, err)
	})

	mux.HandleFunc("GET "+Prefix+"/dns-profiles/{ref}", func(w http.ResponseWriter, r *http.Request) {
		resp, err := svc.GetDNSProfile(r.Context(), r.PathValue("ref"))
		respond(w, log, http.StatusOK, resp, err)
	})

	mux.HandleFunc("PATCH "+Prefix+"/dns-profiles/{ref}", func(w http.ResponseWriter, r *http.Request) {
		var req service.UpdateDNSProfileRequest
		if err := decodeJSON(w, r, &req); err != nil {
			respond(w, log, 0, nil, err)
			return
		}
		resp, err := svc.UpdateDNSProfile(r.Context(), r.PathValue("ref"), req)
		respond(w, log, http.StatusOK, resp, err)
	})

	mux.HandleFunc("DELETE "+Prefix+"/dns-profiles/{ref}", func(w http.ResponseWriter, r *http.Request) {
		resp, err := svc.DeleteDNSProfile(r.Context(), r.PathValue("ref"))
		respond(w, log, http.StatusOK, resp, err)
	})

	mux.HandleFunc("POST "+Prefix+"/dns-profiles/{ref}/test", func(w http.ResponseWriter, r *http.Request) {
		resp, err := svc.TestDNSProfile(r.Context(), r.PathValue("ref"))
		respond(w, log, http.StatusOK, resp, err)
	})

	mux.HandleFunc("POST "+Prefix+"/dns-profiles/{ref}/credentials", func(w http.ResponseWriter, r *http.Request) {
		resp, err := svc.DNSProfileCredentials(r.Context(), r.PathValue("ref"))
		respond(w, log, http.StatusOK, resp, err)
	})

	// Storage profiles are saved S3-compatible bucket credentials. {ref} is
	// an ID or name.
	mux.HandleFunc("GET "+Prefix+"/storage-profiles", func(w http.ResponseWriter, r *http.Request) {
		resp, err := svc.ListStorageProfiles(r.Context())
		respond(w, log, http.StatusOK, resp, err)
	})

	mux.HandleFunc("POST "+Prefix+"/storage-profiles", func(w http.ResponseWriter, r *http.Request) {
		var req service.CreateStorageProfileRequest
		if err := decodeJSON(w, r, &req); err != nil {
			respond(w, log, 0, nil, err)
			return
		}
		resp, err := svc.CreateStorageProfile(r.Context(), req)
		respond(w, log, http.StatusCreated, resp, err)
	})

	mux.HandleFunc("GET "+Prefix+"/storage-profiles/{ref}", func(w http.ResponseWriter, r *http.Request) {
		resp, err := svc.GetStorageProfile(r.Context(), r.PathValue("ref"))
		respond(w, log, http.StatusOK, resp, err)
	})

	mux.HandleFunc("PATCH "+Prefix+"/storage-profiles/{ref}", func(w http.ResponseWriter, r *http.Request) {
		var req service.UpdateStorageProfileRequest
		if err := decodeJSON(w, r, &req); err != nil {
			respond(w, log, 0, nil, err)
			return
		}
		resp, err := svc.UpdateStorageProfile(r.Context(), r.PathValue("ref"), req)
		respond(w, log, http.StatusOK, resp, err)
	})

	mux.HandleFunc("DELETE "+Prefix+"/storage-profiles/{ref}", func(w http.ResponseWriter, r *http.Request) {
		resp, err := svc.DeleteStorageProfile(r.Context(), r.PathValue("ref"))
		respond(w, log, http.StatusOK, resp, err)
	})

	mux.HandleFunc("POST "+Prefix+"/storage-profiles/{ref}/test", func(w http.ResponseWriter, r *http.Request) {
		resp, err := svc.TestStorageProfile(r.Context(), r.PathValue("ref"))
		respond(w, log, http.StatusOK, resp, err)
	})

	mux.HandleFunc("POST "+Prefix+"/storage-profiles/{ref}/credentials", func(w http.ResponseWriter, r *http.Request) {
		resp, err := svc.StorageProfileCredentials(r.Context(), r.PathValue("ref"))
		respond(w, log, http.StatusOK, resp, err)
	})

	// Organizations. The member and admin-key routes act on the organization
	// named by the Lewbox-Organization header (an ID or slug), or on an
	// admin key's own organization when the header is absent.
	mux.HandleFunc("GET "+Prefix+"/organizations", func(w http.ResponseWriter, r *http.Request) {
		resp, err := svc.ListOrganizations(r.Context(), service.ListOrganizationsRequest{
			User:   r.URL.Query().Get("user"),
			Status: r.URL.Query().Get("status"),
		})
		respond(w, log, http.StatusOK, resp, err)
	})

	mux.HandleFunc("POST "+Prefix+"/organizations", func(w http.ResponseWriter, r *http.Request) {
		var req service.CreateOrganizationRequest
		if err := decodeJSON(w, r, &req); err != nil {
			respond(w, log, 0, nil, err)
			return
		}
		resp, err := svc.CreateOrganization(r.Context(), req)
		respond(w, log, http.StatusCreated, resp, err)
	})

	// {ref} is an organization ID or slug.
	mux.HandleFunc("GET "+Prefix+"/organizations/{ref}", func(w http.ResponseWriter, r *http.Request) {
		resp, err := svc.GetOrganization(r.Context(), r.PathValue("ref"))
		respond(w, log, http.StatusOK, resp, err)
	})

	mux.HandleFunc("PATCH "+Prefix+"/organizations/{ref}", func(w http.ResponseWriter, r *http.Request) {
		var req service.UpdateOrganizationRequest
		if err := decodeJSON(w, r, &req); err != nil {
			respond(w, log, 0, nil, err)
			return
		}
		resp, err := svc.UpdateOrganization(r.Context(), r.PathValue("ref"), req)
		respond(w, log, http.StatusOK, resp, err)
	})

	mux.HandleFunc("GET "+Prefix+"/organization", func(w http.ResponseWriter, r *http.Request) {
		resp, err := svc.GetOrganization(r.Context(), r.Header.Get(OrganizationHeader))
		respond(w, log, http.StatusOK, resp, err)
	})

	mux.HandleFunc("GET "+Prefix+"/members", func(w http.ResponseWriter, r *http.Request) {
		resp, err := svc.ListMembers(r.Context(), r.Header.Get(OrganizationHeader))
		respond(w, log, http.StatusOK, resp, err)
	})

	mux.HandleFunc("POST "+Prefix+"/members", func(w http.ResponseWriter, r *http.Request) {
		var req service.AddMemberRequest
		if err := decodeJSON(w, r, &req); err != nil {
			respond(w, log, 0, nil, err)
			return
		}
		resp, err := svc.AddMember(r.Context(), r.Header.Get(OrganizationHeader), req)
		respond(w, log, http.StatusCreated, resp, err)
	})

	// {ref} is a membership ID, an account ID or an email.
	mux.HandleFunc("PATCH "+Prefix+"/members/{ref}", func(w http.ResponseWriter, r *http.Request) {
		var req service.SetMemberRoleRequest
		if err := decodeJSON(w, r, &req); err != nil {
			respond(w, log, 0, nil, err)
			return
		}
		resp, err := svc.SetMemberRole(r.Context(), r.Header.Get(OrganizationHeader), r.PathValue("ref"), req)
		respond(w, log, http.StatusOK, resp, err)
	})

	mux.HandleFunc("DELETE "+Prefix+"/members/{ref}", func(w http.ResponseWriter, r *http.Request) {
		resp, err := svc.RemoveMember(r.Context(), r.Header.Get(OrganizationHeader), r.PathValue("ref"))
		respond(w, log, http.StatusOK, resp, err)
	})

	mux.HandleFunc("GET "+Prefix+"/admin-keys", func(w http.ResponseWriter, r *http.Request) {
		resp, err := svc.ListAdminKeys(r.Context(), r.Header.Get(OrganizationHeader))
		respond(w, log, http.StatusOK, resp, err)
	})

	mux.HandleFunc("POST "+Prefix+"/admin-keys", func(w http.ResponseWriter, r *http.Request) {
		var req service.CreateAdminKeyRequest
		if err := decodeJSON(w, r, &req); err != nil {
			respond(w, log, 0, nil, err)
			return
		}
		resp, err := svc.CreateAdminKey(r.Context(), r.Header.Get(OrganizationHeader), req)
		respond(w, log, http.StatusCreated, resp, err)
	})

	// {ref} is a key ID or the name of a live key.
	mux.HandleFunc("DELETE "+Prefix+"/admin-keys/{ref}", func(w http.ResponseWriter, r *http.Request) {
		resp, err := svc.RevokeAdminKey(r.Context(), r.Header.Get(OrganizationHeader), r.PathValue("ref"))
		respond(w, log, http.StatusOK, resp, err)
	})

	mux.HandleFunc("GET "+Prefix+"/sandbox-keys", func(w http.ResponseWriter, r *http.Request) {
		resp, err := svc.ListSandboxKeys(r.Context(), r.Header.Get(OrganizationHeader))
		respond(w, log, http.StatusOK, resp, err)
	})

	mux.HandleFunc("POST "+Prefix+"/sandbox-keys", func(w http.ResponseWriter, r *http.Request) {
		var req service.CreateSandboxKeyRequest
		if err := decodeJSON(w, r, &req); err != nil {
			respond(w, log, 0, nil, err)
			return
		}
		resp, err := svc.CreateSandboxKey(r.Context(), r.Header.Get(OrganizationHeader), req)
		respond(w, log, http.StatusCreated, resp, err)
	})

	// {ref} is a key ID or the name of a live key.
	mux.HandleFunc("DELETE "+Prefix+"/sandbox-keys/{ref}", func(w http.ResponseWriter, r *http.Request) {
		resp, err := svc.RevokeSandboxKey(r.Context(), r.Header.Get(OrganizationHeader), r.PathValue("ref"))
		respond(w, log, http.StatusOK, resp, err)
	})

	mux.HandleFunc("GET "+Prefix+"/sandboxes", func(w http.ResponseWriter, r *http.Request) {
		resp, err := svc.ListSandboxes(r.Context(), service.ListSandboxesRequest{
			Region: r.URL.Query().Get("region"),
			Status: r.URL.Query().Get("status"),
			Org:    requestOrganization(r),
		})
		respond(w, log, http.StatusOK, resp, err)
	})

	mux.HandleFunc("POST "+Prefix+"/sandboxes/{id}/stop", func(w http.ResponseWriter, r *http.Request) {
		resp, err := svc.StopSandbox(r.Context(), r.PathValue("id"), service.RegionScope{
			Region: r.URL.Query().Get("region"),
			Org:    requestOrganization(r),
		})
		respond(w, log, http.StatusOK, resp, err)
	})

	mux.HandleFunc("DELETE "+Prefix+"/sandboxes/{id}", func(w http.ResponseWriter, r *http.Request) {
		resp, err := svc.DeleteSandbox(r.Context(), r.PathValue("id"), service.RegionScope{
			Region: r.URL.Query().Get("region"),
			Org:    requestOrganization(r),
		})
		respond(w, log, http.StatusOK, resp, err)
	})

	mux.HandleFunc("GET "+Prefix+"/snapshots", func(w http.ResponseWriter, r *http.Request) {
		resp, err := svc.ListSnapshots(r.Context(), service.RegionScope{
			Region: r.URL.Query().Get("region"),
			Org:    requestOrganization(r),
		})
		respond(w, log, http.StatusOK, resp, err)
	})

	// {name} is the snapshot's name, which is unique per workspace.
	mux.HandleFunc("DELETE "+Prefix+"/snapshots/{name}", func(w http.ResponseWriter, r *http.Request) {
		resp, err := svc.DeleteSnapshot(r.Context(), r.PathValue("name"), service.RegionScope{
			Region: r.URL.Query().Get("region"),
			Org:    requestOrganization(r),
		})
		respond(w, log, http.StatusOK, resp, err)
	})

	mux.HandleFunc("GET "+Prefix+"/usage", func(w http.ResponseWriter, r *http.Request) {
		resp, err := svc.Usage(r.Context(), service.UsageRequest{
			From:        r.URL.Query().Get("from"),
			To:          r.URL.Query().Get("to"),
			Granularity: r.URL.Query().Get("granularity"),
			Org:         requestOrganization(r),
		})
		respond(w, log, http.StatusOK, resp, err)
	})

	mux.HandleFunc("GET "+Prefix+"/limits", func(w http.ResponseWriter, r *http.Request) {
		resp, err := svc.Limits(r.Context(), r.Header.Get(OrganizationHeader))
		respond(w, log, http.StatusOK, resp, err)
	})

	// Subscriptions are the operator's assignment of a plan in one region.
	mux.HandleFunc("GET "+Prefix+"/subscriptions", func(w http.ResponseWriter, r *http.Request) {
		resp, err := svc.ListSubscriptions(r.Context(), r.Header.Get(OrganizationHeader))
		respond(w, log, http.StatusOK, resp, err)
	})

	// {region} is a cluster ID or slug.
	mux.HandleFunc("PUT "+Prefix+"/subscriptions/{region}", func(w http.ResponseWriter, r *http.Request) {
		var req service.SetSubscriptionRequest
		if err := decodeJSON(w, r, &req); err != nil {
			respond(w, log, 0, nil, err)
			return
		}
		resp, err := svc.SetSubscription(r.Context(), r.Header.Get(OrganizationHeader), r.PathValue("region"), req)
		respond(w, log, http.StatusOK, resp, err)
	})

	mux.HandleFunc("DELETE "+Prefix+"/subscriptions/{region}", func(w http.ResponseWriter, r *http.Request) {
		force, err := queryBool(r, "force")
		if err != nil {
			respond(w, log, 0, nil, err)
			return
		}
		resp, err := svc.DeleteSubscription(r.Context(), r.Header.Get(OrganizationHeader), r.PathValue("region"), force)
		respond(w, log, http.StatusOK, resp, err)
	})

	// Docs are static content and public, so they are an HTTP-only exception
	// to the two-entry-points rule. The consumer book is at /docs and the
	// platform book at /platform.
	serveDocs(mux, Prefix+"/docs", opts.Docs)
	serveDocs(mux, Prefix+"/platform", opts.Platform)

	// Anything else gets the standard error shape instead of a text 404.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, ErrorBody{Error: ErrorDetail{
			Code:    string(service.CodeNotFound),
			Message: "no route for " + r.Method + " " + r.URL.Path,
		}})
	})

	cp := http.NewCrossOriginProtection()
	for _, origin := range opts.TrustedOrigins {
		if err := cp.AddTrustedOrigin(origin); err != nil {
			log.Warn("ignoring invalid trusted origin", "origin", origin, "err", err)
		}
	}
	cp.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusForbidden, ErrorBody{Error: ErrorDetail{
			Code:    string(service.CodeForbidden),
			Message: "cross-origin request rejected",
		}})
	}))

	handler := withClientIP(withAuth(svc, log, opts, mux), opts.TrustedProxies)
	return withNoStore(cp.Handler(handler))
}

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
			next.ServeHTTP(w, r.WithContext(service.WithPrincipal(r.Context(), p)))
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
				Code:    string(service.CodeInvalid),
				Message: message,
			}})
			return
		}
		next.ServeHTTP(w, r.WithContext(service.WithPrincipal(r.Context(), p)))
	})
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

func respond(w http.ResponseWriter, log *slog.Logger, status int, v any, err error) {
	if err != nil {
		status, body := ErrorFrom(err)
		if status == http.StatusInternalServerError {
			log.Error("request failed", "err", err)
		}
		if status == http.StatusTooManyRequests {
			if seconds, ok := retryAfterSeconds(body.Error.Details); ok {
				w.Header().Set("Retry-After", strconv.Itoa(seconds))
			}
		}
		writeJSON(w, status, body)
		return
	}
	writeJSON(w, status, v)
}

// retryAfterSeconds reads the retry_after_seconds detail of a rate_limited
// error.
func retryAfterSeconds(details any) (int, bool) {
	m, ok := details.(map[string]any)
	if !ok {
		return 0, false
	}
	switch v := m["retry_after_seconds"].(type) {
	case int:
		return v, v > 0
	case int64:
		return int(v), v > 0
	case float64:
		return int(v), v > 0
	default:
		return 0, false
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return &service.Error{Code: service.CodeInvalid, Message: "invalid JSON body: " + err.Error()}
	}
	return nil
}

// requestOrganization resolves the organization a fleet-capable route acts
// on: the org query parameter when present, otherwise the header.
func requestOrganization(r *http.Request) string {
	if org := strings.TrimSpace(r.URL.Query().Get("org")); org != "" {
		return org
	}
	return r.Header.Get(OrganizationHeader)
}

// queryBool parses an optional boolean query parameter. An empty value is
// false.
func queryBool(r *http.Request, name string) (bool, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return false, nil
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return false, &service.Error{Code: service.CodeInvalid, Message: "invalid " + name + " query parameter: use true or false"}
	}
	return v, nil
}

// ErrorBody is the API's error shape: {error: {code, message, details}}.
type ErrorBody struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail is the inner object of ErrorBody.
type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

// ErrorFrom maps an error from the service to an HTTP status and error
// body. Errors that aren't *service.Error are internal and their text is
// not exposed.
func ErrorFrom(err error) (int, ErrorBody) {
	se, ok := errors.AsType[*service.Error](err)
	if !ok {
		return http.StatusInternalServerError, ErrorBody{Error: ErrorDetail{Code: "internal", Message: "internal error"}}
	}
	status := http.StatusInternalServerError
	switch se.Code {
	case service.CodeInvalid:
		status = http.StatusBadRequest
	case service.CodeUnauthorized:
		status = http.StatusUnauthorized
	case service.CodeForbidden:
		status = http.StatusForbidden
	case service.CodeNotFound:
		status = http.StatusNotFound
	case service.CodeConflict:
		status = http.StatusConflict
	case service.CodePrecondition:
		status = http.StatusPreconditionFailed
	case service.CodeRateLimited:
		status = http.StatusTooManyRequests
	case service.CodeUnavailable:
		status = http.StatusBadGateway
	}
	return status, ErrorBody{Error: ErrorDetail{Code: string(se.Code), Message: se.Message, Details: se.Details}}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = Encode(w, v)
}

// serveDocs registers one documentation book's nav and page routes. A nil
// site answers 404.
func serveDocs(mux *http.ServeMux, base string, site *docs.Site) {
	mux.HandleFunc("GET "+base, func(w http.ResponseWriter, r *http.Request) {
		if site == nil {
			writeJSON(w, http.StatusNotFound, docsUnavailable())
			return
		}
		writeJSON(w, http.StatusOK, site.Nav())
	})

	mux.HandleFunc("GET "+base+"/{path...}", func(w http.ResponseWriter, r *http.Request) {
		if site == nil {
			writeJSON(w, http.StatusNotFound, docsUnavailable())
			return
		}
		requested := r.PathValue("path")
		page, ok := site.Page(requested)
		if !ok {
			writeJSON(w, http.StatusNotFound, ErrorBody{Error: ErrorDetail{
				Code:    string(service.CodeNotFound),
				Message: fmt.Sprintf("no docs page %q", requested),
			}})
			return
		}
		writeJSON(w, http.StatusOK, page)
	})
}

// docsUnavailable is the 404 body when the router has no docs site.
func docsUnavailable() ErrorBody {
	return ErrorBody{Error: ErrorDetail{
		Code:    string(service.CodeNotFound),
		Message: "docs are not available",
	}}
}

// Encode writes v as one line of JSON. The CLI uses it too, so both entry
// points produce byte-identical output.
func Encode(w io.Writer, v any) error {
	return json.NewEncoder(w).Encode(v)
}
