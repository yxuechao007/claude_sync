package sync

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/yxuechao007/claude_sync/internal/config"
	"github.com/yxuechao007/claude_sync/internal/gist"
)

func TestCalculateLocalHashEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.json")
	if err := os.WriteFile(path, []byte(""), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	engine := &Engine{}
	item := config.SyncItem{
		LocalPath: path,
		Type:      "file",
	}

	hash, err := engine.calculateLocalHash(item)
	if err != nil {
		t.Fatalf("calculateLocalHash: %v", err)
	}
	if hash != "" {
		t.Fatalf("hash = %q, want empty", hash)
	}
}

func TestCalculateLocalHashMissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "missing.json")

	engine := &Engine{}
	item := config.SyncItem{
		LocalPath: path,
		Type:      "file",
	}

	hash, err := engine.calculateLocalHash(item)
	if err != nil {
		t.Fatalf("calculateLocalHash: %v", err)
	}
	if hash != "" {
		t.Fatalf("hash = %q, want empty", hash)
	}
}

func TestGetItemStatusMissingLocalTreatsRemoteAhead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	content := "remote"
	remoteHash := calculateHash(content)

	engine := &Engine{
		state: &config.SyncState{
			Items: map[string]config.ItemState{
				"settings": {
					LocalHash:  "previous",
					RemoteHash: remoteHash,
				},
			},
		},
	}

	item := config.SyncItem{
		Name:      "settings",
		LocalPath: path,
		GistFile:  "settings.json",
		Type:      "file",
	}

	remoteGist := &gist.Gist{
		Files: map[string]gist.GistFile{
			"settings.json": {Content: content},
		},
	}

	status := engine.getItemStatus(item, remoteGist)
	if status.Status != StatusRemoteAhead {
		t.Fatalf("status = %q, want %q", status.Status, StatusRemoteAhead)
	}
}
