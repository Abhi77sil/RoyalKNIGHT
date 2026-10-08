#!/usr/bin/env bash
set -euo pipefail

# RoyalKnight Control Panel - Self-Sufficient Installer
# Target: Linux VPS (systemd-based) - Dedicated Nginx Engine

echo "============================================="
echo "   RoyalKnight Control Panel Installation    "
echo "============================================="

if [ "$(id -u)" -ne 0 ]; then
  echo "Error: This installation script must be run as root (or with sudo)." >&2
  exit 1
fi

# 1. Credentials Configuration
ADMIN_USER="${ADMIN_USER:-}"
ADMIN_PASS="${ADMIN_PASS:-}"

if [ -z "$ADMIN_USER" ]; then
  read -r -p "Enter administrator username [admin]: " ADMIN_USER
  ADMIN_USER="${ADMIN_USER:-admin}"
fi

if [ -z "$ADMIN_PASS" ]; then
  while true; do
    read -r -s -p "Enter administrator password: " ADMIN_PASS
    echo ""
    if [ -z "$ADMIN_PASS" ]; then
      echo "Password cannot be empty. Please enter a valid password."
      continue
    fi
    read -r -s -p "Confirm administrator password: " ADMIN_PASS_CONFIRM
    echo ""
    if [ "$ADMIN_PASS" != "$ADMIN_PASS_CONFIRM" ]; then
      echo "Passwords do not match. Please try again."
    else
      break
    fi
  done
fi

