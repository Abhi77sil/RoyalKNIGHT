package adapter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

type CaddyAdapter struct {
	adminURL string
	client   *http.Client
	mu       sync.RWMutex
	routes   map[string]string // domain -> rootPath
}

func NewCaddyAdapter(adminURL string) *CaddyAdapter {
	if adminURL == "" {
		adminURL = "http://127.0.0.1:2019"
	}
	return &CaddyAdapter{
		adminURL: adminURL,
		client:   &http.Client{Timeout: 5 * time.Second},
		routes:   make(map[string]string),
	}
}

func (c *CaddyAdapter) Name() string {
	return "caddy"
}

func (c *CaddyAdapter) Start() error {
	return ExecSystemctl("start", "caddy")
}

func (c *CaddyAdapter) Stop() error {
	return ExecSystemctl("stop", "caddy")
}

func (c *CaddyAdapter) ApplyConfig(domain string, rootPath string) error {
	c.mu.Lock()
	c.routes[domain] = rootPath
	c.mu.Unlock()

	return c.pushFullConfig()
}

func (c *CaddyAdapter) RemoveConfig(domain string) error {
	c.mu.Lock()
	delete(c.routes, domain)
	c.mu.Unlock()

	return c.pushFullConfig()
}

func (c *CaddyAdapter) ProvisionSSL(domain string) error {
	// Caddy automates Let's Encrypt / ZeroSSL certificate issuance natively
	// when domains are bound to HTTP servers listening on standard ports.
	c.mu.RLock()
	_, exists := c.routes[domain]
	c.mu.RUnlock()

	if !exists {
		return fmt.Errorf("domain %s is not configured in caddy", domain)
	}
	return nil
}

func (c *CaddyAdapter) TestConfig() error {
	req, err := http.NewRequest(http.MethodGet, c.adminURL+"/config/", nil)
	if err != nil {
		return err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("caddy admin API unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("caddy admin returned status: %d", resp.StatusCode)
	}
	return nil
}

// pushFullConfig builds the complete Caddy JSON config structure and applies it via /load.
func (c *CaddyAdapter) pushFullConfig() error {
	c.mu.RLock()
	routesList := make([]map[string]any, 0, len(c.routes))
	for domain, root := range c.routes {
		route := map[string]any{
			"match": []map[string]any{
				{"host": []string{domain}},
			},
			"handle": []map[string]any{
				{
					"handler": "subroute",
					"routes": []map[string]any{
						{
							"handle": []map[string]any{
								{
									"handler": "file_server",
									"root":    root,
								},
							},
						},
					},
				},
			},
			"terminal": true,
		}
		routesList = append(routesList, route)
	}
	c.mu.RUnlock()

	config := map[string]any{
		"apps": map[string]any{
			"http": map[string]any{
				"servers": map[string]any{
					"srv0": map[string]any{
						"listen": []string{":80", ":443"},
						"routes": routesList,
					},
				},
			},
		},
	}

	payload, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal caddy config: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, c.adminURL+"/load", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to create caddy /load request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to communicate with caddy admin API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(resp.Body)
		return fmt.Errorf("caddy rejected config (status %d): %s", resp.StatusCode, buf.String())
	}

	return nil
}
