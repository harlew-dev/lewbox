package service

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/harlew-dev/lewbox/internal/db"
)

// Advanced AerolVM settings Lewbox writes on every node of a cluster. A null
// value means off, or AerolVM's own default.
const (
	// DefaultDockerTimeout is deliberately longer than AerolVM's 180s: the
	// Docker client's default times out a snapshot commit of a large
	// container.
	DefaultDockerTimeout = 15 * time.Minute
	minDockerTimeout     = time.Minute
	maxDockerTimeout     = 2 * time.Hour

	minSandboxCPU      = 1
	minSandboxMemoryMB = 1024
	minSandboxDiskGB   = 10

	maxLifecycle = 720 * time.Hour

	minFirecrackerSlots = 256
	maxFirecrackerSlots = 1024
)

// AdvancedSettings is the per-cluster AerolVM tuning Lewbox manages. It is
// embedded flat in Cluster and NodeSettings.
type AdvancedSettings struct {
	// DockerTimeout is SB_HTTP_CLIENT_TIMEOUT, a Go duration from 1m to 2h.
	DockerTimeout string `json:"docker_timeout"`
	// MaxSandboxCPU, MaxSandboxMemoryMB and MaxSandboxDiskGB cap a sandbox's
	// size in the create policy. Null means no cap.
	MaxSandboxCPU      *float64 `json:"max_sandbox_cpu"`
	MaxSandboxMemoryMB *int64   `json:"max_sandbox_memory_mb"`
	MaxSandboxDiskGB   *int64   `json:"max_sandbox_disk_gb"`
	// IdleStopMinutes is SB_IDLE_TIMEOUT_MIN: stop a running sandbox idle
	// this many minutes when it has no lifecycle timers of its own.
	IdleStopMinutes *int64 `json:"idle_stop_minutes"`
	// DestroyIfIdleFor is SB_DEFAULT_DESTROY_IF_IDLE_FOR, and MaxAge is
	// SB_MAX_AGE. Both are Go durations of at most 720h; empty means off.
	DestroyIfIdleFor string `json:"destroy_if_idle_for"`
	MaxAge           string `json:"max_age"`
	// FirecrackerVMSlots is SB_FIRECRACKER_TAP_POOL_SIZE. Firecracker
	// clusters only; null means AerolVM's default of 256.
	FirecrackerVMSlots *int64 `json:"firecracker_vm_slots"`
}

// DefaultAdvancedSettings is what a cluster gets when nothing is set: only
// Lewbox's longer Docker timeout.
func DefaultAdvancedSettings() AdvancedSettings {
	return AdvancedSettings{DockerTimeout: formatDuration(DefaultDockerTimeout)}
}

// formatDuration drops the trailing zero units Go prints: 1h rather than
// 1h0m0s, which reads the same in an env file and in docs.
func formatDuration(d time.Duration) string {
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = s[:len(s)-2]
	}
	if strings.HasSuffix(s, "h0m") {
		s = s[:len(s)-2]
	}
	return s
}

// Optional is a PATCH field that distinguishes an absent field from an
// explicit null: Set is false when the field was absent, and Value is nil when
// it was null (which clears the setting).
type Optional[T any] struct {
	Set   bool
	Value *T
}

// OptionalOf is a PATCH field set to a value.
func OptionalOf[T any](value T) Optional[T] {
	return Optional[T]{Set: true, Value: &value}
}

// OptionalNull is a PATCH field set to null, which clears the setting.
func OptionalNull[T any]() Optional[T] {
	return Optional[T]{Set: true}
}

// UnmarshalJSON records both a value and an explicit null. The request uses a
// value (not a pointer) so encoding/json calls this for null too.
func (o *Optional[T]) UnmarshalJSON(data []byte) error {
	o.Set = true
	if strings.TrimSpace(string(data)) == "null" {
		o.Value = nil
		return nil
	}
	var value T
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	o.Value = &value
	return nil
}

// MarshalJSON writes a value, or null when the field is cleared. An absent
// Optional never reaches here: UpdateClusterRequest omits it.
func (o Optional[T]) MarshalJSON() ([]byte, error) {
	if o.Value == nil {
		return []byte("null"), nil
	}
	return json.Marshal(*o.Value)
}

// optionalStringValue is the value of a cleared-or-set string field.
func optionalStringValue(o Optional[string]) string {
	if o.Value == nil {
		return ""
	}
	return *o.Value
}

// advancedFromAdd builds the requested settings from a create request.
func advancedFromAdd(req AddClusterRequest) AdvancedSettings {
	out := AdvancedSettings{
		DockerTimeout:      req.DockerTimeout,
		DestroyIfIdleFor:   req.DestroyIfIdleFor,
		MaxAge:             req.MaxAge,
		MaxSandboxCPU:      req.MaxSandboxCPU,
		MaxSandboxMemoryMB: req.MaxSandboxMemoryMB,
		MaxSandboxDiskGB:   req.MaxSandboxDiskGB,
		IdleStopMinutes:    req.IdleStopMinutes,
		FirecrackerVMSlots: req.FirecrackerVMSlots,
	}
	return out
}

