#!/bin/bash
# Rebuild server+web images from host-built artifacts and recreate containers.
set -euo pipefail
export PATH="/usr/local/bin:$PATH"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

# Rootless podman keys off USER/HOME/XDG_*. Agent sandboxes often remap these
# (and a polluted shell may have USER=$XTREAM_USER from .env confusion).
PODMAN_HOME="$(getent passwd "$(id -u)" 2>/dev/null | cut -d: -f6 || true)"
if [[ -z "${PODMAN_HOME}" || ! -d "${PODMAN_HOME}" ]]; then
  PODMAN_HOME="$(cd "$ROOT/.." && pwd)"
fi
fix_podman_env() {
  export HOME="$PODMAN_HOME"
  export USER
  USER="$(id -un)"
  export LOGNAME="$USER"
  unset XDG_CONFIG_HOME XDG_DATA_HOME
  # Prefer a real user runtime dir. Never invent a TMPDIR-based XDG_RUNTIME_DIR —
  # that forks podman's runRoot away from ~/.local/share/containers/run and can
  # leave crun/containers stuck "Stopping".
  if [[ -d "/run/user/$(id -u)" ]]; then
    export XDG_RUNTIME_DIR="/run/user/$(id -u)"
  else
    unset XDG_RUNTIME_DIR
  fi
}
fix_podman_env

set -a
# shellcheck disable=SC1091
source .env
set +a
fix_podman_env

LISTEN="${STEVIE_LISTEN_PORT:-${STEVIE_HTTP_PORT:-28413}}"
RECS="${STEVIE_RECORDINGS_PATH:-/data/stevie/recordings}"

echo "Building server binary…"
(
  cd apps/server
  # Low ulimits: parallel compile forks often hit EAGAIN.
  GOMAXPROCS=1 CGO_ENABLED=0 GOOS=linux go build -p 1 -o "$ROOT/stevie-server.bin" ./cmd/stevie
)

echo "Building web…"
(
  cd apps/web
  npm install --silent
  npm run build
)

echo "Ensuring bundled team logos…"
LOGO_DIR="$ROOT/apps/server/assets/team-logos"
if [[ ! -f "$LOGO_DIR/manifest.json" ]]; then
  (cd "$ROOT/apps/server" && node scripts/fetch-team-logos.mjs)
fi
if [[ ! -f "$LOGO_DIR/leagues.json" ]]; then
  (cd "$ROOT/apps/server" && node scripts/fetch-league-logos.mjs)
fi

echo "Building images…"
mkdir -p /tmp/stevie-server-ctx /tmp/stevie-web-ctx
cp "$ROOT/stevie-server.bin" /tmp/stevie-server-ctx/stevie
rm -rf /tmp/stevie-server-ctx/team-logos
mkdir -p /tmp/stevie-server-ctx/team-logos
cp -a "$LOGO_DIR/." /tmp/stevie-server-ctx/team-logos/
cat >/tmp/stevie-server-ctx/Dockerfile <<'EOF'
FROM alpine:3.20
RUN apk add --no-cache ca-certificates ffmpeg mediainfo tzdata
WORKDIR /app
COPY stevie /app/stevie
COPY team-logos /app/team-logos
ENV STEVIE_DATA_DIR=/data
ENV STEVIE_TEAM_LOGOS_DIR=/app/team-logos
EXPOSE 8080
VOLUME ["/data"]
ENTRYPOINT ["/app/stevie"]
EOF
podman build -t stevie_server -f /tmp/stevie-server-ctx/Dockerfile /tmp/stevie-server-ctx

rm -rf /tmp/stevie-web-ctx/dist
mkdir -p /tmp/stevie-web-ctx/dist
cp -a "$ROOT/apps/web/dist/." /tmp/stevie-web-ctx/dist/
# Vite/umask often leaves dist as 750/640; nginx runs as non-owner → 403.
chmod -R a+rX /tmp/stevie-web-ctx/dist
cp "$ROOT/deploy/nginx-web.conf" /tmp/stevie-web-ctx/default.conf
cat >/tmp/stevie-web-ctx/Dockerfile <<'EOF'
FROM docker.io/library/nginx:1.27-alpine
COPY default.conf /etc/nginx/conf.d/default.conf
COPY dist /usr/share/nginx/html
# Rootless podman maps `nginx` to a subuid that cannot read host-owned files.
RUN chmod -R a+rX /usr/share/nginx/html \
 && sed -i 's/^user[[:space:]]\+nginx;/user root;/' /etc/nginx/nginx.conf
