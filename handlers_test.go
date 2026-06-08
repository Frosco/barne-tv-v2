package main

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// testTemplate mirrors main.go's parse of index.html + cells.html: a template
// named "index.html" with an associated "cells" template.
func testTemplate(t *testing.T) *template.Template {
	t.Helper()
	tmpl := template.Must(template.New("index.html").Parse(
		`<div class="grid" data-seed="{{.Seed}}" data-next-offset="{{.NextOffset}}">{{template "cells" .Videos}}</div>`))
	template.Must(tmpl.New("cells").Parse(
		`{{range .}}<div class="grid-cell" data-video-id="{{.ID}}">{{.Title}}</div>{{end}}`))
	return tmpl
}

func testCache() *VideoCache {
	cache := &VideoCache{}
	cache.Store([]Video{
		{ID: "v1", Title: "One", ThumbnailURL: "http://img/1", SourceID: "S1"},
		{ID: "v2", Title: "Two", ThumbnailURL: "http://img/2", SourceID: "S1"},
		{ID: "v3", Title: "Three", ThumbnailURL: "http://img/3", SourceID: "S2"},
		{ID: "v4", Title: "Four", ThumbnailURL: "http://img/4", SourceID: "S2"},
		{ID: "v5", Title: "Five", ThumbnailURL: "http://img/5", SourceID: "S3"},
		{ID: "v6", Title: "Six", ThumbnailURL: "http://img/6", SourceID: "S3"},
		{ID: "v7", Title: "Seven", ThumbnailURL: "http://img/7", SourceID: "S1"},
		{ID: "v8", Title: "Eight", ThumbnailURL: "http://img/8", SourceID: "S2"},
		{ID: "v9", Title: "Nine", ThumbnailURL: "http://img/9", SourceID: "S3"},
	})
	return cache
}

func TestGridHandlerServesFirstPage(t *testing.T) {
	handler := &GridHandler{Cache: testCache(), Template: testTemplate(t), PageSize: 9}

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if got := strings.Count(body, "data-video-id="); got != 9 {
		t.Errorf("found %d videos, want 9", got)
	}
	if !strings.Contains(body, `data-next-offset="9"`) {
		t.Errorf("missing data-next-offset=\"9\" in %q", body)
	}
	if strings.Contains(body, `data-seed=""`) {
		t.Error("data-seed is empty; expected a generated seed")
	}
}

func TestGridHandlerEmptyCache(t *testing.T) {
	handler := &GridHandler{Cache: &VideoCache{}, Template: testTemplate(t), PageSize: 9}

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if got := strings.Count(body, "data-video-id="); got != 0 {
		t.Errorf("found %d videos, want 0", got)
	}
	if !strings.Contains(body, `data-next-offset="0"`) {
		t.Errorf("missing data-next-offset=\"0\" in %q", body)
	}
}

func TestVideosHandlerReturnsCells(t *testing.T) {
	handler := &VideosHandler{Cache: testCache(), Template: testTemplate(t), PageSize: 9, MaxCount: 60}

	req := httptest.NewRequest("GET", "/videos?seed=1&offset=0&count=5", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if got := strings.Count(w.Body.String(), "data-video-id="); got != 5 {
		t.Errorf("found %d cells, want 5", got)
	}
}

func TestVideosHandlerDefaultsCount(t *testing.T) {
	handler := &VideosHandler{Cache: testCache(), Template: testTemplate(t), PageSize: 9, MaxCount: 60}

	req := httptest.NewRequest("GET", "/videos?seed=1&offset=0", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if got := strings.Count(w.Body.String(), "data-video-id="); got != 9 {
		t.Errorf("found %d cells, want 9 (PageSize default)", got)
	}
}

func TestVideosHandlerLoopsToFillCount(t *testing.T) {
	cache := &VideoCache{}
	cache.Store([]Video{{ID: "v1", SourceID: "S1"}, {ID: "v2", SourceID: "S1"}, {ID: "v3", SourceID: "S2"}})
	handler := &VideosHandler{Cache: cache, Template: testTemplate(t), PageSize: 9, MaxCount: 60}

	req := httptest.NewRequest("GET", "/videos?seed=1&offset=0&count=10", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if got := strings.Count(w.Body.String(), "data-video-id="); got != 10 {
		t.Errorf("found %d cells, want 10 (short pool loops)", got)
	}
}

func TestVideosHandlerEmptyPool(t *testing.T) {
	handler := &VideosHandler{Cache: &VideoCache{}, Template: testTemplate(t), PageSize: 9, MaxCount: 60}

	req := httptest.NewRequest("GET", "/videos?seed=1&offset=0&count=5", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if got := strings.Count(w.Body.String(), "data-video-id="); got != 0 {
		t.Errorf("found %d cells, want 0 (empty pool)", got)
	}
}

func TestVideosHandlerValidatesParams(t *testing.T) {
	handler := &VideosHandler{Cache: testCache(), Template: testTemplate(t), PageSize: 9, MaxCount: 60}

	cases := []string{
		"/videos?offset=0&count=5",          // missing seed
		"/videos?seed=abc&offset=0&count=5", // non-numeric seed
		"/videos?seed=1&offset=-1&count=5",  // negative offset
		"/videos?seed=1&offset=x&count=5",   // non-numeric offset
		"/videos?seed=1&offset=0&count=0",   // count below 1
		"/videos?seed=1&offset=0&count=999", // count above MaxCount
	}
	for _, url := range cases {
		req := httptest.NewRequest("GET", url, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", url, w.Code)
		}
	}
}
