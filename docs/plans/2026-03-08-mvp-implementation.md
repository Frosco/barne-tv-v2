# Barne-TV v2 MVP Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** A Go web app serving a 3x3 grid of random YouTube video thumbnails from parent-approved channels/playlists. Click to play fullscreen, auto-return to grid when done.

**Architecture:** Single Go binary. YAML config defines YouTube sources. Server fetches video metadata on startup, caches in memory, refreshes periodically. Serves one HTML page via `html/template`. Minimal JS for YouTube IFrame player. No database, no auth, no admin UI.

**Tech Stack:** Go 1.24, `gopkg.in/yaml.v3`, `net/http`, `html/template`, YouTube Data API v3 (raw HTTP), YouTube IFrame Player API (JS)

**Process notes:**
- TDD for all Go code. Frontend JS/CSS are tested via manual browser verification.
- Run `go mod tidy` after adding dependencies. Run `go build ./...` before every commit.
- Never add a dependency in a separate commit from the code that uses it.

---

### Task 1: Project scaffold + config parsing

**Files:**
- Create: `go.mod`
- Create: `.gitignore`
- Create: `config.go`
- Create: `config_test.go`
- Create: `config.example.yaml`

**Step 1: Initialize Go module and .gitignore**

```bash
cd /home/niref/dev/frosco/barne-tv-v2
go mod init barne-tv-v2
```

Create `.gitignore`:
```
config.yaml
barne-tv-v2
```

**Step 2: Write the failing test for config parsing**

`config_test.go`:
```go
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	yaml := `youtube_api_key: "test-key-123"
refresh_interval: "2h"
sources:
  - type: channel
    id: "UC123"
    name: "Test Channel"
  - type: playlist
    id: "PL456"
    name: "Test Playlist"
`
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if cfg.YouTubeAPIKey != "test-key-123" {
		t.Errorf("APIKey = %q, want %q", cfg.YouTubeAPIKey, "test-key-123")
	}
	if cfg.RefreshInterval != "2h" {
		t.Errorf("RefreshInterval = %q, want %q", cfg.RefreshInterval, "2h")
	}
	if len(cfg.Sources) != 2 {
		t.Fatalf("Sources count = %d, want 2", len(cfg.Sources))
	}
	if cfg.Sources[0].Type != "channel" || cfg.Sources[0].ID != "UC123" {
		t.Errorf("Sources[0] = %+v, want channel/UC123", cfg.Sources[0])
	}
	if cfg.Sources[1].Type != "playlist" || cfg.Sources[1].ID != "PL456" {
		t.Errorf("Sources[1] = %+v, want playlist/PL456", cfg.Sources[1])
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	yaml := `youtube_api_key: "key"
sources: []
`
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if cfg.RefreshInterval != "6h" {
		t.Errorf("default RefreshInterval = %q, want %q", cfg.RefreshInterval, "6h")
	}
}

func TestLoadConfigMissingAPIKey(t *testing.T) {
	yaml := `sources:
  - type: channel
    id: "UC123"
    name: "Test"
`
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadConfig(path)
	if err == nil {
		t.Error("expected error for missing API key")
	}
}
```

**Step 3: Run test to verify it fails**

Run: `go test -run TestLoadConfig -v`
Expected: FAIL — `LoadConfig` undefined

**Step 4: Write minimal implementation**

`config.go`:
```go
package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Source struct {
	Type string `yaml:"type"`
	ID   string `yaml:"id"`
	Name string `yaml:"name"`
}

type Config struct {
	YouTubeAPIKey   string   `yaml:"youtube_api_key"`
	RefreshInterval string   `yaml:"refresh_interval"`
	Sources         []Source `yaml:"sources"`
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	if cfg.YouTubeAPIKey == "" {
		return nil, fmt.Errorf("youtube_api_key is required")
	}

	if cfg.RefreshInterval == "" {
		cfg.RefreshInterval = "6h"
	}

	return &cfg, nil
}
```

Then run:
```bash
go mod tidy
```

**Step 5: Run tests to verify they pass**

Run: `go test -run TestLoadConfig -v`
Expected: all 3 tests PASS

**Step 6: Create config.example.yaml**

