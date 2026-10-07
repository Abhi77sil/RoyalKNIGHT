package deploy

import (
	"os"
	"path/filepath"
	"testing"

	"royalknight/internal/manager"
	"royalknight/internal/storage"
)

type dummyAdapter struct{}

func (d *dummyAdapter) Name() string                               { return "dummy" }
func (d *dummyAdapter) Start() error                              { return nil }
func (d *dummyAdapter) Stop() error                               { return nil }
func (d *dummyAdapter) ApplyConfig(domain, rootPath string) error { return nil }
func (d *dummyAdapter) RemoveConfig(domain string) error          { return nil }
func (d *dummyAdapter) ProvisionSSL(domain string) error          { return nil }
func (d *dummyAdapter) TestConfig() error                         { return nil }

func TestSnapshotManager_CreateAndPublish(t *testing.T) {
	tempDir := t.TempDir()
	db, err := storage.Open(filepath.Join(tempDir, "test.db"))
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	defer db.Close()

	dummy := &dummyAdapter{}
	orch, _ := manager.NewOrchestrator(db, dummy, dummy)

	liveDir := filepath.Join(tempDir, "live")
	snapDir := filepath.Join(tempDir, "snapshots")
	_ = os.MkdirAll(liveDir, 0755)
	_ = os.WriteFile(filepath.Join(liveDir, "index.html"), []byte("<h1>Version 1</h1>"), 0644)

	_, _ = orch.DeploySite("site.test", liveDir, false)

	sm, err := NewSnapshotManager(snapDir, db, orch)
	if err != nil {
		t.Fatalf("NewSnapshotManager failed: %v", err)
	}

	// 1. Create Snapshot v1
	snap1, err := sm.CreateSnapshot("site.test", "v1-backup", liveDir)
	if err != nil {
		t.Fatalf("CreateSnapshot failed: %v", err)
	}
	if snap1.Name != "v1-backup" {
		t.Fatalf("expected snapshot name v1-backup, got %s", snap1.Name)
	}

	// 2. Modify live site to Version 2
	_ = os.WriteFile(filepath.Join(liveDir, "index.html"), []byte("<h1>Version 2</h1>"), 0644)

	// 3. Publish Snapshot v1 back to live
	if err := sm.PublishSnapshot("site.test", snap1.ID, liveDir); err != nil {
		t.Fatalf("PublishSnapshot failed: %v", err)
	}

	// 4. Verify live content is restored to Version 1
	restored, _ := os.ReadFile(filepath.Join(liveDir, "index.html"))
	if string(restored) != "<h1>Version 1</h1>" {
		t.Fatalf("expected restored content '<h1>Version 1</h1>', got %s", string(restored))
	}
}
