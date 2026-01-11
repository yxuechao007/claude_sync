package sync

import (
	"encoding/json"
	"strings"

	"github.com/yxuechao007/claude_sync/internal/config"
)

const syncMetaFile = "claude_sync.meta.json"

type syncMeta struct {
	Version int    `json:"version"`
	Repo    string `json:"repo,omitempty"`
}

func readSyncMeta(content string) (syncMeta, error) {
	if strings.TrimSpace(content) == "" {
		return syncMeta{}, nil
	}

	var meta syncMeta
	if err := json.Unmarshal([]byte(content), &meta); err != nil {
		return syncMeta{}, err
	}

	return meta, nil
}

func ensureSyncMetaRepo(meta syncMeta) (syncMeta, bool) {
	if meta.Repo != "" {
		return meta, false
	}
	meta.Repo = config.RepoURL
	return meta, true
}
