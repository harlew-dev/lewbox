package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/harlew-dev/lewbox/internal/aerolvm"
	"github.com/harlew-dev/lewbox/internal/db"
)

const (
	minAerolVMVersion = "v0.6.13"
	defaultRuntime    = "docker"
	defaultJoinTTL    = "15m"
	defaultJoinRole   = "mixed"
)

// Check is one requirement a cluster must meet.
type Check struct {
	Name   string `json:"name"` // health, admin_token, version, runtime, token_ttl, endpoint, members
	OK     bool   `json:"ok"`
	Fatal  bool   `json:"fatal,omitempty"`  // a failed fatal check blocks registration
	Detail string `json:"detail,omitempty"` // what was found, and how to fix it when not OK
}

// Cluster is an AerolVM cluster registered with Lewbox.
type Cluster struct {
	ID              string     `json:"id"`
	Slug            string     `json:"slug"`
	Name            string     `json:"name"`
	AdminURL        string     `json:"admin_url"`
	EndpointURL     string     `json:"endpoint_url"`
	Runtime         string     `json:"runtime"`
	AdminTokenLast4 string     `json:"admin_token_last4"`
	Status          string     `json:"status"`
	StatusDetail    string     `json:"status_detail,omitempty"`
	Version         string     `json:"version,omitempty"`
	Nodes           int        `json:"nodes"`
	CheckedAt       *time.Time `json:"checked_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}

// ClusterDetail is a cluster with its last checks and its nodes.
type ClusterDetail struct {
	Cluster
	Checks   []Check `json:"checks"`
	NodeList []Node  `json:"node_list"`
}

// ClusterList is returned by ListClusters. Data is never null.
type ClusterList struct {
	Data []Cluster `json:"data"`
}

// AddClusterRequest is the input to AddCluster.
type AddClusterRequest struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	AdminURL    string `json:"admin_url"`
	EndpointURL string `json:"endpoint_url"`
	Runtime     string `json:"runtime"`
	AdminToken  string `json:"admin_token"`
}

// JoinTokenRequest is the input to CreateJoinToken.
type JoinTokenRequest struct {
	Role string `json:"role"` // worker | ingress | server | mixed; default "mixed"
	TTL  string `json:"ttl"`  // default "15m"
}

// JoinToken is a freshly minted token for a machine that wants to join.
type JoinToken struct {
	ClusterID  string    `json:"cluster_id"`
	Cluster    string    `json:"cluster"`
	Runtime    string    `json:"runtime"`
	Version    string    `json:"version"`
	Role       string    `json:"role"`
	TokenRole  string    `json:"token_role"`
	Token      string    `json:"token"`
	CACertHash string    `json:"ca_cert_hash"`
	JoinURLs   []string  `json:"join_urls"`
	ExpiresAt  time.Time `json:"expires_at"`
	Command    string    `json:"command"`
}

// AddCluster validates a cluster, stores it and mirrors its members as nodes.
func (s *Service) AddCluster(ctx context.Context, req AddClusterRequest) (ClusterDetail, error) {
	if _, err := requireAdmin(ctx); err != nil {
		return ClusterDetail{}, err
	}
	if s.keyring == nil {
		return ClusterDetail{}, errors.New("no keyring configured")
	}

	slug := strings.TrimSpace(req.Slug)
	if !nodeName.MatchString(slug) {
		return ClusterDetail{}, invalid("slug must be 1-63 lowercase letters, digits or hyphens, and start and end with a letter or digit")
	}
	if _, err := uuid.Parse(slug); err == nil {
		return ClusterDetail{}, invalid("slug must not be a UUID")
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = slug
	}
	adminURL, err := normalizeURL(req.AdminURL)
	if err != nil {
		return ClusterDetail{}, err
	}
	endpointURL := strings.TrimSpace(req.EndpointURL)
	if endpointURL == "" {
		endpointURL = adminURL
	} else if endpointURL, err = normalizeURL(endpointURL); err != nil {
		return ClusterDetail{}, err
	}
	runtime := strings.ToLower(strings.TrimSpace(req.Runtime))
	if runtime == "" {
		runtime = defaultRuntime
	}
	if runtime != "docker" && runtime != "firecracker" {
		return ClusterDetail{}, invalid("runtime must be docker or firecracker")
	}
	token := strings.TrimSpace(req.AdminToken)
	if token == "" {
		return ClusterDetail{}, invalid("admin_token is required")
	}

	if _, err := s.q.GetClusterBySlug(ctx, slug); err == nil {
		return ClusterDetail{}, conflict("a cluster with slug %q already exists", slug)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return ClusterDetail{}, fmt.Errorf("get cluster: %w", err)
	}

	checks, result := s.runChecks(ctx, clusterSpec{
		Slug:        slug,
		AdminURL:    adminURL,
		EndpointURL: endpointURL,
		Runtime:     runtime,
		AdminToken:  token,
	})
	if result.fatal {
		return ClusterDetail{}, precondition(checks, "cluster %q failed its checks", slug)
	}
	status, detail := summarizeChecks(checks)

	sealed, err := s.keyring.Seal([]byte(token))
	if err != nil {
		return ClusterDetail{}, err
	}
	now := s.now()
	row, err := s.q.CreateCluster(ctx, db.CreateClusterParams{
		ID:              uuid.NewV7().String(),
		Slug:            slug,
		Name:            name,
		AdminURL:        adminURL,
		EndpointURL:     endpointURL,
		Runtime:         runtime,
		AdminTokenEnc:   sealed,
		AdminTokenID:    result.principalID,
		AdminTokenLast4: last4(token),
		Status:          status,
		StatusDetail:    detail,
		Version:         result.version,
		Checks:          encodeChecks(checks),
		CheckedAt:       sql.NullTime{Time: now, Valid: true},
		CreatedAt:       now,
	})
	if db.IsUniqueViolation(err) {
		return ClusterDetail{}, conflict("a cluster with slug %q already exists", slug)
	}
	if err != nil {
		return ClusterDetail{}, fmt.Errorf("create cluster: %w", err)
	}

	if result.membersAvailable {
		conflicts, err := s.syncNodes(ctx, row, result.members)
		if err != nil {
			return ClusterDetail{}, err
		}
		if len(conflicts) > 0 {
			checks = applyConflicts(checks, conflicts)
			status, detail = summarizeChecks(checks)
			if err := s.storeCheck(ctx, row.ID, status, detail, row.Version, checks); err != nil {
				return ClusterDetail{}, err
			}
		}
	}
	return s.clusterDetail(ctx, row.ID)
}

// ListClusters returns every cluster, ordered by slug.
func (s *Service) ListClusters(ctx context.Context) (ClusterList, error) {
	if _, err := requireUser(ctx); err != nil {
		return ClusterList{}, err
	}
	rows, err := s.q.ListClusters(ctx)
	if err != nil {
		return ClusterList{}, fmt.Errorf("list clusters: %w", err)
	}
	counts, err := s.nodeCounts(ctx)
	if err != nil {
		return ClusterList{}, err
	}
	out := ClusterList{Data: make([]Cluster, 0, len(rows))}
	for _, r := range rows {
		out.Data = append(out.Data, toCluster(r, counts[r.ID]))
	}
	return out, nil
}

// GetCluster returns a cluster by ID or slug without calling AerolVM.
func (s *Service) GetCluster(ctx context.Context, ref string) (ClusterDetail, error) {
	if _, err := requireUser(ctx); err != nil {
		return ClusterDetail{}, err
	}
	row, err := s.findCluster(ctx, ref)
	if err != nil {
		return ClusterDetail{}, err
	}
	return s.clusterDetail(ctx, row.ID)
}

// CheckCluster runs the checks again with the stored token and re-syncs nodes.
// A failed check marks the cluster unreachable or degraded; it deletes nothing.
func (s *Service) CheckCluster(ctx context.Context, ref string) (ClusterDetail, error) {
	if _, err := requireAdmin(ctx); err != nil {
		return ClusterDetail{}, err
	}
	row, err := s.findCluster(ctx, ref)
	if err != nil {
		return ClusterDetail{}, err
	}
	token, err := s.clusterToken(row)
	if err != nil {
		return ClusterDetail{}, err
	}
	checks, result := s.runChecks(ctx, clusterSpec{
		Slug:        row.Slug,
		AdminURL:    row.AdminURL,
		EndpointURL: row.EndpointURL,
		Runtime:     row.Runtime,
		AdminToken:  token,
	})
	status, detail := summarizeChecks(checks)
	if healthFailed(checks) {
		status = "unreachable"
	}
	version := result.version
	if version == "" {
		version = row.Version
	}
	// An unreachable cluster can't say which nodes are up, so stop claiming
	// they are.
	if healthFailed(checks) {
		if err := s.q.MarkClusterNodesDown(ctx, sql.NullString{String: row.ID, Valid: true}); err != nil {
			return ClusterDetail{}, fmt.Errorf("mark nodes down: %w", err)
		}
	}
	if result.membersAvailable {
		conflicts, err := s.syncNodes(ctx, row, result.members)
		if err != nil {
			return ClusterDetail{}, err
		}
		if len(conflicts) > 0 {
			checks = applyConflicts(checks, conflicts)
			status, detail = summarizeChecks(checks)
			if healthFailed(checks) {
				status = "unreachable"
			}
		}
	}
	if err := s.storeCheck(ctx, row.ID, status, detail, version, checks); err != nil {
		return ClusterDetail{}, err
	}
	return s.clusterDetail(ctx, row.ID)
}

// RemoveCluster revokes the cluster's admin token (best effort) and deletes
// the cluster. Its nodes go too.
func (s *Service) RemoveCluster(ctx context.Context, ref string) (Cluster, error) {
	if _, err := requireAdmin(ctx); err != nil {
		return Cluster{}, err
	}
	if s.keyring == nil {
		return Cluster{}, errors.New("no keyring configured")
	}
	row, err := s.findCluster(ctx, ref)
	if err != nil {
		return Cluster{}, err
	}
	nodes, err := s.q.ListNodesByCluster(ctx, sql.NullString{String: row.ID, Valid: true})
	if err != nil {
		return Cluster{}, fmt.Errorf("list cluster nodes: %w", err)
	}
	if token, err := s.clusterToken(row); err == nil {
		client := s.aerolvmClient(row.AdminURL, token)
		_ = client.RevokeToken(ctx, row.AdminTokenID)
	}
	n, err := s.q.DeleteCluster(ctx, row.ID)
	if err != nil {
		return Cluster{}, fmt.Errorf("delete cluster: %w", err)
	}
	if n == 0 {
		return Cluster{}, notFound("cluster %q not found", ref)
	}
	return toCluster(row, len(nodes)), nil
}

// ResolveCluster finds a cluster by its ID or slug, or by the name or ID of
// one of its nodes. "cluster/<x>" and "node/<x>" remove any ambiguity.
func (s *Service) ResolveCluster(ctx context.Context, ref string) (Cluster, error) {
	if _, err := requireUser(ctx); err != nil {
		return Cluster{}, err
	}
	row, err := s.resolveClusterRow(ctx, ref)
	if err != nil {
		return Cluster{}, err
	}
	return s.clusterWithCount(ctx, row)
}

// CreateJoinToken mints a join token on the cluster so a new machine can join.
func (s *Service) CreateJoinToken(ctx context.Context, ref string, req JoinTokenRequest) (JoinToken, error) {
	if _, err := requireAdmin(ctx); err != nil {
		return JoinToken{}, err
	}
	row, err := s.resolveClusterRow(ctx, ref)
	if err != nil {
		return JoinToken{}, err
	}
	if s.keyring == nil {
		return JoinToken{}, errors.New("no keyring configured")
	}

	role := strings.ToLower(strings.TrimSpace(req.Role))
	if role == "" {
		role = defaultJoinRole
	}
	switch role {
	case "worker", "ingress", "server", "mixed":
	default:
		return JoinToken{}, invalid("role must be worker, ingress, server or mixed")
	}
	ttl := strings.TrimSpace(req.TTL)
	if ttl == "" {
		ttl = defaultJoinTTL
	}
	tokenRole := role
	if role == "mixed" || role == "server" {
		tokenRole = "server"
	}

	token, err := s.clusterToken(row)
	if err != nil {
		return JoinToken{}, err
	}
	client := s.aerolvmClient(row.AdminURL, token)
	health, err := client.Health(ctx)
	if err != nil {
		return JoinToken{}, unavailable("cluster %q: %s", row.Slug, err)
	}
	join, err := client.CreateJoinToken(ctx, aerolvm.JoinTokenRequest{
		Role:     tokenRole,
		TTL:      ttl,
		Reusable: false,
	})
	if err != nil {
		return JoinToken{}, unavailable("cluster %q: %s", row.Slug, err)
	}
	joinURL := ""
	if len(join.JoinURLs) > 0 {
		joinURL = join.JoinURLs[0]
	}
	return JoinToken{
		ClusterID:  row.ID,
		Cluster:    row.Slug,
		Runtime:    row.Runtime,
		Version:    health.Version,
		Role:       role,
		TokenRole:  tokenRole,
		Token:      join.Token,
		CACertHash: join.CACertHash,
		JoinURLs:   join.JoinURLs,
		ExpiresAt:  join.ExpiresAt.UTC(),
		Command:    joinCommand(health.Version, joinURL, join.Token, join.CACertHash, role),
	}, nil
}

// releaseTag is an AerolVM release tag, as its installer accepts one.
var releaseTag = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+([.+-][0-9A-Za-z.-]+)?$`)

