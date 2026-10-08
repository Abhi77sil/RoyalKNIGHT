package web

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"royalknight/internal/auth"
	"royalknight/internal/deploy"
	"royalknight/internal/health"
	"royalknight/internal/manager"
	"royalknight/internal/metrics"
	"royalknight/internal/storage"
)

//go:embed templates/* static/*
var contentFS embed.FS

type Server struct {
	db             *storage.DB
	authSvc        *auth.Service
	orchestrator   *manager.Orchestrator
	snapshotMgr    *deploy.SnapshotManager
	metricsMonitor *metrics.Monitor
	healthChecker  *health.Checker
	ipResolver     *metrics.IPResolver
	sitesBaseDir   string
	templates      *template.Template
}

func NewServer(db *storage.DB, authSvc *auth.Service, orch *manager.Orchestrator, snapMgr *deploy.SnapshotManager, sitesBaseDir string) (*Server, error) {
	if sitesBaseDir == "" {
		sitesBaseDir = "/var/www"
	}
	if err := os.MkdirAll(sitesBaseDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create sites directory: %w", err)
	}

	tmpl, err := template.ParseFS(contentFS, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("failed to parse templates: %w", err)
	}

	return &Server{
		db:             db,
		authSvc:        authSvc,
		orchestrator:   orch,
		snapshotMgr:    snapMgr,
		metricsMonitor: metrics.NewMonitor(),
		healthChecker:  health.NewChecker(),
		ipResolver:     metrics.NewIPResolver(),
		sitesBaseDir:   sitesBaseDir,
		templates:      tmpl,
	}, nil
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	// Static assets
	mux.Handle("/static/", http.FileServer(http.FS(contentFS)))

	// Public routes
	mux.HandleFunc("GET /login", s.handleLoginPage)
	mux.HandleFunc("POST /api/login", s.handleLoginAPI)
	mux.HandleFunc("POST /api/logout", s.handleLogoutAPI)

	// Traffic tracking endpoint (can be invoked by edge/site requests)
	mux.HandleFunc("POST /api/traffic/track", s.handleTrackTraffic)

	// Protected routes wrapper
	protected := http.NewServeMux()
	protected.HandleFunc("GET /admin", s.handleAdminDashboard)
	protected.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/admin", http.StatusSeeOther)
			return
		}
		http.NotFound(w, r)
	})

	protected.HandleFunc("GET /api/sites", s.handleListSites)
	protected.HandleFunc("POST /api/sites/upload", s.handleUploadSite)
	protected.HandleFunc("POST /api/sites/{domain}/ssl", s.handleIssueSSL)
	protected.HandleFunc("DELETE /api/sites/{domain}", s.handleDeleteSite)

	protected.HandleFunc("GET /api/server/status", s.handleServerStatus)
	protected.HandleFunc("POST /api/server/switch", s.handleServerSwitch)
	protected.HandleFunc("POST /api/server/reload", s.handleServerReload)

	protected.HandleFunc("GET /api/certificates", s.handleListCertificates)
	protected.HandleFunc("GET /api/logs", s.handleListLogs)

	// System metrics & monitoring
	protected.HandleFunc("GET /api/metrics/system", s.handleSystemMetrics)
	protected.HandleFunc("GET /api/system/network", s.handleSystemNetwork)

	// Site Health & Domain validation
	protected.HandleFunc("GET /api/sites/health", s.handleSiteHealth)

	// Traffic analytics & Peak hours
	protected.HandleFunc("GET /api/sites/traffic", s.handleSiteTraffic)

	// Snapshot / Site Image management
	protected.HandleFunc("GET /api/snapshots", s.handleListSnapshots)
	protected.HandleFunc("POST /api/snapshots/create", s.handleCreateSnapshot)
	protected.HandleFunc("POST /api/snapshots/upload", s.handleUploadVersionSnapshot)
	protected.HandleFunc("POST /api/snapshots/publish", s.handlePublishSnapshot)
	protected.HandleFunc("DELETE /api/snapshots/{id}", s.handleDeleteSnapshot)

	// File Manager & Code Editor
	protected.HandleFunc("GET /api/files", s.handleListFiles)
	protected.HandleFunc("GET /api/files/content", s.handleReadFileContent)
	protected.HandleFunc("POST /api/files/content", s.handleSaveFileContent)
	protected.HandleFunc("POST /api/files/create", s.handleCreateFileEntry)
	protected.HandleFunc("POST /api/files/rename", s.handleRenameFileEntry)
	protected.HandleFunc("DELETE /api/files", s.handleDeleteFileEntry)
	protected.HandleFunc("POST /api/files/upload", s.handleUploadSingleFile)
	protected.HandleFunc("GET /api/files/download", s.handleDownloadFile)

	// Wrap protected routes with auth middleware
	mux.Handle("/", s.authSvc.Middleware(protected))

	return mux
}

