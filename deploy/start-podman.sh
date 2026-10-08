#!/bin/bash
set -euo pipefail
export PATH="/usr/local/bin:$PATH"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
set -a
# shellcheck disable=SC1091
source .env
set +a
cd deploy

LISTEN="${STEVIE_LISTEN_PORT:-${STEVIE_HTTP_PORT:-28413}}"

if podman pod exists stevie 2>/dev/null; then
  # Old podman-compose always tries `pod create` / `run` and fails when names
  # already exist. Prefer starting the existing stack.
  echo "Pod stevie already exists — starting containers..."
  podman start stevie_web_1 stevie_postgres_1 stevie_redis_1 stevie_server_1 stevie_caddy_1 2>/dev/null || true
  podman pod start stevie 2>/dev/null || true
else
  podman-compose -f docker-compose.yml -p stevie up -d
fi

# Old podman-compose often leaves ${TMDB_API_KEY:-} unexpanded.
TMDB_VAL="$(podman exec stevie_server_1 printenv TMDB_API_KEY 2>/dev/null || true)"
if [[ "$TMDB_VAL" == *'${'* ]] || [[ "$TMDB_VAL" == *'$'* ]] || [[ -z "$TMDB_VAL" && -n "${TMDB_API_KEY:-}" ]]; then
  echo "Recreating server with expanded TMDB_API_KEY..."
  KEY="${TMDB_API_KEY}"
  # Remove leaf (caddy) before server — dependency order matters with --requires.
  podman rm -f stevie_caddy_1
  sleep 1
  podman rm -f stevie_server_1
  sleep 1
  podman run --name=stevie_server_1 -d --pod=stevie \
    --requires=stevie_postgres_1,stevie_redis_1 \
    --label io.podman.compose.project=stevie \
    --label com.docker.compose.service=server \
    -e DATABASE_URL="postgres://stevie:${POSTGRES_PASSWORD}@postgres:5432/stevie?sslmode=disable" \
    -e REDIS_URL="redis://redis:6379/0" \
    -e STEVIE_DATA_DIR=/data \
    -e STEVIE_DOMAIN="${STEVIE_DOMAIN}" \
    -e STEVIE_HTTPS_PORT="${STEVIE_HTTPS_PORT}" \
    -e STEVIE_ADMIN_USER="${STEVIE_ADMIN_USER}" \
    -e STEVIE_ADMIN_PASSWORD="${STEVIE_ADMIN_PASSWORD}" \
    -e STEVIE_SECURE_COOKIES="${STEVIE_SECURE_COOKIES}" \
    -e TMDB_API_KEY="${KEY}" \
    -e TMDB_RPS="${TMDB_RPS}" \
    -e MEDIA_LIBRARY_PATH="${MEDIA_LIBRARY_PATH}" \
    -e STEVIE_MEDIA_MOUNT=/media/library \
    -e LIVE_SOURCES_PATH="${LIVE_SOURCES_PATH}" \
    -e STEVIE_LIVE_MOUNT=/live-sources \
    -e STEVIE_RECORDINGS_PATH="${STEVIE_RECORDINGS_PATH:-/data/stevie/recordings}" \
    -e STEVIE_RECORDINGS_MOUNT=/recordings \
    -e XTREAM_URL="${XTREAM_URL:-}" \
    -e XTREAM_USER="${XTREAM_USER:-}" \
    -e XTREAM_PASSWORD="${XTREAM_PASSWORD:-}" \
    -e THESPORTSDB_API_KEY="${THESPORTSDB_API_KEY:-123}" \
    -e THESPORTSDB_COUNTRIES="${THESPORTSDB_COUNTRIES:-Canada,United_States}" \
    -v stevie_stevie-data:/data \
    -v "${MEDIA_LIBRARY_PATH}:/media/library:ro" \
    -v "${LIVE_SOURCES_PATH}:/live-sources:ro" \
    -v "${STEVIE_RECORDINGS_PATH:-/data/stevie/recordings}:/recordings" \
    --restart unless-stopped --init \
    stevie_server
  podman run --name=stevie_caddy_1 -d --pod=stevie \
    --requires=stevie_server_1,stevie_web_1 \
    --label io.podman.compose.project=stevie \
    --label com.docker.compose.service=caddy \
    -e STEVIE_DOMAIN="${STEVIE_DOMAIN}" \
    -e STEVIE_HTTPS_PORT="${STEVIE_HTTPS_PORT}" \
    -e TLS_MODE="${TLS_MODE}" \
    -e STEVIE_LISTEN_PORT="${LISTEN}" \
    -e STEVIE_HTTP_PORT="${LISTEN}" \
    -v "${ROOT}/deploy/caddy-entrypoint.sh:/entrypoint.sh:ro" \
    -v stevie_caddy-data:/data \
    -v stevie_caddy-config:/config \
    -v "${ROOT}/deploy/certs:/certs:ro" \
    --restart unless-stopped \
    --entrypoint /bin/sh \
    docker.io/library/caddy:2.9-alpine \
    /entrypoint.sh
fi

podman ps --filter name=stevie --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}"
