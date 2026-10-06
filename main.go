package main

import (
	"flag"
	"html/template"
	"log"
	"net/http"
	"time"
)

const historyLimit = 50

// newServeMux registers every route, so main and the repro harness serve the same thing.
func newServeMux(cache *VideoCache, history *WatchHistory, tmpl *template.Template) *http.ServeMux {
	const pageSize = 30
	grid := &GridHandler{Cache: cache, Template: tmpl, PageSize: pageSize}
	// MaxCount caps the work per request; clients may fetch up to two pages in one call.
	videos := &VideosHandler{Cache: cache, Template: tmpl, PageSize: pageSize, MaxCount: 60}

	mux := http.NewServeMux()
	mux.Handle("/", grid)
	mux.Handle("/videos", videos)
	mux.Handle("POST /history", &RecordPlayHandler{Cache: cache, History: history})
	mux.Handle("GET /history", &HistoryHandler{History: history, Template: tmpl})
	// no-cache makes browsers revalidate on every load (a cheap 304) instead of
	// reusing a heuristically cached app.js/style.css that no longer matches
	// the HTML.
	static := http.StripPrefix("/static/", http.FileServer(http.Dir("static")))
	mux.HandleFunc("/static/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		static.ServeHTTP(w, r)
	})
	return mux
}

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	addr := flag.String("addr", ":8080", "listen address")
	historyPath := flag.String("history", "history.json", "path to the watch history file")
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

	history := NewWatchHistory(*historyPath, historyLimit)
	if err := history.Load(); err != nil {
		log.Printf("watch history unreadable, starting empty: %v", err)
	}

	tmpl, err := parseTemplates()
	if err != nil {
		log.Fatalf("parsing templates: %v", err)
	}

	log.Printf("listening on %s with %d sources", *addr, len(cfg.Sources))
	log.Fatal(http.ListenAndServe(*addr, newServeMux(cache, history, tmpl)))
}
