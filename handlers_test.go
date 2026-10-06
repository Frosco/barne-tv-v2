package main

import (
	"bytes"
	"html/template"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// testTemplate is a minimal stub: a template named "index.html" with an
// associated "cells" template.
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

func postPlay(handler http.Handler, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/history", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

func TestRecordPlayHandlerRecordsKnownVideo(t *testing.T) {
	history, _ := newTestHistory(t, 10)
	handler := &RecordPlayHandler{Cache: testCache(), History: history}

	w := postPlay(handler, "id=v3")

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
	want := []Video{{ID: "v3", Title: "Three", ThumbnailURL: "http://img/3", SourceID: "S2"}}
	if got := history.Entries(); !reflect.DeepEqual(got, want) {
		t.Errorf("entries = %v, want %v", got, want)
	}
}

func TestRecordPlayHandlerRejectsUnknownOrMissingID(t *testing.T) {
	history, _ := newTestHistory(t, 10)
	handler := &RecordPlayHandler{Cache: testCache(), History: history}

	for _, body := range []string{"id=nope", "id=", ""} {
		if w := postPlay(handler, body); w.Code != http.StatusBadRequest {
			t.Errorf("body %q: status = %d, want 400", body, w.Code)
		}
	}
	if n := len(history.Entries()); n != 0 {
		t.Errorf("history has %d entries, want 0", n)
	}
}

func TestRecordPlayHandlerRejectsOversizedBody(t *testing.T) {
	history, _ := newTestHistory(t, 10)
	handler := &RecordPlayHandler{Cache: testCache(), History: history}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	mw.WriteField("id", "v1")
	file, _ := mw.CreateFormFile("upload", "big.bin")
	file.Write(bytes.Repeat([]byte("x"), 8<<10))
	mw.Close()
	req := httptest.NewRequest("POST", "/history", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
	if n := len(history.Entries()); n != 0 {
		t.Errorf("history has %d entries, want 0", n)
	}
}

func TestRecordPlayHandlerReportsWriteFailure(t *testing.T) {
	logs := captureLogs(t)
	history := NewWatchHistory(filepath.Join(t.TempDir(), "missing-dir", "history.json"), 10)
	handler := &RecordPlayHandler{Cache: testCache(), History: history}

	w := postPlay(handler, "id=v1")

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	if !strings.Contains(logs.String(), "recording play of v1") {
		t.Errorf("log = %q, want it to mention recording play of v1", logs.String())
	}
}

func realTemplates(t *testing.T) *template.Template {
	t.Helper()
	tmpl, err := parseTemplates()
	if err != nil {
		t.Fatalf("parsing templates: %v", err)
	}
	return tmpl
}

func TestGridHandlerWithRealTemplates(t *testing.T) {
	handler := &GridHandler{Cache: testCache(), Template: realTemplates(t), PageSize: 9}

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if got := strings.Count(body, "data-video-id="); got != 9 {
		t.Errorf("found %d videos, want 9", got)
	}
	for _, want := range []string{
		`<title>Barne-TV</title>`,
		`<meta name="robots" content="noindex, nofollow">`,
		`href="/history" aria-label="Sett før"`,
		`data-seed="`,
		`id="scroll-sentinel"`,
		`data-next-offset="`,
		`id="player-container"`,
		`https://www.youtube.com/iframe_api`,
		`/static/style.css`,
		`/static/app.js`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q", want)
		}
	}
}

func getHistory(t *testing.T, h *WatchHistory) *httptest.ResponseRecorder {
	t.Helper()
	handler := &HistoryHandler{History: h, Template: realTemplates(t)}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/history", nil))
	return w
}

func TestHistoryHandlerRendersNewestFirst(t *testing.T) {
	h, _ := newTestHistory(t, 10)
	record(t, h, vid("v1"), vid("v2"), vid("v3"))

	w := getHistory(t, h)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if got := w.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	body := w.Body.String()
	p3 := strings.Index(body, `data-video-id="v3"`)
	p2 := strings.Index(body, `data-video-id="v2"`)
	p1 := strings.Index(body, `data-video-id="v1"`)
	if p3 < 0 || p2 < 0 || p1 < 0 || !(p3 < p2 && p2 < p1) {
		t.Errorf("positions v3=%d v2=%d v1=%d, want present and ascending", p3, p2, p1)
	}
	if strings.Contains(body, "data-seed") {
		t.Error("history grid must not carry data-seed")
	}
	for _, want := range []string{
		`<title>Barne-TV – sett før</title>`,
		`<h1 class="history-heading">Sett før</h1>`,
		`href="/" aria-label="Tilbake til alle videoer"`,
		`id="player-container"`,
		`/static/style.css`,
		`/static/app.js`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q", want)
		}
	}
}

func TestHistoryHandlerEmptyHistory(t *testing.T) {
	h, _ := newTestHistory(t, 10)

	body := getHistory(t, h).Body.String()

	if !strings.Contains(body, `<p class="history-empty">Ingen videoer sett ennå</p>`) {
		t.Error("body missing the empty-history line")
	}
	if got := strings.Count(body, "data-video-id="); got != 0 {
		t.Errorf("found %d videos, want 0", got)
	}
}

func TestHistoryHandlerEscapesTitles(t *testing.T) {
	h, _ := newTestHistory(t, 10)
	v := vid("v")
	v.Title = "<b>Tom & \"Jerry\"</b>"
	record(t, h, v)

	body := getHistory(t, h).Body.String()

	if !strings.Contains(body, "&lt;b&gt;Tom &amp;") {
		t.Error("title not escaped")
	}
	if strings.Contains(body, "<b>Tom") {
		t.Error("raw title markup reached the page")
	}
}