// advancedFromRow reads a cluster's stored settings.
func advancedFromRow(r db.Cluster) AdvancedSettings {
	out := AdvancedSettings{DockerTimeout: r.DockerTimeout}
	if out.DockerTimeout == "" {
		out.DockerTimeout = formatDuration(DefaultDockerTimeout)
	}
	if r.MaxSandboxCpu.Valid {
		v := r.MaxSandboxCpu.Float64
		out.MaxSandboxCPU = &v
	}
	if r.MaxSandboxMemoryMb.Valid {
		v := r.MaxSandboxMemoryMb.Int64
		out.MaxSandboxMemoryMB = &v
	}
	if r.MaxSandboxDiskGb.Valid {
		v := r.MaxSandboxDiskGb.Int64
		out.MaxSandboxDiskGB = &v
	}
	if r.IdleStopMinutes.Valid {
		v := r.IdleStopMinutes.Int64
		out.IdleStopMinutes = &v
	}
	if r.DestroyIfIdleFor.Valid {
		out.DestroyIfIdleFor = r.DestroyIfIdleFor.String
	}
	if r.MaxAge.Valid {
		out.MaxAge = r.MaxAge.String
	}
	if r.FirecrackerVmSlots.Valid {
		v := r.FirecrackerVmSlots.Int64
		out.FirecrackerVMSlots = &v
	}
	return out
}

// advancedParams turns settings into the columns of CreateCluster and
// UpdateClusterSettings.
func advancedParams(a AdvancedSettings) (string, sql.NullFloat64, sql.NullInt64, sql.NullInt64, sql.NullInt64, sql.NullString, sql.NullString, sql.NullInt64) {
	timeout := a.DockerTimeout
	if timeout == "" {
		timeout = formatDuration(DefaultDockerTimeout)
	}
	var cpu sql.NullFloat64
	if a.MaxSandboxCPU != nil {
		cpu = sql.NullFloat64{Float64: *a.MaxSandboxCPU, Valid: true}
	}
	var memory, disk, idle, slots sql.NullInt64
	if a.MaxSandboxMemoryMB != nil {
		memory = sql.NullInt64{Int64: *a.MaxSandboxMemoryMB, Valid: true}
	}
	if a.MaxSandboxDiskGB != nil {
		disk = sql.NullInt64{Int64: *a.MaxSandboxDiskGB, Valid: true}
	}
	if a.IdleStopMinutes != nil {
		idle = sql.NullInt64{Int64: *a.IdleStopMinutes, Valid: true}
	}
	if a.FirecrackerVMSlots != nil {
		slots = sql.NullInt64{Int64: *a.FirecrackerVMSlots, Valid: true}
	}
	return timeout, cpu, memory, disk, idle,
		sql.NullString{String: a.DestroyIfIdleFor, Valid: a.DestroyIfIdleFor != ""},
		sql.NullString{String: a.MaxAge, Valid: a.MaxAge != ""},
		slots
}

// NormalizeAdvanced validates a cluster's advanced settings and returns their
// canonical form. The CLI uses it before writing a new cluster's first files,
// because AddCluster's own validation runs after that node is configured.
func NormalizeAdvanced(settings AdvancedSettings, runtime string) (AdvancedSettings, error) {
	return normalizeAdvanced(settings, runtime, nil)
}

// normalizeAdvanced validates every setting and returns its canonical form.
// current is the cluster's existing value, for the Firecracker pool's
// raise-only rule; it is nil when creating a cluster.
func normalizeAdvanced(a AdvancedSettings, runtime string, current *AdvancedSettings) (AdvancedSettings, error) {
	out := AdvancedSettings{}
	var err error

	out.DockerTimeout, err = clusterDockerTimeout(a.DockerTimeout)
	if err != nil {
		return AdvancedSettings{}, err
	}
	out.MaxSandboxCPU, err = clusterSandboxCPU(a.MaxSandboxCPU)
	if err != nil {
		return AdvancedSettings{}, err
	}
	out.MaxSandboxMemoryMB, err = clusterSandboxMemoryMB(a.MaxSandboxMemoryMB)
	if err != nil {
		return AdvancedSettings{}, err
	}
	out.MaxSandboxDiskGB, err = clusterSandboxDiskGB(a.MaxSandboxDiskGB)
	if err != nil {
		return AdvancedSettings{}, err
	}
	out.IdleStopMinutes, err = clusterIdleStopMinutes(a.IdleStopMinutes)
	if err != nil {
		return AdvancedSettings{}, err
	}
	out.DestroyIfIdleFor, err = clusterLifecycleDuration("destroy_if_idle_for", a.DestroyIfIdleFor)
	if err != nil {
		return AdvancedSettings{}, err
	}
	out.MaxAge, err = clusterLifecycleDuration("max_age", a.MaxAge)
	if err != nil {
		return AdvancedSettings{}, err
	}
	var previous *int64
	if current != nil {
		previous = current.FirecrackerVMSlots
	}
	out.FirecrackerVMSlots, err = clusterFirecrackerSlots(a.FirecrackerVMSlots, runtime, previous)
	if err != nil {
		return AdvancedSettings{}, err
	}
	return out, nil
}

