// Package backend is the one interface the CLI uses to reach a Lewbox
// instance, with a local (in-process) and a remote (HTTP) implementation.
package backend

import (
	"context"

	"github.com/harlew-dev/lewbox/internal/service"
)

// Backend is every operation the CLI can perform against a Lewbox instance.
// The signatures are the same as the *service.Service methods.
type Backend interface {
	Health(ctx context.Context) (service.HealthResponse, error)
	Login(ctx context.Context, req service.LoginRequest) (service.LoginResponse, error)
	Logout(ctx context.Context) (service.LogoutResponse, error)
	Me(ctx context.Context) (service.Me, error)

	CreateUser(ctx context.Context, req service.CreateUserRequest) (service.User, error)
	ListUsers(ctx context.Context) (service.UserList, error)
	GetUser(ctx context.Context, ref string) (service.User, error)
	RemoveUser(ctx context.Context, ref string) (service.User, error)
	SetUserDisabled(ctx context.Context, ref string, disabled bool) (service.User, error)
	SetPassword(ctx context.Context, ref string, req service.SetPasswordRequest) (service.User, error)

	AddCluster(ctx context.Context, req service.AddClusterRequest) (service.ClusterDetail, error)
	ListClusters(ctx context.Context) (service.ClusterList, error)
	GetCluster(ctx context.Context, ref string) (service.ClusterDetail, error)
	CheckCluster(ctx context.Context, ref string) (service.ClusterDetail, error)
	RemoveCluster(ctx context.Context, ref string) (service.Cluster, error)
	ResolveCluster(ctx context.Context, ref string) (service.Cluster, error)
	CreateJoinToken(ctx context.Context, ref string, req service.JoinTokenRequest) (service.JoinToken, error)

	AddNode(ctx context.Context, req service.AddNodeRequest) (service.Node, error)
	ListNodes(ctx context.Context, req service.ListNodesRequest) (service.NodeList, error)
	GetNode(ctx context.Context, ref string) (service.Node, error)
	RemoveNode(ctx context.Context, ref string, req service.RemoveNodeRequest) (service.Node, error)

	// org is an organization ID or slug; "" means an admin key's own.
	ListOrganizations(ctx context.Context, req service.ListOrganizationsRequest) (service.OrganizationList, error)
	CreateOrganization(ctx context.Context, req service.CreateOrganizationRequest) (service.Organization, error)
	GetOrganization(ctx context.Context, ref string) (service.Organization, error)
	UpdateOrganization(ctx context.Context, ref string, req service.UpdateOrganizationRequest) (service.Organization, error)

	ListMembers(ctx context.Context, org string) (service.MemberList, error)
	AddMember(ctx context.Context, org string, req service.AddMemberRequest) (service.Member, error)
	SetMemberRole(ctx context.Context, org, ref string, req service.SetMemberRoleRequest) (service.Member, error)
	RemoveMember(ctx context.Context, org, ref string) (service.Member, error)

	ListAdminKeys(ctx context.Context, org string) (service.AdminKeyList, error)
	CreateAdminKey(ctx context.Context, org string, req service.CreateAdminKeyRequest) (service.NewAdminKey, error)
	RevokeAdminKey(ctx context.Context, org, ref string) (service.AdminKey, error)
}

// Local runs every operation in-process as the local principal. Whoever can
// open the database already owns the instance, so there is no sign-in.
type Local struct {
	Svc *service.Service
}

var _ Backend = Local{}

// Health reports whether this Lewbox instance is up.
func (l Local) Health(ctx context.Context) (service.HealthResponse, error) {
	return l.Svc.Health(l.withPrincipal(ctx))
}

// Login always fails: local mode has no sign-in.
func (l Local) Login(_ context.Context, _ service.LoginRequest) (service.LoginResponse, error) {
	return service.LoginResponse{}, &service.Error{
		Code:    service.CodeInvalid,
		Message: "local mode has no sign-in; pass --api-url or use a context",
	}
}

// Logout has nothing to end in local mode and says so.
func (l Local) Logout(ctx context.Context) (service.LogoutResponse, error) {
	return l.Svc.Logout(l.withPrincipal(ctx))
}

// Me describes the local principal.
func (l Local) Me(ctx context.Context) (service.Me, error) {
	return l.Svc.Me(l.withPrincipal(ctx))
}

// CreateUser creates an account.
func (l Local) CreateUser(ctx context.Context, req service.CreateUserRequest) (service.User, error) {
	return l.Svc.CreateUser(l.withPrincipal(ctx), req)
}

// ListUsers returns every account.
func (l Local) ListUsers(ctx context.Context) (service.UserList, error) {
	return l.Svc.ListUsers(l.withPrincipal(ctx))
}

// GetUser returns an account by ID or email.
func (l Local) GetUser(ctx context.Context, ref string) (service.User, error) {
	return l.Svc.GetUser(l.withPrincipal(ctx), ref)
}

// RemoveUser removes an account.
func (l Local) RemoveUser(ctx context.Context, ref string) (service.User, error) {
	return l.Svc.RemoveUser(l.withPrincipal(ctx), ref)
}

// SetUserDisabled blocks or allows sign-in.
func (l Local) SetUserDisabled(ctx context.Context, ref string, disabled bool) (service.User, error) {
	return l.Svc.SetUserDisabled(l.withPrincipal(ctx), ref, disabled)
}

// SetPassword changes an account's password.
func (l Local) SetPassword(ctx context.Context, ref string, req service.SetPasswordRequest) (service.User, error) {
	return l.Svc.SetPassword(l.withPrincipal(ctx), ref, req)
}

