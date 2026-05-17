# ATV3 Media Server

Self-hosted media server for **Apple TV 3** (model A1469) running iOS 7.6.2 / 7.7 / 7.9. Impersonates the **Red Bull TV** app via DNS hijacking, shows a catalog of local movies and TV shows with metadata from TMDb, and serves HLS with on-the-fly remux or transcoding so the ATV3 hardware decoder is happy.

Features:

- Movie and TV show grids; show → seasons → episodes navigation.
- TMDb metadata: titles, descriptions, posters, backdrops, episode stills, ratings.
- Quality metadata on detail cards: `1080p · H.264 · AC3 5.1` and resolution/codec badges (PNG icons borrowed from PlexConnect).
- Three HLS paths depending on the source: **remux** (copy), **audio-transcode** (copy video, re-encode audio only), full **transcode**.
- Multi-audio-track playback: per-movie/episode "Audio 1 / Audio 2 …" buttons select a specific stream.
- Search across the movie catalogue.
- Optional web admin with library counts and a "Run scan" button.
- Auto DoH bypass when the ISP DNS-sinkholes TMDb (`image.tmdb.org` / `api.themoviedb.org`) — see [docs/KNOWN_ISSUES.md](docs/KNOWN_ISSUES.md) §2.

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
└── series/
    └── Breaking Bad/
        ├── Season 1/
        │   ├── S01E01 Pilot.mkv
        │   └── S01E02.mkv
        └── Season 2/
            └── S02E01.mkv
```

Name parsing rules:

- Movies: `Name (Year)` in the folder or file name. Year in parentheses or just `Name.Year.tag`.
- TV shows: season folder named `Season N` / `S01`; file name must contain `SxxExx`, `1x02` or `Season X Episode Y`.

Recognised extensions: `.mp4`, `.mkv`, `.m4v`, `.mov`, `.avi`.

## Quick start

```bash
git clone https://github.com/acqa/atv-media-server
cd atv-media-server

cp .env.example .env
# Edit:
#   MEDIA_PATH=...    path to the media root (e.g. /Volumes/Media)
#   MEDIA_SERVER_IP=192.168.1.100   this machine's LAN IP
#   TMDB_API_KEY=...  (optional, for metadata)
#   ADMIN_USER/ADMIN_PASS  (optional, for the web admin)

docker compose up -d
```

On first run the stack will:

1. Generate a self-signed certificate for `appletv.redbull.tv` in `./certs/`.
2. Create the SQLite DB `./data/metadata.db` and apply migrations.
3. Probe whether the system resolver sinkholes `image.tmdb.org`; if yes, route TMDb HTTP through DoH (Cloudflare → Quad9).
4. Scan movies and TV shows. With a TMDb key, fetch metadata and pre-download every poster/backdrop/episode-still size the templates request into `./data/posters/` (so subsequent runs work fully offline).
5. Probe each new file with `ffprobe` to record codec/resolution/audio-channel/audio-stream-count and the remux-vs-transcode decision in the DB.
6. Bring up HTTP/HTTPS on 80/443 and the web admin on 8080 (the `media-server` container), plus dnsmasq on 53 (the `dns` container) hijacking `appletv.redbull.tv` to `MEDIA_SERVER_IP`.

Or, equivalently, via the `Makefile`:

```bash
make env    # copy .env.example -> .env (only if missing)
make up     # docker compose up -d
make logs   # tail media-server logs
make scan   # trigger a library rescan
make help   # list all targets
```

## ATV3 setup

1. On the remote: **Settings → General → Network → Wi-Fi → your network → Configure DNS → Manual** → enter the server IP (`MEDIA_SERVER_IP`).
2. **Settings → General → Send Data to Apple** — choose "No", then press **Play** on the remote on the same item.
3. A hidden "Add Profile" menu appears → **OK** → enter URL: `http://appletv.redbull.tv/redbulltv.cer`.
4. Confirm the certificate installation.
5. On the ATV3 home screen, open **Red Bull TV** — you'll see your catalog.

