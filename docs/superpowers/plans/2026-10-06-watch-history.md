# Watch History Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A server-kept list of the 50 most recently watched videos, opened from a corner button on the wall, where any entry plays exactly like a wall tile.

**Architecture:** A `WatchHistory` store persists `[]Video` to a JSON file with atomic writes. `POST /history` records a play (cache-validated), `GET /history` renders a seedless grid page from shared template partials. `app.js` reports a play once it has played 10 seconds (or ended), and a fixed corner button links wall and history page.

**Tech Stack:** Go 1.24 stdlib (`net/http` method routing, `html/template`, `encoding/json`), vanilla ES5 JS, CSS, Playwright browser scripts.

**Spec:** `docs/superpowers/specs/2026-10-06-watch-history-design.md`

## Global Constraints

- No new dependencies. `gopkg.in/yaml.v3` stays the only one.
- History limit `50`; report threshold `10` seconds; flag `-history`, default `history.json`.
- Exact Norwegian copy: heading `Sett før`; wall button `aria-label="Sett før"`; history button `aria-label="Tilbake til alle videoer"`; empty line `Ingen videoer sett ennå`; history page `<title>Barne-TV – sett før</title>`.
- `static/app.js` is ES5 inside one IIFE: `var`, `function`, no arrow functions, no `let`/`const`. Match its comment style (explain *why*).
- `go test -race ./...` must pass with pristine output; expected log lines are captured and asserted, never left on stderr.
- Tests must never write into the production history: browser scripts stub `fetch` or route `/history`.
- Commit messages follow the repo's `feat:`/`test:`/`docs:` style and end with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- `CLAUDE.md` is untracked by the global gitignore: edit it, never `git add` it.

## Review Focus

1. **A clip shorter than 10 seconds watched to its end** should still count. Pinned in Task 5 (case D: `ENDED` reports once, never twice).
2. **The wall after the template split into partials** must keep its seed, sentinel, player and scripts. Pinned in Task 3 (`TestGridHandlerWithRealTemplates`).
3. **The first play after a corrupt `history.json`** must leave a valid file holding that play. Pinned in Task 1 (`TestWatchHistoryRecordAfterCorruptLoadReplacesFile`).
4. **Titles with `<`, `&` or quotes** must render escaped on the history page. Pinned in Task 3 (`TestHistoryHandlerEscapesTitles`).
5. **A watched video that later drops out of the cache** must still be listed. Pinned in Task 4 (`TestServeMuxRoutes`).

---

### Task 1: The history store

**Files:**
- Create: `history.go`
- Test: `history_test.go`

**Interfaces:**
- Consumes: `Video` (`youtube.go`).
- Produces:
  - `func NewWatchHistory(path string, limit int) *WatchHistory`
  - `func (h *WatchHistory) Load() error`
  - `func (h *WatchHistory) Record(v Video) error`
  - `func (h *WatchHistory) Entries() []Video` (newest first, a copy)
  - Test helpers in `history_test.go`, used by later tasks: `vid(id string) Video`, `ids([]Video) []string`.

- [ ] **Step 1: Write the failing tests** in `history_test.go`

```go
func vid(id string) Video {
	return Video{ID: id, Title: "Title " + id, ThumbnailURL: "http://img/" + id, SourceID: "S1"}
}

func ids(videos []Video) []string {
	out := make([]string, len(videos))
	for i, v := range videos {
		out[i] = v.ID
	}
	return out
}

func newTestHistory(t *testing.T, limit int) (*WatchHistory, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "history.json")
	return NewWatchHistory(path, limit), path
}

func record(t *testing.T, h *WatchHistory, videos ...Video) {
	t.Helper()
	for _, v := range videos {
		if err := h.Record(v); err != nil {
			t.Fatalf("Record(%s): %v", v.ID, err)
		}
	}
}
```

Tests (all assert with `slices.Equal` / `reflect.DeepEqual`):

