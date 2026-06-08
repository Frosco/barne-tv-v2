# Infinite Scroll on the Front Page

## Problem

The front page shows a fixed grid of 30 videos with a shuffle button to draw a
new selection. The primary user (a child on a phone) enjoys the scrolling motion
itself and becomes upset when there is little to scroll through, and does not
understand the shuffle button. The fixed grid both limits how many videos he can
choose from and gives him almost nothing to scroll.

## Goal

Turn the front page into an endless, downward-scrolling wall of thumbnails that
never runs out, so scrolling is the natural way to see more videos and the
shuffle button is no longer needed.

Concretely:

- Scrolling loads more videos continuously.
- When the whole pool is exhausted, the feed reshuffles and continues — it loops
  forever, in a fresh order each cycle, so it can never reach a "nothing more to
  scroll" wall.
- Every fresh page load (initial visit, and the return after any playback or
  back-out) produces a brand-new shuffled feed starting at the top.
- No single screenful is dominated by one source.

## Non-goals / behavior being removed

- **Shuffle button.** Removed from template, CSS, and JS. Scrolling replaces it.
- **`grid` cookie.** Removed. It existed to *preserve* a selection across loads;
  we now want the opposite (a fresh feed every load).
- **`?shuffle=` request handling.** Removed. Every load is already fresh.
- **`RandomCapped`.** Replaced by `FairOrder` (see below). Its behavioral intent
  — per-screenful source diversity — is preserved.

## Approach

A page load mints a random `uint64` seed. The browser server-renders the first
screenful, then fetches further screenfuls from a new endpoint as the user
scrolls, identified by `(seed, offset)`. The server re-derives the same fair
ordering from the seed and returns the requested slice. Looping is handled
entirely server-side via global-offset arithmetic, so the browser stays simple:
it only ever asks for "more videos from offset N".

All ordering, fairness, and looping logic lives in Go (testable). The browser
handles scroll detection, appending, DOM pruning, and playback.

## The fair ordering — `FairOrder`

`RandomCapped` returned a bounded *sample* of N videos. Infinite scroll needs a
deterministic *ordering* of the whole pool that stays diverse at every prefix.

```go
func FairOrder(videos []Video, rng *rand.Rand) []Video
```

Algorithm:

1. Group videos by `SourceID`.
2. Collect the source keys, **sort** them (Go map iteration order is random and
   would break determinism), then shuffle that key order with `rng`.
3. Shuffle each source's video slice with `rng`.
4. **Round-robin interleave:** repeatedly walk the sources in the shuffled key
   order, taking one not-yet-used video from each, until every source is
   exhausted. A source that runs dry is skipped on subsequent passes.

Properties:

- **Prefix diversity.** Any prefix of length k contains at most `⌈k / numSources⌉`
  videos from any single source. Every screenful is spread across sources as
  evenly as the pool allows — strictly more even than the old `1/5` cap.
- **Dominant-source tail.** When one source is much larger than the others, its
  overflow necessarily lands at the end (after smaller sources are exhausted).
  Round-robin pushes that tail as far down as the math permits; since the feed
  loops, the user rarely reaches it before a reshuffle.
- **Determinism.** Same `videos` + same `rng` seed ⇒ identical ordering.
- **Within-source randomness** is preserved (step 3).

`FairOrder` lives in `cache.go` alongside the cache. It takes a `*rand.Rand` so
the caller controls seeding; it does not touch the package-global RNG.

## Looping and the videos endpoint

A page load picks a random base seed `S` (`rand.Uint64()` from the auto-seeded
global RNG — this is not security-sensitive). Given the pool size `M`, any global
feed index `g` maps to a cycle and a position within that cycle:

```
cycle c = g / M
local   = g % M
ordering for cycle c = FairOrder(videos, rand.New(rand.NewPCG(S, c)))
video at g           = orderingForCycle(c)[local]
```

Each cycle reseeds with `NewPCG(S, c)`, so every loop around the pool is a fresh
order — no identical repeats. Because the cycle is derived from the global
offset, the browser never needs to know `M` or the cycle count; it just keeps
asking for higher offsets.

**Endpoint:** `GET /videos?seed=<S>&offset=<O>&count=<N>`

- Returns an **HTML fragment**: a sequence of `.grid-cell` divs, rendered from a
  shared `{{define "cells"}}` template block — the same markup the full page
  uses. The browser appends the returned HTML directly, so tile markup lives in
  exactly one place (the Go template), not duplicated in JS.
- `count` defaults to 30 and is capped at a maximum (e.g. 60) to bound work.
- A request for `[O, O+N)` spans 1–2 cycles; each needed cycle's `FairOrder` is
  built once (O(M), trivial for M ≤ ~2000) and indexed.
- Validation: `seed` must parse as `uint64`, `offset` must be `>= 0`, `count`
  within `[1, max]`. Invalid params ⇒ `400`. The page always supplies valid ones.

