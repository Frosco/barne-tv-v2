# Channel-Share Cap Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Cap any single config source's contribution to the grid at `max(1, gridSize/5)` videos, with best-effort relaxation when sources are sparse.

**Architecture:** Tag every cached video with its config `Source.ID` at refresh time. Replace `VideoCache.Random(n)` with `RandomCapped(n, capPerSource)` that does a two-pass selection: Pass A takes up to the cap from each source, Pass B tops up from leftovers if needed.

**Tech Stack:** Go 1.x stdlib (`math/rand/v2`, `sync`), `gopkg.in/yaml.v3`. No new dependencies.

**Spec:** [docs/superpowers/specs/2026-04-26-channel-share-cap-design.md](../specs/2026-04-26-channel-share-cap-design.md)

---

## File Structure

| File | Change |
|---|---|
| `youtube.go` | Add `SourceID string` field to `Video` struct |
| `cache.go` | Tag `SourceID` in `RefreshAll`; replace `Random` with `RandomCapped` |
| `handlers.go` | Compute cap, call `RandomCapped` instead of `Random` |
| `cache_test.go` | Update `TestVideoCacheRefreshAll` to assert tagging; replace 3 `Random` tests with 6 `RandomCapped` tests |
| `handlers_test.go` | No changes (existing tests pass through relaxation) |

---

## Task 1: Tag videos with their SourceID at refresh time

**Files:**
- Modify: `youtube.go` (Video struct, around line 10)
- Modify: `cache.go` (RefreshAll, around line 67)
- Modify: `cache_test.go` (TestVideoCacheRefreshAll, around line 108)

- [ ] **Step 1: Update `TestVideoCacheRefreshAll` to assert SourceID is set**

Replace the existing test body (lines ~108-143) with:

```go
func TestVideoCacheRefreshAll(t *testing.T) {
	silenceLogs(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/channels":
			w.Write([]byte(`{"items": [{"contentDetails": {"relatedPlaylists": {"uploads": "UU1"}}}]}`))
		case "/playlistItems":
			playlistID := r.URL.Query().Get("playlistId")
			switch playlistID {
			case "UU1":
				w.Write([]byte(`{"items": [{"snippet": {"title": "Chan Vid", "resourceId": {"videoId": "cv1"}, "thumbnails": {"high": {"url": "http://img/cv1"}}}}]}`))
			case "PL1":
				w.Write([]byte(`{"items": [{"snippet": {"title": "PL Vid", "resourceId": {"videoId": "pv1"}, "thumbnails": {"high": {"url": "http://img/pv1"}}}}]}`))
			}
		}
	}))
	defer server.Close()

	yt := &YouTubeClient{APIKey: "key", BaseURL: server.URL, HTTP: server.Client()}
	sources := []Source{
		{Type: "channel", ID: "UC1", Name: "Chan"},
		{Type: "playlist", ID: "PL1", Name: "List"},
	}

	cache := &VideoCache{}
	if err := cache.RefreshAll(yt, sources); err != nil {
		t.Fatalf("RefreshAll: %v", err)
	}

	// Each video should be tagged with the ID of the source that produced it.
	bySource := map[string]string{}
	for _, v := range cache.videos {
		bySource[v.ID] = v.SourceID
	}
	if got := bySource["cv1"]; got != "UC1" {
		t.Errorf("cv1 SourceID = %q, want %q", got, "UC1")
	}
	if got := bySource["pv1"]; got != "PL1" {
		t.Errorf("pv1 SourceID = %q, want %q", got, "PL1")
	}
}
```

Note: this reads `cache.videos` directly (same package). The previous `cache.Random(100)` length check is replaced by the per-video tag check.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run TestVideoCacheRefreshAll ./...`
Expected: FAIL with compile error `v.SourceID undefined (type Video has no field or method SourceID)`.

- [ ] **Step 3: Add `SourceID` field to `Video` struct**

In `youtube.go`, change the `Video` struct (lines 10-14):

```go
type Video struct {
	ID           string
	Title        string
	ThumbnailURL string
	SourceID     string
}
```

- [ ] **Step 4: Tag SourceID in `RefreshAll`**

In `cache.go`, replace the body of the `for _, src := range sources` loop in `RefreshAll` (lines 67-88) with:

```go
	for _, src := range sources {
		var videos []Video
		var err error

		switch src.Type {
		case "channel":
			videos, err = yt.FetchChannelVideos(src.ID)
		case "playlist":
			videos, err = yt.FetchPlaylistVideos(src.ID)
		default:
			log.Printf("unknown source type %q, skipping %s", src.Type, src.Name)
			continue
		}

		if err != nil {
			log.Printf("error fetching %s (%s): %v", src.Name, src.ID, err)
			fetchErrors++
			continue
		}

		for i := range videos {
			videos[i].SourceID = src.ID
		}
		all = append(all, videos...)
	}