| Test | Setup | Assert |
|---|---|---|
| `TestWatchHistoryRecordsNewestFirst` | limit 50; record v1, v2, v3 | `ids(Entries()) == [v3 v2 v1]` |
| `TestWatchHistoryRepeatMovesToTop` | record v1, v2, v3, then v1 with `Title = "Renamed"` | ids `[v1 v3 v2]`; `Entries()[0].Title == "Renamed"` |
| `TestWatchHistoryCapsAtLimit` | limit 3; record v1..v5 | ids `[v5 v4 v3]` |
| `TestWatchHistorySurvivesReload` | record v1, v2; `NewWatchHistory(path, 50).Load()` | no error; `Entries() == []Video{vid("v2"), vid("v1")}` |
| `TestWatchHistoryLoadMissingFile` | `Load()` with no file | no error; `len(Entries()) == 0` |
| `TestWatchHistoryLoadCorruptFile` | write `not json` to path; `Load()` | error non-nil; `len(Entries()) == 0` |
| `TestWatchHistoryRecordAfterCorruptLoadReplacesFile` | corrupt file; `Load()`; record v1; reload from path | reload error nil; ids `[v1]` |
| `TestWatchHistoryWriteFailureKeepsEntryInMemory` | path `<tmp>/missing-dir/history.json`; record v1 | `Record` returns non-nil; ids `[v1]` |
| `TestWatchHistoryLeavesNoTempFiles` | record v1, v2; `os.ReadDir(dir)` | exactly one entry, named `history.json` |
| `TestWatchHistoryEntriesIsACopy` | record v1; `e := Entries(); e[0].ID = "x"` | `Entries()[0].ID == "v1"` |
| `TestWatchHistoryConcurrentRecords` | 20 goroutines each `Record(vid(fmt.Sprintf("v%d", i)))`, `t.Error` on failure | `len(Entries()) == 20`; reload from path also has 20 |

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -race -run TestWatchHistory ./...`
Expected: build failure, `undefined: NewWatchHistory`.

- [ ] **Step 3: Implement `history.go`**

```go
type WatchHistory struct {
	mu      sync.Mutex
	path    string
	limit   int
	entries []Video // newest first
}
```

- `Load`: `os.ReadFile`. `errors.Is(err, fs.ErrNotExist)` returns nil and leaves the history empty. Any other read error, or a `json.Unmarshal` error, sets `entries` to nil and returns the error wrapped with the path.
- `Record`: under `mu`, drop any entry with `v.ID`, prepend `v`, trim to `limit`, then `save()` and return its error. The in-memory update stands even when `save` fails.
- `save` (unexported, caller holds `mu`): `json.MarshalIndent(entries, "", "  ")` so the file is readable over ssh; `os.CreateTemp(filepath.Dir(path), ".history-*.json")`; write; close; `os.Rename` over `path`. On any error after `CreateTemp`, `os.Remove` the temp file.
- `Entries`: `slices.Clone` under `mu`.
- Doc comment on the type: what it is and why it lives in a file (survives restarts, shared across devices).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race -run TestWatchHistory ./...`
Expected: `ok`, no other output.

- [ ] **Step 5: Commit**

```bash
git add history.go history_test.go
git commit -m "feat: add a persisted watch history store"
```

---

### Task 2: Recording a play — `POST /history`

**Files:**
- Modify: `handlers.go` (append the handler)
- Modify: `cache_test.go` (add `captureLogs` next to `silenceLogs`)
- Test: `handlers_test.go`

**Interfaces:**
- Consumes: `WatchHistory.Record`, `WatchHistory.Entries` (Task 1); `VideoCache.GetByIDs` (`cache.go`); `testCache()` (`handlers_test.go`).
- Produces:
  - `type RecordPlayHandler struct { Cache *VideoCache; History *WatchHistory }`
  - `func captureLogs(t *testing.T) *bytes.Buffer` (redirects `log` output into the buffer, restores it on cleanup)

- [ ] **Step 1: Write the failing tests** in `handlers_test.go`

