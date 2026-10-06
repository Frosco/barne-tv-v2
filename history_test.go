package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sync"
	"testing"
)

func vid(id string) Video {
	return Video{ID: id, Title: "Title " + id, ThumbnailURL: "http://img/" + id, SourceID: "S1"}
}

func ids(videos []Video) []string {
	out := make([]string, len(videos))
	for i, v := range videos {
		out[i] = v.ID
	}
	return out
}

func newTestHistory(t *testing.T, limit int) (*WatchHistory, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "history.json")
	return NewWatchHistory(path, limit), path
}

func record(t *testing.T, h *WatchHistory, videos ...Video) {
	t.Helper()
	for _, v := range videos {
		if err := h.Record(v); err != nil {
			t.Fatalf("Record(%s): %v", v.ID, err)
		}
	}
}

func TestWatchHistoryRecordsNewestFirst(t *testing.T) {
	h, _ := newTestHistory(t, 50)
	record(t, h, vid("v1"), vid("v2"), vid("v3"))

	if got, want := ids(h.Entries()), []string{"v3", "v2", "v1"}; !slices.Equal(got, want) {
		t.Errorf("ids = %v, want %v", got, want)
	}
}

func TestWatchHistoryRepeatMovesToTop(t *testing.T) {
	h, _ := newTestHistory(t, 50)
	renamed := vid("v1")
	renamed.Title = "Renamed"
	record(t, h, vid("v1"), vid("v2"), vid("v3"), renamed)

	got := h.Entries()
	if want := []string{"v1", "v3", "v2"}; !slices.Equal(ids(got), want) {
		t.Errorf("ids = %v, want %v", ids(got), want)
	}
	if got[0].Title != "Renamed" {
		t.Errorf("Title = %q, want Renamed", got[0].Title)
	}
}

func TestWatchHistoryCapsAtLimit(t *testing.T) {
	h, _ := newTestHistory(t, 3)
	record(t, h, vid("v1"), vid("v2"), vid("v3"), vid("v4"), vid("v5"))

	if got, want := ids(h.Entries()), []string{"v5", "v4", "v3"}; !slices.Equal(got, want) {
		t.Errorf("ids = %v, want %v", got, want)
	}
}

func TestWatchHistorySurvivesReload(t *testing.T) {
	h, path := newTestHistory(t, 50)
	record(t, h, vid("v1"), vid("v2"))

	reloaded := NewWatchHistory(path, 50)
	if err := reloaded.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, want := reloaded.Entries(), []Video{vid("v2"), vid("v1")}; !reflect.DeepEqual(got, want) {
		t.Errorf("entries = %v, want %v", got, want)
	}
}

func TestWatchHistoryLoadMissingFile(t *testing.T) {
	h, _ := newTestHistory(t, 50)

	if err := h.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if n := len(h.Entries()); n != 0 {
		t.Errorf("len(Entries) = %d, want 0", n)
	}
}

func TestWatchHistoryLoadCorruptFile(t *testing.T) {
	h, path := newTestHistory(t, 50)
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := h.Load(); err == nil {
		t.Error("Load: want error for corrupt file, got nil")
	}
	if n := len(h.Entries()); n != 0 {
		t.Errorf("len(Entries) = %d, want 0", n)
	}
}

func TestWatchHistoryRecordAfterCorruptLoadReplacesFile(t *testing.T) {
	h, path := newTestHistory(t, 50)
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := h.Load(); err == nil {
		t.Fatal("Load: want error for corrupt file, got nil")
	}
	record(t, h, vid("v1"))

	reloaded := NewWatchHistory(path, 50)
	if err := reloaded.Load(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got, want := ids(reloaded.Entries()), []string{"v1"}; !slices.Equal(got, want) {
		t.Errorf("ids = %v, want %v", got, want)
	}
}

func TestWatchHistoryWriteFailureKeepsEntryInMemory(t *testing.T) {
	h := NewWatchHistory(filepath.Join(t.TempDir(), "missing-dir", "history.json"), 50)

	if err := h.Record(vid("v1")); err == nil {
		t.Error("Record: want error when the directory is missing, got nil")
	}
	if got, want := ids(h.Entries()), []string{"v1"}; !slices.Equal(got, want) {
		t.Errorf("ids = %v, want %v", got, want)
	}
}

func TestWatchHistoryRemovesTempFileWhenRenameFails(t *testing.T) {
	h, path := newTestHistory(t, 50)
	// A directory at the history path lets CreateTemp and the write succeed
	// but makes the final rename fail.
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := h.Record(vid("v1")); err == nil {
		t.Error("Record: want error when the rename fails, got nil")
	}

	files, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Name() != "history.json" {
		names := make([]string, len(files))
		for i, f := range files {
			names[i] = f.Name()
		}
		t.Errorf("dir contains %v, want only the history.json directory", names)
	}
}

func TestWatchHistoryLeavesNoTempFiles(t *testing.T) {
	h, path := newTestHistory(t, 50)
	record(t, h, vid("v1"), vid("v2"))

	files, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Name() != "history.json" {
		names := make([]string, len(files))
		for i, f := range files {
			names[i] = f.Name()
		}
		t.Errorf("dir contains %v, want only history.json", names)
	}
}

func TestWatchHistoryEntriesIsACopy(t *testing.T) {
	h, _ := newTestHistory(t, 50)
	record(t, h, vid("v1"))

	e := h.Entries()
	e[0].ID = "x"

	if got := h.Entries()[0].ID; got != "v1" {
		t.Errorf("ID = %q, want v1", got)
	}
}

func TestWatchHistoryConcurrentRecords(t *testing.T) {
	h, path := newTestHistory(t, 50)

	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := h.Record(vid(fmt.Sprintf("v%d", i))); err != nil {
				t.Errorf("Record(v%d): %v", i, err)
			}
		}()
	}
	wg.Wait()

	if n := len(h.Entries()); n != 20 {
		t.Errorf("len(Entries) = %d, want 20", n)
	}
	reloaded := NewWatchHistory(path, 50)
	if err := reloaded.Load(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if n := len(reloaded.Entries()); n != 20 {
		t.Errorf("reloaded len = %d, want 20", n)
	}
}