// joinCommand is the line a machine without the Lewbox CLI runs to join. It
// fetches the installer of the cluster's own version and checks it against
// that release's checksums, instead of piping whatever is newest into sudo.
// Every value is checked or quoted, because a person pastes this into a root
// shell and some of it comes from the cluster.
func joinCommand(version, joinURL, token, caHash, role string) string {
	if !releaseTag.MatchString(version) {
		return ""
	}
	base := "https://github.com/aerol-ai/microvm/releases/download/" + version
	return fmt.Sprintf("curl -fsSLO %s/install.sh && curl -fsSL %s/checksums.txt | grep '  install.sh$' | sha256sum -c - && "+
		"sudo bash install.sh join %s --token %s --ca-cert-hash %s --role %s",
		base, base, shellQuote(joinURL), shellQuote(token), shellQuote(caHash), shellQuote(role))
}

// shellQuote quotes s for a POSIX shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// CheckAllClusters checks every cluster as the local principal. It is used by
// the orchestrator's background loop and returns a joined error.
func (s *Service) CheckAllClusters(ctx context.Context) error {
	ctx = WithPrincipal(ctx, LocalPrincipal())
	rows, err := s.q.ListClusters(ctx)
	if err != nil {
		return fmt.Errorf("list clusters: %w", err)
	}
	var errs []error
	for _, row := range rows {
		if _, err := s.CheckCluster(ctx, row.ID); err != nil {
			errs = append(errs, fmt.Errorf("check cluster %q: %w", row.Slug, err))
		}
	}
	return errors.Join(errs...)
}