func clusterDockerTimeout(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return formatDuration(DefaultDockerTimeout), nil
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return "", invalidField("docker_timeout", "docker_timeout must be a duration such as 15m, from 1m to 2h")
	}
	if d < minDockerTimeout || d > maxDockerTimeout {
		return "", invalidField("docker_timeout", "docker_timeout must be between 1m and 2h")
	}
	return formatDuration(d), nil
}

func clusterSandboxCPU(raw *float64) (*float64, error) {
	if raw == nil {
		return nil, nil
	}
	if math.IsNaN(*raw) || math.IsInf(*raw, 0) || *raw < minSandboxCPU {
		return nil, invalidField("max_sandbox_cpu", "max_sandbox_cpu must be at least %d: AerolVM's default sandbox size is %d CPU, so a lower cap refuses every create that leaves the size out", minSandboxCPU, minSandboxCPU)
	}
	value := *raw
	return &value, nil
}

func clusterSandboxMemoryMB(raw *int64) (*int64, error) {
	if raw == nil {
		return nil, nil
	}
	if *raw < minSandboxMemoryMB {
		return nil, invalidField("max_sandbox_memory_mb", "max_sandbox_memory_mb must be at least %d: AerolVM's default sandbox size is %d MB, so a lower cap refuses every create that leaves the size out", minSandboxMemoryMB, minSandboxMemoryMB)
	}
	value := *raw
	return &value, nil
}

func clusterSandboxDiskGB(raw *int64) (*int64, error) {
	if raw == nil {
		return nil, nil
	}
	if *raw < minSandboxDiskGB {
		return nil, invalidField("max_sandbox_disk_gb", "max_sandbox_disk_gb must be at least %d: AerolVM's default sandbox size is %d GB, so a lower cap refuses every create that leaves the size out", minSandboxDiskGB, minSandboxDiskGB)
	}
	value := *raw
	return &value, nil
}

func clusterIdleStopMinutes(raw *int64) (*int64, error) {
	if raw == nil || *raw == 0 {
		return nil, nil
	}
	if *raw < 0 {
		return nil, invalidField("idle_stop_minutes", "idle_stop_minutes must be a whole number of minutes")
	}
	value := *raw
	return &value, nil
}

func clusterLifecycleDuration(field, raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return "", invalidField(field, "%s must be a duration such as 24h, at most 720h", field)
	}
	if d < 0 {
		return "", invalidField(field, "%s must not be negative", field)
	}
	if d == 0 {
		return "", nil
	}
	if d > maxLifecycle {
		return "", invalidField(field, "%s must be at most 720h", field)
	}
	return formatDuration(d), nil
}

// clusterFirecrackerSlots checks SB_FIRECRACKER_TAP_POOL_SIZE. AerolVM seeds
// the pool once and only ever adds slots: a lower value silently does nothing,
// so Lewbox refuses one below the cluster's current value.
func clusterFirecrackerSlots(raw *int64, runtime string, current *int64) (*int64, error) {
	if runtime != "firecracker" {
		if raw != nil {
			return nil, invalidField("firecracker_vm_slots", "firecracker_vm_slots only applies to Firecracker clusters")
		}
		return nil, nil
	}
	if raw == nil {
		if current != nil && *current > minFirecrackerSlots {
			return nil, invalidField("firecracker_vm_slots", "firecracker_vm_slots cannot be cleared to %d: it is %d and AerolVM keeps the pool it first seeded, so a lower value does nothing", minFirecrackerSlots, *current)
		}
		return nil, nil
	}
	if *raw < minFirecrackerSlots || *raw > maxFirecrackerSlots {
		return nil, invalidField("firecracker_vm_slots", "firecracker_vm_slots must be between %d and %d", minFirecrackerSlots, maxFirecrackerSlots)
	}
	if current != nil && *raw < *current {
		return nil, invalidField("firecracker_vm_slots", "firecracker_vm_slots cannot be lowered from %d to %d: AerolVM keeps the pool it first seeded, so a lower value does nothing", *current, *raw)
	}
	value := *raw
	return &value, nil
}