```yaml
youtube_api_key: "YOUR_YOUTUBE_API_KEY"
refresh_interval: "6h"
sources:
  - type: channel
    id: "UCxxxxxxxxxxxxxxxxxxxxxxxx"
    name: "Example Channel"
  - type: playlist
    id: "PLxxxxxxxxxxxxxxxxxxxxxxxx"
    name: "Example Playlist"
```

**Step 7: Build and commit**

```bash
go build ./...
git add go.mod go.sum .gitignore config.go config_test.go config.example.yaml
git commit -m "feat: project scaffold with YAML config parsing"
```

---

### Task 2: YouTube API client

**Files:**
- Create: `youtube.go`
- Create: `youtube_test.go`

**Context:** We call YouTube Data API v3 via raw HTTP (no Google client library — we only need 2 endpoints). For channels, we first get the uploads playlist ID via `channels.list`, then fetch videos via `playlistItems.list`. For playlists, we call `playlistItems.list` directly.

**API endpoints:**
- `GET https://www.googleapis.com/youtube/v3/channels?part=contentDetails&id={channelId}&key={apiKey}`
- `GET https://www.googleapis.com/youtube/v3/playlistItems?part=snippet&playlistId={playlistId}&maxResults=50&key={apiKey}`

**Step 1: Write failing test for FetchPlaylistVideos**

`youtube_test.go`:
```go
package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchPlaylistVideos(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/playlistItems" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("playlistId") != "PL123" {
			t.Errorf("unexpected playlistId: %s", r.URL.Query().Get("playlistId"))
		}
		if r.URL.Query().Get("key") != "test-key" {
			t.Errorf("unexpected key: %s", r.URL.Query().Get("key"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"items": [
				{
					"snippet": {
						"title": "Video One",
						"resourceId": {"videoId": "vid1"},
						"thumbnails": {"high": {"url": "https://img.youtube.com/vi/vid1/hqdefault.jpg"}}
					}
				},
				{
					"snippet": {
						"title": "Video Two",
						"resourceId": {"videoId": "vid2"},
						"thumbnails": {"high": {"url": "https://img.youtube.com/vi/vid2/hqdefault.jpg"}}
					}
				}
			]
		}`))
	}))
	defer server.Close()

	client := &YouTubeClient{
		APIKey:  "test-key",
		BaseURL: server.URL,
		HTTP:    server.Client(),
	}

	videos, err := client.FetchPlaylistVideos("PL123")
	if err != nil {
		t.Fatalf("FetchPlaylistVideos: %v", err)
	}

	if len(videos) != 2 {
		t.Fatalf("got %d videos, want 2", len(videos))
	}
	if videos[0].ID != "vid1" || videos[0].Title != "Video One" {
		t.Errorf("videos[0] = %+v", videos[0])
	}
	if videos[1].ID != "vid2" || videos[1].Title != "Video Two" {
		t.Errorf("videos[1] = %+v", videos[1])
	}
}

func TestFetchPlaylistVideosPagination(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		callCount++
		if callCount == 1 {
			w.Write([]byte(`{
				"items": [{"snippet": {"title": "V1", "resourceId": {"videoId": "v1"}, "thumbnails": {"high": {"url": "http://img/v1"}}}}],
				"nextPageToken": "page2"
			}`))
		} else {
			if r.URL.Query().Get("pageToken") != "page2" {
				t.Errorf("expected pageToken=page2, got %s", r.URL.Query().Get("pageToken"))
			}
			w.Write([]byte(`{
				"items": [{"snippet": {"title": "V2", "resourceId": {"videoId": "v2"}, "thumbnails": {"high": {"url": "http://img/v2"}}}}]
			}`))
		}
	}))
	defer server.Close()

	client := &YouTubeClient{APIKey: "key", BaseURL: server.URL, HTTP: server.Client()}
	videos, err := client.FetchPlaylistVideos("PL123")
	if err != nil {
		t.Fatalf("FetchPlaylistVideos: %v", err)
	}
	if len(videos) != 2 {
		t.Fatalf("got %d videos, want 2", len(videos))
	}
}

