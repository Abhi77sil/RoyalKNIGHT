#!/usr/bin/env bash
set -euo pipefail

# RoyalKnight Control Panel - Self-Sufficient Installer
# Target: Linux VPS (systemd-based) - Nginx Dedicated Engine

echo "============================================="
echo "   RoyalKnight Control Panel Installation    "
echo "============================================="

if [ "$(id -u)" -ne 0 ]; then
  echo "Error: This installation script must be run as root (or with sudo)." >&2
  exit 1
fi

# 1. Credentials Configuration (Supports Environment Variables or Prompts)
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

# 2. Detect Server IP Address
SERVER_IP=$(curl -s --max-time 3 https://api.ipify.org || curl -s --max-time 3 https://ifconfig.me || hostname -I | awk '{print $1}')
SERVER_IP="${SERVER_IP:-127.0.0.1}"

# 3. Take Full Control: Stop & Clear Any Older/Competing Services
echo "[1/7] Taking control: Stopping old services and conflicting processes..."

# Stop systemd services that conflict with Nginx / RoyalKnight
systemctl stop royalknight.service 2>/dev/null || true
systemctl stop caddy.service 2>/dev/null || true
systemctl disable caddy.service 2>/dev/null || true
systemctl stop apache2.service 2>/dev/null || true
systemctl disable apache2.service 2>/dev/null || true
systemctl stop httpd.service 2>/dev/null || true
systemctl disable httpd.service 2>/dev/null || true
systemctl stop lighttpd.service 2>/dev/null || true
systemctl disable lighttpd.service 2>/dev/null || true
systemctl stop nginx.service 2>/dev/null || true

# Stop Docker containers binding to panel or web ports if Docker exists
if command -v docker >/dev/null 2>&1; then
  docker stop royalknight-panel 2>/dev/null || true
  docker rm -f royalknight-panel 2>/dev/null || true
  for cid in $(docker ps -q --filter "publish=80" --filter "publish=443" --filter "publish=7777" 2>/dev/null); do
    echo "Stopping conflicting Docker container: $cid"
    docker stop "$cid" 2>/dev/null || true
  done
fi

# Kill any rogue running processes directly
pkill -9 -x royalknight 2>/dev/null || true
pkill -9 -x caddy 2>/dev/null || true
pkill -9 -x apache2 2>/dev/null || true
pkill -9 -x httpd 2>/dev/null || true

# Force-free ports 80, 443, and 7777
free_tcp_port() {
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

free_tcp_port 80
free_tcp_port 443
free_tcp_port 7777

# Clean up obsolete Caddy installation traces
rm -f /usr/local/bin/caddy
rm -rf /etc/caddy
rm -f /etc/systemd/system/caddy.service
systemctl daemon-reload 2>/dev/null || true

# 4. Install Dependencies
echo "[2/7] Installing core system packages (Nginx, Certbot, utilities)..."
if command -v apt-get >/dev/null 2>&1; then
  export DEBIAN_FRONTEND=noninteractive
  apt-get update -qq
  apt-get install -y -qq nginx certbot python3-certbot-nginx psmisc lsof curl ca-certificates tar
elif command -v dnf >/dev/null 2>&1; then
  dnf install -y -q nginx certbot python3-certbot-nginx psmisc lsof curl ca-certificates tar
elif command -v yum >/dev/null 2>&1; then
  yum install -y -q epel-release || true
  yum install -y -q nginx certbot python3-certbot-nginx psmisc lsof curl ca-certificates tar
fi

# Ensure port killing utilities are used after package installation
free_tcp_port 80
free_tcp_port 443
free_tcp_port 7777

# 5. Setup System Directories & Permissions
echo "[3/7] Setting up system directories and permissions..."
mkdir -p /var/lib/royalknight/snapshots
mkdir -p /var/www/certbot
chmod 755 /var/www/certbot
mkdir -p /etc/ssl/royalknight/certs
mkdir -p /etc/ssl/royalknight/private
chmod 700 /etc/ssl/royalknight/private
mkdir -p /var/www
chmod 755 /var/www

# Configure Nginx for low-RAM performance and clean out conflicting configs
mkdir -p /etc/nginx/sites-available /etc/nginx/sites-enabled
rm -f /etc/nginx/sites-enabled/default
rm -f /etc/nginx/conf.d/default.conf
find /etc/nginx/sites-enabled/ -xtype l -delete 2>/dev/null || true

# Open firewall ports if UFW is active
if command -v ufw >/dev/null 2>&1 && ufw status | grep -q "Status: active"; then
  echo "Opening ports 80, 443, 7777 in UFW firewall..."
  ufw allow 80/tcp >/dev/null 2>&1 || true
  ufw allow 443/tcp >/dev/null 2>&1 || true
  ufw allow 7777/tcp >/dev/null 2>&1 || true
fi

# Test and start Nginx
echo "[4/7] Testing and starting Nginx engine..."
nginx -t
systemctl enable nginx.service
systemctl restart nginx.service

# 6. Binary Installation
echo "[5/7] Installing RoyalKnight binary to /usr/local/bin/royalknight..."
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Ensure no lock on target binary
pkill -9 -x royalknight 2>/dev/null || true

if [ -f "$SCRIPT_DIR/bin/royalknight" ]; then
  cp "$SCRIPT_DIR/bin/royalknight" /usr/local/bin/royalknight.new
  mv -f /usr/local/bin/royalknight.new /usr/local/bin/royalknight
else
  if ! command -v go >/dev/null 2>&1; then
    echo "Go compiler not found. Automatically installing Go..."
    if command -v apt-get >/dev/null 2>&1; then
      export DEBIAN_FRONTEND=noninteractive
      apt-get update -y && apt-get install -y golang-go git curl || true
    elif command -v dnf >/dev/null 2>&1; then
      dnf install -y golang git curl || true
    elif command -v yum >/dev/null 2>&1; then
      yum install -y golang git curl || true
    elif command -v apk >/dev/null 2>&1; then
      apk add --no-cache go git curl || true
    fi
  fi

  if command -v go >/dev/null 2>&1; then
    echo "Compiling static binary..."
    (cd "$SCRIPT_DIR" && CGO_ENABLED=0 go build -ldflags="-s -w" -o /usr/local/bin/royalknight cmd/server/main.go)
  else
    echo "Error: Precompiled binary bin/royalknight not found and Go is not available." >&2
    exit 1
  fi
fi

chmod 755 /usr/local/bin/royalknight

# 7. Initialize Database with User Credentials
echo "[6/7] Initializing database and administrator account..."
/usr/local/bin/royalknight \
  -db /var/lib/royalknight/panel.db \
  -sites-dir /var/www \
  -snapshots-dir /var/lib/royalknight/snapshots \
  -admin-user "$ADMIN_USER" \
  -admin-pass "$ADMIN_PASS" \
  -init-admin

# 8. Install Systemd Service & Launch
echo "[7/7] Installing systemd service and starting RoyalKnight..."
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
ExecStart=/usr/local/bin/royalknight -port 7777 -db /var/lib/royalknight/panel.db -sites-dir /var/www -snapshots-dir /var/lib/royalknight/snapshots -nginx-dir /etc/nginx -ssl-dir /etc/ssl/royalknight
Restart=always
RestartSec=5s
LimitNOFILE=65535
AmbientCapabilities=CAP_NET_BIND_SERVICE CAP_SYS_ADMIN
NoNewPrivileges=false

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable royalknight.service
systemctl restart royalknight.service

# Verify services are healthy
sleep 1
if ! systemctl is-active --quiet royalknight.service; then
  echo "Warning: royalknight service failed to start. Journal log:"
  journalctl -u royalknight.service -n 20 --no-pager || true
fi

echo ""
echo "============================================="
echo "   Installation Completed Successfully       "
echo "============================================="
echo "Panel URL: http://${SERVER_IP}:7777/admin"
echo "Username:  ${ADMIN_USER}"
echo "Password:  (hidden as configured)"
echo "Engine:    Nginx (Active on port 80/443)"
echo "Status:    systemctl status royalknight"
echo "============================================="
