package sync

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/yxuechao007/claude_sync/internal/config"
)

const mcpMetaFile = "claude_sync.meta.json"

var mcpKeys = []string{"mcp", "mcpServers"}

type mcpMeta struct {
	Version int    `json:"version"`
	Repo    string `json:"repo,omitempty"`
}

type localWrite struct {
	item    config.SyncItem
	content string
}

type remoteInfo struct {
	content string
	exists  bool
}

// SyncMCPOnInit merges MCP config on init based on gist version.
func (e *Engine) SyncMCPOnInit() error {
	remoteGist, err := e.client.Get(e.cfg.GistID)
	if err != nil {
		return fmt.Errorf("failed to get gist: %w", err)
	}

	meta, err := readMCPMeta(remoteGist.Files[mcpMetaFile].Content)
	if err != nil {
		return fmt.Errorf("failed to parse MCP meta: %w", err)
	}

	remoteVersion := meta.Version
	localVersion := e.state.MCPVersion
	preferLocal := localVersion >= remoteVersion
	metaNeedsUpdate := false
	if meta.Repo == "" {
		meta.Repo = config.RepoURL
		metaNeedsUpdate = true
	}

	updates := make(map[string]string)
	var writes []localWrite
	remoteByItem := make(map[string]remoteInfo)
	var mcpItems []config.SyncItem

	for _, item := range e.cfg.GetEnabledItems() {
		if !isMCPItem(item) {
			continue
		}

		mcpItems = append(mcpItems, item)

		localPath, err := config.ExpandPath(item.LocalPath)
		if err != nil {
			return err
		}

		localData, localExists, err := readFileOptional(localPath)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", localPath, err)
		}

		remoteContent := ""
		remoteExists := false
		if remoteFile, ok := remoteGist.Files[item.GistFile]; ok {
			remoteContent = remoteFile.Content
			remoteExists = true
		}
		remoteByItem[item.Name] = remoteInfo{content: remoteContent, exists: remoteExists}

		localObj, err := parseJSONMap(localData)
		if err != nil {
			return fmt.Errorf("failed to parse local %s: %w", localPath, err)
		}

		remoteObj, err := parseJSONMap([]byte(remoteContent))
		if err != nil {
			return fmt.Errorf("failed to parse remote %s: %w", item.GistFile, err)
		}

		forceLocalWrite := false
		if !localExists && len(bytes.TrimSpace([]byte(remoteContent))) > 0 {
			cloned, err := cloneJSONMap(remoteObj)
			if err != nil {
				return fmt.Errorf("failed to clone remote %s: %w", item.GistFile, err)
			}
			localObj = cloned
			forceLocalWrite = hasMCPKeys(remoteObj)
		}

		localChanged := false
		remoteChanged := false

		for _, key := range mcpKeys {
			merged, keyLocalChanged, keyRemoteChanged := mergeMCPValue(localObj[key], remoteObj[key], preferLocal)
			if merged != nil {
				localObj[key] = merged
				remoteObj[key] = merged
			}
			if keyLocalChanged {
				localChanged = true
			}
			if keyRemoteChanged {
				remoteChanged = true
			}
		}

		if remoteChanged {
			newRemoteContent, err := marshalJSON(remoteObj)
			if err != nil {
				return fmt.Errorf("failed to marshal remote %s: %w", item.GistFile, err)
			}
			if len(bytes.TrimSpace(newRemoteContent)) > 0 {
				updates[item.GistFile] = string(newRemoteContent)
				remoteByItem[item.Name] = remoteInfo{content: string(newRemoteContent), exists: true}
			}
		}

		if localChanged || forceLocalWrite {
			newLocalContent, err := marshalJSON(localObj)
			if err != nil {
				return fmt.Errorf("failed to marshal local %s: %w", localPath, err)
			}
			if len(bytes.TrimSpace(newLocalContent)) > 0 || localExists {
				writes = append(writes, localWrite{item: item, content: string(newLocalContent)})
			}
		}
	}

	if len(updates) > 0 || metaNeedsUpdate {
		metaToWrite := meta
		if len(updates) > 0 {
			metaToWrite.Version = maxInt(remoteVersion, localVersion) + 1
			remoteVersion = metaToWrite.Version
		}
		metaContent, err := marshalJSON(metaToWrite)
		if err != nil {
			return fmt.Errorf("failed to marshal MCP meta: %w", err)
		}
		updates[mcpMetaFile] = string(metaContent)

		if _, err := e.client.Update(e.cfg.GistID, updates); err != nil {
			return fmt.Errorf("failed to update gist: %w", err)
		}
	}

	for _, write := range writes {
		if err := e.writeLocalContent(write.item, write.content); err != nil {
			return fmt.Errorf("failed to write %s: %w", write.item.LocalPath, err)
		}
	}

	now := time.Now()
	for _, item := range mcpItems {
		info := remoteByItem[item.Name]
		remoteHash := ""
		if info.exists {
			remoteHash = calculateHash(info.content)
		}
		e.state.Items[item.Name] = config.ItemState{
			LocalHash:  remoteHash,
			RemoteHash: remoteHash,
			LastSync:   &now,
		}
	}
	if len(mcpItems) > 0 {
		e.state.LastSync = &now
	}
	e.state.MCPVersion = remoteVersion
	if err := e.state.Save(); err != nil {
		return fmt.Errorf("failed to save state: %w", err)
	}

	return nil
}