func TestFetchChannelVideos(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/channels":
			if r.URL.Query().Get("id") != "UC123" {
				t.Errorf("unexpected channel id: %s", r.URL.Query().Get("id"))
			}
			w.Write([]byte(`{
				"items": [{"contentDetails": {"relatedPlaylists": {"uploads": "UU123"}}}]
			}`))
		case "/playlistItems":
			if r.URL.Query().Get("playlistId") != "UU123" {
				t.Errorf("unexpected playlistId: %s", r.URL.Query().Get("playlistId"))
			}
			w.Write([]byte(`{
				"items": [{"snippet": {"title": "Channel Vid", "resourceId": {"videoId": "cv1"}, "thumbnails": {"high": {"url": "http://img/cv1"}}}}]
			}`))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := &YouTubeClient{APIKey: "key", BaseURL: server.URL, HTTP: server.Client()}
	videos, err := client.FetchChannelVideos("UC123")
	if err != nil {
		t.Fatalf("FetchChannelVideos: %v", err)
	}
	if len(videos) != 1 || videos[0].ID != "cv1" {
		t.Errorf("videos = %+v", videos)
	}
}

func TestFetchChannelVideosNoItems(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"items": []}`))
	}))
	defer server.Close()

	client := &YouTubeClient{APIKey: "key", BaseURL: server.URL, HTTP: server.Client()}
	_, err := client.FetchChannelVideos("UC999")
	if err == nil {
		t.Error("expected error for empty channel response")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test -run TestFetch -v`
Expected: FAIL — `YouTubeClient` undefined

**Step 3: Implement YouTube client**

`youtube.go`:
```go
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

type Video struct {
	ID           string
	Title        string
	ThumbnailURL string
}

type YouTubeClient struct {
	APIKey  string
	BaseURL string // default: https://www.googleapis.com/youtube/v3
	HTTP    *http.Client
}

func NewYouTubeClient(apiKey string) *YouTubeClient {
	return &YouTubeClient{
		APIKey:  apiKey,
		BaseURL: "https://www.googleapis.com/youtube/v3",
		HTTP:    http.DefaultClient,
	}
}

// playlistItemsResponse matches the YouTube playlistItems.list JSON response.
type playlistItemsResponse struct {
	Items []struct {
		Snippet struct {
			Title      string `json:"title"`
			ResourceID struct {
				VideoID string `json:"videoId"`
			} `json:"resourceId"`
			Thumbnails struct {
				High struct {
					URL string `json:"url"`
				} `json:"high"`
			} `json:"thumbnails"`
		} `json:"snippet"`
	} `json:"items"`
	NextPageToken string `json:"nextPageToken"`
}

// channelsResponse matches the YouTube channels.list JSON response.
type channelsResponse struct {
	Items []struct {
		ContentDetails struct {
			RelatedPlaylists struct {
				Uploads string `json:"uploads"`
			} `json:"relatedPlaylists"`
		} `json:"contentDetails"`
	} `json:"items"`
}

func (yt *YouTubeClient) FetchPlaylistVideos(playlistID string) ([]Video, error) {
	var all []Video
	pageToken := ""

	for {
		params := url.Values{
			"part":       {"snippet"},
			"playlistId": {playlistID},
			"maxResults": {"50"},
			"key":        {yt.APIKey},
		}
		if pageToken != "" {
			params.Set("pageToken", pageToken)
		}

		resp, err := yt.HTTP.Get(yt.BaseURL + "/playlistItems?" + params.Encode())
		if err != nil {
			return nil, fmt.Errorf("fetching playlist %s: %w", playlistID, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("YouTube API returned %d for playlist %s", resp.StatusCode, playlistID)
		}

		var result playlistItemsResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, fmt.Errorf("decoding playlist response: %w", err)
		}

		for _, item := range result.Items {
			all = append(all, Video{
				ID:           item.Snippet.ResourceID.VideoID,
				Title:        item.Snippet.Title,
				ThumbnailURL: item.Snippet.Thumbnails.High.URL,
			})
		}

		if result.NextPageToken == "" {
			break
		}
		pageToken = result.NextPageToken
	}

	return all, nil
}

func (yt *YouTubeClient) FetchChannelVideos(channelID string) ([]Video, error) {
	params := url.Values{
		"part": {"contentDetails"},
		"id":   {channelID},
		"key":  {yt.APIKey},
	}

	resp, err := yt.HTTP.Get(yt.BaseURL + "/channels?" + params.Encode())
	if err != nil {
		return nil, fmt.Errorf("fetching channel %s: %w", channelID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("YouTube API returned %d for channel %s", resp.StatusCode, channelID)
	}

	var result channelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding channel response: %w", err)
	}

	if len(result.Items) == 0 {
		return nil, fmt.Errorf("channel %s not found", channelID)
	}

	uploadsPlaylistID := result.Items[0].ContentDetails.RelatedPlaylists.Uploads
	return yt.FetchPlaylistVideos(uploadsPlaylistID)
}
```

