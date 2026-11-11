// Package client implements backend.Backend over the Lewbox HTTP API.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/harlew-dev/lewbox/internal/backend"
	"github.com/harlew-dev/lewbox/internal/service"
)

const (
	// defaultTimeout bounds ordinary calls.
	defaultTimeout = 30 * time.Second
	// longTimeout bounds the calls that talk to AerolVM.
	longTimeout = 120 * time.Second
)

// Client calls a Lewbox server. It implements backend.Backend.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

var _ backend.Backend = (*Client)(nil)

// New returns a client for baseURL that sends token as a bearer credential.
// A nil http.Client gets a default one; timeouts are applied per call.
func New(baseURL, token string, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{}
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), token: token, http: hc}
}

// Health reports whether the server is up.
func (c *Client) Health(ctx context.Context) (service.HealthResponse, error) {
	var out service.HealthResponse
	err := c.do(ctx, http.MethodGet, "/v0/health", nil, &out, defaultTimeout)
	return out, err
}

// Login checks an email and password and starts a session.
func (c *Client) Login(ctx context.Context, req service.LoginRequest) (service.LoginResponse, error) {
	var out service.LoginResponse
	err := c.do(ctx, http.MethodPost, "/v0/auth/login", req, &out, defaultTimeout)
	return out, err
}

// Logout ends the caller's session.
func (c *Client) Logout(ctx context.Context) (service.LogoutResponse, error) {
	var out service.LogoutResponse
	err := c.do(ctx, http.MethodPost, "/v0/auth/logout", nil, &out, defaultTimeout)
	return out, err
}

// Me describes the caller.
func (c *Client) Me(ctx context.Context) (service.Me, error) {
	var out service.Me
	err := c.do(ctx, http.MethodGet, "/v0/me", nil, &out, defaultTimeout)
	return out, err
}

// CreateUser creates an account.
func (c *Client) CreateUser(ctx context.Context, req service.CreateUserRequest) (service.User, error) {
	var out service.User
	err := c.do(ctx, http.MethodPost, "/v0/users", req, &out, defaultTimeout)
	return out, err
}

// ListUsers returns every account.
func (c *Client) ListUsers(ctx context.Context) (service.UserList, error) {
	var out service.UserList
	err := c.do(ctx, http.MethodGet, "/v0/users", nil, &out, defaultTimeout)
	return out, err
}

// GetUser returns an account by ID or email.
func (c *Client) GetUser(ctx context.Context, ref string) (service.User, error) {
	var out service.User
	err := c.do(ctx, http.MethodGet, "/v0/users/"+url.PathEscape(ref), nil, &out, defaultTimeout)
	return out, err
}

// RemoveUser removes an account.
func (c *Client) RemoveUser(ctx context.Context, ref string) (service.User, error) {
	var out service.User
	err := c.do(ctx, http.MethodDelete, "/v0/users/"+url.PathEscape(ref), nil, &out, defaultTimeout)
	return out, err
}

// SetUserDisabled blocks or allows sign-in.
func (c *Client) SetUserDisabled(ctx context.Context, ref string, disabled bool) (service.User, error) {
	action := "enable"
	if disabled {
		action = "disable"
	}
	var out service.User
	err := c.do(ctx, http.MethodPost, "/v0/users/"+url.PathEscape(ref)+"/"+action, nil, &out, defaultTimeout)
	return out, err
}

// SetPassword changes an account's password.
func (c *Client) SetPassword(ctx context.Context, ref string, req service.SetPasswordRequest) (service.User, error) {
	var out service.User
	err := c.do(ctx, http.MethodPut, "/v0/users/"+url.PathEscape(ref)+"/password", req, &out, defaultTimeout)
	return out, err
}

// AddCluster registers and validates a cluster.
func (c *Client) AddCluster(ctx context.Context, req service.AddClusterRequest) (service.ClusterDetail, error) {
	var out service.ClusterDetail
	err := c.do(ctx, http.MethodPost, "/v0/clusters", req, &out, longTimeout)
	return out, err
}

