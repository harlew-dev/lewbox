package service

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
	"uuid"

	"github.com/harlew-dev/lewbox/internal/aerolvm"
	"github.com/harlew-dev/lewbox/internal/db"
	"github.com/harlew-dev/lewbox/internal/objstore"
)

// Archive statuses. A row moves pending → pushing → archived, or leaves to
// cancelled, failed (then retried) or deleted (DECISIONS D36).
const (
	archivePending   = "pending"
	archivePushing   = "pushing"
	archiveArchived  = "archived"
	archiveFailed    = "failed"
	archiveCancelled = "cancelled"
	archiveDeleted   = "deleted"

	archiveRegistryHost = "127.0.0.1:5000"
)

const (
	// archiveStepTimeout bounds one archive step: the calls one pass makes
	// for one row.
	archiveStepTimeout = 30 * time.Second
	// archivePushTimeout is how long a snapshot push may stay unfinished
	// before the row is marked failed and retried later.
	archivePushTimeout = 30 * time.Minute
	// archiveRetryDelay is how long a failed row waits before another try.
	archiveRetryDelay = 10 * time.Minute
	// archiveDeferDelay is how long an AerolVM hiccup holds a row back.
	archiveDeferDelay = time.Minute
	// archivePerCluster caps the rows one cluster advances at once.
	archivePerCluster = 2
	// archiveRestoredTag records the sandbox an archive came from.
	archiveRestoredTag = "lewbox.restored_from"
)

// ArchivePass discovers stopped sandboxes on archive clusters and advances
// every due archive by one step. It is the archiver's background pass, so it
// runs as the local principal when the caller has none.
func (s *Service) ArchivePass(ctx context.Context) error {
	if _, ok := PrincipalFrom(ctx); ok {
		if _, err := requireAdmin(ctx); err != nil {
			return err
		}
	} else {
		ctx = WithPrincipal(ctx, LocalPrincipal())
	}
	s.archiveMu.Lock()
	defer s.archiveMu.Unlock()

	now := s.now()
	if err := s.discoverArchives(ctx, now); err != nil {
		return err
	}
	rows, err := s.q.ListDueArchives(ctx, sql.NullTime{Time: now, Valid: true})
	if err != nil {
		return fmt.Errorf("list due archives: %w", err)
	}

	// Archives of a cluster that is no longer archiving are left alone.
	clusters := make(map[string]db.Cluster, len(rows))
	groups := make(map[string][]db.Archive, len(rows))
	for _, row := range rows {
		cluster, ok := clusters[row.ClusterID]
		if !ok {
			cluster, err = s.q.GetClusterByID(ctx, row.ClusterID)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return fmt.Errorf("get archive cluster: %w", err)
			}
			clusters[row.ClusterID] = cluster
		}
		if cluster.StopPolicy != "archive" || !cluster.StorageProfileID.Valid || cluster.StorageProfileID.String == "" {
			continue
		}
		groups[row.ClusterID] = append(groups[row.ClusterID], row)
	}

	var (
		mu   sync.Mutex
		errs []error
		wg   sync.WaitGroup
	)
	for clusterID, list := range groups {
		cluster := clusters[clusterID]
		wg.Add(1)
		go func(cluster db.Cluster, list []db.Archive) {
			defer wg.Done()
			var inner sync.WaitGroup
			sem := make(chan struct{}, archivePerCluster)
			for _, row := range list {
				inner.Add(1)
				sem <- struct{}{}
				go func(row db.Archive) {
					defer inner.Done()
					defer func() { <-sem }()
					if err := s.advanceArchive(ctx, cluster, row); err != nil {
						s.log.Warn("archive step failed", "archive", row.ID, "cluster", cluster.Slug, "err", err)
						mu.Lock()
						errs = append(errs, fmt.Errorf("archive %q on %q: %w", row.SandboxID, cluster.Slug, err))
						mu.Unlock()
					}
				}(row)
			}
			inner.Wait()
		}(cluster, list)
	}
	wg.Wait()
	return errors.Join(errs...)
}