type clusterSpec struct {
	Slug        string
	AdminURL    string
	EndpointURL string
	Runtime     string
	AdminToken  string
}

type checkResult struct {
	fatal            bool
	principalID      string
	version          string
	members          []aerolvm.Member
	membersAvailable bool
}

// runChecks runs the cluster requirements in order and stops at the first
// fatal failure. It returns the checks it ran and the facts later steps need.
func (s *Service) runChecks(ctx context.Context, spec clusterSpec) ([]Check, checkResult) {
	checks := make([]Check, 0, 7)
	var result checkResult
	add := func(c Check) { checks = append(checks, c) }
	client := s.aerolvmClient(spec.AdminURL, spec.AdminToken)

	health, err := client.Health(ctx)
	if err != nil {
		result.fatal = true
		add(Check{
			Name:   "health",
			Fatal:  true,
			Detail: fmt.Sprintf("GET %s/health failed: %v; check admin_url and that sandboxd is running", spec.AdminURL, err),
		})
		return checks, result
	}
	result.version = health.Version
	add(Check{Name: "health", OK: true, Detail: fmt.Sprintf("%s, %d node(s)", health.Version, health.ClusterNodes)})

	if !versionAtLeast(health.Version, minAerolVMVersion) {
		result.fatal = true
		add(Check{
			Name:   "version",
			Fatal:  true,
			Detail: fmt.Sprintf("%s is older than %s; upgrade AerolVM on every node", health.Version, minAerolVMVersion),
		})
		return checks, result
	}
	add(Check{Name: "version", OK: true, Detail: health.Version})

	who, err := client.WhoAmI(ctx)
	if err != nil {
		result.fatal = true
		detail := fmt.Sprintf("GET %s/v1/whoami failed: %v", spec.AdminURL, err)
		var apiErr *aerolvm.APIError
		if errors.As(err, &apiErr) && apiErr.Status == 401 {
			detail = "whoami returned 401: check SB_IDENTITY_ENABLED=true on every node and that the token is an admin token (aerolvm token create --role admin --team '*' --expires-in 36500d)"
		}
		add(Check{Name: "admin_token", Fatal: true, Detail: detail})
		return checks, result
	}
	if who.Role != "admin" || who.Team != "*" {
		result.fatal = true
		add(Check{
			Name:   "admin_token",
			Fatal:  true,
			Detail: fmt.Sprintf("token is %q on team %q, want admin on *; mint one with aerolvm token create --role admin --team '*' --expires-in 36500d", who.Role, who.Team),
		})
		return checks, result
	}
	result.principalID = who.Principal.ID
	add(Check{Name: "admin_token", OK: true, Detail: "admin on team *"})

	members, err := client.Members(ctx)
	if err != nil {
		var apiErr *aerolvm.APIError
		if errors.As(err, &apiErr) && apiErr.Status == 503 && strings.Contains(apiErr.Message, "not enabled") {
			result.members = []aerolvm.Member{{
				NodeID:   spec.Slug,
				NodeName: spec.Slug,
				APIURL:   spec.AdminURL,
				Role:     "mixed",
				Alive:    true,
				Version:  health.Version,
			}}
			result.membersAvailable = true
			add(Check{Name: "members", OK: true, Detail: "single node, not in cluster mode"})
		} else {
			add(Check{
				Name:   "members",
				Detail: fmt.Sprintf("GET %s/v1/cluster/members failed: %v", spec.AdminURL, err),
			})
		}
	} else {
		result.members = members.Members
		// An empty list can't be right (the answering node is a member), and
		// syncing it would unregister every node.
		result.membersAvailable = len(members.Members) > 0
		var down []string
		for _, m := range members.Members {
			if !m.Alive {
				down = append(down, memberName(m))
			}
		}
		switch {
		case len(members.Members) == 0:
			add(Check{Name: "members", Detail: "the cluster listed no members; nodes were left as they were"})
		case len(down) > 0:
			add(Check{
				Name:   "members",
				Detail: fmt.Sprintf("nodes not alive: %s; check sandboxd on them", strings.Join(down, ", ")),
			})
		default:
			add(Check{Name: "members", OK: true, Detail: fmt.Sprintf("%d member(s)", len(members.Members))})
		}
	}

	add(runtimeCheck(result.members, spec.Runtime))

	token, err := client.CreateToken(ctx, aerolvm.CreateTokenRequest{
		Name:      "lewbox-ttl-check",
		Role:      "runner",
		Team:      "lewbox-check",
		ExpiresIn: "36500d",
	})
	if err != nil {
		add(Check{
			Name:   "token_ttl",
			Detail: fmt.Sprintf("minting a 36500d runner token failed: %v; set SB_TOKEN_MAX_TTL=876000h on every node so keys can last", err),
		})
	} else {
		_ = client.RevokeToken(ctx, token.ID)
		add(Check{Name: "token_ttl", OK: true, Detail: "a 36500d runner token was created and revoked"})
	}

	if spec.EndpointURL == spec.AdminURL {
		add(Check{Name: "endpoint", OK: true, Detail: "same as admin_url"})
	} else {
		endpoint := s.aerolvmClient(spec.EndpointURL, "")
		if _, err := endpoint.Health(ctx); err != nil {
			add(Check{
				Name:   "endpoint",
				Detail: fmt.Sprintf("GET %s/health failed: %v; check endpoint_url", spec.EndpointURL, err),
			})
		} else {
			add(Check{Name: "endpoint", OK: true, Detail: fmt.Sprintf("GET %s/health answered", spec.EndpointURL)})
		}
	}
	return checks, result
}

