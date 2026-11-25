// Package httpapi exposes internal/service over HTTP.
package httpapi

import (
	"log/slog"
	"net/http"
	"net/netip"

	"github.com/harlew-dev/lewbox/internal/docs"
	"github.com/harlew-dev/lewbox/internal/logsafe"
	"github.com/harlew-dev/lewbox/internal/service"
)

// Prefix is the API version prefix (MVP.md §12).
const Prefix = "/v0"

// OrganizationHeader names the organization a member or admin-key route acts
// on: its ID or slug. An admin key's own organization is the default.
const OrganizationHeader = "Lewbox-Organization"

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

// NewRouter returns the HTTP handler for the Lewbox API. Every route lives in
// the registry below, with the handler that serves it.
func NewRouter(svc *service.Service, log *slog.Logger, options ...RouterOption) http.Handler {
	var opts RouterOptions
	for _, option := range options {
		option(&opts)
	}
	log = logsafe.WrapLogger(log)
	s := &server{svc: svc, log: log, opts: opts}
	mux := http.NewServeMux()

	// Public and auth routes.
	var (
		health   = Health{s}
		instance = Instance{s}
		profile  = Profile{s}
		sessions = Sessions{s}
		signups  = Signups{s}
	)
	mux.HandleFunc("GET "+Prefix+"/health", health.Show)
	mux.HandleFunc("GET "+Prefix+"/instance", instance.Show)
	mux.HandleFunc("POST "+Prefix+"/auth/login", sessions.Login)
	mux.HandleFunc("POST "+Prefix+"/auth/session", sessions.Store)
	mux.HandleFunc("POST "+Prefix+"/auth/signup", signups.Store)
	mux.HandleFunc("POST "+Prefix+"/auth/logout", sessions.Destroy)
	mux.HandleFunc("GET "+Prefix+"/profile", profile.Show)

	// Setup links: instance admins, except the public check.
	var setupLinks = SetupLinks{s}
	mux.HandleFunc("GET "+Prefix+"/setup-links", setupLinks.Index)
	mux.HandleFunc("POST "+Prefix+"/setup-links", setupLinks.Store)
	mux.HandleFunc("POST "+Prefix+"/setup-links/check", setupLinks.Check)
	mux.HandleFunc("DELETE "+Prefix+"/setup-links/{setup_link}", setupLinks.Destroy)

	// Accounts and their access tokens.
	var (
		accessTokens   = AccessTokens{s}
		users          = Users{s}
		userSuspension = UserSuspension{s}
		userPassword   = UserPassword{s}
	)
	mux.HandleFunc("GET "+Prefix+"/access-tokens", accessTokens.Index)
	mux.HandleFunc("POST "+Prefix+"/access-tokens", accessTokens.Store)
	mux.HandleFunc("DELETE "+Prefix+"/access-tokens/{access_token}", accessTokens.Destroy)
	mux.HandleFunc("GET "+Prefix+"/users", users.Index)
	mux.HandleFunc("POST "+Prefix+"/users", users.Store)
	mux.HandleFunc("GET "+Prefix+"/users/{user}", users.Show)
	mux.HandleFunc("DELETE "+Prefix+"/users/{user}", users.Destroy)
	mux.HandleFunc("POST "+Prefix+"/users/{user}/suspension", userSuspension.Store)
	mux.HandleFunc("DELETE "+Prefix+"/users/{user}/suspension", userSuspension.Destroy)
	mux.HandleFunc("PATCH "+Prefix+"/users/{user}/password", userPassword.Update)

	// Clusters, their node settings and join tokens.
	var (
		clusters            = Clusters{s}
		clusterNodeSettings = ClusterNodeSettings{s}
		joinTokens          = JoinTokens{s}
	)
	mux.HandleFunc("GET "+Prefix+"/clusters", clusters.Index)
	mux.HandleFunc("POST "+Prefix+"/clusters", clusters.Store)
	mux.HandleFunc("GET "+Prefix+"/clusters/resolve", clusters.Resolve)
	mux.HandleFunc("GET "+Prefix+"/clusters/{cluster}", clusters.Show)
	mux.HandleFunc("PATCH "+Prefix+"/clusters/{cluster}", clusters.Update)
	mux.HandleFunc("DELETE "+Prefix+"/clusters/{cluster}", clusters.Destroy)
	mux.HandleFunc("POST "+Prefix+"/clusters/{cluster}/check", clusters.Check)
	mux.HandleFunc("GET "+Prefix+"/clusters/{cluster}/health", clusters.Health)
	mux.HandleFunc("GET "+Prefix+"/clusters/{cluster}/metrics", clusters.Metrics)
	mux.HandleFunc("GET "+Prefix+"/clusters/{cluster}/node-settings", clusterNodeSettings.Show)
	mux.HandleFunc("POST "+Prefix+"/clusters/{cluster}/join-tokens", joinTokens.Store)

	// Nodes and what they report.
	var (
		nodes        = Nodes{s}
		nodeSettings = NodeSettings{s}
	)
	mux.HandleFunc("GET "+Prefix+"/nodes", nodes.Index)
	mux.HandleFunc("POST "+Prefix+"/nodes", nodes.Store)
	mux.HandleFunc("GET "+Prefix+"/nodes/{node}", nodes.Show)
	mux.HandleFunc("DELETE "+Prefix+"/nodes/{node}", nodes.Destroy)
	mux.HandleFunc("PATCH "+Prefix+"/nodes/{node}/settings", nodeSettings.Update)

	// Regions, instance settings and the console home.
	var (
		regions  = Regions{s}
		settings = Settings{s}
		overview = Overview{s}
		events   = Events{s}
	)
	mux.HandleFunc("GET "+Prefix+"/regions", regions.Index)
	mux.HandleFunc("GET "+Prefix+"/settings", settings.Show)
	mux.HandleFunc("PATCH "+Prefix+"/settings", settings.Update)
	mux.HandleFunc("GET "+Prefix+"/overview", overview.Show)
	mux.HandleFunc("GET "+Prefix+"/events", events.Index)

	// Plans and the two profile kinds.
	var (
		plans           = Plans{s}
		dnsProfiles     = DNSProfiles{s}
		storageProfiles = StorageProfiles{s}
	)
	mux.HandleFunc("GET "+Prefix+"/plans", plans.Index)
	mux.HandleFunc("POST "+Prefix+"/plans", plans.Store)
	mux.HandleFunc("GET "+Prefix+"/plans/{plan}", plans.Show)
	mux.HandleFunc("PATCH "+Prefix+"/plans/{plan}", plans.Update)
	mux.HandleFunc("DELETE "+Prefix+"/plans/{plan}", plans.Destroy)
	mux.HandleFunc("GET "+Prefix+"/dns-profiles", dnsProfiles.Index)
	mux.HandleFunc("POST "+Prefix+"/dns-profiles", dnsProfiles.Store)
	mux.HandleFunc("GET "+Prefix+"/dns-profiles/{dns_profile}", dnsProfiles.Show)
	mux.HandleFunc("PATCH "+Prefix+"/dns-profiles/{dns_profile}", dnsProfiles.Update)
	mux.HandleFunc("DELETE "+Prefix+"/dns-profiles/{dns_profile}", dnsProfiles.Destroy)
	mux.HandleFunc("POST "+Prefix+"/dns-profiles/{dns_profile}/check", dnsProfiles.Check)
	mux.HandleFunc("POST "+Prefix+"/dns-profiles/{dns_profile}/credentials", dnsProfiles.Credentials)
	mux.HandleFunc("GET "+Prefix+"/storage-profiles", storageProfiles.Index)
	mux.HandleFunc("POST "+Prefix+"/storage-profiles", storageProfiles.Store)
	mux.HandleFunc("GET "+Prefix+"/storage-profiles/{storage_profile}", storageProfiles.Show)
	mux.HandleFunc("PATCH "+Prefix+"/storage-profiles/{storage_profile}", storageProfiles.Update)
	mux.HandleFunc("DELETE "+Prefix+"/storage-profiles/{storage_profile}", storageProfiles.Destroy)
	mux.HandleFunc("POST "+Prefix+"/storage-profiles/{storage_profile}/check", storageProfiles.Check)
	mux.HandleFunc("POST "+Prefix+"/storage-profiles/{storage_profile}/credentials", storageProfiles.Credentials)

	// Organizations, their members and keys.
	var (
		organizations = Organizations{s}
		members       = Members{s}
		adminKeys     = AdminKeys{s}
		sandboxKeys   = SandboxKeys{s}
	)
	mux.HandleFunc("GET "+Prefix+"/organizations", organizations.Index)
	mux.HandleFunc("POST "+Prefix+"/organizations", organizations.Store)
	mux.HandleFunc("GET "+Prefix+"/organization", organizations.Current)
	mux.HandleFunc("GET "+Prefix+"/organizations/{organization}", organizations.Show)
	mux.HandleFunc("PATCH "+Prefix+"/organizations/{organization}", organizations.Update)
	mux.HandleFunc("GET "+Prefix+"/members", members.Index)
	mux.HandleFunc("POST "+Prefix+"/members", members.Store)
	mux.HandleFunc("PATCH "+Prefix+"/members/{member}", members.Update)
	mux.HandleFunc("DELETE "+Prefix+"/members/{member}", members.Destroy)
	mux.HandleFunc("GET "+Prefix+"/admin-keys", adminKeys.Index)
	mux.HandleFunc("POST "+Prefix+"/admin-keys", adminKeys.Store)
	mux.HandleFunc("DELETE "+Prefix+"/admin-keys/{admin_key}", adminKeys.Destroy)
	mux.HandleFunc("GET "+Prefix+"/sandbox-keys", sandboxKeys.Index)
	mux.HandleFunc("POST "+Prefix+"/sandbox-keys", sandboxKeys.Store)
	mux.HandleFunc("DELETE "+Prefix+"/sandbox-keys/{sandbox_key}", sandboxKeys.Destroy)

	// Sandboxes, snapshots, archives, usage and subscriptions.
	var (
		sandboxes     = Sandboxes{s}
		snapshots     = Snapshots{s}
		archives      = Archives{s}
		usage         = Usage{s}
		limits        = Limits{s}
		subscriptions = Subscriptions{s}
	)
	mux.HandleFunc("GET "+Prefix+"/sandboxes", sandboxes.Index)
	mux.HandleFunc("POST "+Prefix+"/sandboxes/{sandbox}/stop", sandboxes.Stop)
	mux.HandleFunc("DELETE "+Prefix+"/sandboxes/{sandbox}", sandboxes.Destroy)
	mux.HandleFunc("GET "+Prefix+"/snapshots", snapshots.Index)
	mux.HandleFunc("DELETE "+Prefix+"/snapshots/{snapshot}", snapshots.Destroy)
	mux.HandleFunc("GET "+Prefix+"/archives/{archive}", archives.Show)
	mux.HandleFunc("POST "+Prefix+"/archives/{archive}/restore", archives.Restore)
	mux.HandleFunc("DELETE "+Prefix+"/archives/{archive}", archives.Destroy)
	mux.HandleFunc("GET "+Prefix+"/usage", usage.Show)
	mux.HandleFunc("GET "+Prefix+"/limits", limits.Show)
	mux.HandleFunc("GET "+Prefix+"/subscriptions", subscriptions.Index)
	mux.HandleFunc("PATCH "+Prefix+"/subscriptions/{region}", subscriptions.Update)
	mux.HandleFunc("DELETE "+Prefix+"/subscriptions/{region}", subscriptions.Destroy)

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

	// Routing guards sit outside auth: a wrong method or a bad query never
	// needs a session to be refused.
	routed := withMethodNotAllowed(withQueryAllowList(mux), mux)
	handler := withClientIP(withAuth(svc, log, opts, routed), opts.TrustedProxies)
	handler = cp.Handler(handler)
	handler = withRecovery(handler, log)
	handler = withAccessLog(handler, mux, log)
	handler = withNoStore(handler)
	return withRequestID(handler)
}
