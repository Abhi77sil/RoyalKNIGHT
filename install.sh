#!/usr/bin/env bash
set -euo pipefail

# RoyalKnight Control Panel - Installer
# Target: Linux VPS (systemd-based)

echo "============================================="
echo "   RoyalKnight Control Panel Installation    "
echo "============================================="

if [ "$(id -u)" -ne 0 ]; then
  echo "Error: This installation script must be run as root (or with sudo)." >&2
  exit 1
fi

# 1. Interactive Credentials Configuration
echo ""
read -r -p "Enter administrator username [admin]: " ADMIN_USER
ADMIN_USER="${ADMIN_USER:-admin}"

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

# 2. Detect Server IP Address
SERVER_IP=$(curl -s --max-time 3 https://api.ipify.org || curl -s --max-time 3 https://ifconfig.me || hostname -I | awk '{print $1}')
SERVER_IP="${SERVER_IP:-127.0.0.1}"

# 3. Create Required System Directories
echo "[1/6] Setting up system directories..."
mkdir -p /var/lib/royalknight/snapshots
mkdir -p /var/www
mkdir -p /etc/ssl/royalknight/certs
mkdir -p /etc/ssl/royalknight/private
chmod 700 /etc/ssl/royalknight/private

# 4. Install Web Server Engines (Nginx & Caddy)
echo "[2/6] Installing and optimizing web servers (Nginx & Caddy)..."
if command -v apt-get >/dev/null 2>&1; then
  export DEBIAN_FRONTEND=noninteractive
  apt-get update -qq
  apt-get install -y -qq nginx curl ca-certificates tar

  if ! command -v caddy >/dev/null 2>&1; then
    echo "Installing Caddy repository..."
    apt-get install -y -qq debian-keyring debian-archive-keyring apt-transport-https || true
    curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg 2>/dev/null || true
    curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | tee /etc/apt/sources.list.d/caddy-stable.list >/dev/null 2>&1 || true
    apt-get update -qq && apt-get install -y -qq caddy || true
  fi
fi

# Fallback: install static Caddy binary if package repository not available
if ! command -v caddy >/dev/null 2>&1; then
  echo "Installing static Caddy binary..."
  curl -fsSL "https://github.com/caddyserver/caddy/releases/download/v2.8.4/caddy_2.8.4_linux_amd64.tar.gz" -o /tmp/caddy.tar.gz 2>/dev/null && \
    tar -xzf /tmp/caddy.tar.gz -C /usr/local/bin caddy && \
    chmod +x /usr/local/bin/caddy && \
    rm -f /tmp/caddy.tar.gz || true
fi

# Configure Nginx for low-RAM server optimization
mkdir -p /etc/nginx/sites-available /etc/nginx/sites-enabled
rm -f /etc/nginx/sites-enabled/default

# Configure Caddy for low-RAM server optimization
mkdir -p /etc/caddy
if [ ! -f /etc/caddy/Caddyfile ]; then
  cat << 'EOF' > /etc/caddy/Caddyfile
{
    admin 127.0.0.1:2019
}
EOF
fi

# Ensure Caddy systemd service exists and is enabled
if ! systemctl list-unit-files 2>/dev/null | grep -q "caddy.service"; then
  cat << 'EOF' > /etc/systemd/system/caddy.service
[Unit]
Description=Caddy Web Server
After=network.target network-online.target
Wants=network-online.target

[Service]
Type=notify
User=root
Group=root
ExecStart=/usr/local/bin/caddy run --environ --config /etc/caddy/Caddyfile
ExecReload=/usr/local/bin/caddy reload --config /etc/caddy/Caddyfile --force
TimeoutStopSec=5s
LimitNOFILE=65535
Restart=always

[Install]
WantedBy=multi-user.target
EOF
  systemctl daemon-reload
fi

systemctl enable caddy.service 2>/dev/null || true
systemctl start caddy.service 2>/dev/null || true
systemctl stop nginx.service 2>/dev/null || true

# 5. Binary Installation
echo "[3/6] Installing binary to /usr/local/bin/royalknight..."
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [ -f "$SCRIPT_DIR/bin/royalknight" ]; then
  cp "$SCRIPT_DIR/bin/royalknight" /usr/local/bin/royalknight
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
    echo "Error: Go compiler is not available." >&2
    echo "Please install Go (sudo apt update && sudo apt install -y golang-go) and run ./install.sh again." >&2
    exit 1
  fi
fi

chmod 755 /usr/local/bin/royalknight

# 6. Initialize Database with User Credentials
echo "[4/6] Initializing database and administrator account..."
/usr/local/bin/royalknight \
  -db /var/lib/royalknight/panel.db \
  -sites-dir /var/www \
  -snapshots-dir /var/lib/royalknight/snapshots \
  -admin-user "$ADMIN_USER" \
  -admin-pass "$ADMIN_PASS" \
  -init-admin

# 7. Install Systemd Service
echo "[5/6] Installing systemd service..."
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

# 8. Enable and Start Service
echo "[6/6] Enabling and starting royalknight service..."
systemctl daemon-reload
systemctl enable royalknight.service
systemctl restart royalknight.service

echo ""
echo "============================================="
echo "   Installation Completed Successfully       "
echo "============================================="
echo "Panel URL: http://${SERVER_IP}:7777/admin"
echo "Username:  ${ADMIN_USER}"
echo "Password:  (hidden as configured)"
echo "Service:   systemctl status royalknight"
echo "============================================="