func runtimeCheck(members []aerolvm.Member, runtime string) Check {
	if len(members) == 0 {
		return Check{Name: "runtime", OK: true, Detail: "no members reported"}
	}
	var missing, unknown []string
	for _, m := range members {
		if len(m.Capacity.SupportedRuntimes) == 0 {
			unknown = append(unknown, memberName(m))
			continue
		}
		found := false
		for _, r := range m.Capacity.SupportedRuntimes {
			if r == runtime {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, fmt.Sprintf("%s (%s)", memberName(m), strings.Join(m.Capacity.SupportedRuntimes, ", ")))
		}
	}
	if len(missing) > 0 {
		return Check{
			Name:   "runtime",
			Detail: fmt.Sprintf("%s does not support %s; install the runtime there or register the cluster with the other runtime", strings.Join(missing, ", "), runtime),
		}
	}
	if len(unknown) == len(members) {
		return Check{Name: "runtime", OK: true, Detail: "runtimes not reported yet by " + strings.Join(unknown, ", ")}
	}
	detail := fmt.Sprintf("members support %s", runtime)
	if len(unknown) > 0 {
		detail += "; not reported yet by " + strings.Join(unknown, ", ")
	}
	return Check{Name: "runtime", OK: true, Detail: detail}
}

// syncNodes mirrors members into nodes. It returns a failed members check for
// every name that already belongs to another cluster.
func (s *Service) syncNodes(ctx context.Context, cluster db.Cluster, members []aerolvm.Member) ([]Check, error) {
	now := s.now()
	seen := make(map[string]bool, len(members))
	conflicts := make([]Check, 0)
	for _, m := range members {
		name := strings.TrimSpace(m.NodeID)
		if name == "" {
			name = strings.TrimSpace(m.NodeName)
		}
		if name == "" {
			continue
		}
		seen[name] = true

		existing, err := s.q.GetNodeByName(ctx, name)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("get node: %w", err)
		}
		if err == nil && existing.ClusterID.Valid && existing.ClusterID.String != cluster.ID {
			slug := existing.ClusterID.String
			if other, err := s.q.GetClusterByID(ctx, existing.ClusterID.String); err == nil {
				slug = other.Slug
			}
			conflicts = append(conflicts, Check{
				Name:   "members",
				Detail: fmt.Sprintf("node %q already belongs to cluster %q; rename it or remove it there", name, slug),
			})
			continue
		}

		id := uuid.NewV7().String()
		createdAt := now
		url := m.APIURL
		lastSeen := sql.NullTime{}
		if err == nil {
			id = existing.ID
			createdAt = existing.CreatedAt
			if url == "" {
				url = existing.URL
			}
			if !m.Alive && existing.LastSeenAt.Valid {
				lastSeen = existing.LastSeenAt
			}
		}
		if m.Alive {
			lastSeen = sql.NullTime{Time: now, Valid: true}
		}
		_, err = s.q.UpsertClusterNode(ctx, db.UpsertClusterNodeParams{
			ID:         id,
			Name:       name,
			URL:        url,
			ClusterID:  sql.NullString{String: cluster.ID, Valid: true},
			Role:       m.Role,
			Version:    m.Version,
			Alive:      sql.NullInt64{Int64: boolInt(m.Alive), Valid: true},
			LastSeenAt: lastSeen,
			CreatedAt:  createdAt,
		})
		if err != nil {
			return nil, fmt.Errorf("upsert node %q: %w", name, err)
		}
	}

	rows, err := s.q.ListNodesByCluster(ctx, sql.NullString{String: cluster.ID, Valid: true})
	if err != nil {
		return nil, fmt.Errorf("list cluster nodes: %w", err)
	}
	for _, row := range rows {
		if seen[row.Name] {
			continue
		}
		if _, err := s.q.DeleteNode(ctx, row.ID); err != nil {
			return nil, fmt.Errorf("delete node %q: %w", row.Name, err)
		}
	}
	return conflicts, nil
}

