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

`deploy.sh` syncs the binary, templates, and static assets, but **not**
`config.yaml` (it's gitignored and holds the API key). When sources or other
config change, push it separately. The config is read only at startup, so the
service must be restarted to pick up changes:

```bash
scp config.yaml root@refsnes-barnetv.no:/opt/barne-tv/config.yaml
ssh root@refsnes-barnetv.no "chown barnetv:barnetv /opt/barne-tv/config.yaml && systemctl restart barne-tv"
```

The `chown` is needed because `scp` as `root` leaves the file root-owned, while
the service runs as `barnetv`.

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

### Browser regression scripts

[`browser-tests/`](browser-tests/) holds Playwright scripts for the JS
behaviour Go tests can't reach. Each one is a single `async (page) => {...}`
function returning a JSON verdict, run through Playwright's code runner.

They drive the **live site** and inject the working tree's `static/app.js` and
`static/style.css` over it (the deployed copies are aborted), so a rule already
deleted locally can't leak in from production. Running them against the live
origin is deliberate: real YouTube embeds only load from a real origin.

| Script | Guards |
|---|---|
| `return-to-grid.js` | Leaving a video keeps the same tiles and scroll position, never navigates. Back-out returns instantly; a finished video rests on black first. A backgrounded video doesn't make the next one swallow a Back press. Escape gets out when fullscreen was refused, and space still pauses. |
| `real-playback.js` | The same, against the real grid with a real YouTube player in real fullscreen. |
| `player-touch.js` | A drag on the player can't scroll the feed underneath, and tap-to-pause still works. |

Verdict fields are booleans; every one must be `true`. If a result surprises
you, rebuild the rig before believing it — a stale page or a tap issued
straight after a synthetic drag produces confident false failures.

## License

MIT - see [LICENSE](LICENSE).