```go
func postPlay(handler http.Handler, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/history", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}
```

- `TestRecordPlayHandlerRecordsKnownVideo`: history in `t.TempDir()`; `postPlay(handler, "id=v3")` → `w.Code == 204`; `history.Entries()` deep-equals `[]Video{{ID: "v3", Title: "Three", ThumbnailURL: "http://img/3", SourceID: "S2"}}`.
- `TestRecordPlayHandlerRejectsUnknownOrMissingID`: for each body in `"id=nope"`, `"id="`, `""` → `w.Code == 400`; afterwards `len(history.Entries()) == 0`.
- `TestRecordPlayHandlerReportsWriteFailure`: `logs := captureLogs(t)`; history path `<tmp>/missing-dir/history.json`; `postPlay(handler, "id=v1")` → `w.Code == 500`; `strings.Contains(logs.String(), "recording play of v1")`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -race -run TestRecordPlayHandler ./...`
Expected: build failure, `undefined: RecordPlayHandler`.

- [ ] **Step 3: Implement `RecordPlayHandler.ServeHTTP`**

Read `r.FormValue("id")`, then look it up with `h.Cache.GetByIDs([]string{id})`. An empty ID or a nil result gets `http.Error(..., 400)`. A `Record` error is logged as `log.Printf("recording play of %s: %v", id, err)` and answered with a 500. Success is `w.WriteHeader(http.StatusNoContent)`. The doc comment says why only cached IDs are accepted: the history can't be filled from outside the configured sources.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race ./...`
Expected: `ok`, no other output.

- [ ] **Step 5: Commit**

```bash
git add handlers.go handlers_test.go cache_test.go
git commit -m "feat: record watched videos via POST /history"
```

---

### Task 3: The history page and shared template partials

**Files:**
- Create: `templates/partials.html`, `templates/history.html`
- Modify: `templates/index.html`, `handlers.go`, `main.go` (template parsing only), `repro_harness_test.go` (template parsing only)
- Test: `handlers_test.go`

**Interfaces:**
- Consumes: `WatchHistory.Entries` (Task 1); `vid`, `ids` (Task 1 tests); the `cells` template.
- Produces:
  - `func parseTemplates() (*template.Template, error)` in `handlers.go`
  - `type HistoryHandler struct { History *WatchHistory; Template *template.Template }`
  - Template names `head`, `player` (in `partials.html`), `history.html`; its data is `[]Video`.
  - Markup hooks for Task 5: `a.corner-button`, `h1.history-heading`, `p.history-empty`.

The history-page tests run against the real template files through `parseTemplates()`. The spec suggested extending the stub `testTemplate` instead, but asserting the empty-state line against a stub would only test the stub.

- [ ] **Step 1: Write the failing tests** in `handlers_test.go`

```go
func realTemplates(t *testing.T) *template.Template {
	t.Helper()
	tmpl, err := parseTemplates()
	if err != nil {
		t.Fatalf("parsing templates: %v", err)
	}
	return tmpl
}
```

- `TestGridHandlerWithRealTemplates`: `GridHandler{Cache: testCache(), Template: realTemplates(t), PageSize: 9}` → status 200, 9 × `data-video-id=`, and the body contains each of:
  - `<title>Barne-TV</title>`
  - `<meta name="robots" content="noindex, nofollow">`
  - `href="/history" aria-label="Sett før"`
  - `data-seed="`
  - `id="scroll-sentinel"`
  - `id="player-container"`
  - `https://www.youtube.com/iframe_api`
  - `/static/app.js`
- `TestHistoryHandlerRendersNewestFirst`: record `vid("v1")`, `vid("v2")`, `vid("v3")`; GET `/history` → status 200, `Content-Type` `text/html; charset=utf-8`. The positions of `data-video-id="v3"` < `"v2"` < `"v1"` are all ≥ 0. The body has no `data-seed` and contains each of:
  - `<title>Barne-TV – sett før</title>`
  - `<h1 class="history-heading">Sett før</h1>`
  - `href="/" aria-label="Tilbake til alle videoer"`
  - `id="player-container"`
  - `/static/app.js`
