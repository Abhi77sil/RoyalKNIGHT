#!/usr/bin/env bash
set -e

ADMIN_USER="${ADMIN_USER:-admin}"
ADMIN_PASS="${ADMIN_PASS:-admin123456}"
PORT="${PORT:-7777}"

mkdir -p /var/lib/royalknight/snapshots
mkdir -p /var/www
mkdir -p /etc/ssl/royalknight/certs
mkdir -p /etc/ssl/royalknight/private
mkdir -p /etc/nginx/sites-available
mkdir -p /etc/nginx/sites-enabled
chmod 700 /etc/ssl/royalknight/private

# Start default Caddy server in background
systemctl start caddy

echo "============================================="
echo "   RoyalKnight Container Initialized         "
echo "============================================="
echo "Panel URL: http://0.0.0.0:${PORT}/admin"
echo "Username:  ${ADMIN_USER}"
echo "Password:  ${ADMIN_PASS}"
echo "============================================="

exec /usr/local/bin/royalknight \
  -port "${PORT}" \
  -db /var/lib/royalknight/panel.db \
  -sites-dir /var/www \
  -snapshots-dir /var/lib/royalknight/snapshots \
  -nginx-dir /etc/nginx \
  -ssl-dir /etc/ssl/royalknight \
  -admin-user "${ADMIN_USER}" \
  -admin-pass "${ADMIN_PASS}"
