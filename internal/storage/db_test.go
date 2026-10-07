package storage

import (
	"path/filepath"
	"testing"
	"time"
)

func TestStorage_DatabaseOperations(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	// Users & Sessions
	user, err := db.CreateUser("admin", "hashed_password")
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	if user.Username != "admin" {
		t.Fatalf("expected username admin, got %s", user.Username)
	}

	token := "session_token_12345"
	expires := time.Now().UTC().Add(1 * time.Hour)
	if err := db.CreateSession(token, user.ID, expires); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	authUser, err := db.ValidateSession(token)
	if err != nil || authUser.Username != "admin" {
		t.Fatalf("ValidateSession failed: %v", err)
	}

	// Sites
	site, err := db.UpsertSite("test.example.com", "/var/www/test", true)
	if err != nil {
		t.Fatalf("UpsertSite failed: %v", err)
	}
	if site.Domain != "test.example.com" {
		t.Fatalf("expected domain test.example.com, got %s", site.Domain)
	}

	sites, err := db.ListSites()
	if err != nil || len(sites) != 1 {
		t.Fatalf("ListSites failed: %v (len=%d)", err, len(sites))
	}

	// Certificates
	cert := &Certificate{
		Domain:    "test.example.com",
		CertPath:  "/etc/ssl/cert.pem",
		KeyPath:   "/etc/ssl/cert.key",
		Issuer:    "Let's Encrypt",
		ExpiresAt: time.Now().UTC().Add(90 * 24 * time.Hour),
		Status:    "VALID",
	}
	if err := db.UpsertCertificate(cert); err != nil {
		t.Fatalf("UpsertCertificate failed: %v", err)
	}

	loadedCert, err := db.GetCertificate("test.example.com")
	if err != nil || loadedCert.Domain != "test.example.com" {
		t.Fatalf("GetCertificate failed: %v", err)
	}

	// Server state & switch logs
	if err := db.SetActiveServer("caddy"); err != nil {
		t.Fatalf("SetActiveServer failed: %v", err)
	}
	active, err := db.GetActiveServer()
	if err != nil || active != "caddy" {
		t.Fatalf("GetActiveServer returned %s, err %v", active, err)
	}

	if err := db.LogServerSwitch("caddy", "nginx", "SUCCESS", ""); err != nil {
		t.Fatalf("LogServerSwitch failed: %v", err)
	}
	logs, err := db.ListSwitchLogs(10)
	if err != nil || len(logs) != 1 {
		t.Fatalf("ListSwitchLogs failed: %v", err)
	}

	// Traffic & Analytics
	_ = db.RecordTrafficHit("test.example.com", "1.1.1.1", "/", "Mozilla", 200)
	_ = db.RecordTrafficHit("test.example.com", "2.2.2.2", "/about", "Chrome", 200)
	_ = db.RecordTrafficHit("test.example.com", "1.1.1.1", "/contact", "Mozilla", 200)

	summary, err := db.GetTrafficSummary("test.example.com")
	if err != nil {
		t.Fatalf("GetTrafficSummary failed: %v", err)
	}
	if summary.TotalHits != 3 {
		t.Fatalf("expected 3 hits, got %d", summary.TotalHits)
	}
	if summary.UniqueVisitors != 2 {
		t.Fatalf("expected 2 unique visitors, got %d", summary.UniqueVisitors)
	}
}
