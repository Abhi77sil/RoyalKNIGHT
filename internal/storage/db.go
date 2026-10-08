package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type DB struct {
	db *sql.DB
}

func Open(dbPath string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0750); err != nil {
		return nil, fmt.Errorf("failed to create db directory: %w", err)
	}

	dsn := fmt.Sprintf("%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)", dbPath)
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	conn.SetMaxOpenConns(1)
	conn.SetMaxIdleConns(1)
	conn.SetConnMaxLifetime(0)

	instance := &DB{db: conn}
	if err := instance.migrate(); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("migration failure: %w", err)
	}

	return instance, nil
}

func (d *DB) Close() error {
	return d.db.Close()
}

func (d *DB) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		created_at DATETIME NOT NULL
	);

	CREATE TABLE IF NOT EXISTS sessions (
		token TEXT PRIMARY KEY,
		user_id INTEGER NOT NULL,
		expires_at DATETIME NOT NULL,
		FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS sites (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		domain TEXT UNIQUE NOT NULL,
		root_path TEXT NOT NULL,
		ssl_enabled BOOLEAN NOT NULL DEFAULT 1,
		status TEXT NOT NULL DEFAULT 'active',
		created_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL
	);

	CREATE TABLE IF NOT EXISTS certificates (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		domain TEXT UNIQUE NOT NULL,
		cert_path TEXT NOT NULL,
		key_path TEXT NOT NULL,
		issuer TEXT NOT NULL,
		expires_at DATETIME NOT NULL,
		status TEXT NOT NULL,
		updated_at DATETIME NOT NULL
	);

	CREATE TABLE IF NOT EXISTS server_state (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		updated_at DATETIME NOT NULL
	);

	CREATE TABLE IF NOT EXISTS server_switch_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		from_server TEXT NOT NULL,
		to_server TEXT NOT NULL,
		status TEXT NOT NULL,
		error_message TEXT,
		created_at DATETIME NOT NULL
	);

	CREATE TABLE IF NOT EXISTS site_snapshots (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		domain TEXT NOT NULL,
		name TEXT NOT NULL,
		path TEXT NOT NULL,
		is_active BOOLEAN NOT NULL DEFAULT 0,
		created_at DATETIME NOT NULL,
		UNIQUE(domain, name)
	);

	CREATE TABLE IF NOT EXISTS traffic_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		domain TEXT NOT NULL,
		ip TEXT NOT NULL,
		path TEXT NOT NULL,
		user_agent TEXT,
		status_code INTEGER,
		created_at DATETIME NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_traffic_domain_time ON traffic_events(domain, created_at);
	`
	_, err := d.db.Exec(schema)
	if err != nil {
		return err
	}

	// Default active server state to nginx if unset
	var val string
	err = d.db.QueryRow("SELECT value FROM server_state WHERE key = 'active_server'").Scan(&val)
	if err == sql.ErrNoRows {
		_, err = d.db.Exec("INSERT INTO server_state (key, value, updated_at) VALUES ('active_server', 'nginx', ?)", time.Now().UTC())
	}
	return err
}

// User operations
func (d *DB) CreateUser(username, passwordHash string) (*User, error) {
	now := time.Now().UTC()
	res, err := d.db.Exec("INSERT INTO users (username, password_hash, created_at) VALUES (?, ?, ?)", username, passwordHash, now)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return &User{ID: id, Username: username, PasswordHash: passwordHash, CreatedAt: now}, nil
}

func (d *DB) GetUserByUsername(username string) (*User, error) {
	u := &User{}
	err := d.db.QueryRow("SELECT id, username, password_hash, created_at FROM users WHERE username = ?", username).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (d *DB) UpsertUser(username, passwordHash string) (*User, error) {
	u, err := d.GetUserByUsername(username)
	if err == nil && u != nil {
		_, err := d.db.Exec("UPDATE users SET password_hash = ? WHERE username = ?", passwordHash, username)
		if err != nil {
			return nil, err
		}
		u.PasswordHash = passwordHash
		return u, nil
	}
	return d.CreateUser(username, passwordHash)
}

func (d *DB) UserCount() (int, error) {
	var count int
	err := d.db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	return count, err
}

// Session operations
func (d *DB) CreateSession(token string, userID int64, expiresAt time.Time) error {
	_, err := d.db.Exec("INSERT INTO sessions (token, user_id, expires_at) VALUES (?, ?, ?)", token, userID, expiresAt)
	return err
}

func (d *DB) ValidateSession(token string) (*User, error) {
	query := `
	SELECT u.id, u.username, u.password_hash, u.created_at
	FROM sessions s
	JOIN users u ON s.user_id = u.id
	WHERE s.token = ? AND s.expires_at > ?
	`
	u := &User{}
	err := d.db.QueryRow(query, token, time.Now().UTC()).
		Scan(&u.ID, &u.Username, &u.PasswordHash, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (d *DB) DeleteSession(token string) error {
	_, err := d.db.Exec("DELETE FROM sessions WHERE token = ?", token)
	return err
}

// Site operations
func (d *DB) UpsertSite(domain, rootPath string, sslEnabled bool) (*Site, error) {
	now := time.Now().UTC()
	query := `
	INSERT INTO sites (domain, root_path, ssl_enabled, status, created_at, updated_at)
	VALUES (?, ?, ?, 'active', ?, ?)
	ON CONFLICT(domain) DO UPDATE SET
		root_path = excluded.root_path,
		ssl_enabled = excluded.ssl_enabled,
		updated_at = excluded.updated_at
	`
	_, err := d.db.Exec(query, domain, rootPath, sslEnabled, now, now)
	if err != nil {
		return nil, err
	}
	return d.GetSite(domain)
}

func (d *DB) GetSite(domain string) (*Site, error) {
	s := &Site{}
	err := d.db.QueryRow("SELECT id, domain, root_path, ssl_enabled, status, created_at, updated_at FROM sites WHERE domain = ?", domain).
		Scan(&s.ID, &s.Domain, &s.RootPath, &s.SSLEnabled, &s.Status, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (d *DB) ListSites() ([]Site, error) {
	rows, err := d.db.Query("SELECT id, domain, root_path, ssl_enabled, status, created_at, updated_at FROM sites ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sites []Site
	for rows.Next() {
		var s Site
		if err := rows.Scan(&s.ID, &s.Domain, &s.RootPath, &s.SSLEnabled, &s.Status, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		sites = append(sites, s)
	}
	return sites, rows.Err()
}

func (d *DB) DeleteSite(domain string) error {
	_, err := d.db.Exec("DELETE FROM sites WHERE domain = ?", domain)
	return err
}

func (d *DB) UpdateSiteSSL(domain string, sslEnabled bool) error {
	now := time.Now().UTC()
	_, err := d.db.Exec("UPDATE sites SET ssl_enabled = ?, updated_at = ? WHERE domain = ?", sslEnabled, now, domain)
	return err
}

// Certificate operations
func (d *DB) UpsertCertificate(cert *Certificate) error {
	now := time.Now().UTC()
	query := `
	INSERT INTO certificates (domain, cert_path, key_path, issuer, expires_at, status, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(domain) DO UPDATE SET
		cert_path = excluded.cert_path,
		key_path = excluded.key_path,
		issuer = excluded.issuer,
		expires_at = excluded.expires_at,
		status = excluded.status,
		updated_at = excluded.updated_at
	`
	_, err := d.db.Exec(query, cert.Domain, cert.CertPath, cert.KeyPath, cert.Issuer, cert.ExpiresAt, cert.Status, now)
	return err
}

func (d *DB) GetCertificate(domain string) (*Certificate, error) {
	c := &Certificate{}
	err := d.db.QueryRow("SELECT id, domain, cert_path, key_path, issuer, expires_at, status, updated_at FROM certificates WHERE domain = ?", domain).
		Scan(&c.ID, &c.Domain, &c.CertPath, &c.KeyPath, &c.Issuer, &c.ExpiresAt, &c.Status, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (d *DB) ListCertificates() ([]Certificate, error) {
	rows, err := d.db.Query("SELECT id, domain, cert_path, key_path, issuer, expires_at, status, updated_at FROM certificates ORDER BY domain ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var certs []Certificate
	for rows.Next() {
		var c Certificate
		if err := rows.Scan(&c.ID, &c.Domain, &c.CertPath, &c.KeyPath, &c.Issuer, &c.ExpiresAt, &c.Status, &c.UpdatedAt); err != nil {
			return nil, err
		}
		certs = append(certs, c)
	}
	return certs, rows.Err()
}

// Server state operations
func (d *DB) GetActiveServer() (string, error) {
	var val string
	err := d.db.QueryRow("SELECT value FROM server_state WHERE key = 'active_server'").Scan(&val)
	if err == sql.ErrNoRows {
		return "caddy", nil
	}
	return val, err
}

func (d *DB) SetActiveServer(server string) error {
	now := time.Now().UTC()
	query := `
	INSERT INTO server_state (key, value, updated_at)
	VALUES ('active_server', ?, ?)
	ON CONFLICT(key) DO UPDATE SET
		value = excluded.value,
		updated_at = excluded.updated_at
	`
	_, err := d.db.Exec(query, server, now)
	return err
}

// Server switch logs
func (d *DB) LogServerSwitch(fromServer, toServer, status, errMsg string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := d.db.ExecContext(ctx, "INSERT INTO server_switch_logs (from_server, to_server, status, error_message, created_at) VALUES (?, ?, ?, ?, ?)",
		fromServer, toServer, status, errMsg, time.Now().UTC())
	return err
}

func (d *DB) ListSwitchLogs(limit int) ([]SwitchLog, error) {
	rows, err := d.db.Query("SELECT id, from_server, to_server, status, COALESCE(error_message, ''), created_at FROM server_switch_logs ORDER BY created_at DESC LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []SwitchLog
	for rows.Next() {
		var l SwitchLog
		if err := rows.Scan(&l.ID, &l.FromServer, &l.ToServer, &l.Status, &l.ErrorMessage, &l.CreatedAt); err != nil {
			return nil, err
		}
		logs = append(logs, l)
	}
	return logs, rows.Err()
}

// Snapshot operations
func (d *DB) CreateSnapshot(domain, name, path string, isActive bool) (*SiteSnapshot, error) {
	now := time.Now().UTC()
	query := `
	INSERT INTO site_snapshots (domain, name, path, is_active, created_at)
	VALUES (?, ?, ?, ?, ?)
	ON CONFLICT(domain, name) DO UPDATE SET
		path = excluded.path,
		is_active = excluded.is_active,
		created_at = excluded.created_at
	`
	res, err := d.db.Exec(query, domain, name, path, isActive, now)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return &SiteSnapshot{
		ID:        id,
		Domain:    domain,
		Name:      name,
		Path:      path,
		IsActive:  isActive,
		CreatedAt: now,
	}, nil
}

func (d *DB) ListSnapshots(domain string) ([]SiteSnapshot, error) {
	var rows *sql.Rows
	var err error
	if domain != "" {
		rows, err = d.db.Query("SELECT id, domain, name, path, is_active, created_at FROM site_snapshots WHERE domain = ? ORDER BY created_at DESC", domain)
	} else {
		rows, err = d.db.Query("SELECT id, domain, name, path, is_active, created_at FROM site_snapshots ORDER BY created_at DESC")
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []SiteSnapshot
	for rows.Next() {
		var s SiteSnapshot
		if err := rows.Scan(&s.ID, &s.Domain, &s.Name, &s.Path, &s.IsActive, &s.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, s)
	}
	return list, rows.Err()
}

func (d *DB) GetActiveSnapshot(domain string) (*SiteSnapshot, error) {
	s := &SiteSnapshot{}
	err := d.db.QueryRow("SELECT id, domain, name, path, is_active, created_at FROM site_snapshots WHERE domain = ? AND is_active = 1", domain).
		Scan(&s.ID, &s.Domain, &s.Name, &s.Path, &s.IsActive, &s.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (d *DB) GetSnapshot(id int64) (*SiteSnapshot, error) {
	s := &SiteSnapshot{}
	err := d.db.QueryRow("SELECT id, domain, name, path, is_active, created_at FROM site_snapshots WHERE id = ?", id).
		Scan(&s.ID, &s.Domain, &s.Name, &s.Path, &s.IsActive, &s.CreatedAt)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (d *DB) SetActiveSnapshot(domain string, snapshotID int64) error {
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("UPDATE site_snapshots SET is_active = 0 WHERE domain = ?", domain); err != nil {
		return err
	}
	if _, err := tx.Exec("UPDATE site_snapshots SET is_active = 1 WHERE domain = ? AND id = ?", domain, snapshotID); err != nil {
		return err
	}
	return tx.Commit()
}

func (d *DB) DeleteSnapshot(id int64) error {
	_, err := d.db.Exec("DELETE FROM site_snapshots WHERE id = ?", id)
	return err
}

// Traffic & Analytics
func (d *DB) RecordTrafficHit(domain, ip, path, userAgent string, statusCode int) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	query := `INSERT INTO traffic_events (domain, ip, path, user_agent, status_code, created_at) VALUES (?, ?, ?, ?, ?, ?)`
	_, err := d.db.ExecContext(ctx, query, domain, ip, path, userAgent, statusCode, time.Now().UTC())
	return err
}

func (d *DB) GetTrafficSummary(domain string) (*TrafficSummary, error) {
	summary := &TrafficSummary{
		Domain:   domain,
		TopPaths: []PathStat{},
	}

	// 1. Total hits and unique visitors
	err := d.db.QueryRow(`SELECT COUNT(*), COUNT(DISTINCT ip) FROM traffic_events WHERE domain = ?`, domain).
		Scan(&summary.TotalHits, &summary.UniqueVisitors)
	if err != nil {
		return summary, err
	}

	// 2. Hits and visitors today
	midnight := time.Now().UTC().Truncate(24 * time.Hour)
	_ = d.db.QueryRow(`SELECT COUNT(*), COUNT(DISTINCT ip) FROM traffic_events WHERE domain = ? AND created_at >= ?`, domain, midnight).
		Scan(&summary.HitsToday, &summary.VisitorsToday)

	// 3. Hourly distribution and peak hour
	rows, err := d.db.Query(`
		SELECT CAST(strftime('%H', created_at) AS INTEGER) AS hr, COUNT(*) 
		FROM traffic_events 
		WHERE domain = ? AND created_at >= ?
		GROUP BY hr
	`, domain, time.Now().UTC().Add(-24*time.Hour))
	if err == nil {
		defer rows.Close()
		var maxHits int64
		for rows.Next() {
			var hr int
			var count int64
			if err := rows.Scan(&hr, &count); err == nil && hr >= 0 && hr < 24 {
				summary.HourlyHits[hr] = count
				if count > maxHits {
					maxHits = count
					summary.PeakHour = hr
				}
			}
		}
	}

	// 4. Top visited paths
	pRows, err := d.db.Query(`
		SELECT path, COUNT(*) as cnt 
		FROM traffic_events 
		WHERE domain = ? 
		GROUP BY path 
		ORDER BY cnt DESC 
		LIMIT 10
	`, domain)
	if err == nil {
		defer pRows.Close()
		for pRows.Next() {
			var ps PathStat
			if err := pRows.Scan(&ps.Path, &ps.Count); err == nil {
				summary.TopPaths = append(summary.TopPaths, ps)
			}
		}
	}

	return summary, nil
}


