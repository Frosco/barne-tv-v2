package main

import (
	"fmt"
	"log"
	"math/rand/v2"
	"sort"
	"sync"
	"time"
)

type VideoCache struct {
	mu     sync.RWMutex
	videos []Video
}

func (c *VideoCache) Store(videos []Video) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.videos = videos
}

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
	for i := range count {
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
		take := min(capPerSource, len(group))
		result = append(result, group[:take]...)
		leftovers = append(leftovers, group[take:]...)
	}

	// Pass B: top up from leftovers if we're under n.
	if len(result) < n && len(leftovers) > 0 {
		rand.Shuffle(len(leftovers), func(i, j int) {
			leftovers[i], leftovers[j] = leftovers[j], leftovers[i]
		})
		need := min(n-len(result), len(leftovers))
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

func (c *VideoCache) GetByIDs(ids []string) []Video {
	c.mu.RLock()
	defer c.mu.RUnlock()

	lookup := make(map[string]Video)
	for _, v := range c.videos {
		lookup[v.ID] = v
	}

	result := make([]Video, 0, len(ids))
	for _, id := range ids {
		v, ok := lookup[id]
		if !ok {
			return nil // missing video, force new selection
		}
		result = append(result, v)
	}
	return result
}

func (c *VideoCache) RefreshAll(yt *YouTubeClient, sources []Source) error {
	var all []Video
	var fetchErrors int

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

	c.Store(all)
	log.Printf("cache refreshed: %d videos from %d sources", len(all), len(sources))

	if len(all) == 0 && fetchErrors > 0 {
		return fmt.Errorf("all %d sources failed to fetch", fetchErrors)
	}
	return nil
}

func (c *VideoCache) StartPeriodicRefresh(yt *YouTubeClient, sources []Source, interval time.Duration) (stop func()) {
	ticker := time.NewTicker(interval)
	done := make(chan struct{})

	go func() {
		for {
			select {
			case <-ticker.C:
				if err := c.RefreshAll(yt, sources); err != nil {
					log.Printf("periodic refresh error: %v", err)
				}
			case <-done:
				ticker.Stop()
				return
			}
		}
	}()

	return func() { close(done) }
}
