package deploy

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"royalknight/internal/manager"
	"royalknight/internal/storage"
)

var snapshotNameRegex = regexp.MustCompile(`^[a-zA-Z0-9_\-\.]+$`)

type SnapshotManager struct {
	baseDir string
	db      *storage.DB
	orch    *manager.Orchestrator
}

func NewSnapshotManager(baseDir string, db *storage.DB, orch *manager.Orchestrator) (*SnapshotManager, error) {
	if baseDir == "" {
		baseDir = "./data/snapshots"
	}
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create snapshots directory: %w", err)
	}

	return &SnapshotManager{
		baseDir: baseDir,
		db:      db,
		orch:    orch,
	}, nil
}

// UploadVersionArchive extracts an uploaded .zip archive into an isolated version image.
func (sm *SnapshotManager) UploadVersionArchive(domain, versionName, zipPath string, makeLive bool, liveRoot string, sslEnabled bool) (*storage.SiteSnapshot, error) {
	versionName = strings.TrimSpace(versionName)
	if !snapshotNameRegex.MatchString(versionName) {
		return nil, fmt.Errorf("invalid version name: alphanumeric, dashes, dots, and underscores only")
	}

	targetDir := filepath.Join(sm.baseDir, domain, versionName)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create version directory: %w", err)
	}

	if err := ExtractZip(zipPath, targetDir); err != nil {
		_ = os.RemoveAll(targetDir)
		return nil, fmt.Errorf("archive extraction failed: %w", err)
	}

	snap, err := sm.db.CreateSnapshot(domain, versionName, targetDir, makeLive)
	if err != nil {
		return nil, fmt.Errorf("failed to save snapshot in database: %w", err)
	}

	if makeLive {
		if err := sm.PublishSnapshot(domain, snap.ID, liveRoot); err != nil {
			return snap, fmt.Errorf("version saved, but failed to make live: %w", err)
		}
	}

	return snap, nil
}

// CreateSnapshot saves a frozen copy of the live site as a named image.
func (sm *SnapshotManager) CreateSnapshot(domain, name string, liveRoot string) (*storage.SiteSnapshot, error) {
	name = strings.TrimSpace(name)
	if !snapshotNameRegex.MatchString(name) {
		return nil, fmt.Errorf("invalid snapshot name: alphanumeric, dashes, dots, and underscores only")
	}

	snapTargetDir := filepath.Join(sm.baseDir, domain, name)
	if err := os.MkdirAll(snapTargetDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create snapshot directory: %w", err)
	}

	if err := CopyDir(liveRoot, snapTargetDir); err != nil {
		_ = os.RemoveAll(snapTargetDir)
		return nil, fmt.Errorf("failed to copy site files into snapshot: %w", err)
	}

	return sm.db.CreateSnapshot(domain, name, snapTargetDir, false)
}

// PublishSnapshot activates a saved snapshot image, replacing the live site files and re-applying server config.
func (sm *SnapshotManager) PublishSnapshot(domain string, snapshotID int64, liveRoot string) error {
	snap, err := sm.db.GetSnapshot(snapshotID)
	if err != nil {
		return fmt.Errorf("snapshot not found: %w", err)
	}
	if snap.Domain != domain {
		return fmt.Errorf("snapshot does not belong to domain %s", domain)
	}

	if _, err := os.Stat(snap.Path); err != nil {
		return fmt.Errorf("snapshot files missing on disk at %s: %w", snap.Path, err)
	}

	// Atomic or clean swap to the new live version
	if err := os.RemoveAll(liveRoot); err != nil {
		return fmt.Errorf("failed to clear live directory: %w", err)
	}
	if err := os.MkdirAll(liveRoot, 0755); err != nil {
		return fmt.Errorf("failed to re-create live directory: %w", err)
	}

	if err := CopyDir(snap.Path, liveRoot); err != nil {
		return fmt.Errorf("failed to copy snapshot files to live directory: %w", err)
	}

	// Update active marker in database
	if err := sm.db.SetActiveSnapshot(domain, snapshotID); err != nil {
		return fmt.Errorf("failed to set active snapshot in database: %w", err)
	}

	// Reapply active server config to ensure clean state
	site, err := sm.db.GetSite(domain)
	if err == nil && site != nil {
		_, _ = sm.orch.DeploySite(domain, liveRoot, site.SSLEnabled)
	}

	return nil
}

// DeleteSnapshot removes snapshot files and database record.
func (sm *SnapshotManager) DeleteSnapshot(snapshotID int64) error {
	snap, err := sm.db.GetSnapshot(snapshotID)
	if err != nil {
		return fmt.Errorf("snapshot not found: %w", err)
	}

	_ = os.RemoveAll(snap.Path)
	return sm.db.DeleteSnapshot(snapshotID)
}
