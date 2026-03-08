package main

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func setupTestHandler(t *testing.T) (*GridHandler, *VideoCache) {
	t.Helper()
	cache := &VideoCache{}
	cache.Store([]Video{
		{ID: "v1", Title: "Video One", ThumbnailURL: "http://img/1"},
		{ID: "v2", Title: "Video Two", ThumbnailURL: "http://img/2"},
		{ID: "v3", Title: "Video Three", ThumbnailURL: "http://img/3"},
		{ID: "v4", Title: "Video Four", ThumbnailURL: "http://img/4"},
		{ID: "v5", Title: "Video Five", ThumbnailURL: "http://img/5"},
		{ID: "v6", Title: "Video Six", ThumbnailURL: "http://img/6"},
		{ID: "v7", Title: "Video Seven", ThumbnailURL: "http://img/7"},
		{ID: "v8", Title: "Video Eight", ThumbnailURL: "http://img/8"},
		{ID: "v9", Title: "Video Nine", ThumbnailURL: "http://img/9"},
	})

	tmpl := template.Must(template.New("index.html").Parse(
		`{{range .Videos}}<div data-id="{{.ID}}">{{.Title}}</div>{{end}}`,
	))

	handler := &GridHandler{Cache: cache, Template: tmpl, GridSize: 9}
	return handler, cache
}

func TestGridHandlerServesVideos(t *testing.T) {
	handler, _ := setupTestHandler(t)

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	body := w.Body.String()
	// Should contain 9 video divs
	count := strings.Count(body, "data-id=")
	if count != 9 {
		t.Errorf("found %d videos in response, want 9", count)
	}
}

func TestGridHandlerSetsCookie(t *testing.T) {
	handler, _ := setupTestHandler(t)

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	cookies := w.Result().Cookies()
	var gridCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "grid" {
			gridCookie = c
			break
		}
	}
	if gridCookie == nil {
		t.Fatal("no grid cookie set")
	}
	ids := strings.Split(gridCookie.Value, ",")
	if len(ids) != 9 {
		t.Errorf("cookie has %d IDs, want 9", len(ids))
	}
}

func TestGridHandlerReadsFromCookie(t *testing.T) {
	handler, _ := setupTestHandler(t)

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "grid", Value: "v1,v2,v3,v4,v5,v6,v7,v8,v9"})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()
	// Videos should appear in cookie order
	idx1 := strings.Index(body, `data-id="v1"`)
	idx2 := strings.Index(body, `data-id="v2"`)
	if idx1 == -1 || idx2 == -1 {
		t.Fatal("expected v1 and v2 in response")
	}
	if idx1 > idx2 {
		t.Error("cookie order not preserved")
	}
}

func TestGridHandlerShuffleIgnoresCookie(t *testing.T) {
	handler, _ := setupTestHandler(t)

	req := httptest.NewRequest("GET", "/?shuffle=1", nil)
	req.AddCookie(&http.Cookie{Name: "grid", Value: "v1,v2,v3,v4,v5,v6,v7,v8,v9"})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Should still work (200 OK with 9 videos)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	// Cookie should be set with potentially different values
	cookies := w.Result().Cookies()
	var gridCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "grid" {
			gridCookie = c
			break
		}
	}
	if gridCookie == nil {
		t.Fatal("no grid cookie set on shuffle")
	}
}

func TestGridHandlerInvalidCookieFallsBack(t *testing.T) {
	handler, _ := setupTestHandler(t)

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "grid", Value: "v1,v2,MISSING_VIDEO"})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	// Should fall back to random selection (9 videos)
	body := w.Body.String()
	count := strings.Count(body, "data-id=")
	if count != 9 {
		t.Errorf("found %d videos, want 9", count)
	}
}

func TestGridHandlerEmptyCache(t *testing.T) {
	cache := &VideoCache{}
	tmpl := template.Must(template.New("index.html").Parse(
		`{{range .Videos}}<div data-id="{{.ID}}">{{.Title}}</div>{{end}}`,
	))
	handler := &GridHandler{Cache: cache, Template: tmpl, GridSize: 9}

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}
