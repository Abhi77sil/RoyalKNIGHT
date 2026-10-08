package manager

import (
	"path/filepath"
	"testing"

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
	provisioned   []string
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
func (m *mockAdapter) ApplyConfig(domain, rootPath string, sslEnabled bool) error {
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
func (m *mockAdapter) ProvisionSSL(domain string) error {
	m.provisioned = append(m.provisioned, domain)
	return nil
}
func (m *mockAdapter) TestConfig() error { return m.testErr }

func TestOrchestrator_DeployAndManage(t *testing.T) {
	tempDir := t.TempDir()
	db, err := storage.Open(filepath.Join(tempDir, "test.db"))
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	defer db.Close()

	nginxMock := newMockAdapter("nginx")

	orch, err := NewOrchestrator(db, nginxMock)
	if err != nil {
		t.Fatalf("NewOrchestrator failed: %v", err)
	}

	if orch.GetActiveServerName() != "nginx" {
		t.Fatalf("expected active server nginx, got %s", orch.GetActiveServerName())
	}

	site, err := orch.DeploySite("site1.com", "/var/www/site1", false)
	if err != nil {
		t.Fatalf("expected successful deploy, got: %v", err)
	}
	if site.Domain != "site1.com" {
		t.Fatalf("expected domain site1.com, got %s", site.Domain)
	}

	if _, ok := nginxMock.appliedRoutes["site1.com"]; !ok {
		t.Fatal("expected site1.com route to be applied to nginx")
	}

	if err := orch.ProvisionSSL("site1.com"); err != nil {
		t.Fatalf("ProvisionSSL failed: %v", err)
	}
	if len(nginxMock.provisioned) != 1 || nginxMock.provisioned[0] != "site1.com" {
		t.Fatalf("expected site1.com provisioned, got %+v", nginxMock.provisioned)
	}

	if err := orch.SwitchServer("nginx"); err != nil {
		t.Fatalf("expected switch to nginx to succeed, got %v", err)
	}
	if err := orch.SwitchServer("caddy"); err == nil {
		t.Fatal("expected switch to caddy to fail")
	}

	if err := orch.RemoveSite("site1.com"); err != nil {
		t.Fatalf("expected RemoveSite to succeed, got %v", err)
	}
	if _, ok := nginxMock.appliedRoutes["site1.com"]; ok {
		t.Fatal("expected site1.com route to be removed from nginx")
	}
}
