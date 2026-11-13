// Package aerolvmtest provides an in-memory fake AerolVM cluster for tests.
package aerolvmtest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/harlew-dev/lewbox/internal/aerolvm"
)

// Server is a fake AerolVM node backed by an httptest.Server.
type Server struct {
	t   testing.TB
	srv *httptest.Server

	// AdminToken is a token that is always accepted.
	AdminToken string

	// ClusterID names this cluster in snapshot registry refs.
	ClusterID string

	mu          sync.Mutex
	version     string
	members     []aerolvm.Member
	clusterOn   bool
	tokens      []aerolvm.Token
	tokenByVal  map[string]aerolvm.Token
	joins       []aerolvm.JoinToken
	drained     []string
	deleted     []string
	revoked     []string
	failures    map[string]*failure
	tokenTTLCap time.Duration
	certExpiry  time.Time
	nextID      int

	sandboxes    []aerolvm.Sandbox
	sandboxTeams map[string]string
	sandboxNodes map[string]string
	snapshots    map[string][]aerolvm.Snapshot
	autoPush     bool
	partial      bool
	pageLimit    int
	quotas       map[string]aerolvm.TeamQuota
	quotaPushes  map[string][]aerolvm.TeamQuota
	quotaDeletes map[string]int
}

type failure struct {
	status int
	body   string
	n      int
}

// New starts a fake node and closes it when the test finishes.
func New(t testing.TB) *Server {
	t.Helper()
	s := &Server{
		t:            t,
		version:      "v0.7.0",
		clusterOn:    true,
		ClusterID:    "01a00000-0000-7000-8000-000000000001",
		tokenByVal:   make(map[string]aerolvm.Token),
		failures:     make(map[string]*failure),
		sandboxTeams: make(map[string]string),
		sandboxNodes: make(map[string]string),
		snapshots:    make(map[string][]aerolvm.Snapshot),
		quotas:       make(map[string]aerolvm.TeamQuota),
		quotaPushes:  make(map[string][]aerolvm.TeamQuota),
		quotaDeletes: make(map[string]int),
	}
	s.members = []aerolvm.Member{{
		NodeID:   "node-a",
		NodeName: "node-a",
		Role:     "mixed",
		Alive:    true,
		Version:  s.version,
		Capacity: aerolvm.Capacity{
			HostCPUCores:      4,
			HostMemoryTotalMB: 15364,
			LiveMemoryFreeMB:  8552,
			HostDiskFreeGB:    13,
			ReservedCPU:       1,
			ReservedMemoryMB:  1024,
			CanAdmit:          true,
			SupportedRuntimes: []string{"docker"},
		},
	}}
	s.srv = httptest.NewServer(s.routes())
	s.members[0].APIURL = s.srv.URL
	t.Cleanup(s.srv.Close)
	return s
}

// URL is the fake node's base URL.
func (s *Server) URL() string { return s.srv.URL }

// SetVersion changes what /health reports.
func (s *Server) SetVersion(v string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.version = v
	for i := range s.members {
		s.members[i].Version = v
	}
}

// SetMembers replaces the cluster's member list.
func (s *Server) SetMembers(members []aerolvm.Member) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.members = append([]aerolvm.Member(nil), members...)
}

// SetClusterMode turns the /v1/cluster/* routes on or off. Off models a
// single-node daemon that was started without a cluster.
func (s *Server) SetClusterMode(on bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clusterOn = on
}

// SetTokenTTLCap rejects token creates whose expires_in exceeds d. Zero
// removes the cap.
func (s *Server) SetTokenTTLCap(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokenTTLCap = d
}

// SetCertExpiry makes GET /v1/metrics report this node's cluster certificate
// expiring at t. The zero time reports no certificate (gauge 0).
func (s *Server) SetCertExpiry(t time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.certExpiry = t
}

// AddSandbox adds a sandbox owned by team on the first member. Tests use it
// to fill the sandbox list and the cluster sandbox index.
func (s *Server) AddSandbox(team string, sandbox aerolvm.Sandbox) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.addSandboxLocked(team, "", sandbox)
}