func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = s.templates.ExecuteTemplate(w, "login.html", nil)
}

func (s *Server) handleLoginAPI(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid payload"})
		return
	}

	session, err := s.authSvc.Authenticate(req.Username, req.Password)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "session_token",
		Value:    session.Token,
		Path:     "/",
		Expires:  session.ExpiresAt,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	writeJSON(w, http.StatusOK, map[string]any{"status": "authenticated", "token": session.Token})
}

func (s *Server) handleLogoutAPI(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("session_token")
	if err == nil && cookie.Value != "" {
		_ = s.authSvc.DestroySession(cookie.Value)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "session_token",
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
	})

	writeJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

func (s *Server) handleAdminDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = s.templates.ExecuteTemplate(w, "index.html", nil)
}

func (s *Server) handleSystemMetrics(w http.ResponseWriter, r *http.Request) {
	stats := s.metricsMonitor.Collect(s.sitesBaseDir)
	writeJSON(w, http.StatusOK, stats)
}

func (s *Server) handleSiteHealth(w http.ResponseWriter, r *http.Request) {
	domain := r.URL.Query().Get("domain")
	if domain != "" {
		site, err := s.db.GetSite(domain)
		sslEnabled := true
		if err == nil && site != nil {
			sslEnabled = site.SSLEnabled
		}
		res := s.healthChecker.CheckSite(domain, sslEnabled)
		writeJSON(w, http.StatusOK, res)
		return
	}

	sites, err := s.db.ListSites()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	results := make([]health.SiteHealth, 0, len(sites))
	for _, st := range sites {
		results = append(results, s.healthChecker.CheckSite(st.Domain, st.SSLEnabled))
	}
	writeJSON(w, http.StatusOK, results)
}

func (s *Server) handleSiteTraffic(w http.ResponseWriter, r *http.Request) {
	domain := r.URL.Query().Get("domain")
	if domain == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "domain parameter required"})
		return
	}

	summary, err := s.db.GetTrafficSummary(domain)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, summary)
}

