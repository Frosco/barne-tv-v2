package main

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func silenceLogs(t *testing.T) {
	t.Helper()
	orig := log.Writer()
	log.SetOutput(io.Discard)
	t.Cleanup(func() { log.SetOutput(orig) })
}

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
	err := cache.RefreshAll(yt, sources)
	if err != nil {
		t.Fatalf("RefreshAll: %v", err)
	}

	all := cache.Random(100)
	if len(all) != 2 {
		t.Fatalf("got %d videos, want 2", len(all))
	}
}

func TestVideoCacheRefreshAllAllFail(t *testing.T) {
	silenceLogs(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	yt := &YouTubeClient{APIKey: "key", BaseURL: server.URL, HTTP: server.Client()}
	sources := []Source{
		{Type: "channel", ID: "UC1", Name: "Bad Chan"},
		{Type: "playlist", ID: "PL1", Name: "Bad List"},
	}

	cache := &VideoCache{}
	err := cache.RefreshAll(yt, sources)
	if err == nil {
		t.Error("expected error when all sources fail")
	}
}

func TestStartPeriodicRefresh(t *testing.T) {
	silenceLogs(t)
	var callCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
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

	if callCount.Load() < 2 {
		t.Errorf("expected at least 2 refresh calls, got %d", callCount.Load())
	}
}
