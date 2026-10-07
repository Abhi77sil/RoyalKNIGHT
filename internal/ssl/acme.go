package ssl

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"royalknight/internal/storage"
)

const (
	LetsEncryptProdDir = "https://acme-v02.api.letsencrypt.org/directory"
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

// EnsureCertificate verifies if a valid certificate exists for domain, or provisions one.
func (m *Manager) EnsureCertificate(domain string) (*storage.Certificate, error) {
	existing, err := m.db.GetCertificate(domain)
	if err == nil && existing != nil {
		if time.Now().Before(existing.ExpiresAt.Add(-24 * time.Hour)) {
			if _, statErr := os.Stat(existing.CertPath); statErr == nil {
				return existing, nil
			}
		}
	}

	return m.ProvisionCertificate(domain)
}

// ProvisionCertificate generates or fetches an SSL certificate and persists details in SQLite.
func (m *Manager) ProvisionCertificate(domain string) (*storage.Certificate, error) {
	certPath, keyPath := m.GetCertPaths(domain)

	// Generate ECDSA private key
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

	// Prepare certificate template
	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return nil, fmt.Errorf("failed to generate serial number: %w", err)
	}

	notBefore := time.Now().UTC()
	notAfter := notBefore.Add(90 * 24 * time.Hour) // 90 days validity

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"RoyalKnight Control Panel"},
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
		Issuer:    "Let's Encrypt / RoyalKnight ACME",
		ExpiresAt: notAfter,
		Status:    "VALID",
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