func (s *Service) clusterDetail(ctx context.Context, id string) (ClusterDetail, error) {
	row, err := s.q.GetClusterByID(ctx, id)
	if err != nil {
		return ClusterDetail{}, fmt.Errorf("get cluster: %w", err)
	}
	checks := make([]Check, 0)
	if strings.TrimSpace(row.Checks) != "" {
		if err := json.Unmarshal([]byte(row.Checks), &checks); err != nil {
			return ClusterDetail{}, fmt.Errorf("decode cluster checks: %w", err)
		}
	}
	if checks == nil {
		checks = make([]Check, 0)
	}
	rows, err := s.q.ListNodesByCluster(ctx, sql.NullString{String: row.ID, Valid: true})
	if err != nil {
		return ClusterDetail{}, fmt.Errorf("list cluster nodes: %w", err)
	}
	nodes := make([]Node, 0, len(rows))
	for _, r := range rows {
		n := toNode(r)
		n.Cluster = row.Slug
		nodes = append(nodes, n)
	}
	return ClusterDetail{Cluster: toCluster(row, len(nodes)), Checks: checks, NodeList: nodes}, nil
}

func (s *Service) findCluster(ctx context.Context, ref string) (db.Cluster, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return db.Cluster{}, invalid("a cluster ID or slug is required")
	}
	var (
		row db.Cluster
		err error
	)
	if id, perr := uuid.Parse(ref); perr == nil {
		row, err = s.q.GetClusterByID(ctx, id.String())
	} else {
		row, err = s.q.GetClusterBySlug(ctx, ref)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return db.Cluster{}, notFound("cluster %q not found", ref)
	}
	if err != nil {
		return db.Cluster{}, fmt.Errorf("get cluster: %w", err)
	}
	return row, nil
}