**Step 4: Run tests to verify they pass**

Run: `go test -run TestFetch -v`
Expected: all 4 tests PASS

**Step 5: Build and commit**

```bash
go build ./...
git add youtube.go youtube_test.go
git commit -m "feat: YouTube API client for playlist and channel video fetching"
```

---

### Task 3: Video cache

**Files:**
- Create: `cache.go`
- Create: `cache_test.go`

**Step 1: Write failing tests for VideoCache**

`cache_test.go`:
```go
package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestVideoCacheRandomSelection(t *testing.T) {
	cache := &VideoCache{}
	videos := []Video{
		{ID: "v1", Title: "One", ThumbnailURL: "http://img/1"},
		{ID: "v2", Title: "Two", ThumbnailURL: "http://img/2"},
		{ID: "v3", Title: "Three", ThumbnailURL: "http://img/3"},
		{ID: "v4", Title: "Four", ThumbnailURL: "http://img/4"},
		{ID: "v5", Title: "Five", ThumbnailURL: "http://img/5"},
	}
	cache.Store(videos)

	selected := cache.Random(3)
	if len(selected) != 3 {
		t.Fatalf("got %d videos, want 3", len(selected))
	}

	// All selected videos should be from the pool
	pool := make(map[string]bool)
	for _, v := range videos {
		pool[v.ID] = true
	}
	for _, v := range selected {
		if !pool[v.ID] {
			t.Errorf("selected video %s not in pool", v.ID)
		}
	}

	// No duplicates
	seen := make(map[string]bool)
	for _, v := range selected {
		if seen[v.ID] {
			t.Errorf("duplicate video %s in selection", v.ID)
		}
		seen[v.ID] = true
	}
}

func TestVideoCacheRandomSelectionMoreThanPool(t *testing.T) {
	cache := &VideoCache{}
	cache.Store([]Video{
		{ID: "v1", Title: "One", ThumbnailURL: "http://img/1"},
		{ID: "v2", Title: "Two", ThumbnailURL: "http://img/2"},
	})

	selected := cache.Random(9)
	if len(selected) != 2 {
		t.Fatalf("got %d videos, want 2 (pool size)", len(selected))
	}
}

func TestVideoCacheRandomSelectionEmpty(t *testing.T) {
	cache := &VideoCache{}
	selected := cache.Random(9)
	if len(selected) != 0 {
		t.Fatalf("got %d videos from empty cache, want 0", len(selected))
	}
}

func TestVideoCacheGetByIDs(t *testing.T) {
	cache := &VideoCache{}
	cache.Store([]Video{
		{ID: "v1", Title: "One", ThumbnailURL: "http://img/1"},
		{ID: "v2", Title: "Two", ThumbnailURL: "http://img/2"},
		{ID: "v3", Title: "Three", ThumbnailURL: "http://img/3"},
	})

	videos := cache.GetByIDs([]string{"v3", "v1"})
	if len(videos) != 2 {
		t.Fatalf("got %d videos, want 2", len(videos))
	}
	if videos[0].ID != "v3" || videos[1].ID != "v1" {
		t.Errorf("wrong order: got %s, %s", videos[0].ID, videos[1].ID)
	}
}

func TestVideoCacheGetByIDsMissing(t *testing.T) {
	cache := &VideoCache{}
	cache.Store([]Video{
		{ID: "v1", Title: "One", ThumbnailURL: "http://img/1"},
	})

	// If any ID is missing, return nil (forces new random selection)
	videos := cache.GetByIDs([]string{"v1", "v999"})
	if videos != nil {
		t.Errorf("expected nil for missing IDs, got %+v", videos)
	}
}

func TestVideoCacheRefreshAll(t *testing.T) {
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
	err := cache.RefreshAll(yt, sources)
	if err != nil {
		t.Fatalf("RefreshAll: %v", err)
	}

	all := cache.Random(100)
	if len(all) != 2 {
		t.Fatalf("got %d videos, want 2", len(all))
	}
}

func TestStartPeriodicRefresh(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"items": [{"snippet": {"title": "V", "resourceId": {"videoId": "v1"}, "thumbnails": {"high": {"url": "http://img/v1"}}}}]}`))
	}))
	defer server.Close()

	yt := &YouTubeClient{APIKey: "key", BaseURL: server.URL, HTTP: server.Client()}
	sources := []Source{{Type: "playlist", ID: "PL1", Name: "Test"}}
	cache := &VideoCache{}

	stop := cache.StartPeriodicRefresh(yt, sources, 50*time.Millisecond)
	time.Sleep(160 * time.Millisecond)
	stop()

	if callCount < 2 {
		t.Errorf("expected at least 2 refresh calls, got %d", callCount)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test -run TestVideoCache -v`
Expected: FAIL — `VideoCache` undefined

**Step 3: Implement VideoCache**

`cache.go`:
```go
package main

import (
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
			continue
		}

		all = append(all, videos...)
	}

	c.Store(all)
	log.Printf("cache refreshed: %d videos from %d sources", len(all), len(sources))
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
```

**Step 4: Run tests to verify they pass**

Run: `go test -run "TestVideoCache|TestStartPeriodic" -v`
Expected: all 6 tests PASS

**Step 5: Build and commit**

```bash
go build ./...
git add cache.go cache_test.go
git commit -m "feat: in-memory video cache with random selection and periodic refresh"
```

---

### Task 4: HTTP handlers + cookie persistence

**Files:**
- Create: `handlers.go`
- Create: `handlers_test.go`
- Create: `templates/index.html` (minimal placeholder for handler tests)

**Step 1: Write failing tests for handlers**

`handlers_test.go`:
```go
package main

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func setupTestHandler(t *testing.T) (*GridHandler, *VideoCache) {
	t.Helper()
	cache := &VideoCache{}
	cache.Store([]Video{
		{ID: "v1", Title: "Video One", ThumbnailURL: "http://img/1"},
		{ID: "v2", Title: "Video Two", ThumbnailURL: "http://img/2"},
		{ID: "v3", Title: "Video Three", ThumbnailURL: "http://img/3"},
		{ID: "v4", Title: "Video Four", ThumbnailURL: "http://img/4"},
		{ID: "v5", Title: "Video Five", ThumbnailURL: "http://img/5"},
		{ID: "v6", Title: "Video Six", ThumbnailURL: "http://img/6"},
		{ID: "v7", Title: "Video Seven", ThumbnailURL: "http://img/7"},
		{ID: "v8", Title: "Video Eight", ThumbnailURL: "http://img/8"},
		{ID: "v9", Title: "Video Nine", ThumbnailURL: "http://img/9"},
	})

	tmpl := template.Must(template.New("index.html").Parse(
		`{{range .Videos}}<div data-id="{{.ID}}">{{.Title}}</div>{{end}}`,
	))

	handler := &GridHandler{Cache: cache, Template: tmpl, GridSize: 9}
	return handler, cache
}

