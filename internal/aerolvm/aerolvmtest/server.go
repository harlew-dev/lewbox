// Package aerolvmtest provides an in-memory fake AerolVM cluster for tests.
package aerolvmtest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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
	nextID      int

	sandboxes    []aerolvm.Sandbox
	sandboxTeams map[string]string
	sandboxNodes map[string]string
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
		tokenByVal:   make(map[string]aerolvm.Token),
		failures:     make(map[string]*failure),
		sandboxTeams: make(map[string]string),
		sandboxNodes: make(map[string]string),
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
		Capacity: aerolvm.Capacity{SupportedRuntimes: []string{"docker"}},
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

// AddSandbox adds a sandbox owned by team on the first member. Tests use it
// to fill the sandbox list and the cluster sandbox index.
func (s *Server) AddSandbox(team string, sandbox aerolvm.Sandbox) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.addSandboxLocked(team, "", sandbox)
}

// AddSandboxOn is AddSandbox with an explicit owner node.
func (s *Server) AddSandboxOn(team, node string, sandbox aerolvm.Sandbox) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.addSandboxLocked(team, node, sandbox)
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

// TeamQuota returns the quota currently set for team.
func (s *Server) TeamQuota(team string) (aerolvm.TeamQuota, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	q, ok := s.quotas[team]
	return q, ok
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
	mux.HandleFunc("GET /v1/whoami", s.handleWhoAmI)
	mux.HandleFunc("GET /v1/cluster/members", s.handleMembers)
	mux.HandleFunc("POST /v1/tokens", s.handleCreateToken)
	mux.HandleFunc("GET /v1/tokens", s.handleListTokens)
	mux.HandleFunc("DELETE /v1/tokens/{id}", s.handleRevokeToken)
	mux.HandleFunc("POST /v1/cluster/join-tokens", s.handleCreateJoinToken)
	mux.HandleFunc("POST /v1/cluster/nodes/{id}/drain", s.handleDrainNode)
	mux.HandleFunc("DELETE /v1/cluster/nodes/{id}", s.handleDeleteNode)
	mux.HandleFunc("GET /v1/sandboxes", s.handleListSandboxes)
	mux.HandleFunc("POST /v1/sandboxes/{id}/stop", s.handleStopSandbox)
	mux.HandleFunc("DELETE /v1/sandboxes/{id}", s.handleDeleteSandbox)
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

func (s *Server) handleCreateToken(w http.ResponseWriter, r *http.Request) {
	var req aerolvm.CreateTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
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
