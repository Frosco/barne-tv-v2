# Channel-Share Cap on the Video Grid

## Problem

The video cache pools all videos from all configured sources into a single
slice and shuffles it for each grid render. A source with many videos
therefore dominates the grid in proportion to its size — one channel with
hundreds of uploads crowds out smaller sources.

## Goal

Cap the number of videos any single source can contribute to one grid render,
so smaller sources get reliable representation without starving the grid when
sources are sparse.

## Approach

Tag every cached video with the ID of the config source it came from, then
replace the single-pool shuffle with a capped two-pass selection:

1. **Pass A — capped fill:** for each source, take up to `capPerSource`
   randomly chosen videos.
2. **Pass B — relaxed fill:** if Pass A produced fewer than `gridSize`
   videos, top up from the leftover tails ignoring the cap.
3. Final shuffle so cap-respecting picks and overflow picks are interleaved.

The cap is `max(1, gridSize / 5)` — currently 6 with `gridSize = 30`.

The cap is treated as an aesthetic preference, not a hard invariant. When
the math can't satisfy the cap (fewer than ~5 sources, or some sources too
small), the grid stays full at the cost of letting some source exceed the
cap.

## Data model

`Video` gains a `SourceID` field:

```go
type Video struct {
    ID           string
    Title        string
    ThumbnailURL string
    SourceID     string // ID of the config Source that produced this video
}
```

`VideoCache.RefreshAll` already iterates `cfg.Sources`. When it appends each
source's videos, it stamps `SourceID = src.ID` on every Video before they
land in the cache.

Source identity is `Source.ID` from config (channel ID or playlist ID),
which is stable. `Source.Name` is a human label and is not used as a
bucket key. Each entry in `sources:` is its own bucket; if the same channel
appears twice in config it becomes two buckets, which is harmless.

## Selection algorithm

Replace `VideoCache.Random(n)` with:

```go
func (c *VideoCache) RandomCapped(n, capPerSource int) []Video
```

`handlers.go` is the only caller. The handler computes
`cap := max(1, h.GridSize / 5)` and passes both values.

Algorithm:

1. Snapshot the cached videos under the read lock.
2. Group videos by `SourceID` into per-source slices and shuffle each
   slice independently (within-source randomness is preserved).
3. **Pass A:** for each source, take the first `min(capPerSource, len(slice))`
   videos from its shuffled slice. Append them to `result`. Track the
   leftover tail of each slice.
4. **Pass B:** if `len(result) < n`, concatenate all leftover tails,
   shuffle that combined pool, and append until `len(result) == n` or the
   pool is empty.
5. Shuffle `result` once more so source A's overflow doesn't all sit at
   the end of the grid.
6. If `len(result) > n`, truncate to `n`.

## Edge cases

- **Empty cache** — return `nil`, matching today's behavior.
- **Single source** — Pass A takes `cap` videos; Pass B fills the rest from
  the same source's tail. End state is essentially identical to today.
- **Source smaller than `cap`** — take what it has; its tail is empty and
  contributes nothing to Pass B.
- **Total videos < `n`** — return everything available (matches today's
  truncation behavior in `Random`).
- **`gridSize < 5`** — `gridSize / 5` rounds to 0; the `max(1, …)` guard
  raises the cap to 1.

## Out of scope

- **Cross-source duplicates.** If the same `VideoID` appears in two
  different sources (e.g., a video that lives in both a channel and a
  playlist that are both configured), it can still appear twice on the
  same grid. Pre-existing behavior; not addressed here.
- **Configurable cap.** The `1/5` ratio is hardcoded. If a future
  configuration knob is needed, it can be added without changing the
  algorithm.
- **Cookie format.** The `grid` cookie stores video IDs only. Existing
  cookies remain valid; no migration needed.

## Testing

Following TDD. Existing tests need adjustment for the renamed method and
the new `SourceID` field; new tests cover the cap behavior.

**Updated existing tests:**

- `TestVideoCacheRefreshAll` — additionally asserts every returned video
  carries the correct `SourceID` matching the source it was loaded from.
- `TestVideoCacheRandomSelection`, `TestVideoCacheRandomSelectionMoreThanPool`,
  `TestVideoCacheRandomSelectionEmpty` — adapt to the `RandomCapped`
  signature.
- All `TestGridHandler*` tests — adjust for the new cache method.

**New tests in `cache_test.go`:**

1. `TestRandomCappedRespectsCap` — pool with one dominant source (100
   videos from A, 10 each from B–E). `RandomCapped(30, 6)` returns at most
   6 videos from source A.
2. `TestRandomCappedRelaxesWhenUnderFilled` — two sources of 50 videos
   each. `RandomCapped(30, 6)` returns exactly 30 videos and at least one
   source contributes more than 6, proving Pass B engaged.
3. `TestRandomCappedSingleSource` — one source with 100 videos.
   `RandomCapped(30, 6)` returns 30 videos, all from that source.
4. `TestRandomCappedFewerVideosThanGrid` — 5 videos total in cache.
   `RandomCapped(30, 6)` returns 5 videos.
5. `TestRandomCappedEmpty` — empty cache returns `nil`.
6. `TestRandomCappedDistributesAcrossSources` — five sources with 20
   videos each. `RandomCapped(30, 6)` returns 30 videos with each source
   contributing exactly 6.

The interleaving from the final shuffle is not asserted directly — the
property is just "result is shuffled," which is implicit and not worth
a brittle test.
