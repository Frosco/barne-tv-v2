# Barne-TV v2

A kid-friendly video wall that shows a shuffled grid of YouTube thumbnails. Click a thumbnail to watch it fullscreen with the YouTube IFrame player. Videos are fetched from configured YouTube channels and playlists, cached in memory, and refreshed periodically.

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
- **handlers.go** - HTTP handler serving a shuffled 3x3 grid, persisted in a cookie
- **main.go** - Wires config, cache, and HTTP together
- **templates/** - Go HTML template for the video grid page
- **static/** - CSS and JS (YouTube IFrame API integration, fullscreen playback)

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

## License

MIT - see [LICENSE](LICENSE).