// AddCluster registers and validates a cluster.
func (l Local) AddCluster(ctx context.Context, req service.AddClusterRequest) (service.ClusterDetail, error) {
	return l.Svc.AddCluster(l.withPrincipal(ctx), req)
}

// ListClusters returns every cluster.
func (l Local) ListClusters(ctx context.Context) (service.ClusterList, error) {
	return l.Svc.ListClusters(l.withPrincipal(ctx))
}

// GetCluster returns a cluster by ID or slug.
func (l Local) GetCluster(ctx context.Context, ref string) (service.ClusterDetail, error) {
	return l.Svc.GetCluster(l.withPrincipal(ctx), ref)
}

// CheckCluster runs the cluster checks now.
func (l Local) CheckCluster(ctx context.Context, ref string) (service.ClusterDetail, error) {
	return l.Svc.CheckCluster(l.withPrincipal(ctx), ref)
}

// RemoveCluster unregisters a cluster.
func (l Local) RemoveCluster(ctx context.Context, ref string) (service.Cluster, error) {
	return l.Svc.RemoveCluster(l.withPrincipal(ctx), ref)
}

// ResolveCluster finds a cluster by cluster or node reference.
func (l Local) ResolveCluster(ctx context.Context, ref string) (service.Cluster, error) {
	return l.Svc.ResolveCluster(l.withPrincipal(ctx), ref)
}

// CreateJoinToken mints a join token on the cluster.
func (l Local) CreateJoinToken(ctx context.Context, ref string, req service.JoinTokenRequest) (service.JoinToken, error) {
	return l.Svc.CreateJoinToken(l.withPrincipal(ctx), ref, req)
}

// AddNode registers a node.
func (l Local) AddNode(ctx context.Context, req service.AddNodeRequest) (service.Node, error) {
	return l.Svc.AddNode(l.withPrincipal(ctx), req)
}

// ListNodes returns nodes, optionally filtered by cluster.
func (l Local) ListNodes(ctx context.Context, req service.ListNodesRequest) (service.NodeList, error) {
	return l.Svc.ListNodes(l.withPrincipal(ctx), req)
}

// GetNode returns a node by ID or name.
func (l Local) GetNode(ctx context.Context, ref string) (service.Node, error) {
	return l.Svc.GetNode(l.withPrincipal(ctx), ref)
}

// RemoveNode removes a node.
func (l Local) RemoveNode(ctx context.Context, ref string, req service.RemoveNodeRequest) (service.Node, error) {
	return l.Svc.RemoveNode(l.withPrincipal(ctx), ref, req)
}

// ListOrganizations returns organizations.
func (l Local) ListOrganizations(ctx context.Context, req service.ListOrganizationsRequest) (service.OrganizationList, error) {
	return l.Svc.ListOrganizations(l.withPrincipal(ctx), req)
}

// CreateOrganization creates an organization.
func (l Local) CreateOrganization(ctx context.Context, req service.CreateOrganizationRequest) (service.Organization, error) {
	return l.Svc.CreateOrganization(l.withPrincipal(ctx), req)
}

// GetOrganization returns an organization by ID or slug.
func (l Local) GetOrganization(ctx context.Context, ref string) (service.Organization, error) {
	return l.Svc.GetOrganization(l.withPrincipal(ctx), ref)
}

// UpdateOrganization renames, suspends or restores an organization.
func (l Local) UpdateOrganization(ctx context.Context, ref string, req service.UpdateOrganizationRequest) (service.Organization, error) {
	return l.Svc.UpdateOrganization(l.withPrincipal(ctx), ref, req)
}

// ListMembers returns an organization's members.
func (l Local) ListMembers(ctx context.Context, org string) (service.MemberList, error) {
	return l.Svc.ListMembers(l.withPrincipal(ctx), org)
}

// AddMember adds an account to an organization.
func (l Local) AddMember(ctx context.Context, org string, req service.AddMemberRequest) (service.Member, error) {
	return l.Svc.AddMember(l.withPrincipal(ctx), org, req)
}

// SetMemberRole changes a member's role.
func (l Local) SetMemberRole(ctx context.Context, org, ref string, req service.SetMemberRoleRequest) (service.Member, error) {
	return l.Svc.SetMemberRole(l.withPrincipal(ctx), org, ref, req)
}

// RemoveMember removes a member.
func (l Local) RemoveMember(ctx context.Context, org, ref string) (service.Member, error) {
	return l.Svc.RemoveMember(l.withPrincipal(ctx), org, ref)
}

// ListAdminKeys returns an organization's admin keys.
func (l Local) ListAdminKeys(ctx context.Context, org string) (service.AdminKeyList, error) {
	return l.Svc.ListAdminKeys(l.withPrincipal(ctx), org)
}

// CreateAdminKey creates an admin key and returns its token once.
func (l Local) CreateAdminKey(ctx context.Context, org string, req service.CreateAdminKeyRequest) (service.NewAdminKey, error) {
	return l.Svc.CreateAdminKey(l.withPrincipal(ctx), org, req)
}

// RevokeAdminKey revokes an admin key.
func (l Local) RevokeAdminKey(ctx context.Context, org, ref string) (service.AdminKey, error) {
	return l.Svc.RevokeAdminKey(l.withPrincipal(ctx), org, ref)
}

func (l Local) withPrincipal(ctx context.Context) context.Context {
	return service.WithPrincipal(ctx, service.LocalPrincipal())
}