// discoverArchives inserts a pending row for every stopped customer sandbox
// on an archive cluster that has no live archive. The insert itself refuses
// a duplicate, so two passes cannot archive one sandbox twice.
func (s *Service) discoverArchives(ctx context.Context, now time.Time) error {
	candidates, err := s.q.ListArchivableSandboxes(ctx)
	if err != nil {
		return fmt.Errorf("list archivable sandboxes: %w", err)
	}
	for _, candidate := range candidates {
		id := uuid.NewV7().String()
		inserted, err := s.q.InsertArchive(ctx, db.InsertArchiveParams{
			ArchiveID: id,
			CreatedAt: now,
			ClusterID: candidate.ClusterID,
			SandboxID: candidate.SandboxID,
		})
		if err != nil {
			return fmt.Errorf("discover archive for sandbox %q: %w", candidate.SandboxID, err)
		}
		if inserted == 0 {
			continue
		}
		s.log.Info("archive state changed", "archive", id, "sandbox", candidate.SandboxID,
			"cluster", candidate.ClusterID, "from", "", "to", archivePending)
	}
	return nil
}

// advanceArchive takes one archive at most one step further.
func (s *Service) advanceArchive(ctx context.Context, cluster db.Cluster, row db.Archive) error {
	ctx, cancel := context.WithTimeout(ctx, archiveStepTimeout)
	defer cancel()

	org, err := s.q.GetOrganizationByID(ctx, row.OrgID)
	if err != nil {
		return fmt.Errorf("get archive organization: %w", err)
	}
	if !org.Team.Valid || org.Team.String == "" {
		return s.deferArchive(ctx, cluster, row, "the workspace has no AerolVM team")
	}
	token, err := s.archiveToken(ctx, cluster, org)
	if err != nil {
		return err
	}
	client := s.aerolvmClient(cluster.AdminURL, token.value)

	switch row.Status {
	case archivePending:
		return s.archivePending(ctx, cluster, row, client)
	case archivePushing:
		return s.archivePushing(ctx, cluster, row, client)
	case archiveFailed:
		// A failed row keeps its snapshot name: look at the push again with
		// a fresh deadline before deciding to take the snapshot over. The
		// deadline is set once, so a retried push that stays stuck fails
		// again after another archivePushTimeout.
		if !row.PushingSince.Valid {
			now := s.now()
			if err := s.q.RestartArchivePush(ctx, db.RestartArchivePushParams{
				PushingSince: sql.NullTime{Time: now, Valid: true},
				ID:           row.ID,
			}); err != nil {
				return fmt.Errorf("restart archive push: %w", err)
			}
			row.PushingSince = sql.NullTime{Time: now, Valid: true}
			row.NextAttemptAt = sql.NullTime{}
		}
		return s.archivePushing(ctx, cluster, row, client)
	}
	return nil
}

// archivePending reads the sandbox, takes its snapshot and waits for the
// push. The sandbox is gone or started again: the archive is cancelled.
func (s *Service) archivePending(ctx context.Context, cluster db.Cluster, row db.Archive, client *aerolvm.Client) error {
	sandbox, err := client.GetSandbox(ctx, row.SandboxID)
	if err != nil {
		if isStatus(err, http.StatusNotFound) {
			return s.cancelArchive(ctx, cluster, row, "the sandbox is gone")
		}
		return s.deferArchive(ctx, cluster, row, err.Error())
	}
	if sandbox.Status != "stopped" {
		return s.cancelArchive(ctx, cluster, row, "started again before it was archived")
	}

	if _, err := client.CreateSnapshot(ctx, row.SandboxID, row.SnapshotName); err != nil {
		switch {
		case isStatus(err, http.StatusConflict):
			// A step was interrupted after the snapshot was taken.
		case isStatus(err, http.StatusNotFound):
			return s.cancelArchive(ctx, cluster, row, "the sandbox is gone")
		default:
			return s.deferArchive(ctx, cluster, row, err.Error())
		}
	}

	tags, err := encodeTags(sandbox.Tags)
	if err != nil {
		return fmt.Errorf("encode archive tags: %w", err)
	}
	now := s.now()
	if err := s.q.MarkArchivePushing(ctx, db.MarkArchivePushingParams{
		SandboxName:  sandbox.Name,
		Image:        sandbox.Image,
		Runtime:      sandbox.Runtime,
		Cpu:          sandbox.CPU,
		MemoryMb:     sandbox.MemoryMB,
		DiskGb:       sandbox.DiskGB,
		Tags:         tags,
		PushingSince: sql.NullTime{Time: now, Valid: true},
		ID:           row.ID,
	}); err != nil {
		return fmt.Errorf("mark archive pushing: %w", err)
	}
	s.logChange(cluster, row, archivePushing)
	return nil
}

