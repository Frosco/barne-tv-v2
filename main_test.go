package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServeMuxRoutes(t *testing.T) {
	cache := testCache()
	history, _ := newTestHistory(t, historyLimit)
	mux := newServeMux(cache, history, realTemplates(t))

	if w := postPlay(mux, "id=v3"); w.Code != http.StatusNoContent {
		t.Fatalf("POST /history status = %d, want 204", w.Code)
	}
	// A source refresh drops v3 from the cache; the history keeps it.
	cache.Store([]Video{{ID: "v1", SourceID: "S1"}})

	get := func(url string) string {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", url, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, want 200", url, w.Code)
		}
		return w.Body.String()
	}

	body := get("/history")
	for _, want := range []string{`data-video-id="v3"`, "Sett før"} {
		if !strings.Contains(body, want) {
			t.Errorf("GET /history body missing %q", want)
		}
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/static/style.css", nil))
	if w.Code != http.StatusOK {
		t.Errorf("GET /static/style.css status = %d, want 200", w.Code)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("GET /static/style.css Cache-Control = %q, want no-cache", got)
	}
	if body := get("/"); !strings.Contains(body, `id="scroll-sentinel"`) {
		t.Errorf("GET / body missing scroll sentinel")
	}
	if body := get("/videos?seed=1&offset=0&count=1"); strings.Count(body, "data-video-id=") != 1 {
		t.Errorf("GET /videos returned %d tiles, want 1", strings.Count(body, "data-video-id="))
	}
}