// ListClusters returns every cluster.
func (c *Client) ListClusters(ctx context.Context) (service.ClusterList, error) {
	var out service.ClusterList
	err := c.do(ctx, http.MethodGet, "/v0/clusters", nil, &out, defaultTimeout)
	return out, err
}

// GetCluster returns a cluster by ID or slug.
func (c *Client) GetCluster(ctx context.Context, ref string) (service.ClusterDetail, error) {
	var out service.ClusterDetail
	err := c.do(ctx, http.MethodGet, "/v0/clusters/"+url.PathEscape(ref), nil, &out, defaultTimeout)
	return out, err
}

// CheckCluster runs the cluster checks now.
func (c *Client) CheckCluster(ctx context.Context, ref string) (service.ClusterDetail, error) {
	var out service.ClusterDetail
	err := c.do(ctx, http.MethodPost, "/v0/clusters/"+url.PathEscape(ref)+"/check", nil, &out, longTimeout)
	return out, err
}

// RemoveCluster unregisters a cluster.
func (c *Client) RemoveCluster(ctx context.Context, ref string) (service.Cluster, error) {
	var out service.Cluster
	err := c.do(ctx, http.MethodDelete, "/v0/clusters/"+url.PathEscape(ref), nil, &out, defaultTimeout)
	return out, err
}

// ResolveCluster finds a cluster by cluster or node reference. A ref may
// contain "/" (cluster/<x>), which the server's wildcard route decodes.
func (c *Client) ResolveCluster(ctx context.Context, ref string) (service.Cluster, error) {
	var out service.Cluster
	err := c.do(ctx, http.MethodGet, "/v0/clusters/resolve/"+url.PathEscape(ref), nil, &out, defaultTimeout)
	return out, err
}

// CreateJoinToken mints a join token on the cluster.
func (c *Client) CreateJoinToken(ctx context.Context, ref string, req service.JoinTokenRequest) (service.JoinToken, error) {
	var out service.JoinToken
	err := c.do(ctx, http.MethodPost, "/v0/clusters/"+url.PathEscape(ref)+"/join-tokens", req, &out, defaultTimeout)
	return out, err
}

// AddNode registers a node.
func (c *Client) AddNode(ctx context.Context, req service.AddNodeRequest) (service.Node, error) {
	var out service.Node
	err := c.do(ctx, http.MethodPost, "/v0/nodes", req, &out, defaultTimeout)
	return out, err
}

// ListNodes returns nodes, optionally filtered by cluster.
func (c *Client) ListNodes(ctx context.Context, req service.ListNodesRequest) (service.NodeList, error) {
	path := "/v0/nodes"
	if req.Cluster != "" {
		path += "?cluster=" + url.QueryEscape(req.Cluster)
	}
	var out service.NodeList
	err := c.do(ctx, http.MethodGet, path, nil, &out, defaultTimeout)
	return out, err
}

// GetNode returns a node by ID or name.
func (c *Client) GetNode(ctx context.Context, ref string) (service.Node, error) {
	var out service.Node
	err := c.do(ctx, http.MethodGet, "/v0/nodes/"+url.PathEscape(ref), nil, &out, defaultTimeout)
	return out, err
}

// RemoveNode removes a node.
func (c *Client) RemoveNode(ctx context.Context, ref string, req service.RemoveNodeRequest) (service.Node, error) {
	path := "/v0/nodes/" + url.PathEscape(ref) + "?force=" + strconv.FormatBool(req.Force)
	var out service.Node
	err := c.do(ctx, http.MethodDelete, path, nil, &out, defaultTimeout)
	return out, err
}

// ListOrganizations returns organizations.
func (c *Client) ListOrganizations(ctx context.Context, req service.ListOrganizationsRequest) (service.OrganizationList, error) {
	query := url.Values{}
	if req.User != "" {
		query.Set("user", req.User)
	}
	if req.Status != "" {
		query.Set("status", req.Status)
	}
	path := "/v0/organizations"
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	var out service.OrganizationList
	err := c.do(ctx, http.MethodGet, path, nil, &out, defaultTimeout)
	return out, err
}

