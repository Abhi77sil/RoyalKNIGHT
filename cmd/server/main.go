package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"royalknight/internal/adapter"
	"royalknight/internal/auth"
	"royalknight/internal/deploy"
	"royalknight/internal/manager"
	"royalknight/internal/ssl"
	"royalknight/internal/storage"
	"royalknight/internal/web"
)

func main() {
	port := flag.Int("port", 7777, "Port for RoyalKnight admin panel")
	dbPath := flag.String("db", "./data/panel.db", "Path to SQLite database file")
	sitesDir := flag.String("sites-dir", "./data/www", "Base directory for hosted sites")
	snapshotsDir := flag.String("snapshots-dir", "./data/snapshots", "Base directory for site snapshot images")
	nginxDir := flag.String("nginx-dir", "/etc/nginx", "Base directory for Nginx configuration")
	sslDir := flag.String("ssl-dir", "./data/ssl", "Base directory for SSL certificates")
	adminUser := flag.String("admin-user", "admin", "Initial admin username")
	adminPass := flag.String("admin-pass", "admin123456", "Initial admin password")
	initAdminOnly := flag.Bool("init-admin", false, "Initialize database with administrator credentials and exit")
	flag.Parse()

	log.Printf("[Init] Starting RoyalKnight Control Panel on port %d...", *port)

	// 1. Initialize SQLite Database (modernc.org/sqlite, CGO-free)
	db, err := storage.Open(*dbPath)
	if err != nil {
		log.Fatalf("[Init] Failed to open SQLite database at %s: %v", *dbPath, err)
	}
	defer db.Close()
	log.Printf("[Init] SQLite database initialized at %s", *dbPath)

	// 2. Initialize Authentication & seed initial administrator
	authSvc := auth.NewService(db)
	userCount, err := db.UserCount()
	if err != nil {
		log.Fatalf("[Init] Failed to count users: %v", err)
	}
	if userCount == 0 || *initAdminOnly {
		hash, err := authSvc.HashPassword(*adminPass)
		if err != nil {
			log.Fatalf("[Init] Failed to hash password: %v", err)
		}
		if _, err := db.UpsertUser(*adminUser, hash); err != nil {
			log.Fatalf("[Init] Failed to seed/update admin user: %v", err)
		}
		log.Printf("[Init] Administrator configured: %s", *adminUser)
	}

	if *initAdminOnly {
		log.Println("[Init] Administrator credential setup complete. Exiting.")
		return
	}

	// 3. Initialize SSL Manager
	sslMgr, err := ssl.NewManager(*sslDir, db)
	if err != nil {
		log.Fatalf("[Init] Failed to initialize SSL manager: %v", err)
	}

	// 4. Initialize Nginx WebServer Adapter
	nginxAdapter, err := adapter.NewNginxAdapter(*nginxDir, sslMgr)
	if err != nil {
		log.Printf("[Init] Note: Nginx directory setup: %v", err)
	}

	// 5. Initialize Server Orchestrator (Nginx dedicated engine)
	orch, err := manager.NewOrchestrator(db, nginxAdapter)
	if err != nil {
		log.Fatalf("[Init] Failed to initialize orchestrator: %v", err)
	}
	log.Printf("[Init] Active web server engine: %s", orch.GetActiveServerName())

	// 6. Initialize Snapshot Manager
	snapMgr, err := deploy.NewSnapshotManager(*snapshotsDir, db, orch)
	if err != nil {
		log.Fatalf("[Init] Failed to initialize snapshot manager: %v", err)
	}

	// 7. Initialize Web Server & Routes
	webServer, err := web.NewServer(db, authSvc, orch, snapMgr, *sitesDir)
	if err != nil {
		log.Fatalf("[Init] Failed to initialize HTTP web server: %v", err)
	}

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", *port),
		Handler:      webServer.Routes(),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Background server listener
	go func() {
		log.Printf("[Server] RoyalKnight panel listening at http://0.0.0.0:%d/admin", *port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[Server] Listen error: %v", err)
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit
	log.Println("[Shutdown] Signal received, shutting down gracefully...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("[Shutdown] Forced shutdown: %v", err)
	}
	log.Println("[Shutdown] RoyalKnight terminated clean.")
}