func TestGridHandlerServesVideos(t *testing.T) {
	handler, _ := setupTestHandler(t)

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	body := w.Body.String()
	// Should contain 9 video divs
	count := strings.Count(body, "data-id=")
	if count != 9 {
		t.Errorf("found %d videos in response, want 9", count)
	}
}

func TestGridHandlerSetsCookie(t *testing.T) {
	handler, _ := setupTestHandler(t)

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	cookies := w.Result().Cookies()
	var gridCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "grid" {
			gridCookie = c
			break
		}
	}
	if gridCookie == nil {
		t.Fatal("no grid cookie set")
	}
	ids := strings.Split(gridCookie.Value, ",")
	if len(ids) != 9 {
		t.Errorf("cookie has %d IDs, want 9", len(ids))
	}
}

func TestGridHandlerReadsFromCookie(t *testing.T) {
	handler, _ := setupTestHandler(t)

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "grid", Value: "v1,v2,v3,v4,v5,v6,v7,v8,v9"})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	// Videos should appear in cookie order
	idx1 := strings.Index(body, `data-id="v1"`)
	idx2 := strings.Index(body, `data-id="v2"`)
	if idx1 == -1 || idx2 == -1 {
		t.Fatal("expected v1 and v2 in response")
	}
	if idx1 > idx2 {
		t.Error("cookie order not preserved")
	}
}

