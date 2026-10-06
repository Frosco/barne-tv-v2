package main

import (
	"bytes"
	"html/template"
	"log"
	"math/rand/v2"
	"net/http"
	"strconv"
)

// parseTemplates parses the page templates and their shared partials.
// index.html must stay first: GridHandler calls Template.Execute, which runs
// the first file of the set.
func parseTemplates() (*template.Template, error) {
	return template.ParseFiles("templates/index.html", "templates/history.html",
		"templates/cells.html", "templates/partials.html")
}

// GridHandler renders the front page: a fresh fair ordering seeded per request,
// with the first screenful server-rendered and the seed + next offset embedded
// for the client's infinite scroll.
type GridHandler struct {
	Cache    *VideoCache
	Template *template.Template
	PageSize int
}

type indexData struct {
	Videos     []Video
	Seed       uint64
	NextOffset int
}

func (h *GridHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	seed := rand.Uint64()
	videos := h.Cache.Page(seed, 0, h.PageSize)

	data := indexData{
		Videos:     videos,
		Seed:       seed,
		NextOffset: len(videos),
	}
	var buf bytes.Buffer
	if err := h.Template.Execute(&buf, data); err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = buf.WriteTo(w)
}

// VideosHandler serves successive screenfuls as an HTML fragment of grid cells,
// identified by (seed, offset). It re-derives the same fair ordering from the
// seed and slices it, looping past the end of the pool.
type VideosHandler struct {
	Cache    *VideoCache
	Template *template.Template
	PageSize int
	MaxCount int
}

func (h *VideosHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	seed, err := strconv.ParseUint(q.Get("seed"), 10, 64)
	if err != nil {
		http.Error(w, "invalid seed", http.StatusBadRequest)
		return
	}

	offset, err := strconv.Atoi(q.Get("offset"))
	if err != nil || offset < 0 {
		http.Error(w, "invalid offset", http.StatusBadRequest)
		return
	}

	count := h.PageSize
	if raw := q.Get("count"); raw != "" {
		count, err = strconv.Atoi(raw)
		if err != nil || count < 1 || count > h.MaxCount {
			http.Error(w, "invalid count", http.StatusBadRequest)
			return
		}
	}

	videos := h.Cache.Page(seed, offset, count)
	var buf bytes.Buffer
	if err := h.Template.ExecuteTemplate(&buf, "cells", videos); err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = buf.WriteTo(w)
}

// HistoryHandler renders the watch history page, newest video first.
type HistoryHandler struct {
	History  *WatchHistory
	Template *template.Template
}

func (h *HistoryHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var buf bytes.Buffer
	if err := h.Template.ExecuteTemplate(&buf, "history.html", h.History.Entries()); err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = buf.WriteTo(w)
}

// RecordPlayHandler records a played video into the watch history. It accepts
// only IDs present in the cache, so the history can't be filled with videos
// from outside the configured sources.
type RecordPlayHandler struct {
	Cache   *VideoCache
	History *WatchHistory
}

func (h *RecordPlayHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id := r.FormValue("id")
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	videos := h.Cache.GetByIDs([]string{id})
	if videos == nil {
		http.Error(w, "unknown video", http.StatusBadRequest)
		return
	}
	if err := h.History.Record(videos[0]); err != nil {
		log.Printf("recording play of %s: %v", id, err)
		http.Error(w, "could not record play", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
