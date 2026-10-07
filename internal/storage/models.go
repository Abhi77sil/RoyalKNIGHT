package storage

import (
	"time"
)

type User struct {
	ID           int64     `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
}

type Session struct {
	Token     string    `json:"token"`
	UserID    int64     `json:"user_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

type Site struct {
	ID         int64     `json:"id"`
	Domain     string    `json:"domain"`
	RootPath   string    `json:"root_path"`
	SSLEnabled bool      `json:"ssl_enabled"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type Certificate struct {
	ID        int64     `json:"id"`
	Domain    string    `json:"domain"`
	CertPath  string    `json:"cert_path"`
	KeyPath   string    `json:"key_path"`
	Issuer    string    `json:"issuer"`
	ExpiresAt time.Time `json:"expires_at"`
	Status    string    `json:"status"`
	UpdatedAt time.Time `json:"updated_at"`
}

type SwitchLog struct {
	ID           int64     `json:"id"`
	FromServer   string    `json:"from_server"`
	ToServer     string    `json:"to_server"`
	Status       string    `json:"status"`
	ErrorMessage string    `json:"error_message,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type SiteSnapshot struct {
	ID        int64     `json:"id"`
	Domain    string    `json:"domain"`
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
}

type PathStat struct {
	Path  string `json:"path"`
	Count int64  `json:"count"`
}

type TrafficSummary struct {
	Domain         string     `json:"domain"`
	TotalHits      int64      `json:"total_hits"`
	UniqueVisitors int64      `json:"unique_visitors"`
	HitsToday      int64      `json:"hits_today"`
	VisitorsToday  int64      `json:"visitors_today"`
	PeakHour       int        `json:"peak_hour"`
	HourlyHits     [24]int64  `json:"hourly_hits"`
	TopPaths       []PathStat `json:"top_paths"`
}


