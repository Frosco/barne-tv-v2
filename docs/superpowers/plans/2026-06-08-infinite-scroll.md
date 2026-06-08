# Infinite Scroll Front Page — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the fixed 30-video grid + shuffle button with an endless, looping, downward-scrolling video wall.

**Architecture:** A page load mints a random `uint64` seed; the server renders the first screenful and embeds the seed. As the user scrolls, JS fetches more screenfuls from a new `/videos` endpoint, which re-derives a deterministic *fair ordering* of the whole pool from the seed and returns an HTML fragment. Looping is server-side global-offset arithmetic (each loop reseeds for a fresh order). All ordering/fairness/looping logic stays in Go; the browser handles scroll detection, appending, DOM pruning, and playback.

**Tech Stack:** Go 1.24 (`math/rand/v2`, `html/template`, `net/http`), vanilla JS (`IntersectionObserver`, `fetch`), CSS grid. No new dependencies.

**Spec:** `docs/superpowers/specs/2026-06-08-infinite-scroll-design.md`

---

## File structure

| File | Responsibility | Change |
|------|----------------|--------|
| `cache.go` | Pool storage + `FairOrder` (whole-pool fair ordering) + `Page` (seeded, looping pagination). `RandomCapped` removed. | Modify |
| `cache_test.go` | Tests for `FairOrder` + `Page`. `RandomCapped` tests removed. | Modify |
| `handlers.go` | `GridHandler` (index page, seed embed) + new `VideosHandler` (`/videos` fragment). Cookie + `?shuffle=` removed. | Modify |
| `handlers_test.go` | Tests for both handlers. Cookie tests removed. | Modify |
| `main.go` | Parse `index.html` + `cells.html`; wire `/` and `/videos`. | Modify |
| `templates/index.html` | Page shell; embeds seed + next offset; renders first page via shared `cells` template; sentinel div; shuffle button removed. | Modify |
| `templates/cells.html` | `{{define "cells"}}` — the single source of `.grid-cell` markup. | Create |
| `static/app.js` | Infinite scroll (observer/fetch/append/prune) + playback (always returns to a fresh feed). Shuffle handler removed. | Modify |
| `static/style.css` | Repeating accent palette, `content-visibility` on cells, shuffle-button styles removed. | Modify |

**Build-green ordering:** Tasks 1–2 are additive. Task 3 switches handlers/main/templates over to `Page` (after which `RandomCapped` is dead but still compiles). Task 4 deletes the dead `RandomCapped`. Tasks 5–6 update static assets (no effect on `go build`/`go test`). The app is only fully coherent in a browser after Task 6; automated build+tests stay green at every commit.

---

## Task 1: `FairOrder` — whole-pool fair ordering

**Files:**
- Modify: `cache.go`
- Test: `cache_test.go`

`FairOrder` is a pure function: group by source, shuffle source order and each group with the provided RNG, then round-robin interleave so every prefix is source-diverse.

- [ ] **Step 1: Write the failing tests**

Add to `cache_test.go` (the file already imports `fmt` and `testing`; add `"math/rand/v2"` to its import block):

