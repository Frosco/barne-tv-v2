package main

import (
	"fmt"
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


func TestRandomCappedEmpty(t *testing.T) {
	cache := &VideoCache{}
	if got := cache.RandomCapped(9, 2); got != nil {
		t.Errorf("RandomCapped on empty cache = %+v, want nil", got)
	}
}

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

func TestRandomCappedRespectsCap(t *testing.T) {
	cache := &VideoCache{}
	var videos []Video
	for i := range 100 {
		videos = append(videos, Video{ID: fmt.Sprintf("a%d", i), SourceID: "A"})
	}
	for _, src := range []string{"B", "C", "D", "E"} {
		for i := range 10 {
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

func TestRandomCappedDistributesAcrossSources(t *testing.T) {
	cache := &VideoCache{}
	var videos []Video
	for _, src := range []string{"A", "B", "C", "D", "E"} {
		for i := range 20 {
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

func TestRandomCappedRelaxesWhenUnderFilled(t *testing.T) {
	cache := &VideoCache{}
	var videos []Video
	for _, src := range []string{"A", "B"} {
		for i := range 50 {
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
