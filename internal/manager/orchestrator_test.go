package manager

import (
	"errors"
	"path/filepath"
	"testing"

	"royalknight/internal/adapter"
	"royalknight/internal/storage"
)

type mockAdapter struct {
	name          string
	startErr      error
	stopErr       error
	applyErr      error
	testErr       error
	startCount    int
	stopCount     int
	appliedRoutes map[string]string
}

func newMockAdapter(name string) *mockAdapter {
	return &mockAdapter{
		name:          name,
		appliedRoutes: make(map[string]string),
	}
}

func (m *mockAdapter) Name() string { return m.name }
func (m *mockAdapter) Start() error {
	m.startCount++
	return m.startErr
}
func (m *mockAdapter) Stop() error {
	m.stopCount++
	return m.stopErr
}
func (m *mockAdapter) ApplyConfig(domain, rootPath string) error {
	if m.applyErr != nil {
		return m.applyErr
	}
	m.appliedRoutes[domain] = rootPath
	return nil
}
func (m *mockAdapter) RemoveConfig(domain string) error {
	delete(m.appliedRoutes, domain)
	return nil
}
func (m *mockAdapter) ProvisionSSL(domain string) error { return nil }
func (m *mockAdapter) TestConfig() error                { return m.testErr }

func TestOrchestrator_SwitchSuccess(t *testing.T) {
	tempDir := t.TempDir()
	db, err := storage.Open(filepath.Join(tempDir, "test.db"))
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	defer db.Close()

	caddyMock := newMockAdapter("caddy")
	nginxMock := newMockAdapter("nginx")

	orch := &Orchestrator{
		db: db,
		adapters: map[string]adapter.WebServerAdapter{
			"caddy": caddyMock,
			"nginx": nginxMock,
		},
		activeServer: "caddy",
		currentState: StateIdle,
	}

	_, _ = orch.DeploySite("site1.com", "/var/www/site1", true)

	if err := orch.SwitchServer("nginx"); err != nil {
		t.Fatalf("expected successful switch, got: %v", err)
	}

	if orch.GetActiveServerName() != "nginx" {
		t.Fatalf("expected active server nginx, got %s", orch.GetActiveServerName())
	}
	if caddyMock.stopCount != 1 {
		t.Fatalf("expected caddy to be stopped once, got %d", caddyMock.stopCount)
	}
	if nginxMock.startCount != 1 {
		t.Fatalf("expected nginx to be started once, got %d", nginxMock.startCount)
	}
	if _, ok := nginxMock.appliedRoutes["site1.com"]; !ok {
		t.Fatal("expected site1.com route to be applied to nginx")
	}

	logs, err := db.ListSwitchLogs(1)
	if err != nil || len(logs) == 0 || logs[0].Status != "SUCCESS" {
		t.Fatalf("unexpected switch logs: %+v", logs)
	}
}

func TestOrchestrator_SwitchFailureRollback(t *testing.T) {
	tempDir := t.TempDir()
	db, err := storage.Open(filepath.Join(tempDir, "test.db"))
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	defer db.Close()

	caddyMock := newMockAdapter("caddy")
	nginxMock := newMockAdapter("nginx")
	nginxMock.startErr = errors.New("address already in use :80") // Simulate port bind error

	orch := &Orchestrator{
		db: db,
		adapters: map[string]adapter.WebServerAdapter{
			"caddy": caddyMock,
			"nginx": nginxMock,
		},
		activeServer: "caddy",
		currentState: StateIdle,
	}

	_, _ = orch.DeploySite("site2.com", "/var/www/site2", true)

	err = orch.SwitchServer("nginx")
	if err == nil {
		t.Fatal("expected switch to fail, but succeeded")
	}

	// Active server must remain caddy
	if orch.GetActiveServerName() != "caddy" {
		t.Fatalf("expected active server to remain caddy, got %s", orch.GetActiveServerName())
	}

	// Original server caddy should have been restarted during rollback
	if caddyMock.startCount != 1 {
		t.Fatalf("expected caddy to be restarted during rollback, got %d", caddyMock.startCount)
	}

	logs, err := db.ListSwitchLogs(1)
	if err != nil || len(logs) == 0 {
		t.Fatalf("failed to query logs: %v", err)
	}
	if logs[0].Status != "ROLLED_BACK" {
		t.Fatalf("expected log status ROLLED_BACK, got %s", logs[0].Status)
	}
}
