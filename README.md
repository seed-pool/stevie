# Stevie

Self-hosted media app for **Xtream VOD** (movies & TV), **Live TV** (M3U / Xtream + EPG), **Sports** (scores, guide matchups, streamed.pk), and **recordings**. Runs on localhost with no domain, or with TLS on a hostname via Caddy.

## Features

### Movies & TV (Xtream VOD)
- Browse imported movie and series categories from an Xtream Codes panel
- Detail pages, in-browser playback (direct or ffmpeg remux when needed)
- Tech badges (resolution / HDR / audio) and optional player tech overlay
- Download movie or episode to the recordings folder; cancel in-progress downloads
- Analyze a VOD stream (MediaInfo + ffprobe) from the UI
- Pin favorite categories; pagination and search across the library

### Live TV
- M3U playlist (URL or file under the live-sources mount) and optional XMLTV override
- Xtream live categories import (credentials from `.env`)
- EPG guide with favorites, category pins, and channel search
- Play via browser player; start/stop live recording; schedule from the guide
- Provider EPG sync + manual refresh from Settings

### Sports
- Live / upcoming events from TheSportsDB, ESPN scoreboards, EPG harvest, and streamed.pk
- **Hot** strip of currently live high-interest games
- Favorite sports categories; selected sport remembered across tab switches
- Play IPTV channels matched to a game, or streamed.pk embeds via the unlock relay
- Record / schedule from a game’s channel menu (IPTV or streamed when live)

### Recordings
- Active live jobs, scheduled recordings, finished files, and VOD downloads
- Play, delete, or stop/cancel from the Recordings page
- Files are written under `STEVIE_RECORDINGS_PATH`

### App chrome
- Home: recently added movies / shows and Live TV shortcuts
- Unified search (movies, TV, Live)
- Settings: theme, timezone, page size, player tech info, Live + VOD imports
- Single admin login (`STEVIE_ADMIN_USER` / `STEVIE_ADMIN_PASSWORD`)

> **Note:** Older local filesystem movie/TV library scanning still exists in the Go API, but the **UI library is Xtream VOD**. Point `MEDIA_LIBRARY_PATH` at any existing directory if you do not use a local media tree (compose still mounts it).

## Stack

| Piece | Role |
|---|---|
| `apps/server` | Go API, playback, sync jobs (`ffmpeg` / `ffprobe` / `mediainfo` in the image) |
| `apps/web` | React + Vite UI (nginx) |
| `apps/streamed-relay` | Node unlock + HLS relay for streamed.pk / embed.st |
| PostgreSQL 16 | Library, live, sports, sessions |
| Redis 7 | TMDB cache, scan/sync progress |
| Caddy | Front door (local HTTP or TLS) |

## Requirements

- Docker Engine + Compose **or** Podman (`deploy/start-podman.sh`, `deploy/redeploy.sh`)
- Host folders for recordings and (usually) live sources; media path can be a placeholder dir
- Outbound HTTPS for Xtream, TMDB (optional), TheSportsDB, ESPN, streamed.pk

## Quick start (Docker Compose)

```bash
git clone <repo-url> stevie && cd stevie
cp .env.example .env
# edit .env — at least STEVIE_ADMIN_PASSWORD, paths, and ideally XTREAM_* + TMDB_API_KEY
mkdir -p "$MEDIA_LIBRARY_PATH" "$LIVE_SOURCES_PATH" "$STEVIE_RECORDINGS_PATH"  # use your paths

cd deploy
docker compose --env-file ../.env up -d --build
```

Services: **caddy**, **web**, **server**, **streamed**, **postgres**, **redis**.

### Local (no domain)

```bash
# .env
STEVIE_DOMAIN=
STEVIE_LISTEN_PORT=28413
```

Open **http://127.0.0.1:28413**. Caddy serves cleartext on the listen port; secure cookies default off.

### Domain + TLS

```bash
# .env
STEVIE_DOMAIN=stevie.example.com
STEVIE_HTTPS_PORT=8443
TLS_MODE=internal   # or files (+ deploy/certs/tls.crt & tls.key)
```

Open **https://stevie.example.com:8443**. `TLS_MODE=internal` uses Caddy’s internal CA (browser warning until trusted). `TLS_MODE=off` is for an external reverse proxy (cleartext to Caddy on `STEVIE_LISTEN_PORT`).

Rebuild after updates:

```bash
cd deploy
docker compose --env-file ../.env up -d --build
```

### Rootless Podman