func (s *Server) addSandboxLocked(team, node string, sandbox aerolvm.Sandbox) {
	if sandbox.CreatedAt.IsZero() {
		sandbox.CreatedAt = time.Now().UTC().Truncate(time.Second)
	}
	s.sandboxes = append(s.sandboxes, sandbox)
	s.sandboxTeams[sandbox.ID] = team
	if node != "" {
		s.sandboxNodes[sandbox.ID] = node
	}
}

// AddSnapshot adds a snapshot owned by team. A team-bound token sees only its
// team's snapshots, as on a real cluster.
func (s *Server) AddSnapshot(team string, snapshot aerolvm.Snapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if snapshot.CreatedAt.IsZero() {
		snapshot.CreatedAt = time.Now().UTC().Truncate(time.Second)
	}
	s.snapshots[team] = append(s.snapshots[team], snapshot)
}

// Snapshots returns the snapshots stored for team, in order.
func (s *Server) Snapshots(team string) []aerolvm.Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]aerolvm.Snapshot(nil), s.snapshots[team]...)
}

// SetSandboxStatus changes a sandbox's status, such as started after a stop.
func (s *Server) SetSandboxStatus(id, status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.sandboxes {
		if s.sandboxes[i].ID == id {
			// AerolVM bumps updated_at on every start and stop.
			s.sandboxes[i].Status = status
			s.sandboxes[i].UpdatedAt = time.Now().UTC()
			return
		}
	}
}

// Sandbox returns a copy of one sandbox.
func (s *Server) Sandbox(id string) (aerolvm.Sandbox, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sandbox := range s.sandboxes {
		if sandbox.ID == id {
			return sandbox, true
		}
	}
	return aerolvm.Sandbox{}, false
}

// SetSnapshotPush changes a snapshot's push state. For an active push it
// records the registry ref, as a real cluster does.
func (s *Server) SetSnapshotPush(team, name, state, registryRef string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.snapshots[team] {
		if s.snapshots[team][i].Name != name {
			continue
		}
		s.snapshots[team][i].PushState = state
		if state == "active" {
			s.snapshots[team][i].ImageRegistryRef = registryRef
		}
		return
	}
}

// AutoPush makes new snapshots active at once, with a registry ref, instead
// of waiting for a test to advance the push.
func (s *Server) AutoPush(on bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.autoPush = on
}

// AcceptToken makes a token minted on another fake node valid here, the way
// AerolVM replicates tokens across a cluster.
func (s *Server) AcceptToken(token aerolvm.Token) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokenByVal[token.Token] = token
}

// SetSandboxListPartial makes GET /v1/sandboxes say that a node did not
// answer.
func (s *Server) SetSandboxListPartial(partial bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.partial = partial
}

// SetPageLimit caps the page size of the sandbox list and index, so tests can
// exercise paging without hundreds of rows.
func (s *Server) SetPageLimit(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pageLimit = n
}

// QuotaPushes returns the quotas PUT for team, in order.
func (s *Server) QuotaPushes(team string) []aerolvm.TeamQuota {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]aerolvm.TeamQuota(nil), s.quotaPushes[team]...)
}

// QuotaDeletes returns how many times DELETE removed team's quota.
func (s *Server) QuotaDeletes(team string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.quotaDeletes[team]
}

// FailNext makes the next n requests to path fail with status and body,
// before any handler logic runs.
func (s *Server) FailNext(path string, status int, body string, n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failures[path] = &failure{status: status, body: body, n: n}
}

// Tokens returns the tokens created through POST /v1/tokens.
func (s *Server) Tokens() []aerolvm.Token {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]aerolvm.Token(nil), s.tokens...)
}

// JoinTokens returns the join tokens created through POST /v1/cluster/join-tokens.
func (s *Server) JoinTokens() []aerolvm.JoinToken {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]aerolvm.JoinToken(nil), s.joins...)
}

// Drained returns the node IDs passed to the drain route, in order.
func (s *Server) Drained() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.drained...)
}

// Deleted returns the node IDs passed to the delete route, in order.
func (s *Server) Deleted() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.deleted...)
}