// CreateOrganization creates an organization.
func (c *Client) CreateOrganization(ctx context.Context, req service.CreateOrganizationRequest) (service.Organization, error) {
	var out service.Organization
	err := c.do(ctx, http.MethodPost, "/v0/organizations", req, &out, defaultTimeout)
	return out, err
}

// GetOrganization returns an organization by ID or slug, or the admin key's
// own organization when ref is empty.
func (c *Client) GetOrganization(ctx context.Context, ref string) (service.Organization, error) {
	path := "/v0/organization"
	if ref != "" {
		path = "/v0/organizations/" + url.PathEscape(ref)
	}
	var out service.Organization
	err := c.do(ctx, http.MethodGet, path, nil, &out, defaultTimeout)
	return out, err
}

// UpdateOrganization renames, suspends or restores an organization.
func (c *Client) UpdateOrganization(ctx context.Context, ref string, req service.UpdateOrganizationRequest) (service.Organization, error) {
	var out service.Organization
	err := c.do(ctx, http.MethodPatch, "/v0/organizations/"+url.PathEscape(ref), req, &out, defaultTimeout)
	return out, err
}

// ListRegions returns the clusters customers can use.
func (c *Client) ListRegions(ctx context.Context) (service.RegionList, error) {
	var out service.RegionList
	err := c.do(ctx, http.MethodGet, "/v0/regions", nil, &out, defaultTimeout)
	return out, err
}

// GetSettings returns the instance settings.
func (c *Client) GetSettings(ctx context.Context) (service.Settings, error) {
	var out service.Settings
	err := c.do(ctx, http.MethodGet, "/v0/settings", nil, &out, defaultTimeout)
	return out, err
}

// UpdateSettings changes the instance settings.
func (c *Client) UpdateSettings(ctx context.Context, req service.UpdateSettingsRequest) (service.Settings, error) {
	var out service.Settings
	err := c.do(ctx, http.MethodPatch, "/v0/settings", req, &out, defaultTimeout)
	return out, err
}

// ListPlans returns every plan.
func (c *Client) ListPlans(ctx context.Context) (service.PlanList, error) {
	var out service.PlanList
	err := c.do(ctx, http.MethodGet, "/v0/plans", nil, &out, defaultTimeout)
	return out, err
}

// GetPlan returns a plan by ID or slug.
func (c *Client) GetPlan(ctx context.Context, ref string) (service.Plan, error) {
	var out service.Plan
	err := c.do(ctx, http.MethodGet, "/v0/plans/"+url.PathEscape(ref), nil, &out, defaultTimeout)
	return out, err
}

// CreatePlan adds a plan.
func (c *Client) CreatePlan(ctx context.Context, req service.CreatePlanRequest) (service.Plan, error) {
	var out service.Plan
	err := c.do(ctx, http.MethodPost, "/v0/plans", req, &out, defaultTimeout)
	return out, err
}

// UpdatePlan changes a plan.
func (c *Client) UpdatePlan(ctx context.Context, ref string, req service.UpdatePlanRequest) (service.Plan, error) {
	var out service.Plan
	err := c.do(ctx, http.MethodPatch, "/v0/plans/"+url.PathEscape(ref), req, &out, defaultTimeout)
	return out, err
}

// DeletePlan removes a plan that no organization uses.
func (c *Client) DeletePlan(ctx context.Context, ref string) (service.Plan, error) {
	var out service.Plan
	err := c.do(ctx, http.MethodDelete, "/v0/plans/"+url.PathEscape(ref), nil, &out, defaultTimeout)
	return out, err
}

// ListMembers returns an organization's members.
func (c *Client) ListMembers(ctx context.Context, org string) (service.MemberList, error) {
	var out service.MemberList
	err := c.doOrg(ctx, org, http.MethodGet, "/v0/members", nil, &out)
	return out, err
}