> Without an installed profile, Red Bull TV will show a TLS error. If something goes wrong — check the logs in `./data/logs/` or via `docker compose logs media-server`.

## Web admin

If `ADMIN_USER`/`ADMIN_PASS` are set in `.env`, the admin is available at `http://<MEDIA_SERVER_IP>:8080` with Basic Auth. Currently provides:

- Library counters (movies / TV shows).
- A "Run scan" button and the current scan status (auto-refreshes every 3s).

The same scan trigger is also exposed on the public HTTPS mux at `POST /api/library/scan` with status at `GET /api/library/status` (no auth — intended for `make scan` from the host). Without `ADMIN_USER`/`ADMIN_PASS`, only the admin UI is disabled.

## Environment variables

| Variable | Default | Purpose |
|----------|---------|---------|
| `MEDIA_PATH` | _required_ | Host path to the `media/` root |
| `MEDIA_SERVER_IP` | _required_ | This machine's LAN IP (for DNS) |
| `BASE_HOST` | `appletv.redbull.tv` | Host we impersonate |
| `TMDB_API_KEY` | empty | TMDb API key. Without it — no metadata. |
| `ADMIN_USER`/`ADMIN_PASS` | empty | Web admin credentials. Both empty → admin disabled. |
| `ADMIN_PORT` | `8080` | Admin port (read by docker-compose; not set in `.env.example`). |
| `HTTP_PORT` / `HTTPS_PORT` | `80` / `443` | Server ports inside the container (mapped 1:1 to the host). |
| `TRANSCODE_CACHE_MAX_GB` | `50` | HLS segment cache limit. When exceeded, LRU eviction runs hourly. |
| `DOH_URL` | Cloudflare + Quad9 | DNS-over-HTTPS endpoints (comma-separated) used to resolve `*.themoviedb.org`. Auto-engaged when a startup probe shows the system resolver sinkholes `image.tmdb.org`; setting `DOH_URL` explicitly forces DoH on regardless. Providers are tried in order until one yields a non-loopback answer. |
| `LOG_TO_FILE` | `true` (compose) | If true, also write logs to `./data/logs/YYYY-MM-DD.log`. |
| `LOGGING_PATH` | `/data/logs` | Directory for daily log files. |

## What the server does on Play

1. ATV3 requests `/play.xml?id=...&audio=N` (`N`=0 by default; the detail card renders one button per audio stream).
2. The server resolves the id against the `media` and `episodes` tables and returns `player.xml` fast — no transcoding yet.
3. AVPlayer then requests `/stream/<id>/playlist.m3u8?a=N`. *That's* when the HLS bundle is prepared, in a background goroutine, into `/data/transcoded/<id>__a<N>/`.
4. Based on the `ffprobe` data already in the DB (computed at scan time), one of three paths runs:
   - **remux** — source is `h264` (Baseline/Main/High, level ≤ 4.1) with `aac` or `ac3` audio → ffmpeg copies both streams into HLS without re-encoding (real-time × N, no CPU spent on codecs).
   - **audio-transcode** — video is ATV3-friendly but audio is `eac3` / DTS / FLAC / Opus / … → copy video, re-encode audio to AAC stereo 192k. Roughly 5–10× faster than a full transcode because libx264 isn't involved.
   - **transcode** — anything else (HEVC, AV1, H.264 above 4.1, …) → re-encode to H.264 High@4.1 + AAC stereo 192k, capped at 5 Mbps. Slow, CPU-bound.
5. The handler returns as soon as the playlist is "playable" (≥ 3 segments / ≥ 15 s) so AVPlayer doesn't trip its ~30–60s playlist-load timeout. ffmpeg keeps writing in the background until done; later playlist refreshes pick up the growth.
6. The result is cached on disk. On a repeated Play — served from cache without invoking ffmpeg.
7. Once an hour an LRU sweeper trims anything exceeding `TRANSCODE_CACHE_MAX_GB`.