// Revoked returns the token IDs passed to the revoke route, in order.
func (s *Server) Revoked() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.revoked...)
}

// Stop closes the fake node, so callers can model an unreachable cluster.
func (s *Server) Stop() { s.srv.Close() }

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /v1/metrics", s.handleMetrics)
	mux.HandleFunc("GET /v1/whoami", s.handleWhoAmI)
	mux.HandleFunc("GET /v1/cluster/members", s.handleMembers)
	mux.HandleFunc("POST /v1/tokens", s.handleCreateToken)
	mux.HandleFunc("GET /v1/tokens", s.handleListTokens)
	mux.HandleFunc("DELETE /v1/tokens/{id}", s.handleRevokeToken)
	mux.HandleFunc("POST /v1/cluster/join-tokens", s.handleCreateJoinToken)
	mux.HandleFunc("POST /v1/cluster/nodes/{id}/drain", s.handleDrainNode)
	mux.HandleFunc("DELETE /v1/cluster/nodes/{id}", s.handleDeleteNode)
	mux.HandleFunc("GET /v1/sandboxes", s.handleListSandboxes)
	mux.HandleFunc("POST /v1/sandboxes", s.handleCreateSandbox)
	mux.HandleFunc("GET /v1/sandboxes/{id}", s.handleGetSandbox)
	mux.HandleFunc("POST /v1/sandboxes/{id}/stop", s.handleStopSandbox)
	mux.HandleFunc("POST /v1/sandboxes/{id}/snapshot", s.handleCreateSnapshot)
	mux.HandleFunc("DELETE /v1/sandboxes/{id}", s.handleDeleteSandbox)
	mux.HandleFunc("GET /v1/snapshots", s.handleListSnapshots)
	mux.HandleFunc("GET /v1/snapshots/{name}", s.handleGetSnapshot)
	mux.HandleFunc("DELETE /v1/snapshots/{name}", s.handleDeleteSnapshot)
	mux.HandleFunc("GET /v1/cluster/sandbox-index", s.handleSandboxIndex)
	mux.HandleFunc("GET /v1/teams/{team}/quota", s.handleGetTeamQuota)
	mux.HandleFunc("PUT /v1/teams/{team}/quota", s.handlePutTeamQuota)
	mux.HandleFunc("DELETE /v1/teams/{team}/quota", s.handleDeleteTeamQuota)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.injected(w, r) {
			return
		}
		if r.URL.Path != "/health" && !s.authorized(r) {
			s.writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if strings.HasPrefix(r.URL.Path, "/v1/cluster/") && !s.clusterEnabled() {
			s.writeError(w, http.StatusServiceUnavailable, "cluster: not enabled on this node")
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func (s *Server) injected(w http.ResponseWriter, r *http.Request) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.failures[r.URL.Path]
	if !ok || f.n <= 0 {
		return false
	}
	f.n--
	if f.body == "" {
		w.WriteHeader(f.status)
		return true
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(f.status)
	_, _ = w.Write([]byte(f.body))
	return true
}

func (s *Server) authorized(r *http.Request) bool {
	scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if token == s.AdminToken {
		return true
	}
	_, ok = s.tokenByVal[token]
	return ok
}

func (s *Server) clusterEnabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.clusterOn
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	version := s.version
	n := len(s.members)
	s.mu.Unlock()
	s.writeJSON(w, http.StatusOK, map[string]any{
		"status":        "ok",
		"version":       version,
		"cluster_nodes": n,
	})
}

func (s *Server) handleMetrics(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	at := s.certExpiry
	clusterOn := s.clusterOn
	s.mu.Unlock()
	value := int64(0)
	if clusterOn && !at.IsZero() {
		value = at.Unix()
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	fmt.Fprintf(w, "# HELP aerolvm_cluster_mtls_cert_not_after_unix Cluster certificate expiry.\n")
	fmt.Fprintf(w, "# TYPE aerolvm_cluster_mtls_cert_not_after_unix gauge\n")
	fmt.Fprintf(w, "aerolvm_cluster_mtls_cert_not_after_unix %d\n", value)
}

func (s *Server) handleWhoAmI(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, token, _ := strings.Cut(r.Header.Get("Authorization"), " ")
	if token == s.AdminToken {
		s.writeJSON(w, http.StatusOK, aerolvm.WhoAmI{
			Principal: aerolvm.Principal{Kind: "token", ID: "tok_admin", Name: "admin"},
			Role:      "admin",
			Team:      "*",
		})
		return
	}
	record, ok := s.tokenByVal[token]
	if !ok {
		s.writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	s.writeJSON(w, http.StatusOK, aerolvm.WhoAmI{
		Principal: aerolvm.Principal{Kind: "token", ID: record.ID, Name: record.Name},
		Role:      record.Role,
		Team:      record.Team,
		ExpiresAt: record.ExpiresAt,
	})
}

func (s *Server) handleMembers(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	members := append([]aerolvm.Member(nil), s.members...)
	s.mu.Unlock()
	if members == nil {
		members = []aerolvm.Member{}
	}
	s.writeJSON(w, http.StatusOK, aerolvm.Members{Members: members})
}

var tokenName = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

func (s *Server) handleCreateToken(w http.ResponseWriter, r *http.Request) {
	var req aerolvm.CreateTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// AerolVM v0.7.0 refuses other names with 400 (pkg/models/tokens.go).
	if !tokenName.MatchString(req.Name) {
		s.writeError(w, http.StatusBadRequest, "invalid token request: name must be 1 to 64 characters of A-Z, a-z, 0-9, '.', '_' and '-'")
		return
	}
	ttl, err := parseTTL(req.ExpiresIn)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid expires_in")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tokenTTLCap > 0 && ttl > s.tokenTTLCap {
		s.writeError(w, http.StatusBadRequest, "expires_in exceeds SB_TOKEN_MAX_TTL")
		return
	}
	switch req.Role {
	case "runner":
		if req.Team == "" {
			s.writeError(w, http.StatusBadRequest, "runner tokens require a team")
			return
		}
	case "admin":
		if req.Team != "*" {
			s.writeError(w, http.StatusBadRequest, "admin tokens require team *")
			return
		}
	}
	for _, existing := range s.tokens {
		if existing.Name == req.Name && existing.Team == req.Team {
			s.writeError(w, http.StatusConflict, "a token with this name already exists on this team")
			return
		}
	}
	s.nextID++
	tok := aerolvm.Token{
		ID:    fmt.Sprintf("tok_%d", s.nextID),
		Name:  req.Name,
		Role:  req.Role,
		Team:  req.Team,
		Token: fmt.Sprintf("avm_%d", s.nextID),
	}
	if ttl > 0 {
		expires := time.Now().UTC().Add(ttl).Truncate(time.Second)
		tok.ExpiresAt = &expires
	}
	s.tokens = append(s.tokens, tok)
	s.tokenByVal[tok.Token] = tok
	s.writeJSON(w, http.StatusCreated, tok)
}

func (s *Server) handleListTokens(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	tokens := append([]aerolvm.Token(nil), s.tokens...)
	s.mu.Unlock()
	s.writeJSON(w, http.StatusOK, map[string]any{"tokens": tokens})
}

func (s *Server) handleRevokeToken(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.revoked = append(s.revoked, id)
	for i, tok := range s.tokens {
		if tok.ID == id {
			delete(s.tokenByVal, tok.Token)
			s.tokens = append(s.tokens[:i], s.tokens[i+1:]...)
			break
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleCreateJoinToken(w http.ResponseWriter, r *http.Request) {
	var req aerolvm.JoinTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	ttl, err := parseTTL(req.TTL)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid ttl")
		return
	}
	role := req.Role
	if role == "" {
		role = "server"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	join := aerolvm.JoinToken{
		Token:      fmt.Sprintf("join_%d", s.nextID),
		ID:         fmt.Sprintf("jnt_%d", s.nextID),
		Role:       role,
		Reusable:   req.Reusable,
		ExpiresAt:  time.Now().UTC().Add(ttl).Truncate(time.Second),
		CACertHash: "sha256:fake",
		JoinURLs:   []string{s.srv.URL},
	}
	s.joins = append(s.joins, join)
	s.writeJSON(w, http.StatusCreated, join)
}

func (s *Server) handleDrainNode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.mu.Lock()
	s.drained = append(s.drained, id)
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeleteNode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, m := range s.members {
		if m.NodeID == id {
			s.members = append(s.members[:i], s.members[i+1:]...)
			s.deleted = append(s.deleted, id)
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
	s.writeError(w, http.StatusNotFound, "cluster: unknown node")
}

// page bounds returns the slice [start:end] a request asks for.
func (s *Server) page(r *http.Request, n int) (int, int) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || (s.pageLimit > 0 && limit > s.pageLimit) {
		limit = s.pageLimit
	}
	if limit <= 0 {
		limit = n
	}
	start, _ := strconv.Atoi(r.URL.Query().Get("page_token"))
	if start < 0 || start > n {
		start = n
	}
	end := start + limit
	if end > n {
		end = n
	}
	return start, end
}

func (s *Server) handleListSandboxes(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	n := len(s.sandboxes)
	start, end := s.page(r, n)
	page := append([]aerolvm.Sandbox(nil), s.sandboxes[start:end]...)
	partial := s.partial
	s.mu.Unlock()
	if partial {
		w.Header().Set("X-Cluster-List-Partial", "true")
	}
	if end < n {
		w.Header().Set("X-Cluster-List-Next-Page-Token", strconv.Itoa(end))
	}
	s.writeJSON(w, http.StatusOK, page)
}

func (s *Server) handleSandboxIndex(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	placements := make([]aerolvm.Placement, 0, len(s.sandboxes))
	for _, sb := range s.sandboxes {
		node := s.sandboxNodes[sb.ID]
		if node == "" && len(s.members) > 0 {
			node = s.members[0].NodeID
		}
		placements = append(placements, aerolvm.Placement{
			SandboxID:     sb.ID,
			OwnerNodeID:   node,
			OwnerRef:      "team:" + s.sandboxTeams[sb.ID],
			QuotaCPU:      sb.CPU,
			QuotaMemoryMB: sb.MemoryMB,
			CreatedUnix:   sb.CreatedAt.Unix(),
			State:         sb.Status,
		})
	}
	start, end := s.page(r, len(placements))
	page := append([]aerolvm.Placement(nil), placements[start:end]...)
	next := ""
	if end < len(placements) {
		next = strconv.Itoa(end)
	}
	s.mu.Unlock()
	s.writeJSON(w, http.StatusOK, map[string]any{"placements": page, "next_page_token": next})
}

func (s *Server) handleCreateSandbox(w http.ResponseWriter, r *http.Request) {
	team, ok := s.requestTeam(r)
	if !ok {
		s.writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req aerolvm.CreateSandboxRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Image) == "" {
		s.writeError(w, http.StatusBadRequest, "image is required")
		return
	}
	owner := team
	if owner == "*" {
		owner = ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if req.Name != "" {
		for _, existing := range s.sandboxes {
			if s.sandboxTeams[existing.ID] == owner && existing.Name == req.Name {
				s.writeError(w, http.StatusConflict, "a sandbox with this name already exists")
				return
			}
		}
	}
	// Snapshot refs stay with the team that took the snapshot, as on a real
	// cluster (docs/design/snapshot-archive.md, finding 9).
	for other, snapshots := range s.snapshots {
		if other == owner {
			continue
		}
		for _, snapshot := range snapshots {
			if snapshot.ImageRegistryRef != "" && snapshot.ImageRegistryRef == req.Image {
				s.writeError(w, http.StatusBadRequest, fmt.Sprintf("sandbox not found: image %q", req.Image))
				return
			}
		}
	}
	s.nextID++
	sandbox := aerolvm.Sandbox{
		ID:        fmt.Sprintf("sb-%d", s.nextID),
		Name:      req.Name,
		Image:     req.Image,
		Status:    "started",
		Runtime:   req.Runtime,
		CPU:       req.CPU,
		MemoryMB:  req.MemoryMB,
		DiskGB:    req.DiskGB,
		Tags:      req.Tags,
		CreatedAt: time.Now().UTC().Truncate(time.Second),
	}
	s.addSandboxLocked(owner, "", sandbox)
	s.writeJSON(w, http.StatusCreated, sandbox)
}

func (s *Server) handleGetSandbox(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	team, ok := s.requestTeam(r)
	if !ok {
		s.writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sandbox := range s.sandboxes {
		if sandbox.ID != id {
			continue
		}
		if team != "*" && s.sandboxTeams[id] != team {
			break
		}
		s.writeJSON(w, http.StatusOK, sandbox)
		return
	}
	s.writeError(w, http.StatusNotFound, "sandbox not found")
}

// snapshotImage names the local image a snapshot stores, one per sandbox and
// name as on a real node.
func snapshotImage(sandboxID, name string) string {
	sum := sha256.Sum256([]byte(sandboxID + "\x00" + name))
	return "aerolvm-snapshot/" + hex.EncodeToString(sum[:8]) + ":latest"
}

func (s *Server) handleCreateSnapshot(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	team, ok := s.requestTeam(r)
	if !ok {
		s.writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		s.writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var sandbox aerolvm.Sandbox
	found := false
	for _, candidate := range s.sandboxes {
		if candidate.ID != id || (team != "*" && s.sandboxTeams[id] != team) {
			continue
		}
		sandbox = candidate
		found = true
		break
	}
	if !found {
		s.writeError(w, http.StatusNotFound, "sandbox not found")
		return
	}
	if sandbox.Status != "stopped" && sandbox.Status != "started" {
		s.writeError(w, http.StatusBadRequest, fmt.Sprintf("sandbox in status %s cannot be snapshotted", sandbox.Status))
		return
	}
	owner := team
	if owner == "*" {
		owner = s.sandboxTeams[id]
	}
	for _, snapshot := range s.snapshots[owner] {
		if snapshot.Name == req.Name {
			s.writeError(w, http.StatusConflict, "a snapshot with this name already exists")
			return
		}
	}
	snapshot := aerolvm.Snapshot{
		Name:            req.Name,
		Image:           snapshotImage(id, req.Name),
		SourceSandboxID: id,
		CreatedAt:       time.Now().UTC(),
		CPU:             sandbox.CPU,
		MemoryMB:        sandbox.MemoryMB,
		DiskGB:          sandbox.DiskGB,
		PushState:       "pending",
	}
	if s.autoPush {
		snapshot.PushState = "active"
		snapshot.ImageRegistryRef = "127.0.0.1:5000/cluster/" + s.ClusterID + "/snapshots/tenant-" + tenantHash(owner) + "/" + req.Name + ":latest"
	}
	s.snapshots[owner] = append(s.snapshots[owner], snapshot)
	s.writeJSON(w, http.StatusCreated, snapshot)
}

// tenantHash is the path segment a team's snapshots live under.
func tenantHash(team string) string {
	sum := sha256.Sum256([]byte(team))
	return hex.EncodeToString(sum[:8])
}

func (s *Server) handleGetSnapshot(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	team, ok := s.requestTeam(r)
	if !ok {
		s.writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if team == "*" {
		for _, snapshots := range s.snapshots {
			for _, snapshot := range snapshots {
				if snapshot.Name == name {
					s.writeJSON(w, http.StatusOK, snapshot)
					return
				}
			}
		}
	} else {
		for _, snapshot := range s.snapshots[team] {
			if snapshot.Name == name {
				s.writeJSON(w, http.StatusOK, snapshot)
				return
			}
		}
	}
	s.writeError(w, http.StatusNotFound, "snapshot not found")
}

func (s *Server) handleStopSandbox(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.mu.Lock()
	for i := range s.sandboxes {
		if s.sandboxes[i].ID != id {
			continue
		}
		s.sandboxes[i].Status = "stopped"
		sandbox := s.sandboxes[i]
		s.mu.Unlock()
		s.writeJSON(w, http.StatusOK, sandbox)
		return
	}
	s.mu.Unlock()
	s.writeError(w, http.StatusNotFound, "sandbox not found")
}

func (s *Server) handleDeleteSandbox(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, sb := range s.sandboxes {
		if sb.ID != id {
			continue
		}
		s.sandboxes = append(s.sandboxes[:i], s.sandboxes[i+1:]...)
		delete(s.sandboxTeams, id)
		delete(s.sandboxNodes, id)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	s.writeError(w, http.StatusNotFound, "sandbox not found")
}

func (s *Server) requestTeam(r *http.Request) (string, bool) {
	_, token, _ := strings.Cut(r.Header.Get("Authorization"), " ")
	s.mu.Lock()
	defer s.mu.Unlock()
	if token == s.AdminToken {
		return "*", true
	}
	record, ok := s.tokenByVal[token]
	if !ok {
		return "", false
	}
	return record.Team, true
}

func (s *Server) handleListSnapshots(w http.ResponseWriter, r *http.Request) {
	team, ok := s.requestTeam(r)
	if !ok {
		s.writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	s.mu.Lock()
	list := make([]aerolvm.Snapshot, 0)
	if team == "*" {
		for _, snapshots := range s.snapshots {
			list = append(list, snapshots...)
		}
	} else {
		list = append(list, s.snapshots[team]...)
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || (s.pageLimit > 0 && limit > s.pageLimit) {
		limit = s.pageLimit
	}
	if limit <= 0 {
		limit = len(list)
	}
	start := 0
	if after := r.URL.Query().Get("starting_after"); after != "" {
		for i, snapshot := range list {
			if snapshot.Name == after {
				start = i + 1
				break
			}
		}
	}
	if start > len(list) {
		start = len(list)
	}
	end := start + limit
	if end > len(list) {
		end = len(list)
	}
	page := append([]aerolvm.Snapshot(nil), list[start:end]...)
	hasMore := end < len(list)
	s.mu.Unlock()
	s.writeJSON(w, http.StatusOK, map[string]any{"snapshots": page, "has_more": hasMore})
}

func (s *Server) handleDeleteSnapshot(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	team, ok := s.requestTeam(r)
	if !ok {
		s.writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// The admin token's own namespace is empty, as on a real cluster.
	if team != "*" {
		for i, snapshot := range s.snapshots[team] {
			if snapshot.Name != name {
				continue
			}
			s.snapshots[team] = append(s.snapshots[team][:i], s.snapshots[team][i+1:]...)
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
	s.writeError(w, http.StatusNotFound, "snapshot not found")
}

func (s *Server) handleGetTeamQuota(w http.ResponseWriter, r *http.Request) {
	team := r.PathValue("team")
	s.mu.Lock()
	quota, ok := s.quotas[team]
	s.mu.Unlock()
	if !ok {
		s.writeError(w, http.StatusNotFound, "team quota not found")
		return
	}
	s.writeJSON(w, http.StatusOK, quota)
}

func (s *Server) handlePutTeamQuota(w http.ResponseWriter, r *http.Request) {
	team := r.PathValue("team")
	var quota aerolvm.TeamQuota
	if err := json.NewDecoder(r.Body).Decode(&quota); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if quota.MaxSandboxes == 0 && quota.MaxCPU == 0 && quota.MaxMemoryMB == 0 {
		s.writeError(w, http.StatusBadRequest, "a quota needs at least one limit")
		return
	}
	s.mu.Lock()
	s.quotas[team] = quota
	s.quotaPushes[team] = append(s.quotaPushes[team], quota)
	s.mu.Unlock()
	s.writeJSON(w, http.StatusOK, quota)
}

func (s *Server) handleDeleteTeamQuota(w http.ResponseWriter, r *http.Request) {
	team := r.PathValue("team")
	s.mu.Lock()
	delete(s.quotas, team)
	s.quotaDeletes[team]++
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) writeError(w http.ResponseWriter, status int, msg string) {
	s.writeJSON(w, status, map[string]string{"error": msg})
}

// parseTTL accepts Go durations and the <n>d form AerolVM uses.
func parseTTL(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	if days, ok := strings.CutSuffix(s, "d"); ok {
		n, err := strconv.Atoi(days)
		if err != nil || n < 0 {
			return 0, fmt.Errorf("invalid day duration %q", s)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}
