// Package aerolvm is a client for the AerolVM admin API.
//
// It covers the routes Lewbox needs to validate a cluster, mirror its
// members and mint join tokens. Every request is retried only when AerolVM
// says the raft token store has no leader yet, because that is the one
// failure that usually fixes itself.
package aerolvm

import (
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
	NodeID     string   `json:"node_id"`
	NodeName   string   `json:"node_name"`
	APIURL     string   `json:"api_url"`
	RaftAddr   string   `json:"raft_addr"`
	PublicHost string   `json:"public_host"`
	Role       string   `json:"role"`
	Alive      bool     `json:"alive"`
	Version    string   `json:"version"`
	Leader     bool     `json:"leader"`
	Voter      bool     `json:"voter"`
	Capacity   Capacity `json:"capacity"`
}

// Capacity is the part of a member's capacity Lewbox reads.
type Capacity struct {
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
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Image        string    `json:"image"`
	Status       string    `json:"status"`
	StatusReason string    `json:"status_reason"`
	Runtime      string    `json:"runtime"`
	CPU          float64   `json:"cpu"`
	MemoryMB     int64     `json:"memory_mb"`
	DiskGB       int64     `json:"disk_gb"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	LastActiveAt time.Time `json:"last_active_at"`
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