```go
func TestFairOrderEmpty(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 0))
	if got := FairOrder(nil, rng); got != nil {
		t.Errorf("FairOrder(nil) = %+v, want nil", got)
	}
	if got := FairOrder([]Video{}, rng); len(got) != 0 {
		t.Errorf("FairOrder(empty) len = %d, want 0", len(got))
	}
}

func TestFairOrderIsPermutation(t *testing.T) {
	videos := make([]Video, 100)
	for i := range videos {
		videos[i] = Video{ID: fmt.Sprintf("v%d", i), SourceID: "S1"}
	}
	got := FairOrder(videos, rand.New(rand.NewPCG(7, 0)))

	if len(got) != 100 {
		t.Fatalf("len = %d, want 100", len(got))
	}
	seen := map[string]bool{}
	for _, v := range got {
		if seen[v.ID] {
			t.Fatalf("duplicate ID %q", v.ID)
		}
		seen[v.ID] = true
	}
	// Single source must be shuffled, not returned in input order.
	identical := true
	for i := range got {
		if got[i].ID != videos[i].ID {
			identical = false
			break
		}
	}
	if identical {
		t.Error("FairOrder returned single source in input order; expected a shuffle")
	}
}

func TestFairOrderDeterministic(t *testing.T) {
	videos := []Video{
		{ID: "a1", SourceID: "A"}, {ID: "a2", SourceID: "A"},
		{ID: "b1", SourceID: "B"}, {ID: "b2", SourceID: "B"},
		{ID: "c1", SourceID: "C"},
	}
	first := FairOrder(videos, rand.New(rand.NewPCG(42, 0)))
	second := FairOrder(videos, rand.New(rand.NewPCG(42, 0)))

	if len(first) != len(second) {
		t.Fatalf("lengths differ: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i].ID != second[i].ID {
			t.Errorf("position %d differs: %q vs %q (same seed must be deterministic)", i, first[i].ID, second[i].ID)
		}
	}
}

func TestFairOrderDistinctAcrossCycles(t *testing.T) {
	videos := make([]Video, 0, 140)
	for _, src := range []string{"A", "B", "C", "D", "E"} {
		for i := range 28 {
			videos = append(videos, Video{ID: fmt.Sprintf("%s%d", src, i), SourceID: src})
		}
	}
	cycle0 := FairOrder(videos, rand.New(rand.NewPCG(42, 0)))
	cycle1 := FairOrder(videos, rand.New(rand.NewPCG(42, 1)))

	identical := true
	for i := range cycle0 {
		if cycle0[i].ID != cycle1[i].ID {
			identical = false
			break
		}
	}
	if identical {
		t.Error("different cycle seeds produced identical order; loops would repeat")
	}
}

func TestFairOrderPrefixDiversity(t *testing.T) {
	// One dominant source (A=100) and four small ones (10 each).
	var videos []Video
	for i := range 100 {
		videos = append(videos, Video{ID: fmt.Sprintf("a%d", i), SourceID: "A"})
	}
	for _, src := range []string{"B", "C", "D", "E"} {
		for i := range 10 {
			videos = append(videos, Video{ID: fmt.Sprintf("%s%d", src, i), SourceID: src})
		}
	}
	got := FairOrder(videos, rand.New(rand.NewPCG(3, 0)))

	// First 25 = 5 full round-robin rounds over 5 sources => exactly 5 each.
	counts := map[string]int{}
	for _, v := range got[:25] {
		counts[v.SourceID]++
	}
	for _, src := range []string{"A", "B", "C", "D", "E"} {
		if counts[src] != 5 {
			t.Errorf("source %s contributed %d of first 25, want exactly 5 (round-robin)", src, counts[src])
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -run TestFairOrder ./...`
Expected: compile error / FAIL — `undefined: FairOrder`.

- [ ] **Step 3: Implement `FairOrder`**

