package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	yaml := `youtube_api_key: "test-key-123"
refresh_interval: "2h"
sources:
  - type: channel
    id: "UC123"
    name: "Test Channel"
  - type: playlist
    id: "PL456"
    name: "Test Playlist"
`
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if cfg.YouTubeAPIKey != "test-key-123" {
		t.Errorf("APIKey = %q, want %q", cfg.YouTubeAPIKey, "test-key-123")
	}
	if cfg.RefreshInterval != "2h" {
		t.Errorf("RefreshInterval = %q, want %q", cfg.RefreshInterval, "2h")
	}
	if len(cfg.Sources) != 2 {
		t.Fatalf("Sources count = %d, want 2", len(cfg.Sources))
	}
	if cfg.Sources[0].Type != "channel" || cfg.Sources[0].ID != "UC123" {
		t.Errorf("Sources[0] = %+v, want channel/UC123", cfg.Sources[0])
	}
	if cfg.Sources[1].Type != "playlist" || cfg.Sources[1].ID != "PL456" {
		t.Errorf("Sources[1] = %+v, want playlist/PL456", cfg.Sources[1])
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	yaml := `youtube_api_key: "key"
sources: []
`
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if cfg.RefreshInterval != "6h" {
		t.Errorf("default RefreshInterval = %q, want %q", cfg.RefreshInterval, "6h")
	}
}

func TestLoadConfigMissingAPIKey(t *testing.T) {
	yaml := `sources:
  - type: channel
    id: "UC123"
    name: "Test"
`
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadConfig(path)
	if err == nil {
		t.Error("expected error for missing API key")
	}
}
