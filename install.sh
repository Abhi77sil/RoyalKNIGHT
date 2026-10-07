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
echo "[1/5] Setting up system directories..."
mkdir -p /var/lib/royalknight/snapshots
mkdir -p /var/www
mkdir -p /etc/ssl/royalknight/certs
mkdir -p /etc/ssl/royalknight/private
chmod 700 /etc/ssl/royalknight/private

# 4. Binary Installation
echo "[2/5] Installing binary to /usr/local/bin/royalknight..."
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [ -f "$SCRIPT_DIR/bin/royalknight" ]; then
  cp "$SCRIPT_DIR/bin/royalknight" /usr/local/bin/royalknight
elif command -v go >/dev/null 2>&1; then
  echo "Compiling static binary..."
  (cd "$SCRIPT_DIR" && CGO_ENABLED=0 go build -ldflags="-s -w" -o /usr/local/bin/royalknight cmd/server/main.go)
else
  echo "Error: Compiled binary not found at $SCRIPT_DIR/bin/royalknight and go is not installed." >&2
  exit 1
fi

chmod 755 /usr/local/bin/royalknight

# 5. Initialize Database with User Credentials
echo "[3/5] Initializing database and administrator account..."
/usr/local/bin/royalknight \
  -db /var/lib/royalknight/panel.db \
  -sites-dir /var/www \
  -snapshots-dir /var/lib/royalknight/snapshots \
  -admin-user "$ADMIN_USER" \
  -admin-pass "$ADMIN_PASS" \
  -init-admin

# 6. Install Systemd Service
echo "[4/5] Installing systemd service..."
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

# 7. Enable and Start Service
echo "[5/5] Enabling and starting royalknight service..."
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
