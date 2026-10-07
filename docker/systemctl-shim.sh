#!/usr/bin/env bash
set -e

ACTION="${1:-}"
SERVICE="${2:-}"

case "$SERVICE" in
  caddy)
    case "$ACTION" in
      start)
        if ! pgrep -f "caddy run" >/dev/null 2>&1; then
          nohup caddy run --config /etc/caddy/Caddyfile >/var/log/caddy.log 2>&1 &
          sleep 0.5
        fi
        ;;
      stop)
        pkill -f "caddy run" >/dev/null 2>&1 || true
        pkill -x caddy >/dev/null 2>&1 || true
        ;;
      reload)
        caddy reload --config /etc/caddy/Caddyfile >/dev/null 2>&1 || true
        ;;
      restart)
        pkill -f "caddy run" >/dev/null 2>&1 || true
        sleep 0.3
        nohup caddy run --config /etc/caddy/Caddyfile >/var/log/caddy.log 2>&1 &
        sleep 0.5
        ;;
      is-active)
        pgrep -f "caddy run" >/dev/null 2>&1
        ;;
      *)
        echo "Unsupported action: $ACTION" >&2
        exit 1
        ;;
    esac
    ;;
  nginx)
    case "$ACTION" in
      start)
        if ! pgrep -x nginx >/dev/null 2>&1; then
          nginx
        fi
        ;;
      stop)
        nginx -s stop >/dev/null 2>&1 || pkill -x nginx >/dev/null 2>&1 || true
        ;;
      reload)
        nginx -s reload
        ;;
      restart)
        nginx -s stop >/dev/null 2>&1 || pkill -x nginx >/dev/null 2>&1 || true
        sleep 0.3
        nginx
        ;;
      is-active)
        pgrep -x nginx >/dev/null 2>&1
        ;;
      *)
        echo "Unsupported action: $ACTION" >&2
        exit 1
        ;;
    esac
    ;;
  *)
    echo "Unknown service: $SERVICE" >&2
    exit 1
    ;;
esac