func readMCPMeta(content string) (mcpMeta, error) {
	if strings.TrimSpace(content) == "" {
		return mcpMeta{}, nil
	}

	var meta mcpMeta
	if err := json.Unmarshal([]byte(content), &meta); err != nil {
		return mcpMeta{}, err
	}

	return meta, nil
}

func isMCPItem(item config.SyncItem) bool {
	base := filepath.Base(item.LocalPath)
	if item.Name == "settings" || item.Name == "claude-json" {
		return true
	}
	if base == "settings.json" || base == ".claude.json" {
		return true
	}
	return strings.HasSuffix(item.LocalPath, "/.claude.json") || strings.HasSuffix(item.LocalPath, "\\.claude.json")
}

func readFileOptional(path string) ([]byte, bool, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		return data, true, nil
	}
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	return nil, false, err
}

func parseJSONMap(data []byte) (map[string]interface{}, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return make(map[string]interface{}), nil
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, err
	}
	if obj == nil {
		obj = make(map[string]interface{})
	}
	return obj, nil
}

func marshalJSON(v interface{}) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}

func cloneJSONMap(src map[string]interface{}) (map[string]interface{}, error) {
	data, err := json.Marshal(src)
	if err != nil {
		return nil, err
	}
	var dst map[string]interface{}
	if err := json.Unmarshal(data, &dst); err != nil {
		return nil, err
	}
	if dst == nil {
		dst = make(map[string]interface{})
	}
	return dst, nil
}

func mergeMCPValue(localVal, remoteVal interface{}, preferLocal bool) (interface{}, bool, bool) {
	localMap, localIsMap := localVal.(map[string]interface{})
	remoteMap, remoteIsMap := remoteVal.(map[string]interface{})

	if localIsMap || remoteIsMap {
		merged := mergeMCPMaps(localMap, remoteMap, preferLocal)
		if merged == nil {
			return nil, false, false
		}
		localChanged := !reflect.DeepEqual(merged, localMap)
		remoteChanged := !reflect.DeepEqual(merged, remoteMap)
		return merged, localChanged, remoteChanged
	}

	if localVal == nil && remoteVal == nil {
		return nil, false, false
	}

	var merged interface{}
	if localVal == nil {
		merged = remoteVal
	} else if remoteVal == nil {
		merged = localVal
	} else if preferLocal {
		merged = localVal
	} else {
		merged = remoteVal
	}

	localChanged := !reflect.DeepEqual(merged, localVal)
	remoteChanged := !reflect.DeepEqual(merged, remoteVal)

	return merged, localChanged, remoteChanged
}

func mergeMCPMaps(localMap, remoteMap map[string]interface{}, preferLocal bool) map[string]interface{} {
	if localMap == nil && remoteMap == nil {
		return nil
	}

	merged := make(map[string]interface{})
	for key, value := range remoteMap {
		merged[key] = value
	}

	for key, localValue := range localMap {
		remoteValue, exists := merged[key]
		if !exists {
			merged[key] = localValue
			continue
		}
		if !reflect.DeepEqual(remoteValue, localValue) && preferLocal {
			merged[key] = localValue
		}
	}

	return merged
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func hasMCPKeys(obj map[string]interface{}) bool {
	for _, key := range mcpKeys {
		if value, ok := obj[key]; ok && value != nil {
			return true
		}
	}
	return false
}
