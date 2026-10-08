package ssl

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"royalknight/internal/storage"
)

const (
	LetsEncryptProdDir = "https://acme-v02.api.letsencrypt.org/directory"
	ACMEWebrootPath    = "/var/www/certbot"
)

type Manager struct {
	certDir      string
	db           *storage.DB
	directoryURL string
	httpClient   *http.Client
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
		certDir:      certDir,
		db:           db,
		directoryURL: LetsEncryptProdDir,
		httpClient:   &http.Client{Timeout: 10 * time.Second},
	}, nil
}

func (m *Manager) GetCertPaths(domain string) (certPath, keyPath string) {
	certPath = filepath.Join(m.certDir, "certs", fmt.Sprintf("%s.crt", domain))
	keyPath = filepath.Join(m.certDir, "private", fmt.Sprintf("%s.key", domain))
	return certPath, keyPath
}

// EnsureCertificate returns a valid certificate, attempting Let's Encrypt first and falling back to staging.
func (m *Manager) EnsureCertificate(domain string) (*storage.Certificate, error) {
	existing, err := m.db.GetCertificate(domain)
	if err == nil && existing != nil {
		if time.Now().Before(existing.ExpiresAt.Add(-24 * time.Hour)) {
			if _, statErr := os.Stat(existing.CertPath); statErr == nil {
				// If it's already a trusted Let's Encrypt cert, return it
				if existing.Status == "ACTIVE_LETS_ENCRYPT" {
					return existing, nil
				}
			}
		}
	}

	// Try real Let's Encrypt first if certbot is present
	if cert, err := m.ProvisionLetsEncrypt(domain); err == nil {
		return cert, nil
	} else {
		log.Printf("[SSL] Let's Encrypt issuance deferred for %s: %v", domain, err)
	}

	// Fallback to generating or returning staging self-signed cert
	return m.ProvisionStagingCertificate(domain)
}

// ProvisionLetsEncrypt executes Certbot webroot challenge to obtain an official trusted TLS certificate.
func (m *Manager) ProvisionLetsEncrypt(domain string) (*storage.Certificate, error) {
	certbotPath, err := exec.LookPath("certbot")
	if err != nil {
		return nil, fmt.Errorf("certbot is not installed (sudo apt install -y certbot)")
	}

	_ = os.MkdirAll(ACMEWebrootPath, 0755)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, certbotPath, "certonly",
		"--webroot",
		"-w", ACMEWebrootPath,
		"-d", domain,
		"--non-interactive",
		"--agree-tos",
		"--register-unsafely-without-email",
		"--keep-until-expiring",
	)

	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("certbot failed: %w (output: %s)", err, strings.TrimSpace(output.String()))
	}

	// Certbot stores live certs at /etc/letsencrypt/live/<domain>/
	liveFullchain := filepath.Join("/etc/letsencrypt/live", domain, "fullchain.pem")
	livePrivkey := filepath.Join("/etc/letsencrypt/live", domain, "privkey.pem")

	if _, err := os.Stat(liveFullchain); err != nil {
		return nil, fmt.Errorf("certbot completed but fullchain not found at %s", liveFullchain)
	}

	certPath, keyPath := m.GetCertPaths(domain)
	if err := copyFile(liveFullchain, certPath); err != nil {
		return nil, fmt.Errorf("failed to copy fullchain cert: %w", err)
	}
	if err := copyFile(livePrivkey, keyPath); err != nil {
		return nil, fmt.Errorf("failed to copy private key: %w", err)
	}
	_ = os.Chmod(keyPath, 0600)

	// Parse certificate to record exact expiration
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
		return nil, fmt.Errorf("failed to save certificate record: %w", err)
	}

	log.Printf("[SSL] Successfully registered Let's Encrypt certificate for %s (expires: %s)", domain, parsedCert.NotAfter.Format(time.RFC3339))
	return certRecord, nil
}

// ProvisionStagingCertificate generates a temporary ECDSA certificate to permit Nginx binding while awaiting DNS.
func (m *Manager) ProvisionStagingCertificate(domain string) (*storage.Certificate, error) {
	certPath, keyPath := m.GetCertPaths(domain)

	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate private key for %s: %w", domain, err)
	}

	keyDER, err := x509.MarshalECPrivateKey(privKey)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal private key: %w", err)
	}

	keyFile, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to save private key: %w", err)
	}
	if err := pem.Encode(keyFile, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}); err != nil {
		keyFile.Close()
		return nil, fmt.Errorf("failed to encode private key PEM: %w", err)
	}
	keyFile.Close()

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return nil, fmt.Errorf("failed to generate serial number: %w", err)
	}

	notBefore := time.Now().UTC()
	notAfter := notBefore.Add(30 * 24 * time.Hour)

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"RoyalKnight Staging Certificate"},
			CommonName:   domain,
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	if ip := net.ParseIP(domain); ip != nil {
		template.IPAddresses = append(template.IPAddresses, ip)
	} else {
		template.DNSNames = append(template.DNSNames, domain)
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &privKey.PublicKey, privKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create certificate: %w", err)
	}

	certFile, err := os.OpenFile(certPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to save certificate: %w", err)
	}
	if err := pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); err != nil {
		certFile.Close()
		return nil, fmt.Errorf("failed to encode certificate PEM: %w", err)
	}
	certFile.Close()

	certRecord := &storage.Certificate{
		Domain:    domain,
		CertPath:  certPath,
		KeyPath:   keyPath,
		Issuer:    "Self-Signed (Pending DNS / Staging)",
		ExpiresAt: notAfter,
		Status:    "PENDING_DNS_VERIFICATION",
	}

	if err := m.db.UpsertCertificate(certRecord); err != nil {
		return nil, fmt.Errorf("failed to record certificate in database: %w", err)
	}

	return certRecord, nil
}

// FetchACMEDirectory discovers ACME endpoints from the directory provider.
func (m *Manager) FetchACMEDirectory() (map[string]any, error) {
	resp, err := m.httpClient.Get(m.directoryURL)
	if err != nil {
		return nil, fmt.Errorf("failed to query ACME directory: %w", err)
	}
	defer resp.Body.Close()

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode ACME directory response: %w", err)
	}
	return result, nil
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
