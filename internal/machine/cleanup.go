package machine

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/harlew-dev/lewbox/internal/aerolvm"
)

// ArchivePrefix starts the name of every snapshot Lewbox takes to archive a
// stopped sandbox (DECISIONS D36). Customers' own snapshots never match it,
// so the cleanup leaves them alone.
const ArchivePrefix = "archive-"

// The cleanup timer runs `lewbox node cleanup` as root on every node of a
// cluster with storage.
const (
	CleanupBinary    = "/usr/local/lib/lewbox/lewbox"
	CleanupService   = "/etc/systemd/system/lewbox-archive-cleanup.service"
	CleanupTimer     = "/etc/systemd/system/lewbox-archive-cleanup.timer"
	CleanupTimerName = "lewbox-archive-cleanup.timer"
)

// CleanupServiceUnit runs one cleanup pass that removes local copies at
// least olderThan old (the cluster's archive_local_retention). It runs a
// root-owned copy of the lewbox binary, never the user's own, so the user
// can't swap what root runs.
func CleanupServiceUnit(olderThan time.Duration) string {
	flags := " node cleanup --json"
	if olderThan > 0 {
		flags += " --older-than " + olderThan.String()
	}
	return `# Written by lewbox node configure.
[Unit]
Description=Lewbox: remove local copies of archived sandboxes
After=sandboxd.service docker.service

[Service]
Type=oneshot
Environment=LEWBOX_ALLOW_ROOT=1
ExecStart=` + CleanupBinary + flags + `
`
}

// CleanupTimerUnit runs the cleanup every five minutes.
const CleanupTimerUnit = `# Written by lewbox node configure.
[Unit]
Description=Lewbox: remove local copies of archived sandboxes, every 5 minutes

[Timer]
OnBootSec=2min
OnUnitActiveSec=5min
RandomizedDelaySec=30s

[Install]
WantedBy=timers.target
`

// CleanupResult says what one cleanup pass removed. Kept lists images still
// used by a container, which a later pass removes. Young lists images newer
// than the retention, which a later pass removes once they are old enough.
type CleanupResult struct {
	Removed []string `json:"removed"`
	Kept    []string `json:"kept"`
	Young   []string `json:"young"`
}

// CleanupArchives removes this node's copies of archived snapshots, which the
// bucket already holds (docs/design/snapshot-archive.md, findings 3 and 5):
//
//   - the local image of every archive-* snapshot whose push is active, once
//     the snapshot is olderThan old;
//   - images pulled from the node registry for archive-* snapshots, once the
//     pull is olderThan old.
//
// An image whose age can't be read is kept unless olderThan is zero. docker
// refuses to remove an image a container uses; those are kept for a later
// pass. client must be able to list every team's snapshots on this node (the
// PAT over loopback). sudo is false when already root.
func CleanupArchives(ctx context.Context, runner Runner, client *aerolvm.Client, sudo bool, olderThan time.Duration, now time.Time) (CleanupResult, error) {
	result := CleanupResult{Removed: []string{}, Kept: []string{}, Young: []string{}}
	snapshots, err := client.ListSnapshots(ctx)
	if err != nil {
		return result, err
	}
	out, err := runner.Output(ctx, Cmd{Name: "docker", Args: []string{"images", "--format", "{{.Repository}}:{{.Tag}}"}, Sudo: sudo})
	if err != nil {
		return result, err
	}
	var refs []string
	local := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		if ref := strings.TrimSpace(line); ref != "" && !local[ref] {
			local[ref] = true
			refs = append(refs, ref)
		}
	}
	seen := map[string]bool{}
	var images []string
	for _, snap := range snapshots {
		if !strings.HasPrefix(snap.Name, ArchivePrefix) || snap.PushState != "active" || snap.ImageRegistryRef == "" {
			continue
		}
		// The snapshot's record outlives its local image, which an earlier
		// pass removed: skip it rather than fail to remove it every pass.
		if image := strings.TrimSpace(snap.Image); image != "" && local[image] && !seen[image] && !strings.HasPrefix(image, RegistryAddr+"/") {
			seen[image] = true
			if olderThan > 0 && (snap.CreatedAt.IsZero() || now.Sub(snap.CreatedAt) < olderThan) {
				result.Young = append(result.Young, image)
				continue
			}
			images = append(images, image)
		}
	}

	for _, ref := range refs {
		if isPulledArchive(ref) && !seen[ref] {
			seen[ref] = true
			if olderThan > 0 {
				pulled, ok := imageTime(ctx, runner, ref, sudo)
				if !ok || now.Sub(pulled) < olderThan {
					result.Young = append(result.Young, ref)
					continue
				}
			}
			images = append(images, ref)
		}
	}

	for _, image := range images {
		if err := runner.Run(ctx, Cmd{Name: "docker", Args: []string{"image", "rm", image}, Sudo: sudo}); err != nil {
			result.Kept = append(result.Kept, image)
			continue
		}
		result.Removed = append(result.Removed, image)
	}
	return result, nil
}

// imageTime is when an image arrived on this node: the later of its creation
// and its last tag, which a pull sets. ok is false when neither can be read.
func imageTime(ctx context.Context, runner Runner, ref string, sudo bool) (time.Time, bool) {
	out, err := runner.Output(ctx, Cmd{
		Name: "docker",
		Args: []string{"image", "inspect", "--format", "{{json .Created}} {{json .Metadata.LastTagTime}}", ref},
		Sudo: sudo,
	})
	if err != nil {
		return time.Time{}, false
	}
	var latest time.Time
	for _, field := range strings.Fields(string(out)) {
		var t time.Time
		if json.Unmarshal([]byte(field), &t) == nil && t.After(latest) {
			latest = t
		}
	}
	return latest, !latest.IsZero() && latest.Year() > 1
}

// isPulledArchive reports whether ref is an archive snapshot pulled from the
// node registry: 127.0.0.1:5000/cluster/<id>/snapshots/<tenant>/archive-…:<tag>.
func isPulledArchive(ref string) bool {
	rest, ok := strings.CutPrefix(ref, RegistryAddr+"/cluster/")
	if !ok || !strings.Contains(rest, "/snapshots/") {
		return false
	}
	repo := rest
	if i := strings.LastIndex(repo, ":"); i > strings.LastIndex(repo, "/") {
		repo = repo[:i]
	}
	last := repo[strings.LastIndex(repo, "/")+1:]
	return strings.HasPrefix(last, ArchivePrefix)
}