`deploy/start-podman.sh` and `deploy/redeploy.sh` target rootless Podman (host-built binary + Alpine image + pod). Prefer Compose on a normal Docker host. For a shared Podman pod, use `STEVIE_STREAMED_RELAY_URL=http://127.0.0.1:8091`. Compose defaults to `http://streamed:8091`.

## First-run setup

1. Sign in with `STEVIE_ADMIN_USER` / `STEVIE_ADMIN_PASSWORD`.
2. **Settings → Live TV**
   - Set M3U URL or path (e.g. `/live-sources/playlist.m3u`) and optional XMLTV override → Save / refresh EPG.
   - If `XTREAM_*` is set in `.env`, use **Refresh Xtream** to import live categories.
3. **Settings → Movies & TV (Xtream VOD)** → **Import VOD categories** (movies and series tabs).
4. Open **Live**, **Movies**, **TV**, or **Sports** as needed. Sports syncs in the background (TheSportsDB / ESPN / EPG / streamed.pk).

## Configuration

See `.env.example` for the full list. Important variables:

| Variable | Purpose |
|---|---|
| `STEVIE_DOMAIN` | Empty / `localhost` / `127.0.0.1` → local HTTP. Otherwise TLS site hostname. |
| `STEVIE_LISTEN_PORT` | Local / cleartext front door (default `28413`) |
| `STEVIE_HTTPS_PORT` | HTTPS port when a domain + TLS is used |
| `TLS_MODE` | `internal` \| `files` \| `off` (forced `off` in local mode) |
| `STEVIE_ADMIN_*` | Login |
| `STEVIE_SECURE_COOKIES` | Leave unset locally; `true` behind HTTPS |
| `POSTGRES_PASSWORD` | Postgres password (compose) |
| `TMDB_API_KEY` | Optional; metadata / artwork enrichment |
| `TMDB_RPS` | TMDB request rate (default `20`) |
| `MEDIA_LIBRARY_PATH` | Host dir mounted at `/media/library` (legacy local library; keep a real path for compose) |
| `LIVE_SOURCES_PATH` | Host dir mounted at `/live-sources` for local M3U / XMLTV |
| `STEVIE_RECORDINGS_PATH` | Host dir for live recordings + VOD downloads |
| `XTREAM_URL` / `XTREAM_USER` / `XTREAM_PASSWORD` | Xtream panel (not editable in the UI) |
| `THESPORTSDB_API_KEY` | SportsDB key (`123` = free demo) |
| `THESPORTSDB_COUNTRIES` | Country filters for SportsDB schedules |
| `STEVIE_STREAMED_BASE_URL` | streamed.pk API origin |
| `STEVIE_STREAMED_EMBED_URL` | embed.st origin for unlock |
| `STEVIE_STREAMED_RELAY_URL` | Unlock relay (`http://streamed:8091` in Compose) |

## Sports sync (background)

Approximate cadence inside the server:

- Full SportsDB-oriented pass ~ every **45 minutes**
- ESPN scoreboards ~ every **15 seconds**
- SportsDB livescores (when available) ~ every **2 minutes**
- EPG harvest for airing matchups ~ every **5 minutes**
- streamed.pk catalog cached ~ **2 minutes** on demand (Sports / Hot)

## Project layout

```
apps/server/           Go API, migrations, team-logo assets + fetch scripts
apps/web/              React UI
apps/streamed-relay/   streamed.pk unlock relay
deploy/                docker-compose, Dockerfiles, Caddy entrypoint, Podman scripts
.env.example           Documented environment template
```

## Development

API (needs Postgres + Redis reachable; same env vars as production):

```bash
cd apps/server
export DATABASE_URL='postgres://stevie:stevie@localhost:5432/stevie?sslmode=disable'
export REDIS_URL='redis://localhost:6379/0'
export STEVIE_ADMIN_PASSWORD='changeme'
go run ./cmd/stevie
```

Web (dev server; proxy/API URL per your local setup):

```bash
cd apps/web
npm install
npm run dev
```

Streamed relay:

```bash
cd apps/streamed-relay
npm install   # postinstall fetches vendor lock.wasm
npm start
```

CI (`.gitlab-ci.yml`): `go vet`, `go test`, and web production build.

## Privacy

- No telemetry.
- TMDB is used only when `TMDB_API_KEY` is set (metadata / artwork).
- Artwork is cached under the server data volume and served by the API.
- Live-sources and media mounts are read-only; recordings mount is read-write.
- Xtream credentials stay in `.env`, not in the Settings UI.
