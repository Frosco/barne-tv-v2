package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

type Video struct {
	ID           string
	Title        string
	ThumbnailURL string
}

type YouTubeClient struct {
	APIKey  string
	BaseURL string // default: https://www.googleapis.com/youtube/v3
	HTTP    *http.Client
}

func NewYouTubeClient(apiKey string) *YouTubeClient {
	return &YouTubeClient{
		APIKey:  apiKey,
		BaseURL: "https://www.googleapis.com/youtube/v3",
		HTTP:    http.DefaultClient,
	}
}

// playlistItemsResponse matches the YouTube playlistItems.list JSON response.
type playlistItemsResponse struct {
	Items []struct {
		Snippet struct {
			Title      string `json:"title"`
			ResourceID struct {
				VideoID string `json:"videoId"`
			} `json:"resourceId"`
			Thumbnails struct {
				High struct {
					URL string `json:"url"`
				} `json:"high"`
			} `json:"thumbnails"`
		} `json:"snippet"`
	} `json:"items"`
	NextPageToken string `json:"nextPageToken"`
}

// channelsResponse matches the YouTube channels.list JSON response.
type channelsResponse struct {
	Items []struct {
		ContentDetails struct {
			RelatedPlaylists struct {
				Uploads string `json:"uploads"`
			} `json:"relatedPlaylists"`
		} `json:"contentDetails"`
	} `json:"items"`
}

func (yt *YouTubeClient) FetchPlaylistVideos(playlistID string) ([]Video, error) {
	var all []Video
	pageToken := ""

	for {
		params := url.Values{
			"part":       {"snippet"},
			"playlistId": {playlistID},
			"maxResults": {"50"},
			"key":        {yt.APIKey},
		}
		if pageToken != "" {
			params.Set("pageToken", pageToken)
		}

		resp, err := yt.HTTP.Get(yt.BaseURL + "/playlistItems?" + params.Encode())
		if err != nil {
			return nil, fmt.Errorf("fetching playlist %s: %w", playlistID, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("YouTube API returned %d for playlist %s", resp.StatusCode, playlistID)
		}

		var result playlistItemsResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, fmt.Errorf("decoding playlist response: %w", err)
		}

		for _, item := range result.Items {
			all = append(all, Video{
				ID:           item.Snippet.ResourceID.VideoID,
				Title:        item.Snippet.Title,
				ThumbnailURL: item.Snippet.Thumbnails.High.URL,
			})
		}

		if result.NextPageToken == "" {
			break
		}
		pageToken = result.NextPageToken
	}

	return all, nil
}

func (yt *YouTubeClient) FetchChannelVideos(channelID string) ([]Video, error) {
	params := url.Values{
		"part": {"contentDetails"},
		"id":   {channelID},
		"key":  {yt.APIKey},
	}

	resp, err := yt.HTTP.Get(yt.BaseURL + "/channels?" + params.Encode())
	if err != nil {
		return nil, fmt.Errorf("fetching channel %s: %w", channelID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("YouTube API returned %d for channel %s", resp.StatusCode, channelID)
	}

	var result channelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding channel response: %w", err)
	}

	if len(result.Items) == 0 {
		return nil, fmt.Errorf("channel %s not found", channelID)
	}

	uploadsPlaylistID := result.Items[0].ContentDetails.RelatedPlaylists.Uploads
	return yt.FetchPlaylistVideos(uploadsPlaylistID)
}