func TestGridHandlerShuffleIgnoresCookie(t *testing.T) {
	handler, _ := setupTestHandler(t)

	req := httptest.NewRequest("GET", "/?shuffle=1", nil)
	req.AddCookie(&http.Cookie{Name: "grid", Value: "v1,v2,v3,v4,v5,v6,v7,v8,v9"})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Should still work (200 OK with 9 videos)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	// Cookie should be set with potentially different values
	cookies := w.Result().Cookies()
	var gridCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "grid" {
			gridCookie = c
			break
		}
	}
	if gridCookie == nil {
		t.Fatal("no grid cookie set on shuffle")
	}
}

func TestGridHandlerInvalidCookieFallsBack(t *testing.T) {
	handler, _ := setupTestHandler(t)

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "grid", Value: "v1,v2,MISSING_VIDEO"})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	// Should fall back to random selection (9 videos)
	body := w.Body.String()
	count := strings.Count(body, "data-id=")
	if count != 9 {
		t.Errorf("found %d videos, want 9", count)
	}
}

func TestGridHandlerEmptyCache(t *testing.T) {
	cache := &VideoCache{}
	tmpl := template.Must(template.New("index.html").Parse(
		`{{range .Videos}}<div data-id="{{.ID}}">{{.Title}}</div>{{end}}`,
	))
	handler := &GridHandler{Cache: cache, Template: tmpl, GridSize: 9}

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test -run TestGridHandler -v`
Expected: FAIL — `GridHandler` undefined

**Step 3: Implement handlers**

`handlers.go`:
```go
package main

import (
	"html/template"
	"net/http"
	"strings"
)

type GridHandler struct {
	Cache    *VideoCache
	Template *template.Template
	GridSize int
}

type templateData struct {
	Videos []Video
}

func (h *GridHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	shuffle := r.URL.Query().Get("shuffle") != ""

	var videos []Video

	if !shuffle {
		if cookie, err := r.Cookie("grid"); err == nil {
			ids := strings.Split(cookie.Value, ",")
			videos = h.Cache.GetByIDs(ids)
		}
	}

	if videos == nil {
		videos = h.Cache.Random(h.GridSize)
	}

	// Set cookie with current selection
	if len(videos) > 0 {
		ids := make([]string, len(videos))
		for i, v := range videos {
			ids[i] = v.ID
		}
		http.SetCookie(w, &http.Cookie{
			Name:     "grid",
			Value:    strings.Join(ids, ","),
			Path:     "/",
			SameSite: http.SameSiteLaxMode,
		})
	}

	data := templateData{Videos: videos}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.Template.Execute(w, data); err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}
```

**Step 4: Run tests to verify they pass**

Run: `go test -run TestGridHandler -v`
Expected: all 6 tests PASS

**Step 5: Build and commit**

```bash
go build ./...
git add handlers.go handlers_test.go
git commit -m "feat: grid HTTP handler with cookie-based selection persistence"
```

---

### Task 5: HTML template + JavaScript

**Files:**
- Create: `templates/index.html`
- Create: `static/app.js`

**Context:** This task creates the frontend. The template is rendered server-side with video data. The JS handles YouTube IFrame player, fullscreen, and back-to-grid transitions. TDD is not practical here — verify manually in browser.

**Step 1: Create the HTML template**

`templates/index.html` — a complete HTML page with:
- 3x3 video thumbnail grid using CSS grid
- Each cell: thumbnail image + title text below
- Shuffle button (visual icon, e.g. a dice or arrows SVG)
- Hidden player container for YouTube iframe
- Loads YouTube IFrame API script
- Loads `/static/app.js`
- Loads `/static/style.css`
- Template variables: `{{range .Videos}}` with `{{.ID}}`, `{{.Title}}`, `{{.ThumbnailURL}}`

**Step 2: Create app.js**

`static/app.js` — handles:
- Thumbnail click → create `YT.Player` in the player container, call `requestFullscreen()`
- `onPlayerStateChange`: when `event.data == YT.PlayerState.ENDED`, start fade-out, after ~2s exit fullscreen and show grid
- Shuffle button click → `window.location = '/?shuffle=1'`

Key code patterns:
```javascript
// YouTube IFrame API loaded via <script> in template
function onYouTubeIframeAPIReady() { /* API ready, enable clicks */ }

function playVideo(videoId) {
  player = new YT.Player('player', {
    videoId: videoId,
    playerVars: { autoplay: 1, rel: 0, modestbranding: 1 },
    events: { onStateChange: onPlayerStateChange }
  });
  document.getElementById('player-container').requestFullscreen();
}

function onPlayerStateChange(event) {
  if (event.data === YT.PlayerState.ENDED) {
    // fade out, then back to grid
  }
}
```

**Step 3: Verify manually**

Create a temporary `config.yaml` with a real playlist/channel, run the server, open in browser. Verify:
- Grid shows 9 thumbnails with titles
- Clicking opens fullscreen video
- Video ending returns to grid after brief pause
- Shuffle button loads new selection
- Page refresh keeps same selection

**Step 4: Commit**

```bash
mkdir -p templates static
git add templates/index.html static/app.js
git commit -m "feat: HTML template and JavaScript for video grid and player"
```

---

### Task 6: CSS design

**Files:**
- Create: `static/style.css`

**Context:** Use @frontend-design skill methodology to create a playful, colourful, child-friendly design. Key requirements:
- Large thumbnails in a 3x3 CSS grid
- Colourful background (not white/grey)
- Rounded corners, soft shadows
- Titles readable but not dominant
- Shuffle button prominent and fun
- Player container covers full screen
- Fade transition when video ends
- Works well on tablet-sized screens (primary device for kids)
- Responsive: works on phone through TV

**Step 1: Invoke frontend-design skill for the CSS**

Use the @frontend-design skill to design `static/style.css`.

**Step 2: Commit**

```bash
git add static/style.css
git commit -m "feat: playful child-friendly CSS design"
```

---

### Task 7: Main.go wiring + integration

**Files:**
- Create: `main.go`

**Step 1: Write main.go**

`main.go` wires everything together:
1. Load config from `config.yaml` (or path from `-config` flag)
2. Create `YouTubeClient`
3. Create `VideoCache`, run initial `RefreshAll`
4. Parse refresh interval, start `StartPeriodicRefresh`
5. Parse `templates/index.html`
6. Set up routes:
   - `GET /` → `GridHandler`
   - `GET /static/` → `http.FileServer` for `static/` directory
7. Start HTTP server on `:8080` (or `-addr` flag)

```go
package main

import (
	"flag"
	"html/template"
	"log"
	"net/http"
	"time"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	addr := flag.String("addr", ":8080", "listen address")
	flag.Parse()

	cfg, err := LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}

	yt := NewYouTubeClient(cfg.YouTubeAPIKey)

	cache := &VideoCache{}
	if err := cache.RefreshAll(yt, cfg.Sources); err != nil {
		log.Fatalf("initial refresh: %v", err)
	}

	interval, err := time.ParseDuration(cfg.RefreshInterval)
	if err != nil {
		log.Fatalf("invalid refresh_interval %q: %v", cfg.RefreshInterval, err)
	}
	stop := cache.StartPeriodicRefresh(yt, cfg.Sources, interval)
	defer stop()

	tmpl, err := template.ParseFiles("templates/index.html")
	if err != nil {
		log.Fatalf("parsing template: %v", err)
	}

	handler := &GridHandler{Cache: cache, Template: tmpl, GridSize: 9}

	http.Handle("/", handler)
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	log.Printf("listening on %s with %d sources", *addr, len(cfg.Sources))
	log.Fatal(http.ListenAndServe(*addr, nil))
}
```

**Step 2: Build and verify**

```bash
go build ./...
```

Create a `config.yaml` with a real YouTube API key and at least one source. Run:
```bash
./barne-tv-v2
```

Open browser, verify end-to-end.

**Step 3: Commit**

```bash
git add main.go
git commit -m "feat: wire up main.go entry point"
```

---

### Summary of commits

| # | Commit | What it adds |
|---|--------|-------------|
| 1 | project scaffold + config | go.mod, .gitignore, config parsing with tests |
| 2 | YouTube API client | Playlist + channel fetching with httptest tests |
| 3 | Video cache | In-memory cache, random selection, periodic refresh with tests |
| 4 | HTTP handlers | Grid handler, cookie persistence, shuffle with tests |
| 5 | Template + JS | HTML grid template, YouTube IFrame player JS |
| 6 | CSS design | Playful child-friendly styling |
| 7 | Main.go | Entry point wiring everything together |
