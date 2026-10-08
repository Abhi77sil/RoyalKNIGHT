package ssl

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"royalknight/internal/storage"
)

const (
	ACMEWebrootPath = "/var/www/certbot"
)

type Manager struct {
	certDir string
	db      *storage.DB
}

func NewManager(certDir string, db *storage.DB) (*Manager, error) {
	if certDir == "" {
		certDir = "/etc/ssl/royalknight"
	}

	if err := os.MkdirAll(filepath.Join(certDir, "certs"), 0755); err != nil {
		return nil, fmt.Errorf("failed to create certs directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(certDir, "private"), 0700); err != nil {
		return nil, fmt.Errorf("failed to create private keys directory: %w", err)
	}
	_ = os.MkdirAll(ACMEWebrootPath, 0755)

	return &Manager{
		certDir: certDir,
		db:      db,
	}, nil
}

func (m *Manager) GetCertPaths(domain string) (certPath, keyPath string) {
	certPath = filepath.Join(m.certDir, "certs", fmt.Sprintf("%s.crt", domain))
	keyPath = filepath.Join(m.certDir, "private", fmt.Sprintf("%s.key", domain))
	return certPath, keyPath
}

func (m *Manager) GetCertificate(domain string) (*storage.Certificate, error) {
	return m.db.GetCertificate(domain)
}

// HasValidCertificate returns true only if a trusted Let's Encrypt certificate exists on disk and in DB.
func (m *Manager) HasValidCertificate(domain string) bool {
	cert, err := m.db.GetCertificate(domain)
	if err != nil || cert == nil {
		return false
	}
	if cert.Status != "ACTIVE_LETS_ENCRYPT" {
		return false
	}
	if time.Now().After(cert.ExpiresAt.Add(-24 * time.Hour)) {
		return false
	}
	if _, err := os.Stat(cert.CertPath); err != nil {
		return false
	}
	if _, err := os.Stat(cert.KeyPath); err != nil {
		return false
	}
	return true
}

// EnsureCertificate verifies or provisions a trusted Let's Encrypt certificate.
func (m *Manager) EnsureCertificate(domain string) (*storage.Certificate, error) {
	if m.HasValidCertificate(domain) {
		return m.db.GetCertificate(domain)
	}

	return m.ProvisionLetsEncrypt(domain)
}

// ProvisionLetsEncrypt runs Certbot webroot HTTP-01 verification to acquire a trusted TLS certificate.
func (m *Manager) ProvisionLetsEncrypt(domain string) (*storage.Certificate, error) {
	certbotPath, err := exec.LookPath("certbot")
	if err != nil {
		for _, p := range []string{"/usr/bin/certbot", "/snap/bin/certbot", "/usr/local/bin/certbot"} {
			if _, statErr := os.Stat(p); statErr == nil {
				certbotPath = p
				err = nil
				break
			}
		}
	}
	if err != nil {
		return nil, fmt.Errorf("certbot is not installed. Run: sudo apt install -y certbot python3-certbot-nginx")
	}

	_ = os.MkdirAll(ACMEWebrootPath, 0755)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// Resolve whether www subdomain also points to this server to include in the certificate
	targetDomains := []string{domain}
	if !strings.HasPrefix(domain, "www.") {
		www := "www." + domain
		if ips, err := net.LookupIP(www); err == nil && len(ips) > 0 {
			targetDomains = append(targetDomains, www)
		}
	}

	webrootArgs := []string{
		"certonly",
		"--webroot",
		"-w", ACMEWebrootPath,
		"--preferred-challenges", "http",
		"--non-interactive",
		"--agree-tos",
		"--register-unsafely-without-email",
		"--keep-until-expiring",
	}
	for _, d := range targetDomains {
		webrootArgs = append(webrootArgs, "-d", d)
	}

	// 1. Attempt Webroot verification
	cmd := exec.CommandContext(ctx, certbotPath, webrootArgs...)

	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	runErr := cmd.Run()
	if runErr != nil {
		log.Printf("[SSL] Certbot webroot challenge failed for %s (%v). Attempting --nginx plugin...", domain, runErr)

		nginxArgs := []string{
			"certonly",
			"--nginx",
			"--preferred-challenges", "http",
			"--non-interactive",
			"--agree-tos",
			"--register-unsafely-without-email",
			"--keep-until-expiring",
		}
		for _, d := range targetDomains {
			nginxArgs = append(nginxArgs, "-d", d)
		}

		// 2. Fallback to --nginx plugin
		cmdNginx := exec.CommandContext(ctx, certbotPath, nginxArgs...)
		var outputNginx bytes.Buffer
		cmdNginx.Stdout = &outputNginx
		cmdNginx.Stderr = &outputNginx

		if errNginx := cmdNginx.Run(); errNginx != nil {
			return nil, fmt.Errorf("Let's Encrypt challenge failed for %s. Webroot: %s; Nginx: %s",
				domain, strings.TrimSpace(output.String()), strings.TrimSpace(outputNginx.String()))
		}
	}

	// Certbot live certificate paths
	liveFullchain := filepath.Join("/etc/letsencrypt/live", domain, "fullchain.pem")
	livePrivkey := filepath.Join("/etc/letsencrypt/live", domain, "privkey.pem")

	if _, err := os.Stat(liveFullchain); err != nil {
		return nil, fmt.Errorf("certbot finished but fullchain.pem not found at %s", liveFullchain)
	}

	certPath, keyPath := m.GetCertPaths(domain)
	if err := copyFile(liveFullchain, certPath); err != nil {
		return nil, fmt.Errorf("failed to copy fullchain cert: %w", err)
	}
	if err := copyFile(livePrivkey, keyPath); err != nil {
		return nil, fmt.Errorf("failed to copy private key: %w", err)
	}
	_ = os.Chmod(keyPath, 0600)

	certBytes, err := os.ReadFile(certPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read cert file: %w", err)
	}
	block, _ := pem.Decode(certBytes)
	if block == nil {
		return nil, fmt.Errorf("failed to decode certificate PEM block")
	}
	parsedCert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse x509 certificate: %w", err)
	}

	certRecord := &storage.Certificate{
		Domain:    domain,
		CertPath:  certPath,
		KeyPath:   keyPath,
		Issuer:    "Let's Encrypt Authority",
		ExpiresAt: parsedCert.NotAfter,
		Status:    "ACTIVE_LETS_ENCRYPT",
	}

	if err := m.db.UpsertCertificate(certRecord); err != nil {
		return nil, fmt.Errorf("failed to save certificate to database: %w", err)
	}

	log.Printf("[SSL] Successfully registered Let's Encrypt certificate for %s (expires: %s)", domain, parsedCert.NotAfter.Format(time.RFC3339))
	return certRecord, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
