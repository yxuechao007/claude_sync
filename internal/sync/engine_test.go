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

func TestGetStatusUsesRemoteVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte(`{"a":"local"}`), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	cfg := &config.Config{
		SyncItems: []config.SyncItem{
			{
				Name:      "settings",
				LocalPath: path,
				GistFile:  "settings.json",
				Enabled:   true,
				Type:      "file",
			},
		},
	}

	engine := &Engine{
		cfg: cfg,
		state: &config.SyncState{
			Version: 1,
			Items: map[string]config.ItemState{
				"settings": {
					LocalHash:  calculateHash(`{"a":"local"}`),
					RemoteHash: calculateHash(`{"a":"local"}`),
				},
			},
		},
	}

	remoteGist := &gist.Gist{
		Files: map[string]gist.GistFile{
			"settings.json": {Content: `{"a":"remote"}`},
			syncMetaFile:    {Content: `{"version":2,"repo":"` + config.RepoURL + `"}`},
		},
	}

	statuses, _, err := engine.getStatusWithRemote(remoteGist)
	if err != nil {
		t.Fatalf("getStatusWithRemote: %v", err)
	}

	if len(statuses) != 1 {
		t.Fatalf("statuses len = %d, want 1", len(statuses))
	}
	if statuses[0].Status != StatusRemoteAhead {
		t.Fatalf("status = %q, want %q", statuses[0].Status, StatusRemoteAhead)
	}
}

func TestCalculateLocalHashFiltersLocalHooks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	data := `{"hooks":{"on_start":{"command":"echo /Users/test/secret"}}}`
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	engine := &Engine{}
	item := config.SyncItem{
		Name:      "settings",
		LocalPath: path,
		Type:      "file",
	}

	hash, err := engine.calculateLocalHash(item)
	if err != nil {
		t.Fatalf("calculateLocalHash: %v", err)
	}

	expected := calculateHash("{}")
	if hash != expected {
		t.Fatalf("hash = %q, want %q", hash, expected)
	}
}

func TestPrepareWriteContentKeepLocalSkipsWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"a":1}`), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	engine := &Engine{mergeStrategy: "local"}
	item := config.SyncItem{
		Name:      "config",
		LocalPath: path,
		Type:      "file",
	}

	prepared, skipWrite, err := engine.prepareWriteContent(item, `{"a":2}`)
	if err != nil {
		t.Fatalf("prepareWriteContent: %v", err)
	}
	if !skipWrite {
		t.Fatalf("skipWrite = false, want true")
	}
	if prepared != `{"a":1}` {
		t.Fatalf("prepared = %s, want %s", prepared, `{"a":1}`)
	}
}