```

The only new bit is the three-line tagging loop before `all = append(all, videos...)`.

- [ ] **Step 5: Run all tests to verify they pass**

Run: `go test -race ./...`
Expected: PASS for all tests, including `TestVideoCacheRefreshAll`.

- [ ] **Step 6: Commit**

```bash
git add youtube.go cache.go cache_test.go
git commit -m "feat: tag cached videos with their config source ID"
```

---

## Task 2: Implement `RandomCapped` — empty and small-pool cases

**Files:**
- Modify: `cache.go` (replace `Random` method)
- Modify: `cache_test.go` (delete 3 old Random tests, add 2 new tests)

- [ ] **Step 1: Delete the existing Random tests**

Remove these three test functions from `cache_test.go`:
- `TestVideoCacheRandomSelection` (lines ~20-55)
- `TestVideoCacheRandomSelectionMoreThanPool` (lines ~57-68)
- `TestVideoCacheRandomSelectionEmpty` (lines ~70-76)

They will be replaced by the `RandomCapped` test suite over Tasks 2 and 3.

- [ ] **Step 2: Write `TestRandomCappedEmpty`**

Add to `cache_test.go`:

```go
func TestRandomCappedEmpty(t *testing.T) {
	cache := &VideoCache{}
	if got := cache.RandomCapped(9, 2); got != nil {
		t.Errorf("RandomCapped on empty cache = %+v, want nil", got)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test -run TestRandomCappedEmpty ./...`
Expected: FAIL with compile error `cache.RandomCapped undefined`.

- [ ] **Step 4: Replace `Random` with skeleton `RandomCapped`**

In `cache.go`, replace the entire `Random` method (lines 22-41) with:

```go
// RandomCapped returns up to n videos from the cache, with no single source
// contributing more than capPerSource videos when avoidable. If the cap leaves
// the result smaller than n, it relaxes per-source limits and tops up from
// leftover videos until the result is full or the cache is exhausted.
func (c *VideoCache) RandomCapped(n, capPerSource int) []Video {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if len(c.videos) == 0 {
		return nil
	}

	pool := make([]Video, len(c.videos))
	copy(pool, c.videos)

	rand.Shuffle(len(pool), func(i, j int) {
		pool[i], pool[j] = pool[j], pool[i]
	})

	if n > len(pool) {
		n = len(pool)
	}
	return pool[:n]
}
```

This is the same logic as the old `Random`, just with the new name and signature (cap is ignored for now). It's enough to make the empty-pool test pass.

- [ ] **Step 5: Update the handler call site at the same time**

`handlers.go` is the only remaining caller of `Cache.Random`. Removing `Random` without updating the handler would leave the package un-buildable. Update it now in the same step.

In `handlers.go` (line 32), change:

```go
videos = h.Cache.Random(h.GridSize)
```

to:

```go
perSourceCap := h.GridSize / 5
if perSourceCap < 1 {
	perSourceCap = 1
}
videos = h.Cache.RandomCapped(h.GridSize, perSourceCap)
```

(Local variable name `perSourceCap` rather than `cap` to avoid shadowing the Go built-in.)

- [ ] **Step 6: Run the full test suite**

Run: `go test -race ./...`
Expected: PASS for all tests, including the new `TestRandomCappedEmpty`. Handler tests pass through the single-bucket relaxation: their fixture videos have no `SourceID`, so they share the empty-string source bucket, and Pass B fills the grid as before.

- [ ] **Step 7: Write `TestRandomCappedFewerVideosThanGrid`**

Add to `cache_test.go`:

```go
func TestRandomCappedFewerVideosThanGrid(t *testing.T) {
	cache := &VideoCache{}
	cache.Store([]Video{
		{ID: "v1", SourceID: "S1"},
		{ID: "v2", SourceID: "S1"},
		{ID: "v3", SourceID: "S2"},
	})

	got := cache.RandomCapped(30, 6)
	if len(got) != 3 {
		t.Errorf("len = %d, want 3", len(got))
	}
}
```

- [ ] **Step 8: Run test to verify it passes**

Run: `go test -run TestRandomCappedFewerVideosThanGrid ./...`
Expected: PASS (the skeleton already truncates to `min(n, len(pool))`). This test is a regression guard for Task 3's rewrite, not a red-phase cycle.

- [ ] **Step 9: Commit**

```bash
git add cache.go cache_test.go handlers.go
git commit -m "feat: introduce RandomCapped, replace Random callers"
```

---

## Task 3: Implement the per-source cap and relaxation passes

**Files:**
- Modify: `cache.go` (rewrite `RandomCapped` body)
- Modify: `cache_test.go` (add 4 tests)

- [ ] **Step 1: Write `TestRandomCappedSingleSource`**

Add to `cache_test.go`:

```go
func TestRandomCappedSingleSource(t *testing.T) {
	cache := &VideoCache{}
	videos := make([]Video, 100)
	for i := range videos {
		videos[i] = Video{ID: fmt.Sprintf("v%d", i), SourceID: "S1"}
	}
	cache.Store(videos)

	got := cache.RandomCapped(30, 6)
	if len(got) != 30 {
		t.Fatalf("len = %d, want 30", len(got))
	}
	for _, v := range got {
		if v.SourceID != "S1" {
			t.Errorf("got video with SourceID %q, want S1", v.SourceID)
		}
	}
}
```

(Add `"fmt"` to the imports in `cache_test.go` if not already present.)

- [ ] **Step 2: Run test to verify it passes**

Run: `go test -run TestRandomCappedSingleSource ./...`
Expected: PASS — the skeleton from Task 2 still works because it ignores the cap and just returns a random 30 from the pool of 100.

This test is here as a regression guard for Step 6's rewrite, where it must continue to pass.

- [ ] **Step 3: Write `TestRandomCappedRespectsCap`**

Add to `cache_test.go`:

```go
func TestRandomCappedRespectsCap(t *testing.T) {
	cache := &VideoCache{}
	var videos []Video
	for i := 0; i < 100; i++ {
		videos = append(videos, Video{ID: fmt.Sprintf("a%d", i), SourceID: "A"})
	}
	for _, src := range []string{"B", "C", "D", "E"} {
		for i := 0; i < 10; i++ {
			videos = append(videos, Video{ID: fmt.Sprintf("%s%d", src, i), SourceID: src})
		}
	}
	cache.Store(videos)

	got := cache.RandomCapped(30, 6)
	if len(got) != 30 {
		t.Fatalf("len = %d, want 30", len(got))
	}

	countA := 0
	for _, v := range got {
		if v.SourceID == "A" {
			countA++
		}
	}
	if countA > 6 {
		t.Errorf("source A contributed %d videos, want <= 6", countA)
	}
}
```

- [ ] **Step 4: Run test to verify it fails**

Run: `go test -run TestRandomCappedRespectsCap ./...`
Expected: FAIL — the skeleton ignores the cap, so source A (with 100 videos out of 140 total) will contribute roughly 30 * 100/140 ≈ 21 videos, far exceeding 6.

- [ ] **Step 5: Write `TestRandomCappedDistributesAcrossSources`**

Add to `cache_test.go`:

```go
func TestRandomCappedDistributesAcrossSources(t *testing.T) {
	cache := &VideoCache{}
	var videos []Video
	for _, src := range []string{"A", "B", "C", "D", "E"} {
		for i := 0; i < 20; i++ {
			videos = append(videos, Video{ID: fmt.Sprintf("%s%d", src, i), SourceID: src})
		}
	}
	cache.Store(videos)

	got := cache.RandomCapped(30, 6)
	if len(got) != 30 {
		t.Fatalf("len = %d, want 30", len(got))
	}

	counts := map[string]int{}
	for _, v := range got {
		counts[v.SourceID]++
	}
	for _, src := range []string{"A", "B", "C", "D", "E"} {
		if counts[src] != 6 {
			t.Errorf("source %s contributed %d videos, want exactly 6", src, counts[src])
		}
	}
}
```

- [ ] **Step 6: Write `TestRandomCappedRelaxesWhenUnderFilled`**

Add to `cache_test.go`:

```go
func TestRandomCappedRelaxesWhenUnderFilled(t *testing.T) {
	cache := &VideoCache{}
	var videos []Video
	for _, src := range []string{"A", "B"} {
		for i := 0; i < 50; i++ {
			videos = append(videos, Video{ID: fmt.Sprintf("%s%d", src, i), SourceID: src})
		}
	}
	cache.Store(videos)

	got := cache.RandomCapped(30, 6)
	if len(got) != 30 {
		t.Fatalf("len = %d, want 30", len(got))
	}

	counts := map[string]int{}
	for _, v := range got {
		counts[v.SourceID]++
	}
	if counts["A"] <= 6 && counts["B"] <= 6 {
		t.Errorf("both sources at or under cap (A=%d, B=%d); expected at least one to exceed cap because grid couldn't fill at cap=6 with 2 sources", counts["A"], counts["B"])
	}
}
```

- [ ] **Step 7: Run all four new tests to verify the under-cap ones fail**

Run: `go test -run "TestRandomCapped" ./...`
Expected: `TestRandomCappedEmpty`, `TestRandomCappedFewerVideosThanGrid`, `TestRandomCappedSingleSource` PASS. `TestRandomCappedRespectsCap`, `TestRandomCappedDistributesAcrossSources`, `TestRandomCappedRelaxesWhenUnderFilled` FAIL.

- [ ] **Step 8: Rewrite `RandomCapped` to implement the two-pass algorithm**

In `cache.go`, replace the body of `RandomCapped`:

```go
func (c *VideoCache) RandomCapped(n, capPerSource int) []Video {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if len(c.videos) == 0 {
		return nil
	}

	// Group by SourceID and shuffle each group independently.
	bySource := map[string][]Video{}
	for _, v := range c.videos {
		bySource[v.SourceID] = append(bySource[v.SourceID], v)
	}
	for src := range bySource {
		group := bySource[src]
		rand.Shuffle(len(group), func(i, j int) {
			group[i], group[j] = group[j], group[i]
		})
		bySource[src] = group
	}

	// Pass A: take up to capPerSource from each source.
	var result []Video
	var leftovers []Video
	for _, group := range bySource {
		take := capPerSource
		if take > len(group) {
			take = len(group)
		}
		result = append(result, group[:take]...)
		leftovers = append(leftovers, group[take:]...)
	}

	// Pass B: top up from leftovers if we're under n.
	if len(result) < n && len(leftovers) > 0 {
		rand.Shuffle(len(leftovers), func(i, j int) {
			leftovers[i], leftovers[j] = leftovers[j], leftovers[i]
		})
		need := n - len(result)
		if need > len(leftovers) {
			need = len(leftovers)
		}
		result = append(result, leftovers[:need]...)
	}

	// Final shuffle so overflow doesn't all sit at the end.
	rand.Shuffle(len(result), func(i, j int) {
		result[i], result[j] = result[j], result[i]
	})

	if len(result) > n {
		result = result[:n]
	}
	return result
}
```

- [ ] **Step 9: Run all tests to verify they pass**

Run: `go test -race ./...`
Expected: PASS for all tests in `cache_test.go` and `handlers_test.go`.

- [ ] **Step 10: Commit**

```bash
git add cache.go cache_test.go
git commit -m "feat: cap per-source share on grid, relax to fill"
```

---

## Task 4: Verify the build and run final checks

**Files:** none — verification only.

- [ ] **Step 1: Run `go build ./...`**

Run: `go build ./...`
Expected: builds cleanly with no output.

- [ ] **Step 2: Run the race-enabled test suite one more time**

Run: `go test -race ./...`
Expected: all tests pass.

- [ ] **Step 3: Run `go vet`**

Run: `go vet ./...`
Expected: no findings.

- [ ] **Step 4: Manually sanity-check `handlers.go`**

Read `handlers.go` and confirm:
- The cap calculation `perSourceCap := h.GridSize / 5; if perSourceCap < 1 { perSourceCap = 1 }` is correct.
- The call site is `h.Cache.RandomCapped(h.GridSize, perSourceCap)`.
- No stray references to `Cache.Random` remain anywhere in the codebase.

Run: `grep -rn "\.Random(" .` (excluding `.git`)
Expected: no matches in `.go` files (only `RandomCapped`).

- [ ] **Step 5: Build verified — no commit needed (no code changes in this task)**

If grep or any check found issues, fix them in a small follow-up commit before declaring the work done.

---

## Out-of-scope reminders (do not implement here)

- Cross-source duplicate dedupe.
- Configurable cap value via YAML.
- Cookie format / migration.

If any of these come up during implementation, stop and ask Nils — they were explicitly deferred in the spec.
