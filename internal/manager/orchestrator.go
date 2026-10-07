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
	adapters     map[string]adapter.WebServerAdapter
	activeServer string
	currentState SwitchState
}

func NewOrchestrator(db *storage.DB, caddy adapter.WebServerAdapter, nginx adapter.WebServerAdapter) (*Orchestrator, error) {
	active, err := db.GetActiveServer()
	if err != nil {
		active = "caddy"
	}

	adapters := map[string]adapter.WebServerAdapter{
		"caddy": caddy,
		"nginx": nginx,
	}

	if _, ok := adapters[active]; !ok {
		active = "caddy"
		_ = db.SetActiveServer(active)
	}

	return &Orchestrator{
		db:           db,
		adapters:     adapters,
		activeServer: active,
		currentState: StateIdle,
	}, nil
}

func (o *Orchestrator) GetActiveServerName() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.activeServer
}

func (o *Orchestrator) GetCurrentState() SwitchState {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.currentState
}

func (o *Orchestrator) GetActiveAdapter() (adapter.WebServerAdapter, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	ad, ok := o.adapters[o.activeServer]
	if !ok {
		return nil, fmt.Errorf("active adapter %s not found", o.activeServer)
	}
	return ad, nil
}

// DeploySite saves site metadata in SQLite and applies config to active adapter.
func (o *Orchestrator) DeploySite(domain, rootPath string, sslEnabled bool) (*storage.Site, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	site, err := o.db.UpsertSite(domain, rootPath, sslEnabled)
	if err != nil {
		return nil, fmt.Errorf("failed to save site metadata: %w", err)
	}

	activeAdapter, ok := o.adapters[o.activeServer]
	if !ok {
		return nil, fmt.Errorf("active adapter %s not found", o.activeServer)
	}

	if err := activeAdapter.ApplyConfig(domain, rootPath); err != nil {
		return nil, fmt.Errorf("failed to apply config to %s: %w", o.activeServer, err)
	}

	if sslEnabled {
		_ = activeAdapter.ProvisionSSL(domain)
	}

	return site, nil
}

// RemoveSite deletes site from database and active adapter.
func (o *Orchestrator) RemoveSite(domain string) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	activeAdapter, ok := o.adapters[o.activeServer]
	if ok {
		_ = activeAdapter.RemoveConfig(domain)
	}

	return o.db.DeleteSite(domain)
}

// SwitchServer executes atomic server handoff with automatic rollback on failure.
func (o *Orchestrator) SwitchServer(targetName string) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if targetName == o.activeServer {
		return fmt.Errorf("web server %s is already active", targetName)
	}

	targetAdapter, exists := o.adapters[targetName]
	if !exists {
		return fmt.Errorf("unsupported target web server: %s", targetName)
	}

	currentName := o.activeServer
	currentAdapter := o.adapters[currentName]

	sites, err := o.db.ListSites()
	if err != nil {
		return fmt.Errorf("failed to fetch sites for config generation: %w", err)
	}

	// 1. Generate configuration for target server
	o.currentState = StatePreparing
	log.Printf("[Handoff] Step 1: Generating configuration on %s for %d sites", targetName, len(sites))
	for _, s := range sites {
		if err := targetAdapter.ApplyConfig(s.Domain, s.RootPath); err != nil {
			o.currentState = StateIdle
			_ = o.db.LogServerSwitch(currentName, targetName, "FAILED", fmt.Sprintf("Config generation error: %v", err))
			return fmt.Errorf("failed to configure site %s on %s: %w", s.Domain, targetName, err)
		}
	}

	// 2. Stop currently active web server
	o.currentState = StateStoppingActive
	log.Printf("[Handoff] Step 2: Stopping active server %s", currentName)
	if err := currentAdapter.Stop(); err != nil {
		o.currentState = StateIdle
		_ = o.db.LogServerSwitch(currentName, targetName, "FAILED", fmt.Sprintf("Failed to stop %s: %v", currentName, err))
		return fmt.Errorf("failed to stop %s: %w", currentName, err)
	}

	// 3. Attempt to start target web server
	o.currentState = StateStartingTarget
	log.Printf("[Handoff] Step 3: Starting target server %s", targetName)
	if startErr := targetAdapter.Start(); startErr != nil {
		return o.executeRollback(currentName, targetName, currentAdapter, targetAdapter, startErr)
	}

	// 4. Verify target server health
	o.currentState = StateVerifying
	log.Printf("[Handoff] Step 4: Testing target server %s health", targetName)
	if testErr := targetAdapter.TestConfig(); testErr != nil {
		return o.executeRollback(currentName, targetName, currentAdapter, targetAdapter, testErr)
	}

	// 5. Finalize switch
	o.activeServer = targetName
	o.currentState = StateComplete
	if err := o.db.SetActiveServer(targetName); err != nil {
		log.Printf("[Handoff] Warning: Failed to persist active server in database: %v", err)
	}
	_ = o.db.LogServerSwitch(currentName, targetName, "SUCCESS", "")
	o.currentState = StateIdle
	log.Printf("[Handoff] Successfully switched web server from %s to %s", currentName, targetName)
	return nil
}

func (o *Orchestrator) executeRollback(currentName, targetName string, currentAdapter, targetAdapter adapter.WebServerAdapter, cause error) error {
	o.currentState = StateRollback
	log.Printf("[Handoff] Critical: Target %s failed (%v). Initiating immediate rollback to %s", targetName, cause, currentName)

	_ = targetAdapter.Stop()

	restartErr := currentAdapter.Start()
	if restartErr != nil {
		log.Printf("[Handoff] Fatal: Failed to restart original server %s: %v", currentName, restartErr)
		_ = o.db.LogServerSwitch(currentName, targetName, "FAILED_ROLLBACK_CRITICAL",
			fmt.Sprintf("Target error: %v | Restart error: %v", cause, restartErr))
		o.currentState = StateIdle
		return fmt.Errorf("target %s failed (%v) AND rollback restart of %s failed (%w)", targetName, cause, currentName, restartErr)
	}

	_ = o.db.LogServerSwitch(currentName, targetName, "ROLLED_BACK", fmt.Sprintf("Target failed: %v", cause))
	o.currentState = StateIdle
	log.Printf("[Handoff] Rollback successful. %s restored as active server.", currentName)
	return fmt.Errorf("handoff to %s aborted due to failure (%v); safely rolled back to %s", targetName, cause, currentName)
}