// advancedSet reports whether the cluster asks for anything beyond Lewbox's
// defaults. A node that reported none is not judged against a cluster that
// sets none.
func advancedSet(a AdvancedSettings) bool {
	for _, field := range []string{
		"docker_timeout", "max_sandbox_cpu", "max_sandbox_memory_mb", "max_sandbox_disk_gb",
		"idle_stop_minutes", "destroy_if_idle_for", "max_age", "firecracker_vm_slots",
	} {
		if advancedFieldSet(a, field) {
			return true
		}
	}
	return false
}

// advancedFieldSet reports whether one setting is not at its default.
func advancedFieldSet(a AdvancedSettings, field string) bool {
	switch field {
	case "docker_timeout":
		return a.DockerTimeout != "" && a.DockerTimeout != formatDuration(DefaultDockerTimeout)
	case "max_sandbox_cpu":
		return a.MaxSandboxCPU != nil
	case "max_sandbox_memory_mb":
		return a.MaxSandboxMemoryMB != nil
	case "max_sandbox_disk_gb":
		return a.MaxSandboxDiskGB != nil
	case "idle_stop_minutes":
		return a.IdleStopMinutes != nil
	case "destroy_if_idle_for":
		return a.DestroyIfIdleFor != ""
	case "max_age":
		return a.MaxAge != ""
	case "firecracker_vm_slots":
		return a.FirecrackerVMSlots != nil
	}
	return false
}

// advancedValue renders a setting for an audit change or a drift reason.
func advancedValue(a AdvancedSettings, field string) string {
	switch field {
	case "docker_timeout":
		return a.DockerTimeout
	case "max_sandbox_cpu":
		return formatOptionalFloat(a.MaxSandboxCPU)
	case "max_sandbox_memory_mb":
		return formatOptionalInt(a.MaxSandboxMemoryMB)
	case "max_sandbox_disk_gb":
		return formatOptionalInt(a.MaxSandboxDiskGB)
	case "idle_stop_minutes":
		return formatOptionalInt(a.IdleStopMinutes)
	case "destroy_if_idle_for":
		return a.DestroyIfIdleFor
	case "max_age":
		return a.MaxAge
	case "firecracker_vm_slots":
		return formatOptionalInt(a.FirecrackerVMSlots)
	}
	return ""
}

func formatOptionalFloat(value *float64) string {
	if value == nil {
		return ""
	}
	return strconv.FormatFloat(*value, 'f', -1, 64)
}

func formatOptionalInt(value *int64) string {
	if value == nil {
		return ""
	}
	return strconv.FormatInt(*value, 10)
}

// advancedAuditChanges adds one from/to entry per setting that changed.
func advancedAuditChanges(changes map[string]Change, before, after AdvancedSettings) {
	for _, field := range []string{
		"docker_timeout", "max_sandbox_cpu", "max_sandbox_memory_mb", "max_sandbox_disk_gb",
		"idle_stop_minutes", "destroy_if_idle_for", "max_age", "firecracker_vm_slots",
	} {
		from, to := advancedValue(before, field), advancedValue(after, field)
		if from != to {
			changes[field] = Change{From: from, To: to}
		}
	}
}

// advancedDriftReasons names every advanced setting a node has not applied as
// its cluster now asks.
func advancedDriftReasons(want AdvancedSettings, applied sql.NullString, runtime string) ([]string, error) {
	fields := []string{
		"docker_timeout", "max_sandbox_cpu", "max_sandbox_memory_mb", "max_sandbox_disk_gb",
		"idle_stop_minutes", "destroy_if_idle_for", "max_age",
	}
	// The pool exists only on Firecracker clusters.
	if runtime == "firecracker" {
		fields = append(fields, "firecracker_vm_slots")
	}
	reasons := make([]string, 0, len(fields))
	if !applied.Valid {
		for _, field := range fields {
			if !advancedFieldSet(want, field) {
				continue
			}
			reasons = append(reasons, fmt.Sprintf("%s is %s; this node applied none", field, advancedValue(want, field)))
		}
		return reasons, nil
	}
	var got AdvancedSettings
	if err := json.Unmarshal([]byte(applied.String), &got); err != nil {
		return nil, fmt.Errorf("decode node advanced settings: %w", err)
	}
	add := func(field string) {
		wantValue := advancedValue(want, field)
		gotValue := advancedValue(got, field)
		if wantValue == "" {
			wantValue = "off"
		}
		if gotValue == "" {
			gotValue = "off"
		}
		if wantValue != gotValue {
			reasons = append(reasons, fmt.Sprintf("%s is %s; this node applied %s", field, wantValue, gotValue))
		}
	}
	for _, field := range fields {
		add(field)
	}
	return reasons, nil
}