// archivePushing looks at the snapshot's push and finishes, retries or waits.
func (s *Service) archivePushing(ctx context.Context, cluster db.Cluster, row db.Archive, client *aerolvm.Client) error {
	snapshot, err := client.GetSnapshot(ctx, row.SnapshotName)
	if err != nil {
		if isStatus(err, http.StatusNotFound) {
			// The snapshot was never taken, or the node lost it: take it
			// again.
			if err := s.q.RetryArchive(ctx, row.ID); err != nil {
				return fmt.Errorf("retry archive: %w", err)
			}
			s.logChange(cluster, row, archivePending)
			return nil
		}
		return s.deferArchive(ctx, cluster, row, err.Error())
	}

	switch {
	case snapshot.PushState == "active" && snapshot.ImageRegistryRef != "":
		return s.finishArchive(ctx, cluster, row, client, snapshot)
	case snapshot.PushState == "error":
		return s.waitForPush(ctx, cluster, row, snapshot.PushError)
	default:
		// pending, pushing, or an active row without a ref: keep waiting.
		return s.waitForPush(ctx, cluster, row, "")
	}
}

// finishArchive deletes a still-stopped sandbox, or cancels the archive if it
// was started again. A sandbox that is already gone is archived anyway.
//
// A sandbox started and stopped again while its push ran is stopped once more,
// but the snapshot misses what it wrote in between. AerolVM bumps updated_at on
// every start and stop and never on a snapshot (verified on v0.7.0), so a
// sandbox updated after its snapshot was taken is cancelled too; the next pass
// archives it afresh.
func (s *Service) finishArchive(ctx context.Context, cluster db.Cluster, row db.Archive, client *aerolvm.Client, snapshot aerolvm.Snapshot) error {
	registryRef := snapshot.ImageRegistryRef
	sandbox, err := client.GetSandbox(ctx, row.SandboxID)
	changed := err == nil && !snapshot.CreatedAt.IsZero() && sandbox.UpdatedAt.After(snapshot.CreatedAt)
	switch {
	case err == nil && (sandbox.Status != "stopped" || changed):
		// Remove the bucket copy first: the snapshot is how a retry finds
		// this row again.
		if err := s.deleteArchiveFromBucket(ctx, cluster, registryRef); err != nil {
			return s.deferArchive(ctx, cluster, row, err.Error())
		}
		if err := s.deleteTeamSnapshot(ctx, client, row.SnapshotName); err != nil {
			return s.deferArchive(ctx, cluster, row, err.Error())
		}
		reason := "started again before it was archived"
		if sandbox.Status == "stopped" {
			reason = "started and stopped again while it was being archived; archiving it again"
		}
		return s.cancelArchive(ctx, cluster, row, reason)
	case err == nil:
		if err := client.DeleteSandbox(ctx, row.SandboxID); err != nil {
			return s.deferArchive(ctx, cluster, row, err.Error())
		}
	case !isStatus(err, http.StatusNotFound):
		return s.deferArchive(ctx, cluster, row, err.Error())
	}

	now := s.now()
	if err := s.q.ArchivePushed(ctx, db.ArchivePushedParams{
		RegistryRef: registryRef,
		ArchivedAt:  sql.NullTime{Time: now, Valid: true},
		ID:          row.ID,
	}); err != nil {
		return fmt.Errorf("mark archive archived: %w", err)
	}
	s.logChange(cluster, row, archiveArchived)
	return nil
}