func (s *Server) handleTrackTraffic(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Domain     string `json:"domain"`
		Path       string `json:"path"`
		UserAgent  string `json:"user_agent"`
		StatusCode int    `json:"status_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid payload"})
		return
	}

	ip := r.RemoteAddr
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		ip = strings.Split(fwd, ",")[0]
	}
	if colon := strings.LastIndex(ip, ":"); colon != -1 {
		ip = ip[:colon]
	}

	if req.StatusCode == 0 {
		req.StatusCode = 200
	}
	if req.Path == "" {
		req.Path = "/"
	}

	_ = s.db.RecordTrafficHit(req.Domain, ip, req.Path, req.UserAgent, req.StatusCode)
	writeJSON(w, http.StatusOK, map[string]string{"status": "recorded"})
}

type SiteResponse struct {
	storage.Site
	ActiveVersion string `json:"active_version"`
}

func (s *Server) handleListSites(w http.ResponseWriter, r *http.Request) {
	sites, err := s.db.ListSites()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	res := make([]SiteResponse, 0, len(sites))
	for _, st := range sites {
		item := SiteResponse{Site: st, ActiveVersion: "live/baseline"}
		if snap, _ := s.db.GetActiveSnapshot(st.Domain); snap != nil {
			item.ActiveVersion = snap.Name
		}
		res = append(res, item)
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleUploadSite(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(50 * 1024 * 1024); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "payload too large or invalid multipart form"})
		return
	}

	domain := strings.TrimSpace(r.FormValue("domain"))
	if valid, errMsg := health.ValidateDomain(domain); !valid {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid domain: %s", errMsg)})
		return
	}

	sslEnabled := r.FormValue("ssl_enabled") == "true"

	file, _, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing zip file"})
		return
	}
	defer file.Close()

	tempFile, err := os.CreateTemp("", "upload-*.zip")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to allocate temporary file"})
		return
	}
	tempZipPath := tempFile.Name()
	defer os.Remove(tempZipPath)

	if _, err := io.Copy(tempFile, file); err != nil {
		tempFile.Close()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save upload"})
		return
	}
	tempFile.Close()

	siteRoot := filepath.Join(s.sitesBaseDir, domain)
	if err := os.MkdirAll(siteRoot, 0755); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create site root"})
		return
	}

	if err := deploy.ExtractZip(tempZipPath, siteRoot); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("extraction rejected: %v", err)})
		return
	}

	site, err := s.orchestrator.DeploySite(domain, siteRoot, sslEnabled)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("adapter failure: %v", err)})
		return
	}

	// Auto-create initial snapshot
	initialSnapName := fmt.Sprintf("v1-%s", time.Now().Format("20060102-150405"))
	_, _ = s.snapshotMgr.CreateSnapshot(domain, initialSnapName, siteRoot)

	// Record initial deployment traffic event
	_ = s.db.RecordTrafficHit(domain, "127.0.0.1", "/", "Deployment Provisioner", 200)

	writeJSON(w, http.StatusOK, site)
}

func (s *Server) handleDeleteSite(w http.ResponseWriter, r *http.Request) {
	domain := r.PathValue("domain")
	if domain == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "domain is required"})
		return
	}

	if err := s.orchestrator.RemoveSite(domain); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "domain": domain})
}

func (s *Server) handleServerStatus(w http.ResponseWriter, r *http.Request) {
	active := s.orchestrator.GetActiveServerName()
	state := s.orchestrator.GetCurrentState()

	writeJSON(w, http.StatusOK, map[string]any{
		"active_server": active,
		"state":         string(state),
	})
}

func (s *Server) handleIssueSSL(w http.ResponseWriter, r *http.Request) {
	domain := r.PathValue("domain")
	if domain == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "domain is required"})
		return
	}

	if err := s.orchestrator.ProvisionSSL(domain); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("SSL issuance failed: %v", err)})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "issued", "domain": domain})
}

func (s *Server) handleServerReload(w http.ResponseWriter, r *http.Request) {
	if err := s.orchestrator.ReloadServer(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "reloaded", "engine": "nginx"})
}

func (s *Server) handleServerSwitch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Target string `json:"target"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	target := strings.ToLower(strings.TrimSpace(req.Target))
	if target != "nginx" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "engine is dedicated to nginx"})
		return
	}

	if err := s.orchestrator.SwitchServer(target); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":        "active",
		"active_server": "nginx",
	})
}

func (s *Server) handleListCertificates(w http.ResponseWriter, r *http.Request) {
	certs, err := s.db.ListCertificates()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if certs == nil {
		certs = []storage.Certificate{}
	}
	writeJSON(w, http.StatusOK, certs)
}

func (s *Server) handleListLogs(w http.ResponseWriter, r *http.Request) {
	logs, err := s.db.ListSwitchLogs(50)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if logs == nil {
		logs = []storage.SwitchLog{}
	}
	writeJSON(w, http.StatusOK, logs)
}

// Snapshot handlers
func (s *Server) handleListSnapshots(w http.ResponseWriter, r *http.Request) {
	domain := r.URL.Query().Get("domain")
	snaps, err := s.db.ListSnapshots(domain)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if snaps == nil {
		snaps = []storage.SiteSnapshot{}
	}
	writeJSON(w, http.StatusOK, snaps)
}

func (s *Server) handleCreateSnapshot(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Domain string `json:"domain"`
		Name   string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid payload"})
		return
	}

	site, err := s.db.GetSite(req.Domain)
	if err != nil || site == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "site not found"})
		return
	}

	snap, err := s.snapshotMgr.CreateSnapshot(req.Domain, req.Name, site.RootPath)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, snap)
}

