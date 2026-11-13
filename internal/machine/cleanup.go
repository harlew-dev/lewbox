package machine

import (
	"context"
	"strings"

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

// CleanupServiceUnit runs one cleanup pass. It runs a root-owned copy of the
// lewbox binary, never the user's own, so the user can't swap what root runs.
const CleanupServiceUnit = `# Written by lewbox node configure.
[Unit]
Description=Lewbox: remove local copies of archived sandboxes
After=sandboxd.service docker.service

[Service]
Type=oneshot
Environment=LEWBOX_ALLOW_ROOT=1
ExecStart=` + CleanupBinary + ` node cleanup --json
`

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

// CleanupResult says what one cleanup pass removed. Kept counts images still
// used by a container, which a later pass removes.
type CleanupResult struct {
	Removed []string `json:"removed"`
	Kept    []string `json:"kept"`
}

// CleanupArchives removes this node's copies of archived snapshots, which the
// bucket already holds (docs/design/snapshot-archive.md, findings 3 and 5):
//
//   - the local image of every archive-* snapshot whose push is active;
//   - images pulled from the node registry for archive-* snapshots.
//
// docker refuses to remove an image a container uses; those are kept for a
// later pass. client must be able to list every team's snapshots on this
// node (the PAT over loopback). sudo is false when already root.
func CleanupArchives(ctx context.Context, runner Runner, client *aerolvm.Client, sudo bool) (CleanupResult, error) {
	result := CleanupResult{Removed: []string{}, Kept: []string{}}
	snapshots, err := client.ListSnapshots(ctx)
	if err != nil {
		return result, err
	}
	seen := map[string]bool{}
	var images []string
	for _, snap := range snapshots {
		if !strings.HasPrefix(snap.Name, ArchivePrefix) || snap.PushState != "active" || snap.ImageRegistryRef == "" {
			continue
		}
		if image := strings.TrimSpace(snap.Image); image != "" && !seen[image] && !strings.HasPrefix(image, RegistryAddr+"/") {
			seen[image] = true
			images = append(images, image)
		}
	}

	out, err := runner.Output(ctx, Cmd{Name: "docker", Args: []string{"images", "--format", "{{.Repository}}:{{.Tag}}"}, Sudo: sudo})
	if err != nil {
		return result, err
	}
	for _, line := range strings.Split(string(out), "\n") {
		ref := strings.TrimSpace(line)
		if isPulledArchive(ref) && !seen[ref] {
			seen[ref] = true
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