func (s *Service) resolveClusterRow(ctx context.Context, ref string) (db.Cluster, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return db.Cluster{}, invalid("a cluster ID, slug, or node name is required")
	}
	if rest, ok := strings.CutPrefix(ref, "cluster/"); ok {
		return s.findCluster(ctx, rest)
	}
	if rest, ok := strings.CutPrefix(ref, "node/"); ok {
		return s.clusterOfNode(ctx, rest)
	}

	if id, err := uuid.Parse(ref); err == nil {
		if row, err := s.q.GetClusterByID(ctx, id.String()); err == nil {
			return row, nil
		} else if !errors.Is(err, sql.ErrNoRows) {
			return db.Cluster{}, fmt.Errorf("get cluster: %w", err)
		}
		return s.clusterOfNode(ctx, id.String())
	}

	cluster, clusterErr := s.q.GetClusterBySlug(ctx, ref)
	if clusterErr != nil && !errors.Is(clusterErr, sql.ErrNoRows) {
		return db.Cluster{}, fmt.Errorf("get cluster: %w", clusterErr)
	}
	node, nodeErr := s.q.GetNodeByName(ctx, ref)
	if nodeErr != nil && !errors.Is(nodeErr, sql.ErrNoRows) {
		return db.Cluster{}, fmt.Errorf("get node: %w", nodeErr)
	}

	clusterFound := clusterErr == nil
	nodeFound := nodeErr == nil && node.ClusterID.Valid
	if clusterFound && nodeFound && node.ClusterID.String != cluster.ID {
		nodeCluster, err := s.q.GetClusterByID(ctx, node.ClusterID.String)
		if err != nil {
			return db.Cluster{}, fmt.Errorf("get cluster: %w", err)
		}
		return db.Cluster{}, conflict("%q is both a cluster and a node in cluster %q; say cluster/%s or node/%s",
			ref, nodeCluster.Slug, cluster.Slug, node.Name)
	}
	if clusterFound {
		return cluster, nil
	}
	if nodeFound {
		return s.clusterOfNode(ctx, node.ID)
	}
	return db.Cluster{}, notFound("cluster %q not found", ref)
}

