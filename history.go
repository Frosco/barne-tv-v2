package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
)

// WatchHistory keeps the most recently watched videos, newest first. It is
// persisted to a JSON file so the list survives restarts and is shared by
// every device that opens the site.
type WatchHistory struct {
	mu      sync.Mutex
	path    string
	limit   int
	entries []Video // newest first
}

func NewWatchHistory(path string, limit int) *WatchHistory {
	return &WatchHistory{path: path, limit: limit}
}

// Load reads the history file. A missing file is an empty history. An
// unreadable or corrupt file also leaves the history empty, and is reported
// so the caller can log it; the next Record replaces the file.
func (h *WatchHistory) Load() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	data, err := os.ReadFile(h.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err == nil {
		err = json.Unmarshal(data, &h.entries)
	}
	if err != nil {
		h.entries = nil
		return fmt.Errorf("load watch history %s: %w", h.path, err)
	}
	return nil
}

// Record moves v to the top of the history, trimming the oldest entries past
// the limit. The in-memory update stands even when writing the file fails.
func (h *WatchHistory) Record(v Video) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.entries = slices.DeleteFunc(h.entries, func(e Video) bool { return e.ID == v.ID })
	h.entries = slices.Insert(h.entries, 0, v)
	if len(h.entries) > h.limit {
		h.entries = h.entries[:h.limit]
	}
	return h.save()
}

// Entries returns a copy of the history, newest first.
func (h *WatchHistory) Entries() []Video {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.entries)
}

// save writes the entries to a temp file and renames it over the history
// file, so a crash mid-write never leaves a truncated history. The caller
// holds mu.
func (h *WatchHistory) save() error {
	data, err := json.MarshalIndent(h.entries, "", "  ")
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(h.path), ".history-*.json")
	if err != nil {
		return err
	}
	_, err = tmp.Write(data)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), h.path)
	}
	if err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}
