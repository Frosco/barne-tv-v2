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

func (c *VideoCache) Random(n int) []Video {
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