// waitForPush stores a push error and gives up on a push that has been
// running longer than archivePushTimeout.
func (s *Service) waitForPush(ctx context.Context, cluster db.Cluster, row db.Archive, pushError string) error {
	if row.PushingSince.Valid && s.now().Sub(row.PushingSince.Time) > archivePushTimeout {
		if err := s.q.FailArchive(ctx, db.FailArchiveParams{
			NextAttemptAt: sql.NullTime{Time: s.now().Add(archiveRetryDelay), Valid: true},
			Error:         pushError,
			ID:            row.ID,
		}); err != nil {
			return fmt.Errorf("fail archive: %w", err)
		}
		s.logChange(cluster, row, archiveFailed)
		return nil
	}
	if pushError == "" {
		// The push is still running: no row update, it is due next pass.
		return nil
	}
	if err := s.q.DeferArchive(ctx, db.DeferArchiveParams{
		Error:         pushError,
		NextAttemptAt: row.NextAttemptAt,
		ID:            row.ID,
	}); err != nil {
		return fmt.Errorf("store archive push error: %w", err)
	}
	return nil
}

// deferArchive leaves the status alone, records why and holds the row back
// for a minute.
func (s *Service) deferArchive(ctx context.Context, cluster db.Cluster, row db.Archive, message string) error {
	if err := s.q.DeferArchive(ctx, db.DeferArchiveParams{
		Error:         message,
		NextAttemptAt: sql.NullTime{Time: s.now().Add(archiveDeferDelay), Valid: true},
		ID:            row.ID,
	}); err != nil {
		return fmt.Errorf("defer archive: %w", err)
	}
	return nil
}

// cancelArchive gives up on one sandbox.
func (s *Service) cancelArchive(ctx context.Context, cluster db.Cluster, row db.Archive, reason string) error {
	if err := s.q.CancelArchive(ctx, db.CancelArchiveParams{Error: reason, ID: row.ID}); err != nil {
		return fmt.Errorf("cancel archive: %w", err)
	}
	s.logChange(cluster, row, archiveCancelled)
	return nil
}

// logChange is one info line per archive state change.
func (s *Service) logChange(cluster db.Cluster, row db.Archive, to string) {
	s.log.Info("archive state changed", "archive", row.ID, "sandbox", row.SandboxID,
		"cluster", cluster.Slug, "from", row.Status, "to", to)
}