// AddMember adds an account to an organization.
func (c *Client) AddMember(ctx context.Context, org string, req service.AddMemberRequest) (service.Member, error) {
	var out service.Member
	err := c.doOrg(ctx, org, http.MethodPost, "/v0/members", req, &out)
	return out, err
}

// SetMemberRole changes a member's role.
func (c *Client) SetMemberRole(ctx context.Context, org, ref string, req service.SetMemberRoleRequest) (service.Member, error) {
	var out service.Member
	err := c.doOrg(ctx, org, http.MethodPatch, "/v0/members/"+url.PathEscape(ref), req, &out)
	return out, err
}

// RemoveMember removes a member.
func (c *Client) RemoveMember(ctx context.Context, org, ref string) (service.Member, error) {
	var out service.Member
	err := c.doOrg(ctx, org, http.MethodDelete, "/v0/members/"+url.PathEscape(ref), nil, &out)
	return out, err
}

// ListAdminKeys returns an organization's admin keys.
func (c *Client) ListAdminKeys(ctx context.Context, org string) (service.AdminKeyList, error) {
	var out service.AdminKeyList
	err := c.doOrg(ctx, org, http.MethodGet, "/v0/admin-keys", nil, &out)
	return out, err
}

// CreateAdminKey creates an admin key and returns its token once.
func (c *Client) CreateAdminKey(ctx context.Context, org string, req service.CreateAdminKeyRequest) (service.NewAdminKey, error) {
	var out service.NewAdminKey
	err := c.doOrg(ctx, org, http.MethodPost, "/v0/admin-keys", req, &out)
	return out, err
}

// RevokeAdminKey revokes an admin key.
func (c *Client) RevokeAdminKey(ctx context.Context, org, ref string) (service.AdminKey, error) {
	var out service.AdminKey
	err := c.doOrg(ctx, org, http.MethodDelete, "/v0/admin-keys/"+url.PathEscape(ref), nil, &out)
	return out, err
}

// doOrg is do for the member and admin-key routes, which name their
// organization in a header ("" leaves it to the admin key).
func (c *Client) doOrg(ctx context.Context, org, method, path string, in, out any) error {
	return c.request(ctx, method, path, org, in, out, defaultTimeout)
}

func (c *Client) do(ctx context.Context, method, path string, in, out any, timeout time.Duration) error {
	return c.request(ctx, method, path, "", in, out, timeout)
}

func (c *Client) request(ctx context.Context, method, path, org string, in, out any, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var body io.Reader
	if in != nil {
		data, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("encode %s %s: %w", method, path, err)
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("build %s %s: %w", method, path, err)
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if org != "" {
		req.Header.Set("Lewbox-Organization", org)
	}

	res, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("cannot reach %s: timed out after %s", c.baseURL, timeout)
		}
		return fmt.Errorf("cannot reach %s: %w", c.baseURL, err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("read %s %s: %w", method, path, err)
	}

	if res.StatusCode < 200 || res.StatusCode > 299 {
		var envelope struct {
			Error struct {
				Code    string          `json:"code"`
				Message string          `json:"message"`
				Details json.RawMessage `json:"details"`
			} `json:"error"`
		}
		if json.Unmarshal(data, &envelope) == nil && envelope.Error.Code != "" {
			var details any
			if len(envelope.Error.Details) > 0 && string(envelope.Error.Details) != "null" {
				// Keep the server's bytes so the CLI prints the same error
				// body in local and remote mode.
				details = envelope.Error.Details
			}
			return &service.Error{
				Code:    service.Code(envelope.Error.Code),
				Message: envelope.Error.Message,
				Details: details,
			}
		}
		return fmt.Errorf("%s %s: HTTP %d: %s", method, path, res.StatusCode, strings.TrimSpace(string(data)))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode %s %s: %w", method, path, err)
	}
	return nil
}
