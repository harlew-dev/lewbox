package service

import (
	"strings"
	"time"
)

// Archive local retention values: how long a node keeps its local copy of an
// archived sandbox before the cleanup timer removes it. The bucket always
// holds the archive; a local copy only makes a restore on that node faster.
const (
	RetentionInstant = "instant"
	RetentionNever   = "never"
)

// archiveRetentions maps each retention to the minimum age of a local copy
// the cleanup removes. never has no entry: its nodes run no cleanup.
var archiveRetentions = map[string]time.Duration{
	RetentionInstant: 0,
	"1h":             time.Hour,
	"1d":             24 * time.Hour,
	"7d":             7 * 24 * time.Hour,
}

// ArchiveRetentionAge returns the minimum age of a local copy the cleanup
// removes under a retention, and false for never (no cleanup at all). An
// unknown value is treated as instant, the default.
func ArchiveRetentionAge(retention string) (time.Duration, bool) {
	if retention == RetentionNever {
		return 0, false
	}
	return archiveRetentions[retention], true
}

// clusterArchiveRetention checks the archive_local_retention setting. An empty
// value is instant.
func clusterArchiveRetention(raw string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		value = RetentionInstant
	}
	if _, ok := archiveRetentions[value]; !ok && value != RetentionNever {
		return "", invalid("archive_local_retention must be instant, 1h, 1d, 7d or never")
	}
	return value, nil
}

// Archive snapshot name styles.
const (
	snapshotNameID   = "id"   // archive-<sandbox id>
	snapshotNameName = "name" // archive-<sandbox name>-<sandbox id>
)

// clusterArchiveSnapshotName checks the archive_snapshot_name setting. An
// empty value is id.
func clusterArchiveSnapshotName(raw string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		value = snapshotNameID
	}
	if value != snapshotNameID && value != snapshotNameName {
		return "", invalid("archive_snapshot_name must be id or name")
	}
	return value, nil
}

// maxSnapshotNamePart caps the sandbox name's share of a snapshot name, so
// the registry repository path stays well inside Docker's limits.
const maxSnapshotNamePart = 40

// archiveSnapshotName names the snapshot that archives a sandbox. It always
// starts with archive- (node cleanup, the bucket delete guard and the
// customer snapshot list find archives by it) and always ends with the
// sandbox ID (names are not unique, and a restored sandbox keeps its name).
// The name style adds the sandbox's name, lowercased to letters, digits and
// dashes; a name with nothing left falls back to the ID alone.
func archiveSnapshotName(style, sandboxID, sandboxName string) string {
	if style == snapshotNameName {
		if part := snapshotNamePart(sandboxName); part != "" {
			return "archive-" + part + "-" + sandboxID
		}
	}
	return "archive-" + sandboxID
}

// snapshotNamePart reduces a sandbox name to lowercase letters, digits and
// single dashes, at most maxSnapshotNamePart long.
func snapshotNamePart(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case b.Len() > 0 && !dash:
			b.WriteByte('-')
			dash = true
		}
	}
	part := strings.TrimRight(b.String(), "-")
	if len(part) > maxSnapshotNamePart {
		part = strings.TrimRight(part[:maxSnapshotNamePart], "-")
	}
	return part
}