// restoreArchive creates a new sandbox from an archived one, with the old
// name, tags and size. The archive is kept, so it can be restored again.
func (s *Service) restoreArchive(ctx context.Context, row db.Archive) (aerolvm.Sandbox, error) {
	if row.Status != archiveArchived {
		return aerolvm.Sandbox{}, precondition(nil, "archive %q is %s, not archived", row.SandboxID, row.Status)
	}
	if row.RegistryRef == "" {
		return aerolvm.Sandbox{}, precondition(nil, "archive %q has no registry ref", row.SandboxID)
	}
	cluster, err := s.q.GetClusterByID(ctx, row.ClusterID)
	if err != nil {
		return aerolvm.Sandbox{}, fmt.Errorf("get archive cluster: %w", err)
	}
	org, err := s.q.GetOrganizationByID(ctx, row.OrgID)
	if err != nil {
		return aerolvm.Sandbox{}, fmt.Errorf("get archive organization: %w", err)
	}
	if !org.Team.Valid || org.Team.String == "" {
		return aerolvm.Sandbox{}, precondition(nil, "workspace %q has no AerolVM team", org.Slug)
	}
	token, err := s.archiveToken(ctx, cluster, org)
	if err != nil {
		return aerolvm.Sandbox{}, err
	}
	client := s.aerolvmClient(cluster.AdminURL, token.value)

	tags, err := decodeTags(row.Tags)
	if err != nil {
		return aerolvm.Sandbox{}, err
	}
	tags[archiveRestoredTag] = row.SandboxID
	req := aerolvm.CreateSandboxRequest{
		Image:    row.RegistryRef,
		Name:     row.SandboxName,
		Tags:     tags,
		CPU:      row.Cpu,
		MemoryMB: row.MemoryMb,
		DiskGB:   row.DiskGb,
		Runtime:  row.Runtime,
	}

	callCtx, cancel := context.WithTimeout(ctx, requestCallTimeout)
	sandbox, err := client.CreateSandbox(callCtx, req)
	cancel()
	if err != nil && req.Name != "" && isStatus(err, http.StatusConflict) {
		req.Name = req.Name + "-" + randomHex(2)
		callCtx, cancel = context.WithTimeout(ctx, requestCallTimeout)
		sandbox, err = client.CreateSandbox(callCtx, req)
		cancel()
	}
	if err != nil {
		if apiErr, ok := errors.AsType[*aerolvm.APIError](err); ok && apiErr.Status == http.StatusTooManyRequests {
			return aerolvm.Sandbox{}, precondition(map[string]string{"region": cluster.Slug},
				"region %q: the workspace's plan limit is reached", cluster.Slug)
		}
		return aerolvm.Sandbox{}, s.regionUnavailable(ctx, cluster.Slug, err)
	}

	now := s.now()
	if err := s.q.RecordArchiveRestore(ctx, db.RecordArchiveRestoreParams{
		LastRestoredSandboxID: sandbox.ID,
		LastRestoredAt:        sql.NullTime{Time: now, Valid: true},
		ID:                    row.ID,
	}); err != nil {
		return aerolvm.Sandbox{}, fmt.Errorf("record archive restore: %w", err)
	}
	return sandbox, nil
}

// deleteArchive removes an archive's snapshot and its keys in the bucket,
// then marks the row deleted. A pending row may have no snapshot yet.
func (s *Service) deleteArchive(ctx context.Context, row db.Archive) error {
	switch row.Status {
	case archiveArchived, archiveFailed, archivePending:
	default:
		return precondition(nil, "archive %q is %s and cannot be deleted", row.SandboxID, row.Status)
	}
	cluster, err := s.q.GetClusterByID(ctx, row.ClusterID)
	if err != nil {
		return fmt.Errorf("get archive cluster: %w", err)
	}
	org, err := s.q.GetOrganizationByID(ctx, row.OrgID)
	if err != nil {
		return fmt.Errorf("get archive organization: %w", err)
	}
	if org.Team.Valid && org.Team.String != "" {
		token, err := s.archiveToken(ctx, cluster, org)
		if err != nil {
			return err
		}
		client := s.aerolvmClient(cluster.AdminURL, token.value)
		if err := s.deleteTeamSnapshot(ctx, client, row.SnapshotName); err != nil {
			return s.regionUnavailable(ctx, cluster.Slug, err)
		}
	}
	if row.RegistryRef != "" {
		if err := s.deleteArchiveFromBucket(ctx, cluster, row.RegistryRef); err != nil {
			return err
		}
	}
	now := s.now()
	if err := s.q.MarkArchiveDeleted(ctx, db.MarkArchiveDeletedParams{
		DeletedAt: sql.NullTime{Time: now, Valid: true},
		ID:        row.ID,
	}); err != nil {
		return fmt.Errorf("mark archive deleted: %w", err)
	}
	return nil
}

