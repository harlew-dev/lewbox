package aerolvm

import (
	"errors"
	"time"
)

// ErrNoRollout means the cluster has never had a rollout (GET answers 404).
// It is not an error for status reads: a cluster that was never upgraded is
// simply on its first release.
var ErrNoRollout = errors.New("this cluster never had a rollout")

// Upgrade rollout states, as the API reports them in "state". They are
// AerolVM's names; Lewbox maps them in internal/service so the console sees
// one stable vocabulary.
const (
	UpgradeStateStaging     = "staging"
	UpgradeStateRunning     = "running"
	UpgradeStatePaused      = "paused"
	UpgradeStateDone        = "done"
	UpgradeStateFailed      = "failed"
	UpgradeStateStopped     = "stopped"
	UpgradeStateFinalizing  = "finalizing"
	UpgradeStateFinalized   = "finalized"
	UpgradeStateRollingBack = "rolling-back"
	UpgradeStateRolledBack  = "rolled-back"
)

// Per-node upgrade states, as the API reports them.
const (
	UpgradeNodePending   = "pending"
	UpgradeNodeStaged    = "staged"
	UpgradeNodeUpgrading = "upgrading"
	UpgradeNodeDone      = "done"
	UpgradeNodeFailed    = "failed"
	UpgradeNodeManual    = "manual"
	UpgradeNodeSkipped   = "skipped"
)

// Upgrade directions.
const (
	UpgradeDirectionUpgrade  = "upgrade"
	UpgradeDirectionRollback = "rollback"
)

// Upgrade sources.
const (
	UpgradeSourceRelease = "release"
	UpgradeSourceUpload  = "upload"
)

// Error codes the upgrade routes answer with.
const (
	UpgradeCodeNeedsDowntime = "needs_allow_downtime"
	UpgradeCodeSkew          = "version_skew"
)

// UpgradeApplyRequest is the body of POST /v1/cluster/upgrade.
type UpgradeApplyRequest struct {
	Version       string            `json:"version"`
	Files         map[string]string `json:"files"`
	Source        string            `json:"source"`
	Hold          bool              `json:"hold,omitempty"`
	Batch         string            `json:"batch,omitempty"`
	PauseAfter    int               `json:"pause_after,omitempty"`
	NodeTimeout   string            `json:"node_timeout,omitempty"`
	AllowDowntime bool              `json:"allow_downtime,omitempty"`
}

// UpgradeRollout is one rollout as GET/POST /v1/cluster/upgrade reports it.
type UpgradeRollout struct {
	ID              string        `json:"id"`
	State           string        `json:"state"`
	Direction       string        `json:"direction"`
	TargetVersion   string        `json:"target_version"`
	PreviousVersion string        `json:"previous_version,omitempty"`
	Source          string        `json:"source"`
	Caller          string        `json:"caller"`
	Hold            bool          `json:"hold,omitempty"`
	Batch           string        `json:"batch"`
	PauseAfter      int           `json:"pause_after,omitempty"`
	NodeTimeout     string        `json:"node_timeout"`
	AllowDowntime   bool          `json:"allow_downtime,omitempty"`
	CreatedAt       time.Time     `json:"created_at"`
	UpdatedAt       time.Time     `json:"updated_at"`
	Error           string        `json:"error,omitempty"`
	ActiveVersion   string        `json:"active_version,omitempty"`
	Nodes           []UpgradeNode `json:"nodes"`
}

// UpgradeNode is one node's progress in a rollout.
type UpgradeNode struct {
	NodeID        string    `json:"node_id"`
	Role          string    `json:"role"`
	Arch          string    `json:"arch"`
	State         string    `json:"state"`
	Version       string    `json:"version,omitempty"`
	Order         int       `json:"order"`
	Batch         int       `json:"batch,omitempty"`
	Error         string    `json:"error,omitempty"`
	ManualCommand string    `json:"manual_command,omitempty"`
	Migrated      bool      `json:"migrated,omitempty"`
	UpdatedAt     time.Time `json:"updated_at"`
}
