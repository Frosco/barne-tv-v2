package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchPlaylistVideos(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/playlistItems" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("playlistId") != "PL123" {
			t.Errorf("unexpected playlistId: %s", r.URL.Query().Get("playlistId"))
		}
		if r.URL.Query().Get("key") != "test-key" {
			t.Errorf("unexpected key: %s", r.URL.Query().Get("key"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"items": [
				{
					"snippet": {
						"title": "Video One",
						"resourceId": {"videoId": "vid1"},
						"thumbnails": {"high": {"url": "https://img.youtube.com/vi/vid1/hqdefault.jpg"}}
					}
				},
				{
					"snippet": {
						"title": "Video Two",
						"resourceId": {"videoId": "vid2"},
						"thumbnails": {"high": {"url": "https://img.youtube.com/vi/vid2/hqdefault.jpg"}}
					}
				}
			]
		}`))
	}))
	defer server.Close()

	client := &YouTubeClient{
		APIKey:  "test-key",
		BaseURL: server.URL,
		HTTP:    server.Client(),
	}

	videos, err := client.FetchPlaylistVideos("PL123")
	if err != nil {
		t.Fatalf("FetchPlaylistVideos: %v", err)
	}

	if len(videos) != 2 {
		t.Fatalf("got %d videos, want 2", len(videos))
	}
	if videos[0].ID != "vid1" || videos[0].Title != "Video One" {
		t.Errorf("videos[0] = %+v", videos[0])
	}
	if videos[1].ID != "vid2" || videos[1].Title != "Video Two" {
		t.Errorf("videos[1] = %+v", videos[1])
	}
}

func TestFetchPlaylistVideosPagination(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		callCount++
		if callCount == 1 {
			w.Write([]byte(`{
				"items": [{"snippet": {"title": "V1", "resourceId": {"videoId": "v1"}, "thumbnails": {"high": {"url": "http://img/v1"}}}}],
				"nextPageToken": "page2"
			}`))
		} else {
			if r.URL.Query().Get("pageToken") != "page2" {
				t.Errorf("expected pageToken=page2, got %s", r.URL.Query().Get("pageToken"))
			}
			w.Write([]byte(`{
				"items": [{"snippet": {"title": "V2", "resourceId": {"videoId": "v2"}, "thumbnails": {"high": {"url": "http://img/v2"}}}}]
			}`))
		}
	}))
	defer server.Close()

	client := &YouTubeClient{APIKey: "key", BaseURL: server.URL, HTTP: server.Client()}
	videos, err := client.FetchPlaylistVideos("PL123")
	if err != nil {
		t.Fatalf("FetchPlaylistVideos: %v", err)
	}
	if len(videos) != 2 {
		t.Fatalf("got %d videos, want 2", len(videos))
	}
}

func TestFetchChannelVideos(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/channels":
			if r.URL.Query().Get("id") != "UC123" {
				t.Errorf("unexpected channel id: %s", r.URL.Query().Get("id"))
			}
			w.Write([]byte(`{
				"items": [{"contentDetails": {"relatedPlaylists": {"uploads": "UU123"}}}]
			}`))
		case "/playlistItems":
			if r.URL.Query().Get("playlistId") != "UU123" {
				t.Errorf("unexpected playlistId: %s", r.URL.Query().Get("playlistId"))
			}
			w.Write([]byte(`{
				"items": [{"snippet": {"title": "Channel Vid", "resourceId": {"videoId": "cv1"}, "thumbnails": {"high": {"url": "http://img/cv1"}}}}]
			}`))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := &YouTubeClient{APIKey: "key", BaseURL: server.URL, HTTP: server.Client()}
	videos, err := client.FetchChannelVideos("UC123")
	if err != nil {
		t.Fatalf("FetchChannelVideos: %v", err)
	}
	if len(videos) != 1 || videos[0].ID != "cv1" {
		t.Errorf("videos = %+v", videos)
	}
}

func TestFetchChannelVideosNoItems(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"items": []}`))
	}))
	defer server.Close()

	client := &YouTubeClient{APIKey: "key", BaseURL: server.URL, HTTP: server.Client()}
	_, err := client.FetchChannelVideos("UC999")
	if err == nil {
		t.Error("expected error for empty channel response")
	}
}