**Index page:** server-renders the first screenful (cycle 0, offset 0,
`FairOrder(videos, NewPCG(S, 0))[:count]`) and embeds `S` and the next offset
(`= count`) as data attributes for the script to continue from.

## Frontend (`app.js`)

- On load, read `seed` and the next `offset` from the page's data attributes.
- A sentinel `<div>` after the grid is watched by an `IntersectionObserver`. When
  it nears the viewport, fetch `/videos?seed=<S>&offset=<O>&count=30`,
  `insertAdjacentHTML("beforeend", …)` onto the grid, and advance the offset by
  the count. A single in-flight flag prevents overlapping fetches.
- **DOM pruning.** Keep a sliding window of roughly 240 tiles. When exceeded,
  remove whole rows from the top and decrement `scrollTop` by their measured
  height so the viewport does not jump. `content-visibility: auto` on
  `.grid-cell` reduces render cost for off-screen tiles, letting the window stay
  generous and pruning stay infrequent.
- **Playback exit.** Both "video ended" and "exited fullscreen / backed out" now
  navigate to `window.location = "/"` after the existing ~1.5 s black-screen
  pause, yielding a fresh feed at the top. The previous two-path logic
  (`?shuffle=1` on end vs. silent return on Escape) and the `shuffle` parameter
  are removed.

## CSS

- The per-cell accent colors and entrance-stagger currently target
  `:nth-child(1)` … `:nth-child(30)`. Because tiles now append indefinitely past
  30, this becomes a **repeating** palette via a modulo `:nth-child` pattern
  (e.g. a cycle of ~10 colors), so appended tiles are always styled.
- The entrance `popIn` animation applies to newly appended tiles as they arrive;
  the large per-index delays tuned for a 30-tile first paint are reworked so they
  do not accumulate unboundedly down the feed.

## Edge cases

- **Empty pool** — the endpoint returns an empty fragment and the first page is
  empty; there is nothing to scroll. Matches today's empty-cache blank grid.
- **Single source** — round-robin degenerates to that source shuffled, then
  reshuffled each loop. Acceptable.
- **Pool smaller than `count`** — `Page` still returns a full `count` by looping
  (each cycle reshuffled), so the first screen fills with the few videos repeated
  in fresh order. Unlikely given expected pool sizes (hundreds–2000), but the
  uniform "always returns `count`" contract keeps the code simple. The empty
  pool is the only case that returns fewer (zero).
- **Pool changes mid-session** — the periodic refresh (every few hours) replaces
  the cache, changing `M` and therefore the seed→order mapping. A page fetched
  right at that boundary could duplicate or skip a video once. Rare and
  low-stakes; accepted, not mitigated.
- **Bad endpoint params** — `400`; the page never sends them.

## Out of scope

- **Cross-source duplicates.** If the same video ID appears in two configured
  sources it can still appear twice. Pre-existing; not addressed.
- **Restoring scroll position across playback.** Explicitly chosen against —
  every return is a fresh feed at the top.
- **Configurable page size / window size.** Hardcoded constants; a config knob
  can be added later without changing the design.
- **JS test automation.** The project has no JS test harness; scroll, pruning,
  and playback are verified manually.

## Testing (TDD)

**Go — `cache_test.go` (`FairOrder`):**

1. Prefix diversity — a pool with one dominant source (e.g. 100 from A, 10 each
   from B–E); assert no source is over-represented in early prefixes
   (≈ `⌈k/numSources⌉` bound).
2. Within-source order is shuffled.
3. Determinism — same videos + same seed ⇒ identical slice; different seed ⇒
   different order.
4. Distinct order across cycles — `NewPCG(S, 0)` vs `NewPCG(S, 1)` differ.
5. Single source — returns that source's videos, all of them.
6. Empty pool — returns empty.

**Go — videos endpoint:**

7. Returns a fragment with `count` cells for a valid request.
8. `offset` slices correctly; consecutive requests tile without gaps within a
   cycle.
9. Looping — an `offset` past `M` returns videos (from the next cycle), and the
   next cycle's order differs from cycle 0.
10. Param validation — missing/invalid `seed`, negative `offset`, out-of-range
    `count` ⇒ `400`.

**Go — `handlers_test.go` (index):**

- First page renders `count` cells and embeds a parseable `seed` + next offset.
- Existing cookie tests (`TestGridHandlerSetsCookie`, `…ReadsFromCookie`,
  `…ShuffleIgnoresCookie`, `…InvalidCookieFallsBack`) are deleted along with the
  cookie.
- `TestGridHandlerEmptyCache` retained/adapted.

**Not asserted:** the exact shuffle (only "is shuffled" / "is fair"), and all JS
behavior (manual verification).

## Follow-ups (after implementation)

- Update `docs/solutions/design-patterns/capped-fair-share-sampler-2026-04-26.md`
  and the channel-share-cap spec to reflect that `RandomCapped` was generalized
  into a prefix-fair *ordering* (`FairOrder`) for pagination, and run
  `/ce-compound` to capture the prefix-fairness-under-pagination decision.