func (s *Service) clusterOfNode(ctx context.Context, ref string) (db.Cluster, error) {
	node, err := s.findNode(ctx, ref)
	if err != nil {
		return db.Cluster{}, err
	}
	if !node.ClusterID.Valid {
		return db.Cluster{}, notFound("node %q is not in a cluster", ref)
	}
	row, err := s.q.GetClusterByID(ctx, node.ClusterID.String)
	if err != nil {
		return db.Cluster{}, fmt.Errorf("get cluster: %w", err)
	}
	return row, nil
}

func (s *Service) clusterWithCount(ctx context.Context, row db.Cluster) (Cluster, error) {
	rows, err := s.q.ListNodesByCluster(ctx, sql.NullString{String: row.ID, Valid: true})
	if err != nil {
		return Cluster{}, fmt.Errorf("list cluster nodes: %w", err)
	}
	return toCluster(row, len(rows)), nil
}

func (s *Service) clusterToken(row db.Cluster) (string, error) {
	if s.keyring == nil {
		return "", errors.New("no keyring configured")
	}
	plaintext, err := s.keyring.Open(row.AdminTokenEnc)
	if err != nil {
		return "", fmt.Errorf("open admin token: %w", err)
	}
	return string(plaintext), nil
}

func (s *Service) aerolvmClient(baseURL, token string) *aerolvm.Client {
	return aerolvm.New(baseURL, token, s.aerolvmOptions...)
}

