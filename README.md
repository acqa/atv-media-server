# ATV3 Media Server

Self-hosted media server for **Apple TV 3** (model A1469) running iOS 7.9. Impersonates the **Red Bull TV** app via DNS hijacking, shows a catalog of local movies / TV shows / music with metadata from TMDb, and serves an HLS stream with automatic transcoding of incompatible formats.

Based on [ghokun/appletv3-iptv](https://github.com/ghokun/appletv3-iptv) — the XML templates and JS framework come from there, proven on real ATV3 hardware.

## Requirements

- Apple TV 3 (A1469) on iOS 7.6.2, 7.7 or 7.9.
- A machine on the same LAN as the ATV3 (Linux/macOS/Windows). Can be a Mac mini, NAS, or any Linux server.
- [Docker](https://docs.docker.com/get-docker/) and Docker Compose installed.
- Static local IP on the server host (or a DHCP reservation).
- (Optional) TMDb API key — without it, posters and descriptions won't load. How to get one — see below.

## Getting a TMDb API key

Free, takes 3-5 minutes. Only needed if you want to see posters, descriptions, and ratings on the ATV3 — without a key, movies will still appear, but with a placeholder instead of a cover.

1. **Registration**: <https://www.themoviedb.org/signup> — email, username, password. Confirm email.

2. **Request a key**: log in → <https://www.themoviedb.org/settings/api> → "Request an API Key" → choose **Developer** (Personal use).

3. **Fill out the form**:
   - *Application Name*: anything, e.g. `atv-media-server`
   - *Application URL*: `http://localhost` works, or a link to your GitHub
   - *Application Summary*: 1-2 sentences. Example: "Self-hosted media server for a personal Apple TV 3. Looks up movie/TV metadata to populate a local catalog."

4. Accept the Terms of Use. Approval is usually instant, sometimes a few minutes.

5. On the API page you'll see **two keys** — you need the **API Key (v3 auth)**, the short one (~32 characters). The second one (Read Access Token) is not needed.

6. Write it into `.env`:
   ```bash
   TMDB_API_KEY=your_key_here
   ```

7. Restart the server: `docker compose restart media-server`. The next scan will pick up metadata. To force a rescan — POST to `https://<host>/api/library/scan` or use the button in the admin UI.

**Limits**: TMDb currently does not penalize for volume; formally — ~50 requests / 10 seconds. The client in `internal/metadata/tmdb.go` has a retry on 429 with exponential backoff — more than enough for a typical library (hundreds to thousands of movies).

## Media folder structure

```
media/
├── movies/
│   ├── The Matrix (1999)/
│   │   └── The.Matrix.1999.1080p.mkv
│   └── Inception (2010)/
│       └── Inception.2010.mp4
├── series/
│   └── Breaking Bad/
│       ├── Season 1/
│       │   ├── S01E01 Pilot.mkv
│       │   └── S01E02.mkv
│       └── Season 2/
│           └── S02E01.mkv
└── music/
    └── Pink Floyd/
        └── The Dark Side of the Moon/
            ├── 01 - Speak to Me.flac
            └── cover.jpg
```

Name parsing rules:

- Movies: `Name (Year)` in the folder or file name. Year in parentheses or just `Name.Year.tag`.
- TV shows: season folder named `Season N` / `S01`; file name must contain `SxxExx`, `1x02` or `Season X Episode Y`.
- Music: `Artist/Album/Track` hierarchy. ID3 tags (if present) override the file name. Cover art — `cover.jpg`/`folder.jpg` in the album folder.

## Quick start

```bash
git clone https://github.com/<you>/atv-media-server
cd atv-media-server

cp .env.example .env
# Edit:
#   MEDIA_PATH=...    path to the media root (e.g. /Volumes/Media)
#   MEDIA_SERVER_IP=192.168.1.100   this machine's LAN IP
#   TMDB_API_KEY=...  (optional, for metadata)
#   ADMIN_USER/ADMIN_PASS  (optional, for the web admin)

docker compose up -d
```

On first run the server will:

1. Generate a self-signed certificate for `appletv.redbull.tv` in `./certs/`.
2. Create the SQLite DB `./data/metadata.db`.
3. Scan the library, and if a TMDb key is set, pull posters and descriptions.
4. Bring up HTTP/HTTPS on 80/443, the web admin on 8080, and CoreDNS on 53.

## ATV3 setup

1. On the remote: **Settings → General → Network → Wi-Fi → your network → Configure DNS → Manual** → enter the server IP (`MEDIA_SERVER_IP`).
2. **Settings → General → Send Data to Apple** — choose "No", then press **Play** on the remote on the same item.
3. A hidden "Add Profile" menu appears → **OK** → enter URL: `http://appletv.redbull.tv/redbulltv.cer`.
4. Confirm the certificate installation.
5. On the ATV3 home screen, open **Red Bull TV** — you'll see your catalog.

> Without an installed profile, Red Bull TV will show a TLS error. If something goes wrong — check the logs in `./data/logs/` or via `docker compose logs media-server`.

## Web admin

If `ADMIN_USER`/`ADMIN_PASS` are set in `.env`, the admin is available at `http://<MEDIA_SERVER_IP>:8080` with Basic Auth. Currently provides:

- Library counters (movies / TV shows / artists).
- A "Run scan" button and the current scan status.

Without `ADMIN_USER`/`ADMIN_PASS`, the admin is disabled.

## Environment variables

| Variable | Default | Purpose |
|----------|---------|---------|
| `MEDIA_PATH` | _required_ | Host path to the `media/` root |
| `MEDIA_SERVER_IP` | _required_ | This machine's LAN IP (for DNS) |
| `BASE_HOST` | `appletv.redbull.tv` | Host we impersonate |
| `TMDB_API_KEY` | empty | TMDb API key. Without it — no metadata. |
| `ADMIN_USER`/`ADMIN_PASS` | empty | Web admin credentials. Both empty → admin disabled. |
| `ADMIN_PORT` | `8080` | Admin port |
| `TRANSCODE_CACHE_MAX_GB` | `50` | HLS segment cache limit. When exceeded, LRU eviction runs. |

## What the server does on Play

1. ATV3 requests `/play.xml?id=...`.
2. The server finds the file in the DB (`movies`, `episodes` or `tracks`).
3. Runs [ffprobe](https://ffmpeg.org/ffprobe.html) to determine codecs (if not done before).
4. If the file is `h264 ≤ High@4.1 + (aac|ac3|eac3)` → **remux**: ffmpeg wraps the stream into HLS without re-encoding (fast, ~real-time × N).
5. Otherwise → **transcode**: ffmpeg re-encodes to H.264 High@4.1 + AAC stereo 192k (5 Mbps, slow, CPU-heavy).
6. Audio files (FLAC/MP3/M4A) are transcoded to AAC stereo HLS via a separate branch.
7. The result is cached in `/data/transcoded/<id>/`. On a repeated Play — served from cache.
8. Once an hour an LRU sweeper trims anything exceeding `TRANSCODE_CACHE_MAX_GB`.

## Project structure

```
.
├── server/             # Go server (module github.com/atv-media-server/server)
│   ├── main.go
│   ├── internal/
│   │   ├── admin/      # web admin :8080
│   │   ├── appletv/    # XML renderer + templates
│   │   ├── certs/      # SSL auto-generation
│   │   ├── config/
│   │   ├── library/    # scanners (movies/shows/music) + pipeline
│   │   ├── logging/
│   │   ├── metadata/   # TMDb client
│   │   ├── server/     # HTTP/HTTPS + handlers
│   │   ├── storage/    # SQLite + migrations
│   │   └── transcoder/ # ffmpeg/ffprobe wrappers + GC
│   └── Dockerfile
├── coredns/Corefile    # DNS for hijacking appletv.redbull.tv
├── docker-compose.yml
├── plans/              # per-phase implementation plans
├── PLAN.md / PROGRESS.md
└── rep.md              # original spec
```

## Operations

- **Restart**: `docker compose restart media-server`. SSL/DB survive.
- **Logs**: `docker compose logs -f media-server` or `./data/logs/YYYY-MM-DD.log`.
- **Full rebuild**: `docker compose up -d --build`.
- **Force rescan**: POST to `https://<host>/api/library/scan` or the button in the admin.
- **Clear transcode cache**: `rm -rf data/transcoded && docker compose restart media-server`.

## Troubleshooting

| Symptom | What to check |
|---------|---------------|
| On ATV3: "Cannot verify server identity" | Profile is not installed or installed for a different CN. Make sure `Settings → General → Profiles and Device` contains `appletv.redbull.tv`. |
| `Red Bull TV` hangs on the splash screen | DNS is not hijacked. Check that `Settings → Network → DNS` is set to your IP. |
| One movie plays, another doesn't | Find the file ID via `/movies.xml` and check `data/transcoded/<id>/`. Server logs will show if ffmpeg crashed. |
| Cover art doesn't load | `TMDB_API_KEY` is not set. Without it, ATV3 shows the `missing_logo.png` placeholder. |
| `MEDIA_SERVER_IP must be set` | You didn't start compose with `.env`. `cp .env.example .env` and fill it in. |

## Links

- Detailed spec: [rep.md](rep.md)
- Implementation plan: [PLAN.md](PLAN.md), [PROGRESS.md](PROGRESS.md)
- Reference project: [ghokun/appletv3-iptv](https://github.com/ghokun/appletv3-iptv)
- TMDb API: <https://developer.themoviedb.org/docs>