- `TestHistoryHandlerEmptyHistory`: no records → body contains `<p class="history-empty">Ingen videoer sett ennå</p>` and 0 × `data-video-id=`.
- `TestHistoryHandlerEscapesTitles`: record `v` with `v.Title = "<b>Tom & \"Jerry\"</b>"` → body contains `&lt;b&gt;Tom &amp;` and does not contain `<b>Tom`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -race -run 'TestHistoryHandler|TestGridHandlerWithRealTemplates' ./...`
Expected: build failure, `undefined: parseTemplates`.

- [ ] **Step 3: Write the templates**

`templates/partials.html` defines two templates, moved verbatim from today's `index.html`:
- `head`: the charset, viewport and robots metas plus the stylesheet link. Not `<title>`, which stays per page.
- `player`: `#player-container` with its inner `#player`, the `iframe_api` script and the `/static/app.js` script.

`templates/index.html`: `<head>` becomes `{{template "head"}}` followed by `<title>Barne-TV</title>`. The first child of `<body>` is the wall button, then the existing grid and sentinel unchanged, and `{{template "player"}}` last:

```html
<a class="corner-button" href="/history" aria-label="Sett før"><svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/></svg></a>
```

`templates/history.html`: the same skeleton, with `<title>Barne-TV – sett før</title>`. The body holds the back button, the heading, either the grid or the empty line, and `{{template "player"}}`:

```html
<a class="corner-button" href="/" aria-label="Tilbake til alle videoer"><svg viewBox="0 0 24 24" aria-hidden="true"><rect x="4" y="4" width="7" height="7" rx="1.5"/><rect x="13" y="4" width="7" height="7" rx="1.5"/><rect x="4" y="13" width="7" height="7" rx="1.5"/><rect x="13" y="13" width="7" height="7" rx="1.5"/></svg></a>
<h1 class="history-heading">Sett før</h1>
{{if .}}<div class="grid">
    {{template "cells" .}}
</div>{{else}}<p class="history-empty">Ingen videoer sett ennå</p>{{end}}
```

The grid deliberately has no `data-seed`, `data-next-offset` or `#scroll-sentinel`: that keeps `app.js` infinite scroll off. Put this in a template comment.

- [ ] **Step 4: Implement `parseTemplates` and `HistoryHandler`**

`parseTemplates` returns `template.ParseFiles("templates/index.html", "templates/history.html", "templates/cells.html", "templates/partials.html")`. Add a comment that `index.html` must stay first, because `GridHandler` calls `Template.Execute`, which runs the set's first file.

`HistoryHandler.ServeHTTP` mirrors `GridHandler`: execute `"history.html"` with `h.History.Entries()` into a buffer, return 500 on a template error, and otherwise set `Content-Type` and write the buffer.

Replace the `template.ParseFiles(...)` calls in `main.go` and `repro_harness_test.go` with `parseTemplates()`, keeping their existing error handling. Without this the wall can't find its partials.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test -race ./... && go vet -tags repro ./...`
Expected: `ok`, no other output; vet silent.

- [ ] **Step 6: Commit**

```bash
git add templates/ handlers.go handlers_test.go main.go repro_harness_test.go
git commit -m "feat: add the watch history page, sharing head and player partials with the wall"
```

---

### Task 4: Wire the routes and the history file

**Files:**
- Modify: `main.go`, `repro_harness_test.go`, `.gitignore`, `README.md`, `CLAUDE.md` (local only)
- Create: `main_test.go`

**Interfaces:**
- Consumes: `NewWatchHistory`, `Load` (Task 1); `RecordPlayHandler` (Task 2); `HistoryHandler`, `parseTemplates`, `realTemplates` (Task 3).
- Produces: `func newServeMux(cache *VideoCache, history *WatchHistory, tmpl *template.Template) *http.ServeMux` in `main.go`, plus `const historyLimit = 50`.

