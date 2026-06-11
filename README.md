# Barne-TV v2

A kid-friendly video wall: an endless, looping feed of YouTube thumbnails you scroll through, never running out. Click a thumbnail to watch it fullscreen with the YouTube IFrame player. Videos are fetched from configured YouTube channels and playlists, cached in memory, and refreshed periodically.

Live at [refsnes-barnetv.no](https://refsnes-barnetv.no).

## Quick start

```bash
cp config.example.yaml config.yaml   # add your YouTube Data API v3 key
go run .
# open http://localhost:8080
```

## Configuration

See [`config.example.yaml`](config.example.yaml). Sources can be YouTube channels or playlists:

```yaml
youtube_api_key: "YOUR_KEY"
refresh_interval: "6h"       # how often to re-fetch video lists
sources:
  - type: channel
    id: "UCxxxxxxxx"
    name: "My Channel"
  - type: playlist
    id: "PLxxxxxxxx"
    name: "My Playlist"
```

## Architecture

Single Go binary, no database. Everything runs in one process:

- **youtube.go** - YouTube Data API v3 client (channels + playlists, paginated)
- **cache.go** - Thread-safe in-memory video cache with periodic refresh
- **handlers.go** - `GridHandler` renders the first screenful with a random per-load seed embedded; `VideosHandler` serves successive screenfuls from `/videos?seed=&offset=&count=` as HTML fragments, looping the pool in a fresh fair order each cycle
- **main.go** - Wires config, cache, and HTTP together
- **templates/** - Go HTML templates: `index.html` page shell plus the shared `cells.html` tile fragment
- **static/** - CSS and JS (infinite-scroll fetch/append/DOM-pruning, YouTube IFrame API integration, fullscreen playback)

## Deployment

Deployed as a bare binary on a Hetzner VPS behind Caddy (auto-HTTPS).

```bash
./deploy.sh   # cross-compiles, uploads, restarts service
```

For fresh server setup, see [`deploy/setup-server.sh`](deploy/setup-server.sh).

## Tests

```bash
go test -race ./...
```

### Manual frontend verification

The JS (infinite scroll, DOM pruning, playback) has no automated tests. To
exercise it in a browser without a YouTube API key, a build-tagged harness
([`repro_harness_test.go`](repro_harness_test.go)) serves the real handlers,
templates, and static assets with synthetic videos:

```bash
go test -tags repro -run TestReproHarness -timeout 0
# open http://127.0.0.1:8431
```

## License

MIT - see [LICENSE](LICENSE).