func (s *Server) handlePublishSnapshot(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Domain     string `json:"domain"`
		SnapshotID int64  `json:"snapshot_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid payload"})
		return
	}

	site, err := s.db.GetSite(req.Domain)
	if err != nil || site == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "site not found"})
		return
	}

	if err := s.snapshotMgr.PublishSnapshot(req.Domain, req.SnapshotID, site.RootPath); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "published", "domain": req.Domain})
}

func (s *Server) handleSystemNetwork(w http.ResponseWriter, r *http.Request) {
	info := s.ipResolver.GetNetworkInfo()
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) handleUploadVersionSnapshot(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(50 * 1024 * 1024); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "payload too large or invalid form"})
		return
	}

	domain := strings.TrimSpace(r.FormValue("domain"))
	versionName := strings.TrimSpace(r.FormValue("version_name"))
	makeLive := r.FormValue("make_live") == "true"

	if domain == "" || versionName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "domain and version_name are required"})
		return
	}

	site, err := s.db.GetSite(domain)
	if err != nil || site == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "site not found"})
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing zip archive file"})
		return
	}
	defer file.Close()

	tempFile, err := os.CreateTemp("", "version-*.zip")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to allocate temp file"})
		return
	}
	tempZipPath := tempFile.Name()
	defer os.Remove(tempZipPath)

	if _, err := io.Copy(tempFile, file); err != nil {
		tempFile.Close()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save upload"})
		return
	}
	tempFile.Close()

	snap, err := s.snapshotMgr.UploadVersionArchive(domain, versionName, tempZipPath, makeLive, site.RootPath, site.SSLEnabled)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, snap)
}

func (s *Server) handleDeleteSnapshot(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid snapshot id"})
		return
	}

	if err := s.snapshotMgr.DeleteSnapshot(id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// File Manager handlers
func (s *Server) getSiteRoot(domain string) (string, error) {
	site, err := s.db.GetSite(domain)
	if err != nil || site == nil {
		return "", fmt.Errorf("site %s not found", domain)
	}
	return site.RootPath, nil
}

func (s *Server) handleListFiles(w http.ResponseWriter, r *http.Request) {
	domain := r.URL.Query().Get("domain")
	subPath := r.URL.Query().Get("path")

	root, err := s.getSiteRoot(domain)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	files, err := deploy.ListDirectory(root, subPath)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, files)
}

func (s *Server) handleReadFileContent(w http.ResponseWriter, r *http.Request) {
	domain := r.URL.Query().Get("domain")
	filePath := r.URL.Query().Get("path")

	root, err := s.getSiteRoot(domain)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	content, err := deploy.ReadFileContent(root, filePath)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"path": filePath, "content": content})
}

func (s *Server) handleSaveFileContent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Domain  string `json:"domain"`
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid payload"})
		return
	}

	root, err := s.getSiteRoot(req.Domain)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	if err := deploy.WriteFileContent(root, req.Path, req.Content); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "saved", "path": req.Path})
}

func (s *Server) handleCreateFileEntry(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Domain string `json:"domain"`
		Path   string `json:"path"`
		IsDir  bool   `json:"is_dir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid payload"})
		return
	}

	root, err := s.getSiteRoot(req.Domain)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	if err := deploy.CreateEntry(root, req.Path, req.IsDir); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "created", "path": req.Path})
}

func (s *Server) handleRenameFileEntry(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Domain  string `json:"domain"`
		OldPath string `json:"old_path"`
		NewPath string `json:"new_path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid payload"})
		return
	}

	root, err := s.getSiteRoot(req.Domain)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	if err := deploy.RenameEntry(root, req.OldPath, req.NewPath); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "renamed", "new_path": req.NewPath})
}

func (s *Server) handleDeleteFileEntry(w http.ResponseWriter, r *http.Request) {
	domain := r.URL.Query().Get("domain")
	subPath := r.URL.Query().Get("path")

	root, err := s.getSiteRoot(domain)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	if err := deploy.DeleteEntry(root, subPath); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "path": subPath})
}

func (s *Server) handleUploadSingleFile(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(32 * 1024 * 1024); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "upload too large"})
		return
	}

	domain := r.FormValue("domain")
	destSubPath := r.FormValue("path")

	root, err := s.getSiteRoot(domain)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing file"})
		return
	}
	defer file.Close()

	targetFile := filepath.Join(destSubPath, header.Filename)
	safePath, err := deploy.ResolveSafePath(root, targetFile)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	out, err := os.OpenFile(safePath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to write file"})
		return
	}
	defer out.Close()

	if _, err := io.Copy(out, file); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save file"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "uploaded", "path": targetFile})
}

func (s *Server) handleDownloadFile(w http.ResponseWriter, r *http.Request) {
	domain := r.URL.Query().Get("domain")
	subPath := r.URL.Query().Get("path")

	root, err := s.getSiteRoot(domain)
	if err != nil {
		http.Error(w, "site not found", http.StatusNotFound)
		return
	}

	safePath, err := deploy.ResolveSafePath(root, subPath)
	if err != nil {
		http.Error(w, "access denied", http.StatusForbidden)
		return
	}

	info, err := os.Stat(safePath)
	if err != nil || info.IsDir() {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(safePath)))
	http.ServeFile(w, r, safePath)
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
