//go:build repro

// Manual-verification harness for the frontend: serves the real handlers,
// templates, and static assets against a cache of synthetic videos, so the
// page (infinite scroll, DOM pruning, playback) can be exercised in a browser
// without a YouTube API key. Excluded from normal test runs by the build tag.
//
//	go test -tags repro -run TestReproHarness -timeout 0
//	# open http://127.0.0.1:8431
package main

import (
	"fmt"
	"net/http"
	"testing"
)

const thumbSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="320" height="180"><rect width="320" height="180" fill="#cde"/><text x="160" y="95" text-anchor="middle" font-size="24">thumb</text></svg>`

func TestReproHarness(t *testing.T) {
	cache := &VideoCache{}
	var videos []Video
	for s := range 5 {
		for i := range 120 {
			videos = append(videos, Video{
				ID:           fmt.Sprintf("v%d-%d", s, i),
				Title:        fmt.Sprintf("Source %d video %d", s, i),
				ThumbnailURL: "/thumb.svg",
				SourceID:     fmt.Sprintf("src%d", s),
			})
		}
	}
	cache.Store(videos)

	tmpl, err := parseTemplates()
	if err != nil {
		t.Fatalf("parsing templates: %v", err)
	}

	const pageSize = 30
	mux := http.NewServeMux()
	mux.Handle("/", &GridHandler{Cache: cache, Template: tmpl, PageSize: pageSize})
	mux.Handle("/videos", &VideosHandler{Cache: cache, Template: tmpl, PageSize: pageSize, MaxCount: 60})
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	mux.HandleFunc("/thumb.svg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		fmt.Fprint(w, thumbSVG)
	})

	addr := "127.0.0.1:8431"
	t.Logf("repro harness listening on http://%s/", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		t.Fatalf("ListenAndServe: %v", err)
	}
}