// clusterClient opens the stored admin token and returns a client for the
// cluster's admin URL.
func (s *Service) clusterClient(row db.Cluster) (*aerolvm.Client, error) {
	token, err := s.clusterToken(row)
	if err != nil {
		return nil, err
	}
	return s.aerolvmClient(row.AdminURL, token), nil
}

func (s *Service) nodeCounts(ctx context.Context) (map[string]int, error) {
	rows, err := s.q.CountNodesByCluster(ctx)
	if err != nil {
		return nil, fmt.Errorf("count nodes: %w", err)
	}
	counts := make(map[string]int, len(rows))
	for _, row := range rows {
		if row.ClusterID.Valid {
			counts[row.ClusterID.String] = int(row.N)
		}
	}
	return counts, nil
}

func (s *Service) storeCheck(ctx context.Context, id, status, detail, version string, checks []Check) error {
	if err := s.q.UpdateClusterCheck(ctx, db.UpdateClusterCheckParams{
		Status:       status,
		StatusDetail: detail,
		Version:      version,
		Checks:       encodeChecks(checks),
		CheckedAt:    sql.NullTime{Time: s.now(), Valid: true},
		ID:           id,
	}); err != nil {
		return fmt.Errorf("update cluster: %w", err)
	}
	return nil
}

// applyConflicts folds node-sync conflicts into the members check, so a
// cluster has one members verdict, not two.
func applyConflicts(checks, conflicts []Check) []Check {
	for _, conflict := range conflicts {
		merged := false
		for i := range checks {
			if checks[i].Name != conflict.Name {
				continue
			}
			checks[i].OK = false
			if checks[i].Detail == "" {
				checks[i].Detail = conflict.Detail
			} else {
				checks[i].Detail += "; " + conflict.Detail
			}
			merged = true
			break
		}
		if !merged {
			checks = append(checks, conflict)
		}
	}
	return checks
}

func summarizeChecks(checks []Check) (status, detail string) {
	status = "ready"
	for _, c := range checks {
		if c.OK {
			continue
		}
		status = "degraded"
		if detail == "" {
			detail = c.Detail
		}
	}
	return status, detail
}

func healthFailed(checks []Check) bool {
	return len(checks) > 0 && checks[0].Name == "health" && !checks[0].OK
}

func encodeChecks(checks []Check) string {
	data, err := json.Marshal(checks)
	if err != nil {
		return "[]"
	}
	return string(data)
}

func toCluster(r db.Cluster, nodes int) Cluster {
	c := Cluster{
		ID:              r.ID,
		Slug:            r.Slug,
		Name:            r.Name,
		AdminURL:        r.AdminURL,
		EndpointURL:     r.EndpointURL,
		Runtime:         r.Runtime,
		AdminTokenLast4: r.AdminTokenLast4,
		Status:          r.Status,
		StatusDetail:    r.StatusDetail,
		Version:         r.Version,
		Nodes:           nodes,
		CreatedAt:       r.CreatedAt.UTC(),
	}
	if r.CheckedAt.Valid {
		t := r.CheckedAt.Time.UTC()
		c.CheckedAt = &t
	}
	return c
}

func memberName(m aerolvm.Member) string {
	if m.NodeID != "" {
		return m.NodeID
	}
	return m.NodeName
}

func last4(s string) string {
	if len(s) <= 4 {
		return s
	}
	return s[len(s)-4:]
}

// versionAtLeast compares vMAJOR.MINOR.PATCH and ignores any suffix.
func versionAtLeast(got, want string) bool {
	g, ok := parseVersion(got)
	if !ok {
		return false
	}
	w, ok := parseVersion(want)
	if !ok {
		return false
	}
	for i := range g {
		if g[i] != w[i] {
			return g[i] > w[i]
		}
	}
	return true
}

func parseVersion(v string) ([3]int, bool) {
	var out [3]int
	s := strings.TrimSpace(v)
	s = strings.TrimPrefix(s, "v")
	if i := strings.IndexAny(s, "-+ "); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) < 3 {
		return out, false
	}
	for i := 0; i < 3; i++ {
		n, err := strconv.Atoi(parts[i])
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}