// deleteTeamSnapshot deletes one snapshot with a team token. A missing
// snapshot is not an error.
func (s *Service) deleteTeamSnapshot(ctx context.Context, client *aerolvm.Client, name string) error {
	err := client.DeleteSnapshot(ctx, name)
	if err != nil && isStatus(err, http.StatusNotFound) {
		return nil
	}
	return err
}

// deleteArchiveFromBucket removes the registry repository of one archive
// from the cluster's bucket. Blobs are shared and are never deleted here.
func (s *Service) deleteArchiveFromBucket(ctx context.Context, cluster db.Cluster, registryRef string) error {
	if !cluster.StorageProfileID.Valid || cluster.StorageProfileID.String == "" {
		return fmt.Errorf("cluster %q has no storage profile", cluster.Slug)
	}
	profile, err := s.q.GetStorageProfileByID(ctx, cluster.StorageProfileID.String)
	if err != nil {
		return fmt.Errorf("get cluster storage profile: %w", err)
	}
	repo, err := archiveRepo(registryRef, cluster.ID)
	if err != nil {
		return err
	}
	secret, err := s.keyring.Open(profile.SecretKeyEnc)
	if err != nil {
		return err
	}
	client, err := objstore.New(storageProfileConfig(profile, string(secret)))
	if err != nil {
		return err
	}
	prefix := registryRoot(profile.Prefix, cluster.ID) + "/docker/registry/v2/repositories/" + repo + "/"
	if _, err := client.DeletePrefix(ctx, prefix); err != nil {
		return fmt.Errorf("delete archive %q from the bucket: %w", registryRef, err)
	}
	return nil
}

// archiveRepo turns a snapshot's registry ref into its repository path,
// refusing anything that is not one of this cluster's archive snapshots.
func archiveRepo(registryRef, clusterID string) (string, error) {
	repo := strings.TrimSpace(registryRef)
	if ref, ok := strings.CutPrefix(repo, archiveRegistryHost+"/"); ok {
		repo = ref
	}
	if colon := strings.LastIndex(repo, ":"); colon > strings.LastIndex(repo, "/") {
		repo = repo[:colon]
	}
	want := "cluster/" + strings.ToLower(clusterID) + "/snapshots/"
	if !strings.HasPrefix(strings.ToLower(repo), want) {
		return "", fmt.Errorf("registry ref %q is not an archive of cluster %q", registryRef, clusterID)
	}
	segments := strings.Split(repo, "/")
	if !strings.HasPrefix(segments[len(segments)-1], "archive-") {
		return "", fmt.Errorf("registry ref %q is not an archive", registryRef)
	}
	return repo, nil
}

// registryRoot is the bucket path a cluster's registry stores under. It
// matches machine.RegistryRoot, without the leading slash object keys never
// have.
func registryRoot(prefix, clusterID string) string {
	parts := make([]string, 0, 3)
	if p := strings.Trim(prefix, "/"); p != "" {
		parts = append(parts, p)
	}
	parts = append(parts, "clusters", strings.ToLower(clusterID))
	return strings.Join(parts, "/")
}

// encodeTags stores a sandbox's tags as a JSON object.
func encodeTags(tags map[string]string) (string, error) {
	if len(tags) == 0 {
		return "{}", nil
	}
	data, err := json.Marshal(tags)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// decodeTags reads an archive's tags. An empty or broken value is no tags.
func decodeTags(raw string) (map[string]string, error) {
	tags := make(map[string]string)
	if strings.TrimSpace(raw) == "" {
		return tags, nil
	}
	if err := json.Unmarshal([]byte(raw), &tags); err != nil {
		return nil, fmt.Errorf("read archive tags: %w", err)
	}
	if tags == nil {
		tags = make(map[string]string)
	}
	return tags, nil
}

// randomHex returns n random bytes as hex.
func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%0*x", n*2, time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// isStatus reports whether err is an AerolVM answer with this HTTP status.
func isStatus(err error, status int) bool {
	apiErr, ok := errors.AsType[*aerolvm.APIError](err)
	return ok && apiErr.Status == status
}
