# Barne-TV v2 — MVP Design

## Context

v1 was a Python/FastAPI + Vanilla JS app built from a large upfront spec. It grew complex
and had integration bugs. v2 starts from scratch with a minimal working product, iterating
based on real usage feedback.

## Goal

A web page that shows a 3x3 grid of random video thumbnails from parent-approved YouTube
channels and playlists. Click a video to watch it fullscreen. When it ends, return to the grid.

## Architecture

A single Go binary that:

1. Reads a YAML config file listing YouTube channel/playlist IDs
2. Fetches video metadata (title, thumbnail, video ID) from YouTube Data API v3
3. Caches the video pool in memory, refreshes periodically (default 6 hours)
4. Serves a single HTML page via Go `html/template`
5. Serves static assets (CSS, JS)

No database. No admin UI. No auth. No daily limits.

## User flow

1. **Page load**: Server picks 9 random videos from cached pool, renders the grid
2. **Grid persistence**: Selected video IDs stored in a cookie. Reloading or returning
   from a video shows the same 9. A shuffle button clears the cookie and loads fresh 9.
3. **Click a video**: JS hides the grid, shows a YouTube iframe embed in fullscreen
4. **Video ends**: YouTube iframe API fires `onStateChange`, brief pause (~2-3s with fade),
   then JS removes the player and shows the grid again
5. **Shuffle button**: Visual icon button (no text), loads a new random selection

## Grid design

- 3x3 thumbnail grid
- Video title displayed under each thumbnail (for parents to read)
- Titles truncated if too long to keep grid clean
- Large, tappable thumbnails for small hands
- Playful, colourful, child-friendly design (no reading required for the child)
- Shuffle button is icon-based

## Config file (`config.yaml`)

```yaml
youtube_api_key: "AIza..."
refresh_interval: "6h"
sources:
  - type: channel
    id: "UC..."
    name: "Animal Planet"
  - type: playlist
    id: "PL..."
    name: "Car repair stuff"
```

## File structure

```
barne-tv-v2/
├── main.go              # entry point, server, routes
├── youtube.go           # YouTube API client, video fetching, caching
├── config.go            # YAML config parsing
├── templates/
│   └── index.html       # the single page template
├── static/
│   ├── style.css        # playful, colourful design
│   └── app.js           # grid interaction, video player, shuffle
├── config.yaml          # channel/playlist config (not in git)
├── config.example.yaml  # template for config
└── go.mod
```

## Deployment

- Cross-compile Go binary for linux/amd64
- Hetzner VPS, nginx reverse proxy, systemd service
- No Docker (single binary makes it unnecessary)

## Not in scope (potential future iterations)

- Daily time limits
- Watch history / engagement algorithm
- Admin UI
- Authentication
- Database
- Banned videos list