# 2. Detect Server Public IP
SERVER_IP=$(curl -s --max-time 3 https://api.ipify.org || curl -s --max-time 3 https://ifconfig.me || hostname -I | awk '{print $1}')
SERVER_IP="${SERVER_IP:-127.0.0.1}"

# 3. Kill and Terminate All Competing Web Servers & Older Versions
echo "[1/7] Terminating all competing web servers and older versions..."

# Stop and disable systemd services
systemctl stop royalknight.service 2>/dev/null || true
systemctl stop caddy.service 2>/dev/null || true
systemctl disable caddy.service 2>/dev/null || true
systemctl mask caddy.service 2>/dev/null || true
systemctl stop apache2.service 2>/dev/null || true
systemctl disable apache2.service 2>/dev/null || true
systemctl mask apache2.service 2>/dev/null || true
systemctl stop httpd.service 2>/dev/null || true
systemctl disable httpd.service 2>/dev/null || true
systemctl mask httpd.service 2>/dev/null || true
systemctl stop lighttpd.service 2>/dev/null || true
systemctl disable lighttpd.service 2>/dev/null || true
systemctl mask lighttpd.service 2>/dev/null || true
systemctl stop nginx.service 2>/dev/null || true

# Stop Docker containers holding ports if Docker exists
if command -v docker >/dev/null 2>&1; then
  docker stop royalknight-panel 2>/dev/null || true
  docker rm -f royalknight-panel 2>/dev/null || true
  for cid in $(docker ps -q --filter "publish=80" --filter "publish=443" --filter "publish=7777" --filter "publish=2019" 2>/dev/null); do
    echo "Stopping conflicting container: $cid"
    docker stop "$cid" 2>/dev/null || true
    docker rm -f "$cid" 2>/dev/null || true
  done
fi

# Force kill rogue processes by name and pattern
killall -9 royalknight caddy apache2 httpd lighttpd 2>/dev/null || true
pkill -9 -f royalknight 2>/dev/null || true
pkill -9 -f caddy 2>/dev/null || true
pkill -9 -f apache2 2>/dev/null || true
pkill -9 -f httpd 2>/dev/null || true
pkill -9 -f lighttpd 2>/dev/null || true

# Purge Caddy package from system to prevent port hijacking
if command -v apt-get >/dev/null 2>&1; then
  export DEBIAN_FRONTEND=noninteractive
  apt-get purge -y -qq caddy 2>/dev/null || true
fi

# Force-free TCP ports 80, 443, 7777, 2019
free_port() {
  local port=$1
  if command -v fuser >/dev/null 2>&1; then
    fuser -k -9 "${port}/tcp" 2>/dev/null || true
  fi
  if command -v lsof >/dev/null 2>&1; then
    local pids
    pids=$(lsof -ti :"${port}" 2>/dev/null || true)
    if [ -n "$pids" ]; then
      kill -9 $pids 2>/dev/null || true
    fi
  fi
}

free_port 80
free_port 443
free_port 7777
free_port 2019

# 4. Remove Data & Configurations of Older Versions
echo "[2/7] Purging data of older versions (old databases, configs, certificates)..."
rm -rf /etc/caddy /usr/bin/caddy /usr/local/bin/caddy /var/lib/caddy
rm -f /etc/systemd/system/caddy.service* /usr/lib/systemd/system/caddy.service* /etc/systemd/system/multi-user.target.wants/caddy.service*

# Wipe old SQLite database for a fresh clean state
rm -f /var/lib/royalknight/panel.db /var/lib/royalknight/panel.db-shm /var/lib/royalknight/panel.db-wal

# Wipe old self-signed/staging certs
rm -rf /etc/ssl/royalknight/certs/* /etc/ssl/royalknight/private/*

# Wipe old snapshots
rm -rf /var/lib/royalknight/snapshots/*

# Wipe old virtual hosts to prevent duplicate or conflicting configurations
mkdir -p /etc/nginx/sites-available /etc/nginx/sites-enabled /etc/nginx/conf.d
rm -f /etc/nginx/sites-enabled/* /etc/nginx/sites-available/* /etc/nginx/conf.d/*
rm -rf /var/www/certbot/*

systemctl daemon-reload 2>/dev/null || true

# 5. Install Dependencies (Nginx, Certbot, Utilities)
echo "[3/7] Installing Nginx, Certbot (Let's Encrypt), and diagnostic tools..."
if command -v apt-get >/dev/null 2>&1; then
  apt-get update -qq
  apt-get install -y -qq nginx certbot python3-certbot-nginx psmisc lsof curl ca-certificates tar
elif command -v dnf >/dev/null 2>&1; then
  dnf install -y -q nginx certbot python3-certbot-nginx psmisc lsof curl ca-certificates tar
elif command -v yum >/dev/null 2>&1; then
  yum install -y -q epel-release || true
  yum install -y -q nginx certbot python3-certbot-nginx psmisc lsof curl ca-certificates tar
fi

# Ensure port killing utilities clear any lingering process after package installation
free_port 80
free_port 443
free_port 7777
free_port 2019

# 6. Setup Directory Structures & Nginx ACME Challenge Route
echo "[4/7] Configuring directories and global ACME challenge routing..."
mkdir -p /var/lib/royalknight/snapshots
mkdir -p /var/www/certbot
chmod 755 /var/www
chmod 755 /var/www/certbot
mkdir -p /etc/ssl/royalknight/certs
mkdir -p /etc/ssl/royalknight/private
chmod 700 /etc/ssl/royalknight/private

# Global default server block to immediately handle ACME HTTP-01 challenges on port 80
cat << 'EOF' > /etc/nginx/conf.d/00-acme.conf
server {
    listen 80 default_server;
    listen [::]:80 default_server;
    server_name _;

    location ^~ /.well-known/acme-challenge/ {
        default_type "text/plain";
        root /var/www/certbot;
        allow all;
    }

    location / {
        return 404;
    }
}
EOF

# Open firewall ports if UFW is active
if command -v ufw >/dev/null 2>&1 && ufw status | grep -q "Status: active"; then
  echo "Opening firewall ports 80, 443, 7777 in UFW..."
  ufw allow 80/tcp >/dev/null 2>&1 || true
  ufw allow 443/tcp >/dev/null 2>&1 || true
  ufw allow 7777/tcp >/dev/null 2>&1 || true
fi

# Test and start Nginx
nginx -t
systemctl enable nginx.service
systemctl restart nginx.service

# 7. Install Binary & Initialize Database
echo "[5/7] Installing RoyalKnight binary..."
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

pkill -9 -x royalknight 2>/dev/null || true

if [ -f "$SCRIPT_DIR/bin/royalknight" ]; then
  cp "$SCRIPT_DIR/bin/royalknight" /usr/local/bin/royalknight.new
  mv -f /usr/local/bin/royalknight.new /usr/local/bin/royalknight
else
  if ! command -v go >/dev/null 2>&1; then
    echo "Installing Go compiler..."
    apt-get update -y && apt-get install -y golang-go git curl || true
  fi

  if command -v go >/dev/null 2>&1; then
    echo "Compiling binary from source..."
    (cd "$SCRIPT_DIR" && CGO_ENABLED=0 go build -ldflags="-s -w" -o /usr/local/bin/royalknight cmd/server/main.go)
  else
    echo "Error: Precompiled binary bin/royalknight not found and Go is unavailable." >&2
    exit 1
  fi
fi

chmod 755 /usr/local/bin/royalknight

echo "[6/7] Initializing fresh database and administrator credentials..."
/usr/local/bin/royalknight \
  -db /var/lib/royalknight/panel.db \
  -sites-dir /var/www \
  -snapshots-dir /var/lib/royalknight/snapshots \
  -admin-user "$ADMIN_USER" \
  -admin-pass "$ADMIN_PASS" \
  -init-admin

# 8. Setup & Start Systemd Service
echo "[7/7] Starting RoyalKnight systemd service..."
cat << 'EOF' > /etc/systemd/system/royalknight.service
[Unit]
Description=RoyalKnight Web Hosting Control Panel
After=network.target network-online.target systemd-sysctl.service
Wants=network-online.target

[Service]
Type=simple
User=root
Group=root
WorkingDirectory=/var/lib/royalknight
Environment=PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/snap/bin
ExecStart=/usr/local/bin/royalknight -port 7777 -db /var/lib/royalknight/panel.db -sites-dir /var/www -snapshots-dir /var/lib/royalknight/snapshots -nginx-dir /etc/nginx -ssl-dir /etc/ssl/royalknight
Restart=always
RestartSec=3s
LimitNOFILE=65535
AmbientCapabilities=CAP_NET_BIND_SERVICE CAP_SYS_ADMIN
NoNewPrivileges=false

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable royalknight.service
systemctl restart royalknight.service

sleep 1
if ! systemctl is-active --quiet royalknight.service; then
  echo "Error: royalknight service failed to start. Journal log:" >&2
  journalctl -u royalknight.service -n 25 --no-pager || true
  exit 1
fi

echo ""
echo "============================================="
echo "   Installation Completed Successfully       "
echo "============================================="
echo "Panel URL: http://${SERVER_IP}:7777/admin"
echo "Username:  ${ADMIN_USER}"
echo "Password:  (hidden as configured)"
echo "Engine:    Dedicated Nginx (Ports 80/443)"
echo "Status:    Active and running"
echo "============================================="
