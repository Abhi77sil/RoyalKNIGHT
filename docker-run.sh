#!/usr/bin/env bash
set -e

ACTION="${1:-up}"
CONTAINER_NAME="royalknight-panel"
IMAGE_NAME="royalknight:latest"

case "$ACTION" in
  up|start)
    if [ "$(docker ps -q -f name=${CONTAINER_NAME})" ]; then
      echo "Container ${CONTAINER_NAME} is already running."
      exit 0
    fi
    docker rm -f "${CONTAINER_NAME}" 2>/dev/null || true
    echo "Starting ${CONTAINER_NAME}..."
    docker run -d \
      --name "${CONTAINER_NAME}" \
      --restart unless-stopped \
      -p 7777:7777 \
      -p 80:80 \
      -p 443:443 \
      -e ADMIN_USER=admin \
      -e ADMIN_PASS=admin123456 \
      -e PORT=7777 \
      -v royalknight_data:/var/lib/royalknight \
      -v royalknight_www:/var/www \
      "${IMAGE_NAME}"
    echo "Panel running at: http://localhost:7777/admin"
    ;;
  down|stop)
    echo "Stopping ${CONTAINER_NAME}..."
    docker stop "${CONTAINER_NAME}" 2>/dev/null || true
    docker rm "${CONTAINER_NAME}" 2>/dev/null || true
    ;;
  logs)
    docker logs -f "${CONTAINER_NAME}"
    ;;
  build)
    echo "Compiling static binary..."
    mkdir -p bin
    CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/royalknight cmd/server/main.go
    echo "Building ${IMAGE_NAME}..."
    docker build -t "${IMAGE_NAME}" .
    ;;
  *)
    echo "Usage: ./docker-run.sh [up|down|logs|build]"
    exit 1
    ;;
esac