## Project structure

```
.
├── server/             # Go server (module github.com/atv-media-server/server)
│   ├── main.go
│   ├── internal/
│   │   ├── admin/      # web admin :8080 (Basic Auth)
│   │   ├── appletv/    # XML renderer + ATV3 page templates
│   │   ├── certs/      # self-signed SSL auto-generation
│   │   ├── config/     # env-var loader
│   │   ├── dnsdoh/     # DoH resolver + http.Client dialer for TMDb DNS sinkhole
│   │   ├── library/    # filesystem scanners (movies, series) + ingest pipeline
│   │   ├── logging/
│   │   ├── metadata/   # TMDb v3 client (movies + TV)
│   │   ├── server/     # HTTP/HTTPS handlers, poster cache, badges, scan API
│   │   ├── storage/    # SQLite store + migrations
│   │   └── transcoder/ # ffmpeg/ffprobe wrappers, mode chooser, cache GC
│   └── Dockerfile
├── docs/KNOWN_ISSUES.md # documented quirks (ATV3 firmware bugs, DoH workaround)
├── coredns/Corefile    # legacy CoreDNS config (current stack uses dnsmasq, see docker-compose.yml)
├── scripts/gen-cert.sh # standalone helper to generate the self-signed cert
├── docker-compose.yml  # media-server + dnsmasq services
├── Makefile            # up / down / logs / scan / lint / test ...
├── .env.example
└── LICENSE             # Apache 2.0
```

## Operations

Either run `docker compose` directly or use the `Makefile` wrappers:

- **Restart**: `make restart` (or `docker compose restart media-server`). SSL/DB survive.
- **Logs**: `make logs` (or `docker compose logs -f media-server`) — also written to `./data/logs/YYYY-MM-DD.log`.
- **Full rebuild**: `make rebuild` (or `docker compose up -d --build`).
- **Force rescan**: `make scan` (POSTs to `https://localhost/api/library/scan`) or the button in the admin.
- **Clear transcode cache**: `make clean-cache && make restart`.
- **Reset everything (DB + cache)**: `make clean-data` (irreversible).
- **Dev loop**: `make fmt`, `make vet`, `make lint`, `make test`, `make build`.

## Troubleshooting

| Symptom | What to check |
|---------|---------------|
| On ATV3: "Cannot verify server identity" | Profile is not installed or installed for a different CN. Make sure `Settings → General → Profiles and Device` contains `appletv.redbull.tv`. |
| `Red Bull TV` hangs on the splash screen | DNS is not hijacked. Check that `Settings → Network → DNS` is set to your IP. |
| One movie plays, another doesn't | Find the file ID via `/movies.xml` and check `data/transcoded/<id>__a0/`. Server logs will show if ffmpeg crashed. |
| Cover art doesn't load | `TMDB_API_KEY` is not set. Without it, ATV3 shows the `missing_logo.png` placeholder. |
| TMDb fetches fail with `dial tcp [::1]:443: connect: connection refused` | The ISP DNS-sinkholes TMDb. The server already uses DoH (Cloudflare → Quad9) by default; if both are blocked on your network, override `DOH_URL` with an endpoint that isn't filtered there. |
| `MEDIA_SERVER_IP must be set` | You didn't start compose with `.env`. `cp .env.example .env` and fill it in. |

## Links

- Reference project: [ghokun/appletv3-iptv](https://github.com/ghokun/appletv3-iptv)
- TMDb API: <https://developer.themoviedb.org/docs>
- Known ATV3-firmware quirks and workarounds: [docs/KNOWN_ISSUES.md](docs/KNOWN_ISSUES.md)
- Third-party assets / licenses: [THIRD_PARTY_LICENSES.md](THIRD_PARTY_LICENSES.md)

## License

Apache License 2.0 — see [LICENSE](LICENSE).
