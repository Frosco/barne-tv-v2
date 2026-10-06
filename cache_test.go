package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
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

// captureLogs redirects log output into the returned buffer, for tests that
// expect the code under test to log.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	orig := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(orig) })
	return &buf
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
