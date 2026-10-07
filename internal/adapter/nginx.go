package adapter

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"text/template"
	"time"

	"royalknight/internal/ssl"
)

const nginxConfigTemplate = `server {
    listen 80;
    server_name {{.Domain}};
    return 301 https://$host$request_uri;
}

server {
    listen 443 ssl;
    server_name {{.Domain}};

    root {{.RootPath}};
    index index.html index.htm;

    ssl_certificate {{.CertPath}};
    ssl_certificate_key {{.KeyPath}};
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers HIGH:!aNULL:!MD5;
    ssl_session_cache shared:SSL:2m;
    ssl_session_timeout 10m;

    # Low-RAM performance optimizations
    sendfile on;
    tcp_nopush on;
    tcp_nodelay on;
    keepalive_timeout 15;
    client_body_buffer_size 16k;
    client_header_buffer_size 1k;
    client_max_body_size 50m;
    large_client_header_buffers 2 1k;

    # Gzip compression for static assets
    gzip on;
    gzip_vary on;
    gzip_proxied any;
    gzip_comp_level 5;
    gzip_min_length 256;
    gzip_types text/plain text/css application/json application/javascript text/xml application/xml application/xml+rss text/javascript image/svg+xml;

    location / {
        try_files $uri $uri/ =404;
    }
}
`

type NginxAdapter struct {
	availableDir string
	enabledDir   string
	sslManager   *ssl.Manager
	tmpl         *template.Template
	mu           sync.Mutex
}

func NewNginxAdapter(baseDir string, sslManager *ssl.Manager) (*NginxAdapter, error) {
	if baseDir == "" {
		baseDir = "/etc/nginx"
	}
	avail := filepath.Join(baseDir, "sites-available")
	enab := filepath.Join(baseDir, "sites-enabled")

	if err := os.MkdirAll(avail, 0755); err != nil {
		return nil, fmt.Errorf("failed to create sites-available: %w", err)
	}
	if err := os.MkdirAll(enab, 0755); err != nil {
		return nil, fmt.Errorf("failed to create sites-enabled: %w", err)
	}

	tmpl, err := template.New("nginx").Parse(nginxConfigTemplate)
	if err != nil {
		return nil, fmt.Errorf("failed to parse nginx config template: %w", err)
	}

	return &NginxAdapter{
		availableDir: avail,
		enabledDir:   enab,
		sslManager:   sslManager,
		tmpl:         tmpl,
	}, nil
}

func (n *NginxAdapter) Name() string {
	return "nginx"
}

func (n *NginxAdapter) Start() error {
	return ExecSystemctl("start", "nginx")
}

func (n *NginxAdapter) Stop() error {
	return ExecSystemctl("stop", "nginx")
}

func (n *NginxAdapter) ProvisionSSL(domain string) error {
	_, err := n.sslManager.EnsureCertificate(domain)
	return err
}

func (n *NginxAdapter) ApplyConfig(domain string, rootPath string) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	// Ensure SSL certificates exist before referencing them
	cert, err := n.sslManager.EnsureCertificate(domain)
	if err != nil {
		return fmt.Errorf("failed to ensure ssl certificate for %s: %w", domain, err)
	}

	confName := fmt.Sprintf("%s.conf", sanitizeDomainFilename(domain))
	availablePath := filepath.Join(n.availableDir, confName)
	enabledPath := filepath.Join(n.enabledDir, confName)

	var buf bytes.Buffer
	data := struct {
		Domain   string
		RootPath string
		CertPath string
		KeyPath  string
	}{
		Domain:   domain,
		RootPath: rootPath,
		CertPath: cert.CertPath,
		KeyPath:  cert.KeyPath,
	}

	if err := n.tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("failed to generate nginx config: %w", err)
	}

	if err := os.WriteFile(availablePath, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write nginx config %s: %w", availablePath, err)
	}

	// Create symlink in sites-enabled if not already exists
	_ = os.Remove(enabledPath)
	if err := os.Symlink(availablePath, enabledPath); err != nil {
		return fmt.Errorf("failed to symlink nginx config: %w", err)
	}

	// Validate config syntax
	if err := n.TestConfig(); err != nil {
		_ = os.Remove(enabledPath)
		_ = os.Remove(availablePath)
		return fmt.Errorf("nginx configuration test failed: %w", err)
	}

	// Reload if running
	_ = ExecSystemctl("reload", "nginx")
	return nil
}

func (n *NginxAdapter) RemoveConfig(domain string) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	confName := fmt.Sprintf("%s.conf", sanitizeDomainFilename(domain))
	availablePath := filepath.Join(n.availableDir, confName)
	enabledPath := filepath.Join(n.enabledDir, confName)

	_ = os.Remove(enabledPath)
	_ = os.Remove(availablePath)

	_ = ExecSystemctl("reload", "nginx")
	return nil
}

func (n *NginxAdapter) TestConfig() error {
	if _, err := exec.LookPath("nginx"); err != nil {
		return fmt.Errorf("nginx executable not found in system PATH. Please install Nginx: sudo apt install -y nginx")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "nginx", "-t")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("nginx -t failed: %w (output: %s)", err, stderr.String())
	}
	return nil
}

func sanitizeDomainFilename(domain string) string {
	domain = strings.ReplaceAll(domain, "/", "_")
	domain = strings.ReplaceAll(domain, "..", "_")
	return domain
}
