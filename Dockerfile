FROM alpine:latest

# Install Caddy, Nginx, Curl, Bash, and CA certificates
RUN apk add --no-cache \
    caddy \
    nginx \
    curl \
    bash \
    ca-certificates

# Install systemctl shim for service operations inside container
COPY docker/systemctl-shim.sh /usr/local/bin/systemctl
RUN chmod +x /usr/local/bin/systemctl

# Setup Nginx configuration
COPY docker/nginx.conf /etc/nginx/nginx.conf

# Setup initial Caddy configuration
RUN mkdir -p /etc/caddy && \
    printf "{\n    admin 127.0.0.1:2019\n}\n" > /etc/caddy/Caddyfile

# Copy static RoyalKnight binary
COPY bin/royalknight /usr/local/bin/royalknight
RUN chmod +x /usr/local/bin/royalknight

# Copy entrypoint
COPY docker/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

# Expose panel and web traffic ports
EXPOSE 7777 80 443 2019

ENTRYPOINT ["/entrypoint.sh"]
