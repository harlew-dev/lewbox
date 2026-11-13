// Package aerolvm is a client for the AerolVM admin API.
//
// It covers the routes Lewbox needs to validate a cluster, mirror its
// members and mint join tokens. Every request is retried only when AerolVM
// says the raft token store has no leader yet, because that is the one
// failure that usually fixes itself.
package aerolvm

import (
	"net/netip"
	"strings"
	"time"
)

// Health is the answer to GET /health.
type Health struct {
	Status       string `json:"status"`
	Version      string `json:"version"`
	ClusterNodes int    `json:"cluster_nodes"`
}

// Principal is who a token belongs to, as reported by GET /v1/whoami.
type Principal struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

// WhoAmI is the answer to GET /v1/whoami.
type WhoAmI struct {
	Principal Principal  `json:"principal"`
	Role      string     `json:"role"`
	Team      string     `json:"team"`
	ExpiresAt *time.Time `json:"expires_at"`
}

// Member is one node in GET /v1/cluster/members.
type Member struct {
	NodeID     string `json:"node_id"`
	NodeName   string `json:"node_name"`
	APIURL     string `json:"api_url"`
	RaftAddr   string `json:"raft_addr"`
	PublicHost string `json:"public_host"`
	// DataPlaneHost is the address the node advertises to the cluster.
	DataPlaneHost string   `json:"data_plane_host"`
	Role          string   `json:"role"`
	Alive         bool     `json:"alive"`
	Version       string   `json:"version"`
	Leader        bool     `json:"leader"`
	Voter         bool     `json:"voter"`
	Capacity      Capacity `json:"capacity"`
}

// PublicIP is the member's IP address for DNS records: public_host when it
// is an IP, otherwise data_plane_host when that is. In domain mode AerolVM
// sets public_host to the domain and data_plane_host to the machine's IP
// (v0.7.0, testdata/members_domain.json). It doesn't judge whether the IP is
// public.
func (m Member) PublicIP() (netip.Addr, bool) {
	for _, host := range []string{m.PublicHost, m.DataPlaneHost} {
		if addr, err := netip.ParseAddr(strings.TrimSpace(host)); err == nil && addr.Zone() == "" {
			return addr.Unmap(), true
		}
	}
	return netip.Addr{}, false
}

// Capacity is the part of a member's capacity Lewbox reads.
type Capacity struct {
	HostCPUCores      int64   `json:"host_cpu_cores"`
	HostMemoryTotalMB int64   `json:"host_memory_total_mb"`
	LiveMemoryFreeMB  int64   `json:"live_memory_free_mb"`
	HostDiskFreeGB    int64   `json:"host_disk_free_gb"`
	ReservedCPU       float64 `json:"reserved_cpu"`
	ReservedMemoryMB  int64   `json:"reserved_memory_mb"`
	SandboxesActive   int64   `json:"sandboxes_active"`
	CanAdmit          bool    `json:"can_admit"`

	SupportedRuntimes []string `json:"supported_runtimes"`
}

// Members is the answer to GET /v1/cluster/members.
type Members struct {
	Members []Member `json:"members"`
}

// Token is an API token as returned by the token routes.
type Token struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Role      string     `json:"role"`
	Team      string     `json:"team"`
	Token     string     `json:"token"`
	ExpiresAt *time.Time `json:"expires_at"`
}

// CreateTokenRequest is the body of POST /v1/tokens.
type CreateTokenRequest struct {
	Name      string `json:"name"`
	Role      string `json:"role"`
	Team      string `json:"team"`
	ExpiresIn string `json:"expires_in"`
}

// JoinToken is the answer to POST /v1/cluster/join-tokens.
type JoinToken struct {
	Token      string    `json:"token"`
	ID         string    `json:"id"`
	Role       string    `json:"role"`
	Reusable   bool      `json:"reusable"`
	ExpiresAt  time.Time `json:"expires_at"`
	CACertHash string    `json:"ca_cert_hash"`
	JoinURLs   []string  `json:"join_urls"`
}

// JoinTokenRequest is the body of POST /v1/cluster/join-tokens.
type JoinTokenRequest struct {
	Role     string `json:"role"`
	TTL      string `json:"ttl"`
	Reusable bool   `json:"reusable"`
}

// Sandbox is one entry of GET /v1/sandboxes.
type Sandbox struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Image        string            `json:"image"`
	Status       string            `json:"status"`
	StatusReason string            `json:"status_reason"`
	Runtime      string            `json:"runtime"`
	CPU          float64           `json:"cpu"`
	MemoryMB     int64             `json:"memory_mb"`
	DiskGB       int64             `json:"disk_gb"`
	Tags         map[string]string `json:"tags,omitempty"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
	LastActiveAt time.Time         `json:"last_active_at"`
}

// CreateSandboxRequest is the body of POST /v1/sandboxes. The image is the
// only field AerolVM requires.
type CreateSandboxRequest struct {
	Image    string            `json:"image"`
	Name     string            `json:"name,omitempty"`
	Tags     map[string]string `json:"tags,omitempty"`
	CPU      float64           `json:"cpu,omitempty"`
	MemoryMB int64             `json:"memory_mb,omitempty"`
	DiskGB   int64             `json:"disk_gb,omitempty"`
	Runtime  string            `json:"runtime,omitempty"`
}

// Snapshot is one entry of GET /v1/snapshots. AerolVM reports no owner and no
// size, and lists only the snapshots of the node that answers.
type Snapshot struct {
	Name             string    `json:"name"`
	Image            string    `json:"image"`
	ImageID          string    `json:"image_id"`
	SourceSandboxID  string    `json:"source_sandbox_id"`
	CreatedAt        time.Time `json:"created_at"`
	CPU              float64   `json:"cpu"`
	MemoryMB         int64     `json:"memory_mb"`
	DiskGB           int64     `json:"disk_gb"`
	PushState        string    `json:"push_state"`
	PushError        string    `json:"push_error"`
	ImageRegistryRef string    `json:"image_registry_ref"`
}

// Placement is one entry of the cluster sandbox index: which team owns a
// sandbox and on which node it runs.
type Placement struct {
	SandboxID     string  `json:"sandbox_id"`
	OwnerNodeID   string  `json:"owner_node_id"`
	OwnerRef      string  `json:"owner_ref"`
	QuotaCPU      float64 `json:"quota_cpu"`
	QuotaMemoryMB int64   `json:"quota_memory_mb"`
	CreatedUnix   int64   `json:"created_unix"`
	State         string  `json:"state"`
}

// TeamQuota is the body of PUT /v1/teams/{team}/quota. A zero field means
// "no limit on that axis", so every field is omitted when it is zero.
type TeamQuota struct {
	MaxSandboxes int     `json:"max_sandboxes,omitempty"`
	MaxCPU       float64 `json:"max_cpu,omitempty"`
	MaxMemoryMB  int     `json:"max_memory_mb,omitempty"`
}