EXPOSE 80
EOF
podman build -t stevie_web -f /tmp/stevie-web-ctx/Dockerfile /tmp/stevie-web-ctx

echo "Preparing streamed.pk relay…"
(
  cd "$ROOT/apps/streamed-relay"
  # Ensure vendor lock.wasm / lock.js exist (from CDN).
  node scripts/fetch-vendor.mjs
)

echo "Recreating containers…"
podman rm -f stevie_caddy_1
sleep 1
podman rm -f stevie_server_1 stevie_web_1 stevie_streamed_1
sleep 1

podman run --replace --name=stevie_streamed_1 -d --pod=stevie \
  --label io.podman.compose.project=stevie \
  --label com.docker.compose.service=streamed \
  -e PORT=8091 \
  -e STEVIE_STREAMED_EMBED_URL="${STEVIE_STREAMED_EMBED_URL:-https://embed.st}" \
  -v "${ROOT}/apps/streamed-relay:/app" \
  -w /app \
  --restart unless-stopped \
  docker.io/library/node:22-bookworm-slim \
  bash -lc "npm install --omit=dev --silent && node src/server.mjs"

podman run --replace --name=stevie_server_1 -d --pod=stevie \
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
  -e TMDB_API_KEY="${TMDB_API_KEY}" \
  -e TMDB_RPS="${TMDB_RPS}" \
  -e MEDIA_LIBRARY_PATH="${MEDIA_LIBRARY_PATH}" \
  -e STEVIE_MEDIA_MOUNT=/media/library \
  -e LIVE_SOURCES_PATH="${LIVE_SOURCES_PATH}" \
  -e STEVIE_LIVE_MOUNT=/live-sources \
  -e STEVIE_RECORDINGS_PATH="${RECS}" \
  -e STEVIE_RECORDINGS_MOUNT=/recordings \
  -e STEVIE_STREAMED_RELAY_URL="http://127.0.0.1:8091" \
  -e STEVIE_STREAMED_BASE_URL="${STEVIE_STREAMED_BASE_URL:-https://streamed.pk}" \
  -e STEVIE_STREAMED_EMBED_URL="${STEVIE_STREAMED_EMBED_URL:-https://embed.st}" \
  -e XTREAM_URL="${XTREAM_URL:-}" \
  -e XTREAM_USER="${XTREAM_USER:-}" \
  -e XTREAM_PASSWORD="${XTREAM_PASSWORD:-}" \
  -e THESPORTSDB_API_KEY="${THESPORTSDB_API_KEY:-123}" \
  -e THESPORTSDB_COUNTRIES="${THESPORTSDB_COUNTRIES:-Canada,United_States}" \
  -v stevie_stevie-data:/data \
  -v "${MEDIA_LIBRARY_PATH}:/media/library:ro" \
  -v "${LIVE_SOURCES_PATH}:/live-sources:ro" \
  -v "${RECS}:/recordings" \
  --restart unless-stopped --init \
  stevie_server

podman run --replace --name=stevie_web_1 -d --pod=stevie \
  --label io.podman.compose.project=stevie \
  --label com.docker.compose.service=web \
  --restart unless-stopped stevie_web

podman run --replace --name=stevie_caddy_1 -d --pod=stevie \
  --requires=stevie_server_1,stevie_web_1 \
  --label io.podman.compose.project=stevie --label com.docker.compose.service=caddy \
  -e STEVIE_DOMAIN="${STEVIE_DOMAIN}" \
  -e STEVIE_HTTPS_PORT="${STEVIE_HTTPS_PORT}" \
  -e TLS_MODE="${TLS_MODE}" \
  -e STEVIE_LISTEN_PORT="${LISTEN}" \
  -e STEVIE_HTTP_PORT="${LISTEN}" \
  -v "${ROOT}/deploy/caddy-entrypoint.sh:/entrypoint.sh:ro" \
  -v stevie_caddy-data:/data \
  -v stevie_caddy-config:/config \
  -v "${ROOT}/deploy/certs:/certs:ro" \
  --restart unless-stopped --entrypoint /bin/sh \
  docker.io/library/caddy:2.9-alpine /entrypoint.sh

sleep 3
curl -sS -o /dev/null -w 'health=%{http_code}\n' "http://127.0.0.1:${LISTEN}/healthz"
echo "Done. Open Settings → Import VOD categories."
