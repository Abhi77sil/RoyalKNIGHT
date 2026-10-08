package manager

import (
	"fmt"
	"log"
	"sync"

	"royalknight/internal/adapter"
	"royalknight/internal/storage"
)

type SwitchState string

const (
	StateIdle           SwitchState = "IDLE"
	StatePreparing      SwitchState = "PREPARING_CONFIG"
	StateStoppingActive SwitchState = "STOPPING_ACTIVE"
	StateStartingTarget SwitchState = "STARTING_TARGET"
	StateVerifying      SwitchState = "VERIFYING_TARGET"
	StateRollback       SwitchState = "ROLLING_BACK"
	StateComplete       SwitchState = "COMPLETE"
)

type Orchestrator struct {
	mu           sync.Mutex
	db           *storage.DB
	adapter      adapter.WebServerAdapter
	activeServer string
	currentState SwitchState
}

func NewOrchestrator(db *storage.DB, nginx adapter.WebServerAdapter) (*Orchestrator, error) {
	_ = db.SetActiveServer("nginx")

	return &Orchestrator{
		db:           db,
		adapter:      nginx,
		activeServer: "nginx",
		currentState: StateIdle,
	}, nil
}

func (o *Orchestrator) GetActiveServerName() string {
	return "nginx"
}

func (o *Orchestrator) GetCurrentState() SwitchState {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.currentState
}

func (o *Orchestrator) GetActiveAdapter() (adapter.WebServerAdapter, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.adapter, nil
}

// DeploySite saves site metadata in SQLite and applies config to Nginx.
func (o *Orchestrator) DeploySite(domain, rootPath string, sslEnabled bool) (*storage.Site, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	site, err := o.db.UpsertSite(domain, rootPath, sslEnabled)
	if err != nil {
		return nil, fmt.Errorf("failed to save site metadata: %w", err)
	}

	if err := o.adapter.ApplyConfig(domain, rootPath, sslEnabled); err != nil {
		return nil, fmt.Errorf("failed to apply nginx config: %w", err)
	}

	if sslEnabled {
		go func() {
			if err := o.adapter.ProvisionSSL(domain); err != nil {
				log.Printf("[SSL] Background Let's Encrypt issuance for %s: %v", domain, err)
			}
		}()
	}

	return site, nil
}

// ProvisionSSL manually triggers Let's Encrypt certificate acquisition for a domain.
func (o *Orchestrator) ProvisionSSL(domain string) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	site, err := o.db.GetSite(domain)
	if err != nil || site == nil {
		return fmt.Errorf("site %s not found", domain)
	}

	if err := o.adapter.ProvisionSSL(domain); err != nil {
		return err
	}

	_ = o.db.UpdateSiteSSL(domain, true)
	return nil
}

// RemoveSite deletes site from database and Nginx.
func (o *Orchestrator) RemoveSite(domain string) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if err := o.adapter.RemoveConfig(domain); err != nil {
		log.Printf("[Orchestrator] Warning removing nginx config for %s: %v", domain, err)
	}

	return o.db.DeleteSite(domain)
}

// SwitchServer is a no-op kept for API compatibility since Nginx is the sole engine.
func (o *Orchestrator) SwitchServer(targetName string) error {
	if targetName != "nginx" {
		return fmt.Errorf("nginx is the dedicated high-performance engine for this panel")
	}
	return nil
}

// ReloadServer reloads Nginx configuration.
func (o *Orchestrator) ReloadServer() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.adapter.Start()
}
