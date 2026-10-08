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

const nginxUnifiedTemplate = `server {
    listen 80;
    listen [::]:80;
    server_name {{.Domain}};

    # Let's Encrypt ACME HTTP-01 challenge
    location ^~ /.well-known/acme-challenge/ {
        default_type "text/plain";
        root /var/www/certbot;
        allow all;
    }

{{if .SSLEnabled}}
    location / {
        return 301 https://$host$request_uri;
    }
}

server {
    listen 443 ssl http2;
    listen [::]:443 ssl http2;
    server_name {{.Domain}};

    root {{.RootPath}};
    index index.html index.htm index.php default.html;

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
        try_files $uri $uri/ /index.html =404;
        autoindex on;
    }
}
{{else}}
    root {{.RootPath}};
    index index.html index.htm index.php default.html;

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
        try_files $uri $uri/ /index.html =404;
        autoindex on;
    }
}
{{end}}
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
	_ = os.MkdirAll("/var/www/certbot", 0755)

	tmpl, err := template.New("nginx").Parse(nginxUnifiedTemplate)
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
	n.mu.Lock()
	defer n.mu.Unlock()

	// 1. Attempt official Let's Encrypt issuance
	cert, err := n.sslManager.ProvisionLetsEncrypt(domain)
	if err != nil {
		return fmt.Errorf("Let's Encrypt certificate acquisition failed: %w", err)
	}

	// 2. Re-render template with real certificate enabled
	confName := fmt.Sprintf("%s.conf", sanitizeDomainFilename(domain))
	availablePath := filepath.Join(n.availableDir, confName)
	enabledPath := filepath.Join(n.enabledDir, confName)

	rootPath := filepath.Join("/var/www", domain)

	var buf bytes.Buffer
	data := struct {
		Domain     string
		RootPath   string
		SSLEnabled bool
		CertPath   string
		KeyPath    string
	}{
		Domain:     domain,
		RootPath:   rootPath,
		SSLEnabled: true,
		CertPath:   cert.CertPath,
		KeyPath:    cert.KeyPath,
	}

	if err := n.tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("failed to render nginx template: %w", err)
	}

	if err := os.WriteFile(availablePath, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}
	_ = os.Remove(enabledPath)
	_ = os.Symlink(availablePath, enabledPath)

	if err := n.TestConfig(); err != nil {
		return fmt.Errorf("nginx syntax check failed: %w", err)
	}

	_ = ExecSystemctl("reload", "nginx")
	return nil
}

func (n *NginxAdapter) ApplyConfig(domain string, rootPath string, sslEnabled bool) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	// Only enable HTTPS if a genuine, valid Let's Encrypt certificate exists on disk
	hasValidSSL := false
	var certPath, keyPath string
	if sslEnabled && n.sslManager.HasValidCertificate(domain) {
		cert, err := n.sslManager.GetCertificate(domain)
		if err == nil && cert != nil {
			certPath = cert.CertPath
			keyPath = cert.KeyPath
			hasValidSSL = true
		}
	}

	confName := fmt.Sprintf("%s.conf", sanitizeDomainFilename(domain))
	availablePath := filepath.Join(n.availableDir, confName)
	enabledPath := filepath.Join(n.enabledDir, confName)

	var buf bytes.Buffer
	data := struct {
		Domain     string
		RootPath   string
		SSLEnabled bool
		CertPath   string
		KeyPath    string
	}{
		Domain:     domain,
		RootPath:   rootPath,
		SSLEnabled: hasValidSSL,
		CertPath:   certPath,
		KeyPath:    keyPath,
	}

	if err := n.tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("failed to generate nginx config: %w", err)
	}

	if err := os.WriteFile(availablePath, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write nginx config %s: %w", availablePath, err)
	}

	_ = os.Remove(enabledPath)
	if err := os.Symlink(availablePath, enabledPath); err != nil {
		return fmt.Errorf("failed to symlink nginx config: %w", err)
	}

	if err := n.TestConfig(); err != nil {
		_ = os.Remove(enabledPath)
		_ = os.Remove(availablePath)
		return fmt.Errorf("nginx configuration test failed: %w", err)
	}

	if err := ExecSystemctl("reload", "nginx"); err != nil {
		_ = ExecSystemctl("start", "nginx")
	}
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