In `cache.go`, add `"sort"` to the import block. Add the function (place it where `RandomCapped` is — you'll delete `RandomCapped` in Task 4, so for now add `FairOrder` directly above it):

```go
// FairOrder returns every video in a single ordering whose every prefix is
// spread across sources as evenly as the pool allows. Sources are visited
// round-robin (in a seed-shuffled order, each source's videos seed-shuffled),
// so no early screenful is dominated by one source; a dominant source's
// overflow necessarily trails at the end. Deterministic for a given rng.
func FairOrder(videos []Video, rng *rand.Rand) []Video {
	if len(videos) == 0 {
		return nil
	}

	bySource := map[string][]Video{}
	for _, v := range videos {
		bySource[v.SourceID] = append(bySource[v.SourceID], v)
	}

	// Deterministic source order: sort keys (map iteration is random), then
	// shuffle that order with the seeded rng.
	keys := make([]string, 0, len(bySource))
	for k := range bySource {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	rng.Shuffle(len(keys), func(i, j int) { keys[i], keys[j] = keys[j], keys[i] })

	for _, k := range keys {
		group := bySource[k]
		rng.Shuffle(len(group), func(i, j int) { group[i], group[j] = group[j], group[i] })
	}

	result := make([]Video, 0, len(videos))
	idx := make(map[string]int, len(keys))
	for remaining := len(videos); remaining > 0; {
		for _, k := range keys {
			group := bySource[k]
			if idx[k] < len(group) {
				result = append(result, group[idx[k]])
				idx[k]++
				remaining--
			}
		}
	}
	return result
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test -run TestFairOrder ./...`
Expected: PASS (all 5 tests).

- [ ] **Step 5: Commit**

```bash
git add cache.go cache_test.go
git commit -m "feat: add FairOrder prefix-fair whole-pool ordering"
```

---

## Task 2: `Page` — seeded, looping pagination

**Files:**
- Modify: `cache.go`
- Test: `cache_test.go`

`Page` maps a global feed offset onto looping cycles of `FairOrder`, returning exactly `count` videos for a non-empty pool (looping fills short pools), or `nil` when empty.

- [ ] **Step 1: Write the failing tests**

Add to `cache_test.go`:

```go
func TestPageEmpty(t *testing.T) {
	cache := &VideoCache{}
	if got := cache.Page(1, 0, 30); got != nil {
		t.Errorf("Page on empty cache = %+v, want nil", got)
	}
}

func TestPageReturnsCount(t *testing.T) {
	cache := &VideoCache{}
	var videos []Video
	for _, src := range []string{"A", "B", "C"} {
		for i := range 20 {
			videos = append(videos, Video{ID: fmt.Sprintf("%s%d", src, i), SourceID: src})
		}
	}
	cache.Store(videos)

	if got := cache.Page(1, 0, 30); len(got) != 30 {
		t.Errorf("len = %d, want 30", len(got))
	}
}

func TestPageConsecutiveOffsetsAreDisjoint(t *testing.T) {
	cache := &VideoCache{}
	videos := make([]Video, 100)
	for i := range videos {
		videos[i] = Video{ID: fmt.Sprintf("v%d", i), SourceID: "S1"}
	}
	cache.Store(videos)

	// Within one cycle (pool size 100), offsets 0..29 and 30..59 must not overlap.
	p1 := cache.Page(9, 0, 30)
	p2 := cache.Page(9, 30, 30)

	seen := map[string]bool{}
	for _, v := range p1 {
		seen[v.ID] = true
	}
	for _, v := range p2 {
		if seen[v.ID] {
			t.Errorf("video %q appeared in both consecutive pages within a cycle", v.ID)
		}
	}
}

func TestPageLoopsWithFreshOrder(t *testing.T) {
	cache := &VideoCache{}
	videos := make([]Video, 10)
	for i := range videos {
		videos[i] = Video{ID: fmt.Sprintf("v%d", i), SourceID: "S1"}
	}
	cache.Store(videos)

	cycle0 := cache.Page(5, 0, 10)  // local 0..9 of cycle 0
	cycle1 := cache.Page(5, 10, 10) // local 0..9 of cycle 1

	if len(cycle0) != 10 || len(cycle1) != 10 {
		t.Fatalf("lengths = %d, %d, want 10, 10", len(cycle0), len(cycle1))
	}
	identical := true
	for i := range cycle0 {
		if cycle0[i].ID != cycle1[i].ID {
			identical = false
			break
		}
	}
	if identical {
		t.Error("cycle 1 identical to cycle 0; loops should reshuffle")
	}
}

func TestPageFillsShortPoolByLooping(t *testing.T) {
	cache := &VideoCache{}
	cache.Store([]Video{
		{ID: "v1", SourceID: "S1"},
		{ID: "v2", SourceID: "S1"},
		{ID: "v3", SourceID: "S2"},
	})
	if got := cache.Page(1, 0, 30); len(got) != 30 {
		t.Errorf("len = %d, want 30 (short pool loops to fill count)", len(got))
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -run TestPage ./...`
Expected: compile error / FAIL — `cache.Page undefined`.

- [ ] **Step 3: Implement `Page`**

In `cache.go`, add directly below `FairOrder`:

```go
// Page returns count videos starting at the global feed offset, looping over
// the pool with a freshly reshuffled FairOrder each cycle. The browser only
// tracks a growing offset; cycle/wrap math lives here. Returns nil if the pool
// is empty; otherwise always returns exactly count videos.
func (c *VideoCache) Page(seed uint64, offset, count int) []Video {
	c.mu.RLock()
	m := len(c.videos)
	videos := make([]Video, m)
	copy(videos, c.videos)
	c.mu.RUnlock()

	if m == 0 {
		return nil
	}

	result := make([]Video, 0, count)
	curCycle := -1
	var ordering []Video
	for i := 0; i < count; i++ {
		g := offset + i
		cycle := g / m
		local := g % m
		if cycle != curCycle {
			ordering = FairOrder(videos, rand.New(rand.NewPCG(seed, uint64(cycle))))
			curCycle = cycle
		}
		result = append(result, ordering[local])
	}
	return result
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test -run TestPage ./...`
Expected: PASS (all 5 tests).

- [ ] **Step 5: Commit**

```bash
git add cache.go cache_test.go
git commit -m "feat: add VideoCache.Page seeded looping pagination"
```

---

## Task 3: Switch handlers, main, and templates to `Page`

**Files:**
- Modify: `handlers.go`
- Modify: `handlers_test.go`
- Modify: `main.go`
- Modify: `templates/index.html`
- Create: `templates/cells.html`

This is the atomic switchover: index page mints a seed and renders the first page via `Page`; a new `/videos` handler serves HTML fragments; the cookie and `?shuffle=` logic are gone.

- [ ] **Step 1: Rewrite the handler tests (failing)**

Replace the entire contents of `handlers_test.go` with:

```go
package main

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// testTemplate mirrors main.go's parse of index.html + cells.html: a template
// named "index.html" with an associated "cells" template.
func testTemplate(t *testing.T) *template.Template {
	t.Helper()
	tmpl := template.Must(template.New("index.html").Parse(
		`<div class="grid" data-seed="{{.Seed}}" data-next-offset="{{.NextOffset}}">{{template "cells" .Videos}}</div>`))
	template.Must(tmpl.New("cells").Parse(
		`{{range .}}<div class="grid-cell" data-video-id="{{.ID}}">{{.Title}}</div>{{end}}`))
	return tmpl
}

func testCache() *VideoCache {
	cache := &VideoCache{}
	cache.Store([]Video{
		{ID: "v1", Title: "One", ThumbnailURL: "http://img/1", SourceID: "S1"},
		{ID: "v2", Title: "Two", ThumbnailURL: "http://img/2", SourceID: "S1"},
		{ID: "v3", Title: "Three", ThumbnailURL: "http://img/3", SourceID: "S2"},
		{ID: "v4", Title: "Four", ThumbnailURL: "http://img/4", SourceID: "S2"},
		{ID: "v5", Title: "Five", ThumbnailURL: "http://img/5", SourceID: "S3"},
		{ID: "v6", Title: "Six", ThumbnailURL: "http://img/6", SourceID: "S3"},
		{ID: "v7", Title: "Seven", ThumbnailURL: "http://img/7", SourceID: "S1"},
		{ID: "v8", Title: "Eight", ThumbnailURL: "http://img/8", SourceID: "S2"},
		{ID: "v9", Title: "Nine", ThumbnailURL: "http://img/9", SourceID: "S3"},
	})
	return cache
}

func TestGridHandlerServesFirstPage(t *testing.T) {
	handler := &GridHandler{Cache: testCache(), Template: testTemplate(t), PageSize: 9}

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if got := strings.Count(body, "data-video-id="); got != 9 {
		t.Errorf("found %d videos, want 9", got)
	}
	if !strings.Contains(body, `data-next-offset="9"`) {
		t.Errorf("missing data-next-offset=\"9\" in %q", body)
	}
	if strings.Contains(body, `data-seed=""`) {
		t.Error("data-seed is empty; expected a generated seed")
	}
}

func TestGridHandlerEmptyCache(t *testing.T) {
	handler := &GridHandler{Cache: &VideoCache{}, Template: testTemplate(t), PageSize: 9}

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if got := strings.Count(body, "data-video-id="); got != 0 {
		t.Errorf("found %d videos, want 0", got)
	}
	if !strings.Contains(body, `data-next-offset="0"`) {
		t.Errorf("missing data-next-offset=\"0\" in %q", body)
	}
}

func TestVideosHandlerReturnsCells(t *testing.T) {
	handler := &VideosHandler{Cache: testCache(), Template: testTemplate(t), PageSize: 9, MaxCount: 60}

	req := httptest.NewRequest("GET", "/videos?seed=1&offset=0&count=5", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if got := strings.Count(w.Body.String(), "data-video-id="); got != 5 {
		t.Errorf("found %d cells, want 5", got)
	}
}

func TestVideosHandlerDefaultsCount(t *testing.T) {
	handler := &VideosHandler{Cache: testCache(), Template: testTemplate(t), PageSize: 9, MaxCount: 60}

	req := httptest.NewRequest("GET", "/videos?seed=1&offset=0", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if got := strings.Count(w.Body.String(), "data-video-id="); got != 9 {
		t.Errorf("found %d cells, want 9 (PageSize default)", got)
	}
}

func TestVideosHandlerLoopsToFillCount(t *testing.T) {
	cache := &VideoCache{}
	cache.Store([]Video{{ID: "v1", SourceID: "S1"}, {ID: "v2", SourceID: "S1"}, {ID: "v3", SourceID: "S2"}})
	handler := &VideosHandler{Cache: cache, Template: testTemplate(t), PageSize: 9, MaxCount: 60}

	req := httptest.NewRequest("GET", "/videos?seed=1&offset=0&count=10", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if got := strings.Count(w.Body.String(), "data-video-id="); got != 10 {
		t.Errorf("found %d cells, want 10 (short pool loops)", got)
	}
}

func TestVideosHandlerEmptyPool(t *testing.T) {
	handler := &VideosHandler{Cache: &VideoCache{}, Template: testTemplate(t), PageSize: 9, MaxCount: 60}

	req := httptest.NewRequest("GET", "/videos?seed=1&offset=0&count=5", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if got := strings.Count(w.Body.String(), "data-video-id="); got != 0 {
		t.Errorf("found %d cells, want 0 (empty pool)", got)
	}
}

func TestVideosHandlerValidatesParams(t *testing.T) {
	handler := &VideosHandler{Cache: testCache(), Template: testTemplate(t), PageSize: 9, MaxCount: 60}

	cases := []string{
		"/videos?offset=0&count=5",          // missing seed
		"/videos?seed=abc&offset=0&count=5", // non-numeric seed
		"/videos?seed=1&offset=-1&count=5",  // negative offset
		"/videos?seed=1&offset=x&count=5",   // non-numeric offset
		"/videos?seed=1&offset=0&count=0",   // count below 1
		"/videos?seed=1&offset=0&count=999", // count above MaxCount
	}
	for _, url := range cases {
		req := httptest.NewRequest("GET", url, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", url, w.Code)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -run 'TestGridHandler|TestVideosHandler' ./...`
Expected: compile errors — `GridHandler` has no `PageSize`, `VideosHandler` undefined.

- [ ] **Step 3: Rewrite `handlers.go`**

Replace the entire contents of `handlers.go` with:

```go
package main

import (
	"html/template"
	"math/rand/v2"
	"net/http"
	"strconv"
)

// GridHandler renders the front page: a fresh fair ordering seeded per request,
// with the first screenful server-rendered and the seed + next offset embedded
// for the client's infinite scroll.
type GridHandler struct {
	Cache    *VideoCache
	Template *template.Template
	PageSize int
}

type indexData struct {
	Videos     []Video
	Seed       uint64
	NextOffset int
}

func (h *GridHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	seed := rand.Uint64()
	videos := h.Cache.Page(seed, 0, h.PageSize)

	data := indexData{
		Videos:     videos,
		Seed:       seed,
		NextOffset: len(videos),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.Template.Execute(w, data); err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

// VideosHandler serves successive screenfuls as an HTML fragment of grid cells,
// identified by (seed, offset). It re-derives the same fair ordering from the
// seed and slices it, looping past the end of the pool.
type VideosHandler struct {
	Cache    *VideoCache
	Template *template.Template
	PageSize int
	MaxCount int
}

func (h *VideosHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	seed, err := strconv.ParseUint(q.Get("seed"), 10, 64)
	if err != nil {
		http.Error(w, "invalid seed", http.StatusBadRequest)
		return
	}

	offset, err := strconv.Atoi(q.Get("offset"))
	if err != nil || offset < 0 {
		http.Error(w, "invalid offset", http.StatusBadRequest)
		return
	}

	count := h.PageSize
	if raw := q.Get("count"); raw != "" {
		count, err = strconv.Atoi(raw)
		if err != nil || count < 1 || count > h.MaxCount {
			http.Error(w, "invalid count", http.StatusBadRequest)
			return
		}
	}

	videos := h.Cache.Page(seed, offset, count)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.Template.ExecuteTemplate(w, "cells", videos); err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}
```

- [ ] **Step 4: Create `templates/cells.html`**

```html
{{define "cells"}}{{range .}}<div class="grid-cell" data-video-id="{{.ID}}">
    <img src="{{.ThumbnailURL}}" alt="{{.Title}}" loading="lazy">
    <span class="title">{{.Title}}</span>
</div>
{{end}}{{end}}
```

- [ ] **Step 5: Rewrite `templates/index.html`**

Replace the entire contents with (note: data attributes on `.grid`, the shared `cells` template, the scroll sentinel, and no shuffle button):

```html
<!DOCTYPE html>
<html lang="nb">
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <meta name="robots" content="noindex, nofollow">
    <title>Barne-TV</title>
    <link rel="stylesheet" href="/static/style.css">
</head>
<body>
    <div class="grid" data-seed="{{.Seed}}" data-next-offset="{{.NextOffset}}">
        {{template "cells" .Videos}}
    </div>
    <div id="scroll-sentinel" aria-hidden="true"></div>

    <div id="player-container" class="player-container" hidden>
        <div id="player"></div>
    </div>

    <script src="https://www.youtube.com/iframe_api"></script>
    <script src="/static/app.js"></script>
</body>
</html>
```

- [ ] **Step 6: Update `main.go`**

In `main.go`, change the template parse line and the handler wiring. Replace:

```go
	tmpl, err := template.ParseFiles("templates/index.html")
	if err != nil {
		log.Fatalf("parsing template: %v", err)
	}

	handler := &GridHandler{Cache: cache, Template: tmpl, GridSize: 30}

	http.Handle("/", handler)
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
```

with:

```go
	tmpl, err := template.ParseFiles("templates/index.html", "templates/cells.html")
	if err != nil {
		log.Fatalf("parsing templates: %v", err)
	}

	const pageSize = 30
	grid := &GridHandler{Cache: cache, Template: tmpl, PageSize: pageSize}
	videos := &VideosHandler{Cache: cache, Template: tmpl, PageSize: pageSize, MaxCount: 60}

	http.Handle("/", grid)
	http.Handle("/videos", videos)
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
```

- [ ] **Step 7: Run build and the full test suite**

Run: `go build ./... && go test -race ./...`
Expected: build succeeds; all tests PASS. (`RandomCapped` is now unused but still compiles — its tests still pass; Task 4 removes it.)

- [ ] **Step 8: Commit**

```bash
git add handlers.go handlers_test.go main.go templates/index.html templates/cells.html
git commit -m "feat: serve looping infinite-scroll feed via seed + /videos endpoint"
```

---

## Task 4: Remove the dead `RandomCapped`

**Files:**
- Modify: `cache.go`
- Modify: `cache_test.go`

`RandomCapped` has no callers after Task 3. Delete it and its tests.

- [ ] **Step 1: Delete `RandomCapped` from `cache.go`**

Remove the entire `RandomCapped` method (the doc comment block plus `func (c *VideoCache) RandomCapped(n, capPerSource int) []Video { … }`). Leave `FairOrder`, `Page`, `Store`, `GetByIDs`, `RefreshAll`, `StartPeriodicRefresh` intact.

- [ ] **Step 2: Delete the `RandomCapped` tests from `cache_test.go`**

Remove these five test functions: `TestRandomCappedEmpty`, `TestRandomCappedFewerVideosThanGrid`, `TestRandomCappedSingleSource`, `TestRandomCappedRespectsCap`, `TestRandomCappedDistributesAcrossSources`, `TestRandomCappedRelaxesWhenUnderFilled`. (Keep `TestVideoCacheGetByIDs`, `TestVideoCacheGetByIDsMissing`, `TestVideoCacheRefreshAll`, `TestVideoCacheRefreshAllAllFail`, `TestStartPeriodicRefresh`, and all `TestFairOrder*` / `TestPage*` tests.)

- [ ] **Step 3: Verify the `fmt` import is still used in `cache_test.go`**

`TestFairOrder*` and `TestPage*` use `fmt.Sprintf`, so the `fmt` import stays. If `go vet` reports any now-unused import, remove it.

- [ ] **Step 4: Run build and tests**

Run: `go build ./... && go test -race ./...`
Expected: build succeeds; all tests PASS; no "declared and not used" or unused-import errors.

- [ ] **Step 5: Commit**

```bash
git add cache.go cache_test.go
git commit -m "refactor: remove RandomCapped, superseded by FairOrder"
```

---

## Task 5: CSS — repeating palette, content-visibility, remove shuffle button

**Files:**
- Modify: `static/style.css`

No automated test (CSS). Verify by reading; full browser check happens in Task 8.

- [ ] **Step 1: Replace the per-cell accent block**

Replace the 30 lines `​.grid-cell:nth-child(1) { … }` through `.grid-cell:nth-child(30) { … }` with a repeating 10-color palette using modulo selectors:

```css
/* Accent colors and entrance stagger repeat every 10 cells, so appended
   tiles past the first screenful stay styled and the stagger stays bounded. */
.grid-cell:nth-child(10n+1) { --accent: #FF6B6B; animation-delay: 0s; }
.grid-cell:nth-child(10n+2) { --accent: #4ECDC4; animation-delay: 0.04s; }
.grid-cell:nth-child(10n+3) { --accent: #FFD93D; animation-delay: 0.08s; }
.grid-cell:nth-child(10n+4) { --accent: #A78BFA; animation-delay: 0.12s; }
.grid-cell:nth-child(10n+5) { --accent: #6BCB77; animation-delay: 0.16s; }
.grid-cell:nth-child(10n+6) { --accent: #FFB347; animation-delay: 0.20s; }
.grid-cell:nth-child(10n+7) { --accent: #FF85A1; animation-delay: 0.24s; }
.grid-cell:nth-child(10n+8) { --accent: #45B7D1; animation-delay: 0.28s; }
.grid-cell:nth-child(10n+9) { --accent: #96E6A1; animation-delay: 0.32s; }
.grid-cell:nth-child(10n)   { --accent: #F9A8D4; animation-delay: 0.36s; }
```

- [ ] **Step 2: Add `content-visibility` to `.grid-cell`**

In the `.grid-cell { … }` rule, add these two declarations (keeps a long, pruned feed cheap to render; `auto` height reserves space for off-screen tiles so the scrollbar stays stable):

```css
    content-visibility: auto;
    contain-intrinsic-size: auto 210px;
```

- [ ] **Step 3: Remove the shuffle button styles**

Delete these three blocks entirely:
- The `/* === Shuffle Button === */` section: `.shuffle-btn`, `.shuffle-btn:hover`, `.shuffle-btn:active`.
- The `@keyframes bob { … }` block.
- Inside `@media (max-width: 600px)`: the `.shuffle-btn { … }` and `.shuffle-btn svg { … }` rules.
- Inside `@media (min-width: 1400px)`: the `.shuffle-btn { … }` and `.shuffle-btn svg { … }` rules.

(Leave the `.grid`, `.grid-cell`, `.title`, `.player-container`, `popIn`, and the `--shuffle-bg` variable definition alone — the variable is harmless if left, but you may delete `--shuffle-bg` from `:root` since nothing references it now.)

- [ ] **Step 4: Commit**

```bash
git add static/style.css
git commit -m "feat: repeating tile palette and content-visibility for endless grid; drop shuffle button styles"
```

---

## Task 6: `app.js` — infinite scroll + fresh-feed playback

**Files:**
- Modify: `static/app.js`

No automated test (no JS harness in this project). Verify in Task 8.

- [ ] **Step 1: Replace the entire contents of `static/app.js`**

```js
(function () {
    "use strict";

    var ytReady = false;
    var player = null;
    var playerContainer = document.getElementById("player-container");
    var grid = document.querySelector(".grid");
    var sentinel = document.getElementById("scroll-sentinel");

    var seed = grid ? grid.getAttribute("data-seed") : "";
    var nextOffset = grid ? parseInt(grid.getAttribute("data-next-offset"), 10) || 0 : 0;

    var PAGE_SIZE = 30;
    var MAX_TILES = 240;   // sliding-window cap on rendered cells
    var ROOT_MARGIN = 600; // px before the sentinel enters view to start loading

    var loading = false;
    var exhausted = false;

    // YouTube IFrame API ready callback
    window.onYouTubeIframeAPIReady = function () {
        ytReady = true;
    };

    // ---- Infinite scroll ----

    function columnCount() {
        var cols = getComputedStyle(grid).gridTemplateColumns.split(" ").length;
        return cols > 0 ? cols : 1;
    }

    // Keep the rendered cell count bounded. Remove whole rows from the top and
    // compensate scrollTop by their measured height so the viewport doesn't jump.
    function pruneTop() {
        var cells = grid.querySelectorAll(".grid-cell");
        var excess = cells.length - MAX_TILES;
        if (excess <= 0) return;

        var cols = columnCount();
        var removeCount = Math.floor(excess / cols) * cols;
        if (removeCount <= 0) return;

        var topBefore = cells[0].getBoundingClientRect().top;
        var topAfter = cells[removeCount].getBoundingClientRect().top;
        var removedHeight = topAfter - topBefore;

        for (var i = 0; i < removeCount; i++) {
            grid.removeChild(cells[i]);
        }
        window.scrollBy(0, -removedHeight);
    }

    function loadMore() {
        if (loading || exhausted || !grid || !seed) return;
        loading = true;

        var url = "/videos?seed=" + encodeURIComponent(seed) +
                  "&offset=" + nextOffset + "&count=" + PAGE_SIZE;

        fetch(url)
            .then(function (resp) { return resp.text(); })
            .then(function (html) {
                if (html.trim() === "") {
                    exhausted = true; // only happens when the pool is empty
                    if (observer) observer.disconnect();
                    loading = false;
                    return;
                }
                grid.insertAdjacentHTML("beforeend", html);
                nextOffset += PAGE_SIZE;
                pruneTop();
                loading = false;
                // Keep filling if the sentinel is still within reach (short
                // screens, fast scrolls). IntersectionObserver alone won't
                // re-fire while the sentinel stays continuously visible.
                checkSentinel();
            })
            .catch(function () { loading = false; });
    }

    function checkSentinel() {
        if (loading || exhausted || !sentinel) return;
        if (sentinel.getBoundingClientRect().top < window.innerHeight + ROOT_MARGIN) {
            loadMore();
        }
    }

    var observer = null;
    if (grid && sentinel && seed) {
        observer = new IntersectionObserver(function (entries) {
            if (entries[0].isIntersecting) loadMore();
        }, { rootMargin: ROOT_MARGIN + "px" });
        observer.observe(sentinel);
        // Initial fill in case the first page doesn't reach the sentinel.
        checkSentinel();
    }

    // ---- Playback ----

    grid.addEventListener("click", function (e) {
        var cell = e.target.closest(".grid-cell");
        if (!cell || !ytReady) return;

        var videoId = cell.getAttribute("data-video-id");
        if (!videoId || player) return;

        grid.hidden = true;
        playerContainer.hidden = false;

        player = new YT.Player("player", {
            videoId: videoId,
            playerVars: { autoplay: 1, rel: 0, modestbranding: 1 },
            events: { onStateChange: onPlayerStateChange },
        });

        playerContainer.requestFullscreen().catch(function () {
            // Fullscreen may be blocked by browser; video still plays
        });
    });

    function onPlayerStateChange(event) {
        if (event.data === YT.PlayerState.ENDED) {
            returnToGrid();
        }
    }

    // Tear down the player and return to a fresh feed at the top. Both natural
    // end and user exit (Escape / leaving fullscreen) lead here.
    function returnToGrid() {
        if (!player) return;

        // Destroy immediately to hide YouTube's end-screen recommendations.
        player.destroy();
        player = null;

        var div = document.createElement("div");
        div.id = "player";
        playerContainer.innerHTML = "";
        playerContainer.appendChild(div);

        // Brief pause on black, then reload for a brand-new shuffled feed.
        setTimeout(function () {
            if (document.fullscreenElement) {
                document.exitFullscreen();
            }
            window.location = "/";
        }, 1500);
    }

    document.addEventListener("fullscreenchange", function () {
        if (!document.fullscreenElement && player) {
            returnToGrid();
        }
    });
})();
```

- [ ] **Step 2: Sanity-check the build still passes**

Run: `go build ./... && go test -race ./...`
Expected: PASS (no Go changes, but confirms nothing regressed).

- [ ] **Step 3: Commit**

```bash
git add static/app.js
git commit -m "feat: infinite-scroll loading with DOM pruning; fresh feed after playback"
```

---

## Task 7: Verify the full test suite and vet

**Files:** none (verification only)

- [ ] **Step 1: Run the complete suite with race detector**

Run: `go test -race ./...`
Expected: `ok  barne-tv-v2` — all tests pass.

- [ ] **Step 2: Vet**

Run: `go vet ./...`
Expected: no output (clean).

- [ ] **Step 3: Confirm `RandomCapped` and the cookie are fully gone**

Run: `grep -rn "RandomCapped\|grid\"\|SetCookie\|shuffle" *.go static/ templates/`
Expected: no matches for `RandomCapped`, the `grid` cookie, `SetCookie`, or `shuffle`. (If any remain, they are stragglers to remove.)

---

## Task 8: Manual browser verification

**Files:** none (manual). Requires a real `config.yaml` with a YouTube API key.

- [ ] **Step 1: Run the app**

Run: `go run .`
Open: `http://localhost:8080`

- [ ] **Step 2: Verify infinite scroll**

Scroll down. New rows of thumbnails load continuously before you hit the bottom. There is no shuffle button. Open DevTools → Console: no errors.

- [ ] **Step 3: Verify looping**

Keep scrolling past the full pool size (or temporarily point `config.yaml` at a single small playlist). The feed never ends — videos reappear in a fresh order rather than stopping.

- [ ] **Step 4: Verify DOM pruning**

Scroll a long way. In DevTools → Elements, confirm `.grid` keeps roughly `MAX_TILES` (~240) `.grid-cell` children, not thousands. Scrolling stays smooth; the viewport does not jump when old rows are pruned.

- [ ] **Step 5: Verify fresh-feed-after-playback**

Tap a thumbnail → it plays fullscreen. Let it end (or press Escape). After the ~1.5 s black pause, the page returns to the top with a new shuffled feed.

- [ ] **Step 6: Verify responsive layout**

Narrow the window below 600px: grid drops to 2 columns and still scrolls/loads. Widen above 1400px: larger tiles, still works.

---

## Follow-ups (after merge)

- Update `docs/solutions/design-patterns/capped-fair-share-sampler-2026-04-26.md` and `docs/superpowers/specs/2026-04-26-channel-share-cap-design.md` to note that `RandomCapped` was generalized into the prefix-fair *ordering* `FairOrder` for pagination. Run `/ce-compound` to capture the prefix-fairness-under-pagination decision as a new learning. (`docs/solutions/` is local-only per global gitignore.)
- Update `README.md`: the architecture bullet still says "shuffled 3x3 grid, persisted in a cookie" — change it to describe the seeded looping infinite-scroll feed and the `/videos` endpoint.

---

## Spec coverage check

| Spec section | Task |
|--------------|------|
| `FairOrder` round-robin, deterministic, prefix-diverse | Task 1 |
| Looping via `(seed, offset)` cycle math (`Page`) | Task 2 |
| `/videos` HTML-fragment endpoint + validation | Task 3 |
| First page server-rendered with embedded seed | Task 3 |
| `grid` cookie + `?shuffle=` removed | Task 3, Task 7 |
| `RandomCapped` removed | Task 4 |
| Repeating accent palette + `content-visibility` | Task 5 |
| Shuffle button removed (template/CSS/JS) | Task 3, 5, 6 |
| Infinite scroll (observer, fetch, append) | Task 6 |
| DOM pruning with scroll compensation | Task 6 |
| Fresh feed on every playback exit | Task 6 |
| Empty pool → empty fragment → observer stops | Task 2, 3, 6 |
| Short pool loops to fill `count` | Task 2, 3 |
| Bad params → 400 | Task 3 |
| README architecture note | Follow-ups |
