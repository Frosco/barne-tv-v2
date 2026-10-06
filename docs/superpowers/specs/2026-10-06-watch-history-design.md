# Watch History

## Problem

The child regularly asks to see a particular video again ("the one with the
digger!"). The wall is an endless reshuffled feed, so there is no way to find a
video once it has scrolled away. The parent needs to look up what was watched
recently and play it again.

## Goal

A list of recently watched videos that the parent can open from the wall, spot
the right one by its thumbnail within a few seconds, and play it.

Concretely:

- The list holds the 50 most recently watched distinct videos, newest first.
  Watching a video again moves it to the top instead of duplicating it.
- A video counts as watched once it has played 10 seconds. A mistaken tap
  backed out of straight away is not recorded.
- The list is shared across every device the family uses (tablet, TV, phone),
  so it lives on the server.
- Each entry shows thumbnail and title, and plays exactly like a wall tile.
- Entries stay playable after their video drops out of the cache.

## Non-goals

- **No timestamps shown or stored.** The child names videos by content, not by
  when they were watched; order alone conveys recency.
- **No per-device or per-user history.** There is no login; everyone using the
  site shares one list.
- **Not a kid-facing feature.** The entry point is a small, dim corner button
  for the parent. It is not hidden or locked, but it doesn't invite the child.
- **No search, no deleting entries, no overlay panel.** An overlay over the
  wall was considered and rejected (see Decisions).

## Approach

Three pieces:

1. A server-side `WatchHistory` store, persisted as a small JSON file.
2. Two routes on `/history`: `POST` records a play, `GET` renders the list as
   its own page.
3. In the browser, `app.js` reports a play after 10 seconds, and a corner button
   links between the wall and the history page.

The history page reuses the wall's tile fragment and the existing playback code
unchanged: its grid simply has no `data-seed`, so infinite scroll stays off.

## Server: the history store (`history.go`)

```go
type WatchHistory struct {
    mu      sync.Mutex
    path    string
    limit   int
    entries []Video // newest first
}

func NewWatchHistory(path string, limit int) *WatchHistory
func (h *WatchHistory) Load() error
func (h *WatchHistory) Record(v Video) error
func (h *WatchHistory) Entries() []Video
```

- **`Load`** reads the file. A missing file is not an error: the history starts
  empty. A file that isn't valid JSON returns an error and leaves the history
  empty; the next `Record` overwrites it.
- **`Record`** removes any existing entry with the same ID, prepends `v`, trims
  to `limit`, then writes the whole list. The in-memory list is updated even if
  the write fails, so the entry is visible until the next restart; the write
  error is returned to the caller.
- **Atomic write:** marshal to JSON, write to a temp file in the same directory
  (`os.CreateTemp`), close it, then `os.Rename` it over the real file. On any
  error the temp file is removed. A crash mid-write can never leave a truncated
  history file.
- **`Entries`** returns a copy, newest first.
- The mutex is held across the in-memory update and the file write, so two
  concurrent plays can't interleave their renames out of order.
- File format: a JSON array of `Video` objects (no struct tags, so field names
  are `ID`, `Title`, `ThumbnailURL`, `SourceID`). About 10 KB at 50 entries.

### Location and startup

- New flag `-history`, default `history.json`, alongside `-config` and `-addr`.
  In production the service's working directory is `/opt/barne-tv`, owned by
  the `barnetv` user, so the file lands at `/opt/barne-tv/history.json` with no
  change to the systemd unit. `deploy.sh` only replaces `templates/` and
  `static/`, so deploys leave the file alone.
- `main.go` builds the history with a limit of 50 and calls `Load`. An error is
  logged and startup continues with the empty history: the history is a
  convenience and must not take the wall down or put systemd into a restart
  loop.
- `history.json` is added to `.gitignore`, since `go run .` creates it in the
  repo root.

## HTTP

Go 1.24's `ServeMux` routes by method, so both handlers share one path.
`main.go` registers them next to `/` and `/videos`:

```go
http.Handle("POST /history", &RecordPlayHandler{Cache: cache, History: history})
http.Handle("GET /history", &HistoryHandler{History: history, Template: tmpl})
```

### `POST /history` — `RecordPlayHandler`

- Reads the form field `id`.
- Looks the ID up with `VideoCache.GetByIDs([]string{id})`. That method already
  exists and has had no production caller since the cookie-based grid was
  removed; this gives it one again.
- Missing or unknown ID → `400`. Only videos from the configured sources can
  ever enter the history, so nobody outside can put arbitrary YouTube videos in
  front of the child. The worst a stranger can do is reorder the list with
  videos from the family's own pool.
- `Record` error → log it and respond `500`.
- Success → `204 No Content`.

### `GET /history` — `HistoryHandler`

Renders `history.html` with `History.Entries()`:

- A small heading, "Sett før".
- The entries as a `.grid` of tiles via the existing `cells` template, with
  **no `data-seed` or `data-next-offset`**, so `app.js` never starts the
  infinite-scroll observer.
- With no entries, a short line instead of the grid: "Ingen videoer sett ennå".
- Same `noindex` meta, stylesheet, player container and scripts as the wall, so
  playback, the click-shield, the countdown, Escape and the fullscreen handling
  all work there unchanged. After a video, the player uncovers the history page
  again, ready for the next candidate.

### Templates

`history.html` would otherwise duplicate the `<head>` contents, the player
container and the two script tags from `index.html`. Those move into a new
`templates/partials.html` defining `head` and `player`, used by both pages,
the same way `cells.html` is shared today. Each page keeps its own `<title>`.

`main.go` parses all four files into one template set. `GridHandler` keeps
calling `Template.Execute`, which runs `index.html` as long as it is the first
file parsed; `HistoryHandler` uses `ExecuteTemplate(w, "history.html", ...)`.

## Browser

### Reporting a play (`static/app.js`)

- When a tile is tapped, `app.js` remembers its video ID alongside the rest of
  the per-video state.
- The countdown's existing one-second interval also checks
  `player.getCurrentTime()`. The first time it reaches 10 seconds, `app.js`
  sends the report once for this video:

  ```js
  fetch("/history", { method: "POST", body: new URLSearchParams({ id: videoId }) })
      .catch(function () {});
  ```

  Playback time is what counts, so time spent paused does not.
- Nothing waits on the report and nothing retries it. A failure means that one
  video is missing from the list; playback is never affected.
- `returnToGrid` resets the remembered ID and the "already reported" flag with
  the rest of the per-video state, so the next video starts clean.
- Tap-to-play, the click-shield, Escape, the fullscreen and backgrounding
  handling and `returnToGrid`'s teardown are otherwise untouched.

### Corner button

- The wall gets a small round `<a class="corner-button" href="/history">` with
  an inline SVG clock icon and `aria-label="Sett før"`.
- The history page has the same button in the same spot, with a grid icon,
  `href="/"` and `aria-label="Tilbake til alle videoer"`. This matters on a
  device with no Back button; browser and Android Back work as well.
- `position: fixed` in the top-right corner, so it is reachable without
  scrolling back to the top. At least 44px tap target, low opacity like the
  countdown, brighter on hover.
- Its `z-index` stays below the player container's `100`, so the player covers
  it during a video.
- An accidental tap by the child lands on a page of tiles they can also play,
  so nothing breaks.
- Following either link loads the wall fresh, with a new shuffle. That is the
  accepted cost of a separate page (see Decisions).

## Error handling

| Situation | Behaviour |
|---|---|
| Report request fails (network, 4xx, 5xx) | Swallowed in the browser; playback continues; video missing from the list |
| `POST` with missing or unknown ID | `400`, nothing recorded |
| File write fails on `Record` | Logged, `500`; entry kept in memory until restart |
| `history.json` missing at startup | Empty history, no log noise |
| `history.json` corrupt at startup | Logged; empty history; overwritten on next play |

## Testing

TDD for every piece.

### `history_test.go`

- Record then `Entries` returns newest first.
- Recording an existing ID moves it to the top without duplicating it.
- The list is capped at the limit; the oldest entry drops.
- Save and reload round-trip in `t.TempDir()`.
- `Load` with a missing file: no error, empty history.
- `Load` with a corrupt file: error, empty history.
- `Record` into a directory that doesn't exist returns an error and still keeps
  the entry in memory.
- No temp file left in the directory after a successful write.
- `Entries` returns a copy: mutating it doesn't change the history.
- Concurrent `Record` calls are clean under `-race`.

### `handlers_test.go`

- `POST` with a known ID → `204`, and the video is in the history.
- `POST` with an unknown ID → `400`, history unchanged.
- `POST` with no ID → `400`.
- `GET` renders entries newest first, with no `data-seed`.
- `GET` with an empty history renders the empty message.
- `testTemplate` gains the history page (and any partials the tests need).

### Browser: `browser-tests/history.js`

Same technique as `return-to-grid.js`: the live origin with the deployed
`app.js`, `style.css` and `iframe_api` aborted, the working tree's files
injected, a synthetic DOM, and a stubbed `YT.Player` whose `getCurrentTime` the
script controls. `page.route` intercepts `POST /history` and counts requests.
Verdict fields are booleans, all of which must be `true`.

- No report while current time is below 10 seconds.
- Exactly one report, carrying the tapped video's ID, once it reaches 10
  seconds; still one after several more ticks.
- A Back press at 3 seconds sends no report, and the next video reports
  normally.
- The wall's corner button is `position: fixed`, and while the player is up the
  element at its centre lies inside the player container (the click-shield),
  not the button.
- A seedless grid (the history page's shape) plays a tile and returns to the
  same tiles, without ever requesting `/videos`.

### Repro harness

`repro_harness_test.go` registers both history routes against a
`WatchHistory` in a temp directory, so the whole flow can be clicked through
locally at `127.0.0.1:8431`.

### After deploy

One real check on production: play a video past 10 seconds and see it at the
top of `/history`. This writes one real entry into the family history.

## Documentation

- README: add `history.go` and the `/history` routes to the Architecture list,
  and `history.js` to the browser-tests table.
- `CLAUDE.md` project structure: add `history.go`. (The global gitignore keeps
  `CLAUDE.md` untracked, so this is a local-only edit.)

## Decisions

- **Server storage over `localStorage`.** The child watches on several devices
  and the list must follow. Costs a writable file on the VPS; the "no database"
  principle holds, since it is a single small JSON file.
- **Separate page over an overlay.** An overlay keeps the wall in place but
  needs open/close handling, play-from-panel wiring and, above all, Android
  Back: with no URL change, Back leaves the site entirely, so it would need
  `history.pushState` handling layered on fullscreen and backgrounding code
  that has already caused two bugs. A page gets correct Back behaviour for
  free and reuses playback untouched. Cost: returning to the wall reshuffles
  it. An overlay can still be added later if that cost bites.
- **10-second threshold.** Filters mistaken taps (the reason the instant Back
  exit exists) while still catching a video sampled briefly.
- **Validate against the cache.** Keeps the list inside the curated pool on a
  site with no login.
