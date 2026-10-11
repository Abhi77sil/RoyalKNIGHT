package adapter

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"text/template"
	"time"

	"royalknight/internal/ssl"
	"royalknight/internal/storage"
)

const (
	errorPage404HTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>404 - Page Not Found</title>
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;600;700&display=swap" rel="stylesheet">
<style>
* { box-sizing: border-box; margin: 0; padding: 0; }
body {
  font-family: 'Inter', -apple-system, BlinkMacSystemFont, sans-serif;
  background-color: #0c1017;
  color: #e6edf3;
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
}
.card {
  max-width: 480px;
  width: 100%;
  background: #161b22;
  border: 1px solid #30363d;
  border-radius: 12px;
  padding: 36px 32px;
  text-align: center;
  box-shadow: 0 12px 36px rgba(0,0,0,0.4);
}
.badge {
  display: inline-block;
  font-size: 13px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  color: #f85149;
  background: rgba(248, 81, 73, 0.12);
  border: 1px solid rgba(248, 81, 73, 0.3);
  padding: 4px 12px;
  border-radius: 999px;
  margin-bottom: 20px;
}
h1 { font-size: 40px; font-weight: 700; color: #ffffff; margin-bottom: 12px; letter-spacing: -0.02em; }
p { font-size: 15px; color: #8b949e; line-height: 1.6; margin-bottom: 28px; }
.btn {
  display: inline-block;
  padding: 10px 20px;
  font-size: 14px;
  font-weight: 600;
  color: #ffffff;
  background: #238636;
  border: 1px solid rgba(240, 246, 252, 0.1);
  border-radius: 6px;
  text-decoration: none;
  transition: background 0.15s ease;
}
.btn:hover { background: #2ea043; }
.footer { margin-top: 32px; font-size: 12px; color: #484f58; }
</style>
</head>
<body>
<div class="card">
  <div class="badge">404 Not Found</div>
  <h1>Resource Missing</h1>
  <p>The requested page or resource could not be found on this server. Check the URL for typing errors or return to the homepage.</p>
  <a href="/" class="btn">Back to Home</a>
  <div class="footer">Powered by RoyalKNIGHT High Performance Engine</div>
</div>
</body>
</html>`

	errorPage50xHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>50x - Server Error</title>
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;600;700&display=swap" rel="stylesheet">
<style>
* { box-sizing: border-box; margin: 0; padding: 0; }
body {
  font-family: 'Inter', -apple-system, BlinkMacSystemFont, sans-serif;
  background-color: #0c1017;
  color: #e6edf3;
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
}
.card {
  max-width: 480px;
  width: 100%;
  background: #161b22;
  border: 1px solid #30363d;
  border-radius: 12px;
  padding: 36px 32px;
  text-align: center;
  box-shadow: 0 12px 36px rgba(0,0,0,0.4);
}
.badge {
  display: inline-block;
  font-size: 13px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  color: #d29922;
  background: rgba(210, 153, 34, 0.12);
  border: 1px solid rgba(210, 153, 34, 0.3);
  padding: 4px 12px;
  border-radius: 999px;
  margin-bottom: 20px;
}
h1 { font-size: 40px; font-weight: 700; color: #ffffff; margin-bottom: 12px; letter-spacing: -0.02em; }
p { font-size: 15px; color: #8b949e; line-height: 1.6; margin-bottom: 28px; }
.btn {
  display: inline-block;
  padding: 10px 20px;
  font-size: 14px;
  font-weight: 600;
  color: #ffffff;
  background: #238636;
  border: 1px solid rgba(240, 246, 252, 0.1);
  border-radius: 6px;
  text-decoration: none;
  cursor: pointer;
  transition: background 0.15s ease;
}
.btn:hover { background: #2ea043; }
.footer { margin-top: 32px; font-size: 12px; color: #484f58; }
</style>
</head>
<body>
<div class="card">
  <div class="badge">50x Server Error</div>
  <h1>Service Interrupted</h1>
  <p>The server encountered a temporary error or upstream issue and could not complete your request. Please try again in a few moments.</p>
  <button class="btn" onclick="window.location.reload()">Reload Page</button>
  <div class="footer">Powered by RoyalKNIGHT High Performance Engine</div>
</div>
</body>
</html>`

	errorPageMaintenanceHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Site Under Maintenance</title>
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;600;700&display=swap" rel="stylesheet">
<style>
* { box-sizing: border-box; margin: 0; padding: 0; }
body {
  font-family: 'Inter', -apple-system, BlinkMacSystemFont, sans-serif;
  background-color: #0c1017;
  color: #e6edf3;
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
}
.card {
  max-width: 480px;
  width: 100%;
  background: #161b22;
  border: 1px solid #30363d;
  border-radius: 12px;
  padding: 36px 32px;
  text-align: center;
  box-shadow: 0 12px 36px rgba(0,0,0,0.4);
}
.badge {
  display: inline-block;
  font-size: 13px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  color: #58a6ff;
  background: rgba(56, 139, 253, 0.12);
  border: 1px solid rgba(56, 139, 253, 0.3);
  padding: 4px 12px;
  border-radius: 999px;
  margin-bottom: 20px;
}
h1 { font-size: 32px; font-weight: 700; color: #ffffff; margin-bottom: 12px; letter-spacing: -0.02em; }
p { font-size: 15px; color: #8b949e; line-height: 1.6; margin-bottom: 24px; }
.status-indicator {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  color: #8b949e;
  background: #0d1117;
  padding: 8px 16px;
  border-radius: 6px;
  border: 1px solid #30363d;
}
.dot {
  width: 8px;
  height: 8px;
  background: #3fb950;
  border-radius: 50%;
  animation: pulse 2s infinite;
}
@keyframes pulse {
  0% { transform: scale(0.95); opacity: 0.7; }
  50% { transform: scale(1.15); opacity: 1; }
  100% { transform: scale(0.95); opacity: 0.7; }
}
.footer { margin-top: 32px; font-size: 12px; color: #484f58; }
</style>
</head>
<body>
<div class="card">
  <div class="badge">Scheduled Maintenance</div>
  <h1>We'll Be Right Back</h1>
  <p>Our website is undergoing planned maintenance and upgrades. Normal operations will resume shortly. We apologize for any inconvenience.</p>
  <div class="status-indicator">
    <div class="dot"></div>
    <span>Engine maintenance in progress</span>
  </div>
  <div class="footer">Powered by RoyalKNIGHT High Performance Engine</div>
</div>
</body>
</html>`
)

const nginxUnifiedTemplate = `server {
    listen 80;
    listen [::]:80;
    server_name {{.Domain}} www.{{.Domain}};

    # Let's Encrypt ACME HTTP-01 challenge
    location ^~ /.well-known/acme-challenge/ {
        default_type "text/plain";
        root /var/www/certbot;
        allow all;
    }

{{if and .SSLEnabled .ForceHTTPS}}
    location / {
        return 301 https://$host$request_uri;
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

{{if .GzipEnabled}}
    gzip on;
    gzip_vary on;
    gzip_proxied any;
    gzip_comp_level 5;
    gzip_min_length 256;
    gzip_types text/plain text/css application/json application/javascript text/xml application/xml application/xml+rss text/javascript image/svg+xml;
{{else}}
    gzip off;
{{end}}

{{if .CustomErrorPages}}
    error_page 404 /_rk_errors/404.html;
    error_page 500 502 503 504 /_rk_errors/50x.html;
    location ^~ /_rk_errors/ {
        alias /var/lib/royalknight/error_pages/;
        internal;
    }
{{end}}

{{if .MaintenanceMode}}
    error_page 503 /_rk_errors/maintenance.html;
    location ^~ /_rk_errors/ {
        alias /var/lib/royalknight/error_pages/;
        internal;
    }
    location / {
        return 503;
    }
{{else}}
{{if .AuthEnabled}}
    auth_basic "Restricted Access";
    auth_basic_user_file {{.AuthFile}};
{{end}}

{{if .BrowserCache}}
    location ~* \.(css|js|jpg|jpeg|png|gif|ico|svg|webp|woff|woff2|ttf|eot)$ {
        expires 30d;
        add_header Cache-Control "public, no-transform";
        access_log off;
        try_files $uri =404;
    }
{{end}}

    location / {
        try_files $uri $uri/ /index.html =404;
        autoindex on;
    }
{{end}}
}
{{end}}

{{if .SSLEnabled}}
server {
    listen 443 ssl http2;
    listen [::]:443 ssl http2;
    server_name {{.Domain}} www.{{.Domain}};

    root {{.RootPath}};
    index index.html index.htm index.php default.html;

    ssl_certificate {{.CertPath}};
    ssl_certificate_key {{.KeyPath}};
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers HIGH:!aNULL:!MD5;
    ssl_session_cache shared:SSL:10m;
    ssl_session_timeout 1d;

    # HSTS & Security Headers (Eliminates "Not Secure" browser flags)
    add_header Strict-Transport-Security "max-age=63072000; includeSubDomains; preload" always;
    add_header X-Content-Type-Options nosniff always;
    add_header X-Frame-Options SAMEORIGIN always;
    add_header X-XSS-Protection "1; mode=block" always;

    # Low-RAM performance optimizations
    sendfile on;
    tcp_nopush on;
    tcp_nodelay on;
    keepalive_timeout 15;
    client_body_buffer_size 16k;
    client_header_buffer_size 1k;
    client_max_body_size 50m;
    large_client_header_buffers 2 1k;

{{if .GzipEnabled}}
    gzip on;
    gzip_vary on;
    gzip_proxied any;
    gzip_comp_level 5;
    gzip_min_length 256;
    gzip_types text/plain text/css application/json application/javascript text/xml application/xml application/xml+rss text/javascript image/svg+xml;
{{else}}
    gzip off;
{{end}}

{{if .CustomErrorPages}}
    error_page 404 /_rk_errors/404.html;
    error_page 500 502 503 504 /_rk_errors/50x.html;
    location ^~ /_rk_errors/ {
        alias /var/lib/royalknight/error_pages/;
        internal;
    }
{{end}}

{{if .MaintenanceMode}}
    error_page 503 /_rk_errors/maintenance.html;
    location ^~ /_rk_errors/ {
        alias /var/lib/royalknight/error_pages/;
        internal;
    }
    location / {
        return 503;
    }
{{else}}
{{if .AuthEnabled}}
    auth_basic "Restricted Access";
    auth_basic_user_file {{.AuthFile}};
{{end}}

{{if .BrowserCache}}
    location ~* \.(css|js|jpg|jpeg|png|gif|ico|svg|webp|woff|woff2|ttf|eot)$ {
        expires 30d;
        add_header Cache-Control "public, no-transform";
        access_log off;
        try_files $uri =404;
    }
{{end}}

    location / {
        try_files $uri $uri/ /index.html =404;
        autoindex on;
    }
{{end}}
}
{{end}}
`

type nginxTemplateData struct {
	Domain           string
	RootPath         string
	SSLEnabled       bool
	ForceHTTPS       bool
	CertPath         string
	KeyPath          string
	MaintenanceMode  bool
	CustomErrorPages bool
	GzipEnabled      bool
	BrowserCache     bool
	AuthEnabled      bool
	AuthFile         string
}

type NginxAdapter struct {
	availableDir string
	enabledDir   string
	sslManager   *ssl.Manager
	tmpl         *template.Template
	mu           sync.Mutex
}

func ensureDefaultErrorPages() {
	dir := "/var/lib/royalknight/error_pages"
	if err := os.MkdirAll(dir, 0755); err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(dir, "404.html"), []byte(errorPage404HTML), 0644)
	_ = os.WriteFile(filepath.Join(dir, "50x.html"), []byte(errorPage50xHTML), 0644)
	_ = os.WriteFile(filepath.Join(dir, "maintenance.html"), []byte(errorPageMaintenanceHTML), 0644)
}

func writeBasicAuthFile(domain, user, pass string) (string, error) {
	dir := "/var/lib/royalknight/auth"
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	filePath := filepath.Join(dir, fmt.Sprintf("%s.htpasswd", sanitizeDomainFilename(domain)))
	hasher := sha1.New()
	hasher.Write([]byte(pass))
	hash := "{SHA}" + base64.StdEncoding.EncodeToString(hasher.Sum(nil))
	content := fmt.Sprintf("%s:%s\n", user, hash)
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return "", err
	}
	return filePath, nil
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
	ensureDefaultErrorPages()

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
	if existingData, err := os.ReadFile(availablePath); err == nil {
		lines := strings.Split(string(existingData), "\n")
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "root ") && strings.HasSuffix(trimmed, ";") {
				p := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, "root "), ";"))
				if p != "" && p != "/var/www/certbot" {
					rootPath = p
					break
				}
			}
		}
	}

	authFilePath := filepath.Join("/var/lib/royalknight/auth", fmt.Sprintf("%s.htpasswd", sanitizeDomainFilename(domain)))
	hasAuth := false
	if _, err := os.Stat(authFilePath); err == nil {
		hasAuth = true
	}

	data := nginxTemplateData{
		Domain:           domain,
		RootPath:         rootPath,
		SSLEnabled:       true,
		ForceHTTPS:       true,
		CertPath:         cert.CertPath,
		KeyPath:          cert.KeyPath,
		MaintenanceMode:  false,
		CustomErrorPages: true,
		GzipEnabled:      true,
		BrowserCache:     true,
		AuthEnabled:      hasAuth,
		AuthFile:         authFilePath,
	}

	var buf bytes.Buffer
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

func (n *NginxAdapter) ApplySiteConfig(domain string, rootPath string, sslEnabled bool, settings storage.SiteSettings) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	ensureDefaultErrorPages()

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

	var authFilePath string
	if settings.AuthEnabled && settings.AuthUser != "" && settings.AuthPass != "" {
		fPath, err := writeBasicAuthFile(domain, settings.AuthUser, settings.AuthPass)
		if err == nil {
			authFilePath = fPath
		}
	} else if settings.AuthEnabled {
		// Existing file if any
		checkPath := filepath.Join("/var/lib/royalknight/auth", fmt.Sprintf("%s.htpasswd", sanitizeDomainFilename(domain)))
		if _, err := os.Stat(checkPath); err == nil {
			authFilePath = checkPath
		}
	}

	confName := fmt.Sprintf("%s.conf", sanitizeDomainFilename(domain))
	availablePath := filepath.Join(n.availableDir, confName)
	enabledPath := filepath.Join(n.enabledDir, confName)

	data := nginxTemplateData{
		Domain:           domain,
		RootPath:         rootPath,
		SSLEnabled:       hasValidSSL,
		ForceHTTPS:       settings.ForceHTTPS,
		CertPath:         certPath,
		KeyPath:          keyPath,
		MaintenanceMode:  settings.MaintenanceMode,
		CustomErrorPages: settings.CustomErrorPages,
		GzipEnabled:      settings.GzipEnabled,
		BrowserCache:     settings.BrowserCache,
		AuthEnabled:      settings.AuthEnabled && authFilePath != "",
		AuthFile:         authFilePath,
	}

	var buf bytes.Buffer
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

func (n *NginxAdapter) ApplyConfig(domain string, rootPath string, sslEnabled bool) error {
	defaultSettings := storage.SiteSettings{
		Domain:           domain,
		MaintenanceMode:  false,
		CustomErrorPages: true,
		ForceHTTPS:       true,
		GzipEnabled:      true,
		BrowserCache:     true,
		AuthEnabled:      false,
	}
	return n.ApplySiteConfig(domain, rootPath, sslEnabled, defaultSettings)
}

func (n *NginxAdapter) RemoveConfig(domain string) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	confName := fmt.Sprintf("%s.conf", sanitizeDomainFilename(domain))
	availablePath := filepath.Join(n.availableDir, confName)
	enabledPath := filepath.Join(n.enabledDir, confName)

	_ = os.Remove(enabledPath)
	_ = os.Remove(availablePath)
	_ = os.Remove(filepath.Join("/var/lib/royalknight/auth", fmt.Sprintf("%s.htpasswd", sanitizeDomainFilename(domain))))

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
