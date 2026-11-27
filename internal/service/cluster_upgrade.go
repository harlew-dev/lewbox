package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/harlew-dev/lewbox/internal/aerolvm"
	"github.com/harlew-dev/lewbox/internal/db"
)

// upgradeCallTimeout bounds one rollout call: a Raft write, a leader
// transfer, or the command that follows one.
const upgradeCallTimeout = 2 * time.Minute

// ClusterUpgrade is one AerolVM rollout as Lewbox reports it. The states are
// Lewbox's own: AerolVM's "rolling-back" and "rolled-back" use an underscore
// here, and an unrecognised state is "unknown" rather than passed through.
type ClusterUpgrade struct {
	Cluster         string               `json:"cluster"`
	ClusterID       string               `json:"cluster_id"`
	ID              string               `json:"id"`
	Phase           string               `json:"phase"`
	Direction       string               `json:"direction"`
	TargetVersion   string               `json:"target_version"`
	PreviousVersion string               `json:"previous_version,omitempty"`
	Source          string               `json:"source"`
	Hold            bool                 `json:"hold"`
	Batch           string               `json:"batch"`
	PauseAfter      int                  `json:"pause_after,omitempty"`
	AllowDowntime   bool                 `json:"allow_downtime"`
	Error           string               `json:"error,omitempty"`
	CreatedAt       time.Time            `json:"created_at"`
	UpdatedAt       time.Time            `json:"updated_at"`
	Nodes           []ClusterUpgradeNode `json:"nodes"`
}