`newServeMux` exists because `main.go` and the repro harness register the same routes today, and both would otherwise have to gain the history routes separately. Sharing it also keeps the harness serving exactly what production serves, and makes the routing testable.

- [ ] **Step 1: Write the failing test** in `main_test.go`

`TestServeMuxRoutes`, with `cache := testCache()`, a history in `t.TempDir()`, and `mux := newServeMux(cache, history, realTemplates(t))`:
- `postPlay(mux, "id=v3")` (Task 2's helper; the mux is an `http.Handler`) → 204.
- `cache.Store([]Video{{ID: "v1", SourceID: "S1"}})`, so a source refresh has dropped v3.
- `GET /history` → 200; body contains `data-video-id="v3"` and `Sett før`.
- `GET /` → 200; body contains `id="scroll-sentinel"`.
- `GET /videos?seed=1&offset=0&count=1` → 200, 1 × `data-video-id=`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -race -run TestServeMuxRoutes ./...`
Expected: build failure, `undefined: newServeMux`.

- [ ] **Step 3: Implement `newServeMux` and rewire `main`**

`newServeMux` owns the `pageSize = 30` and `MaxCount: 60` values that currently sit in `main` (keep the existing MaxCount comment) and registers these routes:
- `/` → grid
- `/videos` → videos
- `POST /history` → `RecordPlayHandler`
- `GET /history` → `HistoryHandler`
- `/static/` → the file server

In `main`:
- Add `historyPath := flag.String("history", "history.json", "path to the watch history file")`.
- After the cache is set up, call `history := NewWatchHistory(*historyPath, historyLimit)`. On a `Load()` error, log `watch history unreadable, starting empty: %v` and carry on.
- End with `log.Fatal(http.ListenAndServe(*addr, newServeMux(cache, history, tmpl)))`.

In `repro_harness_test.go`, build the mux with `newServeMux(cache, NewWatchHistory(filepath.Join(t.TempDir(), "history.json"), historyLimit), tmpl)`, then add its `/thumb.svg` route on top.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go build ./... && go vet -tags repro ./... && go test -race ./...`
Expected: all silent except `ok`.

- [ ] **Step 5: Ignore the local history file and document**

- `.gitignore`: add `history.json`.
- `README.md` Architecture:
  - add `history.go`: watch history, the 50 most recent distinct videos, persisted to `history.json` (`-history` flag).
  - extend the handlers bullet with `RecordPlayHandler` (`POST /history`) and `HistoryHandler` (`GET /history`).
  - extend the templates bullet with `history.html` and the shared `partials.html`.
- `CLAUDE.md` project structure: add a `history.go` line. Edit only; it is gitignored.

- [ ] **Step 6: Commit**

```bash
git add main.go main_test.go repro_harness_test.go .gitignore README.md
git commit -m "feat: serve the watch history routes and keep history in history.json"
```

---

### Task 5: Report plays from the browser; style the corner button

**Files:**
- Modify: `static/app.js`, `static/style.css`, `browser-tests/real-playback.js`, `README.md`
- Create: `browser-tests/history.js`

**Interfaces:**
- Consumes: `POST /history` with form field `id` (Task 2); the markup hooks `a.corner-button`, `h1.history-heading`, `p.history-empty` (Task 3).
- Produces: nothing later tasks depend on.

- [ ] **Step 1: Write the failing browser test** `browser-tests/history.js`

Start from `return-to-grid.js`'s rig, which uses the live origin with the deployed `app.js`, `style.css` and `iframe_api` aborted and the local files injected. These scripts can't import, so the rig is copied. Differences:
- `setup({ seeded })`. With `seeded: true` it builds the wall shape: a grid with `data-seed="12345"`, `data-next-offset="60"`, a sentinel, and `<a class="corner-button" href="/history" aria-label="Sett før">`. With `seeded: false` it builds the history shape: `h1.history-heading`, a grid with 12 cells and no `data-*`, no sentinel, and the back button with `href="/"`.
- The stub player's `getCurrentTime` returns `window.__spy.currentTime`, which the script sets.
- `window.fetch` stays a stub, but records every call in `window.__fetches` as `{ url, method: (opts && opts.method) || 'GET', body: opts && opts.body ? String(opts.body) : '' }`. Because `fetch` is stubbed, `page.route` would never see the report, so the spy is the only way to observe it. It also guarantees nothing reaches production.
- Let `reports` be the `__fetches` entries with `url === '/history' && method === 'POST'`.

Cases and verdicts (each must be `true`):

| Case | Script | Verdicts |
|---|---|---|
| A threshold | wall; `currentTime = 9.5`; open v20; wait 2500 ms; then `currentTime = 10`; wait 1500; wait 2500 more | `noReportBelowThreshold`: 0 reports after the first wait. `oneReportAtThreshold`: 1 after the second. `reportCarriesId`: `body === 'id=v20'`. `stillOneAfterMoreTicks`: 1 after the third. |
| B back-out early | wall; `currentTime = 3`; open v20; wait 1500; `exitFullscreen()`; wait 500; then `currentTime = 12`; open v7; wait 1500 | `backOutNotReported`: 0 reports before v7. `nextVideoReported`: exactly 1 report, `id=v7`. |
| C after a reported video | wall; `currentTime = 12`; open v20; wait 1500; `exitFullscreen()`; open v7; wait 1500 | `bothReported`: 2 reports, bodies `id=v20` then `id=v7`. |
| D ends early | wall; `currentTime = 8`; open v20; wait 300; `onStateChange({data: ENDED})`; wait 2500; then `currentTime = 12`; open v7; wait 1500; `ENDED`; wait 2500 | `shortClipReportedOnEnd`: 1 report `id=v20` after the first ENDED. `noDoubleReportOnEnd`: exactly 1 report for v7. |
| E corner button | wall; measure `a.corner-button`; `scrollTo(0, 800)`; measure; open v20; wait 300; `elementFromPoint` at button centre | `fixedPosition`: computed `position === 'fixed'`. `reachableBeforePlay`: the button contains `elementFromPoint`. `staysInCornerWhenScrolled`: rect top unchanged after scroll. `coveredDuringPlay`: `#player-container` contains the element at the centre. |
| F seedless grid | history shape; wait 1000; `scrollTo(0, 99999)`; wait 1000; open v3; wait 300; `exitFullscreen()`; wait 500 | `playedTile`: `__spy.created === 1`. `returnedToSameTiles`: same first 5 IDs and cell count. `neverFetchedVideos`: no `__fetches` URL contains `/videos`. |

Return `JSON.stringify(results, null, 2)`, as the other scripts do.

- [ ] **Step 2: Run it to verify it fails**

Run via Playwright MCP `browser_run_code_unsafe` with `filename: browser-tests/history.js`.
Expected: A `oneReportAtThreshold`, B `nextVideoReported`, C, D and E `fixedPosition` are `false`. F already passes, since it pins existing behaviour the page now relies on. If anything else fails, rebuild the rig before believing it (see the frontend-verification-harness memory).

- [ ] **Step 3: Implement the report in `static/app.js`**

- Constant next to `END_PAUSE_MS`: `var REPORT_AFTER_S = 10;`, commented: a mistaken tap backed out of straight away shouldn't push a real favourite down the history.
- State next to `wasBackgrounded`: `var playingVideoId = null;` and `var playReported = false;`.
- The click handler sets `playingVideoId = videoId` when it creates the player.
- Rename `timeLeftTimer` to `tickTimer`. `setInterval(tick, 1000)`, where `function tick()` calls `updateTimeLeft()`, then calls `reportWatched()` once `player.getCurrentTime() >= REPORT_AFTER_S`, behind the same `typeof ... === "function"` guard `updateTimeLeft` uses.
- `function reportWatched()`: returns if `playReported` or `!playingVideoId`; otherwise sets `playReported = true` and sends `fetch("/history", { method: "POST", body: new URLSearchParams({ id: playingVideoId }) }).catch(function () {});`. Comment: nothing waits on it; a failure only costs this video its place in the list.
- `onPlayerStateChange` on `ENDED`: call `reportWatched()` before `returnToGrid(END_PAUSE_MS)`, with a comment that a clip shorter than the threshold still counts once it has played to the end. The order matters, because `returnToGrid` clears the ID.
- `returnToGrid`: reset `playingVideoId = null` and `playReported = false` beside the `wasBackgrounded` reset. Extend that comment to say the next video starts with a clean slate on all three.

- [ ] **Step 4: Style the corner button and history page in `static/style.css`**

New `/* === Corner button === */` section:
- `.corner-button`: `position: fixed; top: 12px; right: 12px; z-index: 10`. The comment says it stays below the player's `100`, so a video covers it. `width`/`height: 44px; border-radius: 50%`, flex-centred, `background: var(--cell-bg); color: var(--title-color); opacity: 0.5`, the cells' soft shadow, and an opacity transition.
- `.corner-button:hover`, `.corner-button:focus-visible`: `opacity: 1`.
- `.corner-button svg`: `24px` square, `fill: none; stroke: currentColor; stroke-width: 2; stroke-linecap: round; stroke-linejoin: round`.

New `/* === History page === */` section: `.history-heading` and `.history-empty` in the page font and colour. Size and spacing are up to the implementer. The heading sits above the grid, so match the grid's `max-width` and `width: 100%`. The empty line is centred and comfortably below the heading.

- [ ] **Step 5: Run the browser tests to verify**

Run `browser-tests/history.js`, then `return-to-grid.js` and `player-touch.js` (whose stubbed `fetch` now also receives the report), the same way as in Step 2.
Expected: every verdict `true` in all three.

Visual check: in the `history.js` rig, `page.screenshot` both shapes to an absolute path under `.playwright-mcp/`, then Read the screenshots. Confirm the button is legible but dim, doesn't hide a tile's title, and that the heading and empty line look like the rest of the site. Delete `.playwright-mcp/` afterwards.

- [ ] **Step 6: Guard the real-playback script and document**

- `browser-tests/real-playback.js`: after its existing routes, add `await page.route('**/history', r => r.abort());` with the comment `// Never write test plays into the family's real history.`. Its playback is shorter than 10 s today; this keeps that true if it grows.
- Re-run `real-playback.js`. Expected: every verdict `true`.
- `README.md` browser-tests table: add a `history.js` row. It guards that a video is reported once after 10 s of play or at its end and never after an early back-out, that the corner button stays fixed and the player covers it, and that a seedless grid plays and returns without loading more videos.

- [ ] **Step 7: Commit**

```bash
git add static/app.js static/style.css browser-tests/history.js browser-tests/real-playback.js README.md
git commit -m "feat: report watched videos and add the history corner button"
```

---

### Task 6: Finish, deploy, check on production

**Files:** none new.

- [ ] **Step 1: Full verification**

Run: `go build ./... && go vet ./... && go vet -tags repro ./... && go test -race ./...`
Expected: silent except `ok`. All four browser scripts were green in Task 5.

- [ ] **Step 2: Review and integrate**

Use superpowers:requesting-code-review on the branch, address findings, then superpowers:finishing-a-development-branch.

- [ ] **Step 3: Deploy, only after Nils says go**

Deploying is outward-facing: ask first. Then run `./deploy.sh`.

- [ ] **Step 4: Production check** (writes one real entry, as agreed in the spec)

With Playwright on `https://refsnes-barnetv.no/`, tap a tile and confirm its playback passes 10 s. If autoplay was blocked, tap the player once to play. Wait 12 s, leave the video, open `/history`, and check that the first tile's `data-video-id` is the tapped one. If headless playback won't start, ask Nils to do the same check on a real device.
