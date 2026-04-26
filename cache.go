package main

import (
	"fmt"
	"log"
	"math/rand/v2"
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