// ClusterUpgradeNode is one node's progress in a rollout: the version it was
// on, the version the rollout moves it to, and its state.
type ClusterUpgradeNode struct {
	Node        string    `json:"node"`
	Role        string    `json:"role"`
	FromVersion string    `json:"from_version,omitempty"`
	ToVersion   string    `json:"to_version,omitempty"`
	State       string    `json:"state"`
	Order       int       `json:"order"`
	Batch       int       `json:"batch,omitempty"`
	Error       string    `json:"error,omitempty"`
	Migrated    bool      `json:"migrated"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ClusterUpgradePlan is what an upgrade would do. It changes nothing, and it
// is computed from the cluster's live members and the target release's
// release.json, the same way `aerolvm upgrade plan` does.
type ClusterUpgradePlan struct {
	Cluster               string                   `json:"cluster"`
	ClusterID             string                   `json:"cluster_id"`
	TargetVersion         string                   `json:"target_version"`
	Tested                bool                     `json:"tested"`
	Source                string                   `json:"source"`
	Allowed               bool                     `json:"allowed"`
	Refusals              []string                 `json:"refusals"`
	Warnings              []string                 `json:"warnings"`
	Skew                  ClusterUpgradeSkew       `json:"skew"`
	OneWayMigrations      []int                    `json:"one_way_migrations"`
	RollbackAfterFinalize bool                     `json:"rollback_after_finalize"`
	Batch                 string                   `json:"batch"`
	BatchSize             int                      `json:"batch_size"`
	PauseAfter            int                      `json:"pause_after,omitempty"`
	Nodes                 []ClusterUpgradePlanNode `json:"nodes"`
	Metadata              aerolvm.ReleaseMetadata  `json:"metadata"`
}

// ClusterUpgradeSkew is the version jump the plan checked.
type ClusterUpgradeSkew struct {
	UpgradeFromMin string   `json:"upgrade_from_min,omitempty"`
	Below          []string `json:"below"`
}

// ClusterUpgradePlanNode is one node in the plan: its place in the order and
// what the rollout would do with it.
type ClusterUpgradePlanNode struct {
	Node          string `json:"node"`
	Role          string `json:"role"`
	Version       string `json:"version"`
	TargetVersion string `json:"target_version"`
	Alive         bool   `json:"alive"`
	Voter         bool   `json:"voter"`
	Leader        bool   `json:"leader"`
	Order         int    `json:"order,omitempty"`
	Batch         int    `json:"batch,omitempty"`
	Action        string `json:"action"`
}

// Actions in a plan.
const (
	upgradeActionUpgrade  = "upgrade"
	upgradeActionOnTarget = "skip_target"
	upgradeActionDown     = "skip_down"
)

// Tiers, as AerolVM orders a rollout.
const (
	upgradeTierServers = "servers"
	upgradeTierIngress = "ingress"
	upgradeTierWorkers = "workers"
)

// PlanClusterUpgradeRequest is the input to PlanClusterUpgrade. Version empty
// means the version this Lewbox release was tested with.
type PlanClusterUpgradeRequest struct {
	Version    string `json:"version"`
	Batch      string `json:"batch"`
	PauseAfter int    `json:"pause_after"`
}

// StartClusterUpgradeRequest is the input to StartClusterUpgrade.
type StartClusterUpgradeRequest struct {
	Version       string `json:"version"`
	Batch         string `json:"batch"`
	PauseAfter    int    `json:"pause_after"`
	Hold          bool   `json:"hold"`
	AllowDowntime bool   `json:"allow_downtime"`
}

// GetClusterUpgrade returns the cluster's current rollout. A cluster that
// never had one is notFound.
func (s *Service) GetClusterUpgrade(ctx context.Context, ref string) (ClusterUpgrade, error) {
	if _, err := requireAdmin(ctx); err != nil {
		return ClusterUpgrade{}, err
	}
	row, err := s.findCluster(ctx, ref)
	if err != nil {
		return ClusterUpgrade{}, err
	}
	client, err := s.clusterClient(row)
	if err != nil {
		return ClusterUpgrade{}, err
	}
	callCtx, cancel := context.WithTimeout(ctx, upgradeCallTimeout)
	defer cancel()
	out, err := client.UpgradeStatus(callCtx)
	if err != nil {
		return ClusterUpgrade{}, upgradeError(row, err)
	}
	return mapUpgrade(row, out), nil
}

// PlanClusterUpgrade fetches the target release, checks it against its
// checksums, and says what an upgrade would do. It fails closed when the
// release or the plan cannot be read.
func (s *Service) PlanClusterUpgrade(ctx context.Context, ref string, req PlanClusterUpgradeRequest) (ClusterUpgradePlan, error) {
	if _, err := requireAdmin(ctx); err != nil {
		return ClusterUpgradePlan{}, err
	}
	row, err := s.findCluster(ctx, ref)
	if err != nil {
		return ClusterUpgradePlan{}, err
	}
	plan, _, err := s.clusterUpgradePlan(ctx, row, req)
	return plan, err
}

// StartClusterUpgrade plans the upgrade again (so the server refuses a skew
// the caller never saw), then starts AerolVM's rolling upgrade and records
// one event. The AerolVM call is outside any transaction.
func (s *Service) StartClusterUpgrade(ctx context.Context, ref string, req StartClusterUpgradeRequest) (ClusterUpgrade, error) {
	if _, err := requireAdmin(ctx); err != nil {
		return ClusterUpgrade{}, err
	}
	row, err := s.findCluster(ctx, ref)
	if err != nil {
		return ClusterUpgrade{}, err
	}
	planReq := PlanClusterUpgradeRequest{Version: req.Version, Batch: req.Batch, PauseAfter: req.PauseAfter}
	plan, release, err := s.clusterUpgradePlan(ctx, row, planReq)
	if err != nil {
		return ClusterUpgrade{}, err
	}
	if !plan.Allowed {
		return ClusterUpgrade{}, invalidField("version", "%s", strings.Join(plan.Refusals, "; "))
	}
	client, err := s.clusterClient(row)
	if err != nil {
		return ClusterUpgrade{}, err
	}
	callCtx, cancel := context.WithTimeout(ctx, upgradeCallTimeout)
	defer cancel()
	out, err := client.StartUpgrade(callCtx, aerolvm.UpgradeApplyRequest{
		Version:       release.Version,
		Files:         release.Files,
		Source:        aerolvm.UpgradeSourceRelease,
		Hold:          req.Hold,
		Batch:         req.Batch,
		PauseAfter:    req.PauseAfter,
		AllowDowntime: req.AllowDowntime,
	})
	if err != nil {
		return ClusterUpgrade{}, upgradeError(row, err)
	}
	if err := s.record(ctx, Event{
		Name:       "cluster.upgrade.started",
		TargetType: "cluster",
		TargetID:   row.ID,
		TargetName: row.Slug,
		Changes: map[string]Change{
			"version": {From: out.PreviousVersion, To: out.TargetVersion},
		},
	}); err != nil {
		return ClusterUpgrade{}, err
	}
	return mapUpgrade(row, out), nil
}

// StopClusterUpgrade finishes the nodes in progress and starts no more.
func (s *Service) StopClusterUpgrade(ctx context.Context, ref string) (ClusterUpgrade, error) {
	return s.upgradeCommand(ctx, ref, "stop")
}

// ResumeClusterUpgrade goes on after a pause.
func (s *Service) ResumeClusterUpgrade(ctx context.Context, ref string) (ClusterUpgrade, error) {
	return s.upgradeCommand(ctx, ref, "resume")
}

// RollbackClusterUpgrade puts every node back on the previous version.
func (s *Service) RollbackClusterUpgrade(ctx context.Context, ref string) (ClusterUpgrade, error) {
	return s.upgradeCommand(ctx, ref, "rollback")
}

// FinalizeClusterUpgrade turns the new version on for good; after it a
// rollback is no longer possible.
func (s *Service) FinalizeClusterUpgrade(ctx context.Context, ref string) (ClusterUpgrade, error) {
	return s.upgradeCommand(ctx, ref, "finalize")
}

// upgradeCommand sends one rollout command and records its event. The
// AerolVM call is outside any transaction.
func (s *Service) upgradeCommand(ctx context.Context, ref, command string) (ClusterUpgrade, error) {
	if _, err := requireAdmin(ctx); err != nil {
		return ClusterUpgrade{}, err
	}
	row, err := s.findCluster(ctx, ref)
	if err != nil {
		return ClusterUpgrade{}, err
	}
	client, err := s.clusterClient(row)
	if err != nil {
		return ClusterUpgrade{}, err
	}
	callCtx, cancel := context.WithTimeout(ctx, upgradeCallTimeout)
	defer cancel()
	var out aerolvm.UpgradeRollout
	switch command {
	case "stop":
		out, err = client.StopUpgrade(callCtx)
	case "resume":
		out, err = client.ResumeUpgrade(callCtx)
	case "rollback":
		out, err = client.RollbackUpgrade(callCtx)
	case "finalize":
		out, err = client.FinalizeUpgrade(callCtx)
	default:
		return ClusterUpgrade{}, fmt.Errorf("unknown upgrade command %q", command)
	}
	if err != nil {
		return ClusterUpgrade{}, upgradeError(row, err)
	}
	if err := s.record(ctx, Event{
		Name:       "cluster.upgrade." + upgradeEventFact(command),
		TargetType: "cluster",
		TargetID:   row.ID,
		TargetName: row.Slug,
	}); err != nil {
		return ClusterUpgrade{}, err
	}
	return mapUpgrade(row, out), nil
}

// upgradeEventFact turns a command into an event's past-tense fact.
func upgradeEventFact(command string) string {
	switch command {
	case "stop":
		return "stopped"
	case "resume":
		return "resumed"
	case "rollback":
		return "rolled_back"
	case "finalize":
		return "finalized"
	}
	return command
}

// clusterUpgradePlan builds the plan and returns the checked release it is
// based on, so Start can send the same files.
func (s *Service) clusterUpgradePlan(ctx context.Context, row db.Cluster, req PlanClusterUpgradeRequest) (ClusterUpgradePlan, aerolvm.ReleaseFiles, error) {
	version := strings.TrimSpace(req.Version)
	if version == "" {
		version = aerolvm.PinnedVersion
	}
	if version == "latest" {
		return ClusterUpgradePlan{}, aerolvm.ReleaseFiles{}, invalidField("version", "latest is not supported; give a release tag such as v0.7.1")
	}
	if !releaseTag.MatchString(version) {
		return ClusterUpgradePlan{}, aerolvm.ReleaseFiles{}, invalidField("version", "%q is not an AerolVM release tag such as v0.7.0", version)
	}
	if req.PauseAfter < 0 {
		return ClusterUpgradePlan{}, aerolvm.ReleaseFiles{}, invalidField("pause_after", "must not be negative")
	}

	planCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	release, err := aerolvm.NewReleaseSource(s.aerolvmRelease).Fetch(planCtx, version)
	if err != nil {
		return ClusterUpgradePlan{}, aerolvm.ReleaseFiles{}, unavailable("can't read AerolVM %s to plan the upgrade: %v", version, err)
	}

	client, err := s.clusterClient(row)
	if err != nil {
		return ClusterUpgradePlan{}, aerolvm.ReleaseFiles{}, err
	}
	members, err := client.Members(planCtx)
	if err != nil {
		return ClusterUpgradePlan{}, aerolvm.ReleaseFiles{}, unavailable("cluster %q: %v", row.Slug, err)
	}
	if len(members.Members) == 0 {
		return ClusterUpgradePlan{}, aerolvm.ReleaseFiles{}, unavailable("cluster %q reports no nodes, so an upgrade can't be planned", row.Slug)
	}

	plan := ClusterUpgradePlan{
		Cluster:               row.Slug,
		ClusterID:             row.ID,
		TargetVersion:         version,
		Tested:                version == aerolvm.PinnedVersion,
		Source:                release.URL,
		Refusals:              []string{},
		Warnings:              []string{},
		OneWayMigrations:      []int{},
		RollbackAfterFinalize: true,
		Nodes:                 []ClusterUpgradePlanNode{},
		Metadata:              release.Metadata,
		PauseAfter:            req.PauseAfter,
	}
	if len(release.Metadata.OneWayMigrations) > 0 {
		plan.OneWayMigrations = append(plan.OneWayMigrations, release.Metadata.OneWayMigrations...)
		plan.RollbackAfterFinalize = false
	}
	if !plan.Tested {
		plan.Warnings = append(plan.Warnings, fmt.Sprintf(
			"AerolVM %s is not the version this Lewbox release was tested with (%s); the upgrade is untested",
			version, aerolvm.PinnedVersion))
	}

	byTier := map[string][]ClusterUpgradePlanNode{}
	voters, votersDown := 0, 0
	var down []string
	seen := map[string]bool{}
	for _, m := range members.Members {
		name := strings.TrimSpace(m.NodeName)
		if name == "" {
			name = m.NodeID
		}
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		tier := upgradeTierOf(m.Role)
		node := ClusterUpgradePlanNode{
			Node: name, Role: m.Role, Version: m.Version, TargetVersion: version,
			Alive: m.Alive, Voter: m.Voter, Leader: m.Leader,
		}
		switch {
		case !m.Alive:
			node.Action = upgradeActionDown
			down = append(down, name)
		case m.Version == version:
			node.Action = upgradeActionOnTarget
		default:
			node.Action = upgradeActionUpgrade
		}
		byTier[tier] = append(byTier[tier], node)
		if m.Voter {
			voters++
			if !m.Alive {
				votersDown++
			}
		}
	}

	// Servers go last within their tier (the leader last of all), their own
	// order; ingress next; workers in batches. With one voter AerolVM moves
	// that server after the workers instead.
	servers := byTier[upgradeTierServers]
	sort.SliceStable(servers, func(i, j int) bool {
		if servers[i].Leader != servers[j].Leader {
			return !servers[i].Leader
		}
		return servers[i].Node < servers[j].Node
	})
	ingress := byTier[upgradeTierIngress]
	sort.Slice(ingress, func(i, j int) bool { return ingress[i].Node < ingress[j].Node })
	workers := byTier[upgradeTierWorkers]
	sort.Slice(workers, func(i, j int) bool { return workers[i].Node < workers[j].Node })

	if len(workers) > 0 {
		plan.Batch = strings.TrimSpace(req.Batch)
		if plan.Batch == "" {
			plan.Batch = "10%"
		}
		size, err := parseUpgradeBatch(plan.Batch, len(workers))
		if err != nil {
			return ClusterUpgradePlan{}, aerolvm.ReleaseFiles{}, invalidField("batch", "%v", err)
		}
		plan.BatchSize = size
	} else if strings.TrimSpace(req.Batch) != "" {
		plan.Batch = strings.TrimSpace(req.Batch)
		if _, err := parseUpgradeBatch(plan.Batch, 1); err != nil {
			return ClusterUpgradePlan{}, aerolvm.ReleaseFiles{}, invalidField("batch", "%v", err)
		}
	}

	tiers := [][]ClusterUpgradePlanNode{servers, ingress, workers}
	if voters == 1 {
		tiers = [][]ClusterUpgradePlanNode{ingress, workers, servers}
	}
	order := 0
	for _, nodes := range tiers {
		for i := range nodes {
			node := &nodes[i]
			if node.Action == upgradeActionUpgrade {
				order++
				node.Order = order
			}
		}
	}
	batchNo, inBatch := 0, 0
	size := plan.BatchSize
	for i := range workers {
		if workers[i].Action != upgradeActionUpgrade {
			continue
		}
		if batchNo == 0 || inBatch == size {
			batchNo++
			inBatch = 0
			if batchNo > 1 {
				size = plan.BatchSize
			}
			if batchNo == 1 && req.PauseAfter > 0 && req.PauseAfter < size {
				size = req.PauseAfter
			}
		}
		inBatch++
		workers[i].Batch = batchNo
	}
	plan.Nodes = append(append(append([]ClusterUpgradePlanNode{}, servers...), ingress...), workers...)

	if req.PauseAfter > 0 {
		if len(workers) == 0 {
			plan.Warnings = append(plan.Warnings, "--pause-after only pauses worker batches, and this cluster has no worker nodes")
		} else {
			plan.Warnings = append(plan.Warnings, fmt.Sprintf(
				"the rollout pauses after the first %d worker(s); go on with: lewbox cluster upgrade resume %s", min(req.PauseAfter, plan.BatchSize), row.Slug))
		}
	}
	switch {
	case voters == 1:
		plan.Warnings = append(plan.Warnings, "this cluster has one Raft voter: the API is down while it restarts (sandboxes keep running); start the upgrade with --allow-downtime")
	case voters == 2 && votersDown == 0:
		plan.Warnings = append(plan.Warnings, "this cluster has two Raft voters: while either one restarts, one of two is up, which is no majority, so the API is down during each server restart (sandboxes keep running); start the upgrade with --allow-downtime")
	}
	if votersDown > 0 && voters-votersDown-1 <= voters/2 {
		plan.Warnings = append(plan.Warnings, fmt.Sprintf("%d of %d Raft voters are down: restarting another would lose the majority, so the rollout will fail at the servers; bring them back first", votersDown, voters))
	}
	if len(down) > 0 {
		sort.Strings(down)
		plan.Warnings = append(plan.Warnings, fmt.Sprintf("down nodes are skipped and upgraded when they come back; finalize waits for them: %s", strings.Join(down, ", ")))
	}

	// Skew: every node's version must be a release tag between the target's
	// upgrade_from_min and the target itself.
	plan.Skew.UpgradeFromMin = release.Metadata.UpgradeFromMin
	plan.Skew.Below = []string{}
	versions := map[string]bool{}
	for _, node := range plan.Nodes {
		if !node.Alive || node.Version == "" || versions[node.Version] {
			continue
		}
		versions[node.Version] = true
		cmp, err := aerolvm.CompareVersions(node.Version, version)
		if err != nil {
			plan.Refusals = append(plan.Refusals, fmt.Sprintf("%s runs %s, which is not a release tag, so the upgrade can't be ordered", node.Node, node.Version))
			continue
		}
		if cmp > 0 {
			plan.Refusals = append(plan.Refusals, fmt.Sprintf("%s runs %s, which is newer than %s; going back is: lewbox cluster upgrade rollback %s", node.Node, node.Version, version, row.Slug))
			continue
		}
		if release.Metadata.UpgradeFromMin == "" {
			continue
		}
		floor, err := aerolvm.CompareVersions(node.Version, release.Metadata.UpgradeFromMin)
		if err != nil {
			plan.Refusals = append(plan.Refusals, fmt.Sprintf("AerolVM %s has an invalid upgrade_from_min %q", version, release.Metadata.UpgradeFromMin))
			continue
		}
		if floor < 0 {
			plan.Skew.Below = append(plan.Skew.Below, node.Version)
			plan.Refusals = append(plan.Refusals, fmt.Sprintf("%s runs %s, but %s needs %s or later; upgrade through the releases in between first",
				node.Node, node.Version, version, release.Metadata.UpgradeFromMin))
		}
	}
	sort.Strings(plan.Skew.Below)
	plan.Allowed = len(plan.Refusals) == 0
	return plan, release, nil
}

// upgradeTierOf maps an AerolVM role to the tier the rollout uses. A node
// with no role is an older build, which sandboxd reads as mixed: a server.
func upgradeTierOf(role string) string {
	switch role {
	case "ingress":
		return upgradeTierIngress
	case "worker":
		return upgradeTierWorkers
	}
	return upgradeTierServers
}

// parseUpgradeBatch turns "3" or "10%" into a worker batch size, at least 1.
func parseUpgradeBatch(batch string, workers int) (int, error) {
	b := strings.TrimSpace(batch)
	if b == "" {
		b = "10%"
	}
	if pct, ok := strings.CutSuffix(b, "%"); ok {
		p, err := strconv.ParseFloat(pct, 64)
		if err != nil || p <= 0 || p > 100 || math.IsNaN(p) {
			return 0, fmt.Errorf("a percentage must be above 0%% and at most 100%%")
		}
		n := int(math.Ceil(float64(workers) * p / 100))
		if n < 1 {
			n = 1
		}
		return n, nil
	}
	n, err := strconv.Atoi(b)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("give a count of at least 1 or a percentage such as 10%%")
	}
	return n, nil
}

// mapUpgrade turns AerolVM's rollout into Lewbox's own response.
func mapUpgrade(row db.Cluster, out aerolvm.UpgradeRollout) ClusterUpgrade {
	target := out.TargetVersion
	if out.Direction == aerolvm.UpgradeDirectionRollback {
		target = out.PreviousVersion
	}
	resp := ClusterUpgrade{
		Cluster:         row.Slug,
		ClusterID:       row.ID,
		ID:              out.ID,
		Phase:           mapUpgradeState(out.State),
		Direction:       out.Direction,
		TargetVersion:   out.TargetVersion,
		PreviousVersion: out.PreviousVersion,
		Source:          out.Source,
		Hold:            out.Hold,
		Batch:           out.Batch,
		PauseAfter:      out.PauseAfter,
		AllowDowntime:   out.AllowDowntime,
		Error:           out.Error,
		CreatedAt:       out.CreatedAt.UTC(),
		UpdatedAt:       out.UpdatedAt.UTC(),
		Nodes:           make([]ClusterUpgradeNode, 0, len(out.Nodes)),
	}
	for _, n := range out.Nodes {
		nodeTarget := target
		if n.State == aerolvm.UpgradeNodeSkipped && nodeTarget == "" {
			nodeTarget = out.TargetVersion
		}
		resp.Nodes = append(resp.Nodes, ClusterUpgradeNode{
			Node:        n.NodeID,
			Role:        n.Role,
			FromVersion: n.Version,
			ToVersion:   nodeTarget,
			State:       mapUpgradeNodeState(n.State),
			Order:       n.Order,
			Batch:       n.Batch,
			Error:       n.Error,
			Migrated:    n.Migrated,
			UpdatedAt:   n.UpdatedAt.UTC(),
		})
	}
	return resp
}

// mapUpgradeState keeps AerolVM's clear state names but never passes an
// unknown one through.
func mapUpgradeState(state string) string {
	switch state {
	case aerolvm.UpgradeStateStaging, aerolvm.UpgradeStateRunning, aerolvm.UpgradeStatePaused,
		aerolvm.UpgradeStateDone, aerolvm.UpgradeStateFailed, aerolvm.UpgradeStateStopped,
		aerolvm.UpgradeStateFinalizing, aerolvm.UpgradeStateFinalized:
		return state
	case aerolvm.UpgradeStateRollingBack:
		return "rolling_back"
	case aerolvm.UpgradeStateRolledBack:
		return "rolled_back"
	default:
		return "unknown"
	}
}

func mapUpgradeNodeState(state string) string {
	switch state {
	case aerolvm.UpgradeNodePending, aerolvm.UpgradeNodeStaged, aerolvm.UpgradeNodeUpgrading,
		aerolvm.UpgradeNodeDone, aerolvm.UpgradeNodeFailed, aerolvm.UpgradeNodeManual,
		aerolvm.UpgradeNodeSkipped:
		return state
	default:
		return "unknown"
	}
}

// upgradeError turns an AerolVM upgrade refusal into a clear Lewbox error.
func upgradeError(row db.Cluster, err error) error {
	if errors.Is(err, aerolvm.ErrNoRollout) {
		return notFound("no upgrade has run on cluster %q", row.Slug)
	}
	var apiErr *aerolvm.APIError
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.Code == aerolvm.UpgradeCodeNeedsDowntime:
			return conflict("cluster %q needs --allow-downtime: %s", row.Slug, apiErr.Message)
		case apiErr.Code == aerolvm.UpgradeCodeSkew:
			return invalidField("version", "%s", apiErr.Message)
		case apiErr.Status == http.StatusNotFound:
			return notFound("no upgrade has run on cluster %q", row.Slug)
		case apiErr.Status == http.StatusConflict:
			return conflict("cluster %q: %s", row.Slug, apiErr.Message)
		case apiErr.Status == http.StatusBadRequest:
			return invalid("cluster %q refused the upgrade: %s", row.Slug, apiErr.Message)
		case apiErr.Status == http.StatusServiceUnavailable:
			return unavailable("cluster %q: %s", row.Slug, apiErr.Message)
		}
	}
	return unavailable("cluster %q: %v", row.Slug, err)
}
