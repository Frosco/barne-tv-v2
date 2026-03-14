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

	handler := &GridHandler{Cache: cache, Template: tmpl, GridSize: 18}

	http.Handle("/", handler)
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	log.Printf("listening on %s with %d sources", *addr, len(cfg.Sources))
	log.Fatal(http.ListenAndServe(*addr, nil))
}
