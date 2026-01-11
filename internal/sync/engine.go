package sync

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/yxuechao007/claude_sync/internal/archive"
	"github.com/yxuechao007/claude_sync/internal/config"
	"github.com/yxuechao007/claude_sync/internal/diff"
	"github.com/yxuechao007/claude_sync/internal/filter"
	"github.com/yxuechao007/claude_sync/internal/gist"
	"github.com/yxuechao007/claude_sync/internal/mcp"
)

// SyncStatus represents the status of a sync item
type SyncStatus string

const (
	StatusSynced      SyncStatus = "synced"
	StatusLocalAhead  SyncStatus = "local_ahead"
	StatusRemoteAhead SyncStatus = "remote_ahead"
	StatusConflict    SyncStatus = "conflict"
	StatusError       SyncStatus = "error"
	StatusNew         SyncStatus = "new"
)

// ItemStatus holds the status information for a sync item
type ItemStatus struct {
	Name       string
	Status     SyncStatus
	LocalHash  string
	RemoteHash string
	LocalPath  string
	GistFile   string
	Error      error
}

// Engine handles the sync operations
type Engine struct {
	cfg     *config.Config
	state   *config.SyncState
	client  *gist.Client
	autoYes bool // 自动确认所有修改
}

type syncDirection string

const (
	directionSynced   syncDirection = "synced"
	directionLocal    syncDirection = "local"
	directionRemote   syncDirection = "remote"
	directionConflict syncDirection = "conflict"
)

type statusInfo struct {
	meta                   syncMeta
	metaNeedsUpdate        bool
	remoteVersion          int
	effectiveRemoteVersion int
	localVersion           int
	localDirty             bool
	remoteDirty            bool
	direction              syncDirection
}

// SetAutoYes 设置是否自动确认所有修改
func (e *Engine) SetAutoYes(yes bool) {
	e.autoYes = yes
}

// NewEngine creates a new sync engine
func NewEngine(cfg *config.Config, token string) (*Engine, error) {
	state, err := config.LoadState()
	if err != nil {
		return nil, fmt.Errorf("failed to load state: %w", err)
	}

	return &Engine{
		cfg:    cfg,
		state:  state,
		client: gist.NewClient(token),
	}, nil
}

// GetStatus returns the sync status for all items
func (e *Engine) GetStatus() ([]ItemStatus, error) {
	// Get remote gist
	remoteGist, err := e.client.Get(e.cfg.GistID)
	if err != nil {
		return nil, fmt.Errorf("failed to get gist: %w", err)
	}
	statuses, _, err := e.getStatusWithRemote(remoteGist)
	return statuses, err
}

func (e *Engine) getStatusWithRemote(remoteGist *gist.Gist) ([]ItemStatus, statusInfo, error) {
	var info statusInfo
	var metaContent string
	if metaFile, ok := remoteGist.Files[syncMetaFile]; ok {
		metaContent = metaFile.Content
	}
	meta, err := readSyncMeta(metaContent)
	if err != nil {
		return nil, info, fmt.Errorf("failed to parse sync meta: %w", err)
	}
	meta, metaNeedsUpdate := ensureSyncMetaRepo(meta)
	info.meta = meta
	info.metaNeedsUpdate = metaNeedsUpdate
	info.remoteVersion = meta.Version

	type itemSnapshot struct {
		status        ItemStatus
		localHash     string
		remoteHash    string
		localChanged  bool
		remoteChanged bool
	}

	var snapshots []itemSnapshot
	for _, item := range e.cfg.GetEnabledItems() {
		status := ItemStatus{
			Name:      item.Name,
			LocalPath: item.LocalPath,
			GistFile:  item.GistFile,
		}

		localHash, err := e.calculateLocalHash(item)
		if err != nil {
			status.Status = StatusError
			status.Error = err
			snapshots = append(snapshots, itemSnapshot{status: status})
			continue
		}
		status.LocalHash = localHash

		remoteHash := ""
		if remoteFile, exists := remoteGist.Files[item.GistFile]; exists {
			remoteHash = calculateHash(remoteFile.Content)
		}
		status.RemoteHash = remoteHash

		lastState, hasState := e.state.Items[item.Name]
		localChanged := false
		remoteChanged := false
		if !hasState {
			localChanged = localHash != ""
			remoteChanged = remoteHash != ""
		} else {
			localChanged = localHash != lastState.LocalHash
			remoteChanged = remoteHash != lastState.RemoteHash
		}

		if localChanged {
			info.localDirty = true
		}
		if remoteChanged {
			info.remoteDirty = true
		}

		snapshots = append(snapshots, itemSnapshot{
			status:        status,
			localHash:     localHash,
			remoteHash:    remoteHash,
			localChanged:  localChanged,
			remoteChanged: remoteChanged,
		})
	}

	localVersion := e.state.Version
	if info.localDirty {
		localVersion++
	}
	info.localVersion = localVersion

	effectiveRemoteVersion := info.remoteVersion
	if info.remoteDirty && effectiveRemoteVersion <= e.state.Version {
		effectiveRemoteVersion = e.state.Version + 1
	}
	info.effectiveRemoteVersion = effectiveRemoteVersion

	switch {
	case !info.localDirty && !info.remoteDirty:
		info.direction = directionSynced
	case info.localVersion > effectiveRemoteVersion:
		info.direction = directionLocal
	case effectiveRemoteVersion > info.localVersion:
		info.direction = directionRemote
	default:
		if info.localDirty && info.remoteDirty {
			info.direction = directionConflict
		} else if info.localDirty {
			info.direction = directionLocal
		} else {
			info.direction = directionRemote
		}
	}

	var statuses []ItemStatus
	for _, snap := range snapshots {
		status := snap.status
		if status.Status == StatusError {
			statuses = append(statuses, status)
			continue
		}

		if snap.localHash == snap.remoteHash {
			status.Status = StatusSynced
		} else {
			switch info.direction {
			case directionLocal:
				status.Status = StatusLocalAhead
			case directionRemote:
				status.Status = StatusRemoteAhead
			case directionConflict:
				status.Status = StatusConflict
			default:
				status.Status = StatusConflict
			}
		}

		statuses = append(statuses, status)
	}

	return statuses, info, nil
}

// calculateLocalHash calculates the hash of local content
func (e *Engine) calculateLocalHash(item config.SyncItem) (string, error) {
	localPath, err := config.ExpandPath(item.LocalPath)
	if err != nil {
		return "", err
	}

	if item.Type == "directory" {
		content, err := archive.PackDirectory(localPath)
		if err != nil {
			return "", err
		}
		if content == "" {
			return "", nil
		}
		return calculateHash(content), nil
	}

	// File type
	data, err := os.ReadFile(localPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	if len(data) == 0 {
		return "", nil
	}

	if shouldMergeProjectMCP(item, localPath) {
		merged, changed, err := mcp.MergeProjectMCPServersIntoGlobal(data)
		if err != nil {
			return "", err
		}
		if changed {
			data = merged
		}
	}

	// Apply filter if configured
	if item.Filter != nil {
		data, err = filter.FilterJSON(data, item.Filter)
		if err != nil {
			return "", err
		}
	}

	// 对 settings 文件过滤包含本地内容的 hooks（与 push 保持一致）
	if item.Name == "settings" {
		filteredData, filteredTypes, err := filter.FilterLocalHooks(data)
		if err == nil && len(filteredTypes) > 0 {
			data = filteredData
		}
	}
	if len(data) == 0 {
		return "", nil
	}

	return calculateHash(string(data)), nil
}

// Push uploads local content to the gist
func (e *Engine) Push(dryRun bool, force bool) ([]ItemStatus, error) {
	remoteGist, err := e.client.Get(e.cfg.GistID)
	if err != nil {
		return nil, fmt.Errorf("failed to get gist: %w", err)
	}
	statuses, info, err := e.getStatusWithRemote(remoteGist)
	if err != nil {
		return nil, err
	}

	updates := make(map[string]string)
	var results []ItemStatus

	for _, status := range statuses {
		item := e.findItem(status.Name)
		if item == nil {
			continue
		}

		// Check if we should push
		shouldPush := false
		switch status.Status {
		case StatusLocalAhead, StatusNew:
			shouldPush = true
		case StatusConflict:
			if force {
				shouldPush = true
			} else {
				status.Error = fmt.Errorf("conflict detected, use --force to override")
				results = append(results, status)
				continue
			}
		case StatusSynced, StatusRemoteAhead:
			results = append(results, status)
			continue
		case StatusError:
			results = append(results, status)
			continue
		}

		if shouldPush {
			content, skip, err := e.getLocalContent(*item)
			if err != nil {
				status.Error = err
				status.Status = StatusError
				results = append(results, status)
				continue
			}
			if skip {
				results = append(results, status)
				continue
			}

			status.LocalHash = calculateHash(content)
			updates[item.GistFile] = content
			status.Status = StatusSynced
			results = append(results, status)
		}
	}

	if !dryRun && len(updates) > 0 {
		meta := info.meta
		if meta.Version < e.state.Version {
			meta.Version = e.state.Version
		}
		meta.Version = maxInt(meta.Version, info.remoteVersion) + 1
		meta, _ = ensureSyncMetaRepo(meta)

		metaContent, err := marshalJSON(meta)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal sync meta: %w", err)
		}
		updates[syncMetaFile] = string(metaContent)

		if _, err := e.client.Update(e.cfg.GistID, updates); err != nil {
			return nil, fmt.Errorf("failed to update gist: %w", err)
		}

		// Update state
		now := time.Now()
		for _, status := range results {
			if status.Status == StatusSynced && status.LocalHash != "" {
				e.state.Items[status.Name] = config.ItemState{
					LocalHash:  status.LocalHash,
					RemoteHash: status.LocalHash, // After push, remote = local
					LastSync:   &now,
				}
			}
		}
		e.state.LastSync = &now
		e.state.Version = meta.Version
		if err := e.state.Save(); err != nil {
			return nil, fmt.Errorf("failed to save state: %w", err)
		}
	} else if !dryRun && len(updates) == 0 && info.metaNeedsUpdate {
		meta := info.meta
		if meta.Version < e.state.Version {
			meta.Version = e.state.Version
		}
		meta, _ = ensureSyncMetaRepo(meta)
		metaContent, err := marshalJSON(meta)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal sync meta: %w", err)
		}
		if _, err := e.client.Update(e.cfg.GistID, map[string]string{syncMetaFile: string(metaContent)}); err != nil {
			return nil, fmt.Errorf("failed to update gist meta: %w", err)
		}
	}

	return results, nil
}

// Pull downloads content from the gist to local
func (e *Engine) Pull(dryRun bool, force bool) ([]ItemStatus, error) {
	remoteGist, err := e.client.Get(e.cfg.GistID)
	if err != nil {
		return nil, fmt.Errorf("failed to get gist: %w", err)
	}
	statuses, info, err := e.getStatusWithRemote(remoteGist)
	if err != nil {
		return nil, err
	}

	var results []ItemStatus
	appliedAny := false

	for _, status := range statuses {
		item := e.findItem(status.Name)
		if item == nil {
			continue
		}

		// Check if we should pull
		shouldPull := false
		switch status.Status {
		case StatusRemoteAhead:
			shouldPull = true
		case StatusConflict:
			if force {
				shouldPull = true
			} else {
				status.Error = fmt.Errorf("conflict detected, use --force to override")
				results = append(results, status)
				continue
			}
		case StatusSynced, StatusLocalAhead:
			results = append(results, status)
			continue
		case StatusError:
			results = append(results, status)
			continue
		}

		if shouldPull {
			remoteFile, exists := remoteGist.Files[item.GistFile]
			if !exists {
				results = append(results, status)
				continue
			}

			if !dryRun {
				if remoteFile.Content == "" && item.Type != "directory" {
					results = append(results, status)
					continue
				}

				preparedContent := remoteFile.Content
				if item.Type != "directory" {
					updated, err := e.prepareWriteContent(*item, remoteFile.Content)
					if err != nil {
						status.Error = err
						status.Status = StatusError
						results = append(results, status)
						continue
					}
					preparedContent = updated
				}

				// 读取本地当前内容用于 diff 显示
				localPath, _ := config.ExpandPath(item.LocalPath)
				var localContent string
				if item.Type != "directory" {
					if data, err := os.ReadFile(localPath); err == nil {
						localContent = string(data)
					}
				}

				// 显示 diff 并等待确认（仅对文件类型）
				if item.Type != "directory" && localContent != "" {
					diff.ShowDiff(item.LocalPath, localContent, preparedContent)
					result := diff.ConfirmChange(item.LocalPath, e.autoYes)

					switch result {
					case diff.ConfirmNo:
						status.Status = StatusLocalAhead // 保持本地版本
						results = append(results, status)
						continue
					case diff.ConfirmQuit:
						return results, fmt.Errorf("用户取消操作")
					case diff.ConfirmAll:
						e.autoYes = true
					case diff.ConfirmPreview:
						diff.ShowPreview(item.LocalPath, preparedContent)
						// 再次确认
						result = diff.ConfirmChange(item.LocalPath, e.autoYes)
						if result == diff.ConfirmNo {
							status.Status = StatusLocalAhead
							results = append(results, status)
							continue
						} else if result == diff.ConfirmQuit {
							return results, fmt.Errorf("用户取消操作")
						} else if result == diff.ConfirmAll {
							e.autoYes = true
						}
					}
				}

				if err := e.writeLocalContent(*item, remoteFile.Content); err != nil {
					status.Error = err
					status.Status = StatusError
					results = append(results, status)
					continue
				}
				appliedAny = true

				localHash, err := e.calculateLocalHash(*item)
				if err != nil {
					status.Error = err
					status.Status = StatusError
					results = append(results, status)
					continue
				}
				status.LocalHash = localHash
			}

			status.Status = StatusSynced
			results = append(results, status)
		}
	}

	if !dryRun {
		// Update state
		now := time.Now()
		for _, status := range results {
			if status.Status == StatusSynced && status.RemoteHash != "" {
				e.state.Items[status.Name] = config.ItemState{
					LocalHash:  status.LocalHash,
					RemoteHash: status.RemoteHash,
					LastSync:   &now,
				}
			}
		}
		e.state.LastSync = &now
		if appliedAny {
			e.state.Version = info.effectiveRemoteVersion
		}
		if err := e.state.Save(); err != nil {
			return nil, fmt.Errorf("failed to save state: %w", err)
		}

		if appliedAny && (info.effectiveRemoteVersion > info.remoteVersion || info.metaNeedsUpdate) {
			meta := info.meta
			meta.Version = info.effectiveRemoteVersion
			meta, _ = ensureSyncMetaRepo(meta)
			metaContent, err := marshalJSON(meta)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal sync meta: %w", err)
			}
			if _, err := e.client.Update(e.cfg.GistID, map[string]string{syncMetaFile: string(metaContent)}); err != nil {
				return nil, fmt.Errorf("failed to update gist meta: %w", err)
			}
		}
	}

	return results, nil
}

// getLocalContent reads the local content for an item
func (e *Engine) getLocalContent(item config.SyncItem) (string, bool, error) {
	localPath, err := config.ExpandPath(item.LocalPath)
	if err != nil {
		return "", false, err
	}

	if item.Type == "directory" {
		content, err := archive.PackDirectory(localPath)
		if err != nil {
			return "", false, err
		}
		if content == "" {
			return "", true, nil
		}
		return content, false, nil
	}

	data, err := os.ReadFile(localPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", true, nil
		}
		return "", false, err
	}
	if len(data) == 0 {
		return "", true, nil
	}

	if shouldMergeProjectMCP(item, localPath) {
		merged, changed, err := mcp.MergeProjectMCPServersIntoGlobal(data)
		if err != nil {
			return "", false, err
		}
		if changed {
			data = merged
		}
	}

	// Apply filter if configured
	if item.Filter != nil {
		data, err = filter.FilterJSON(data, item.Filter)
		if err != nil {
			return "", false, err
		}
	}

	// 对 settings 文件过滤包含本地内容的 hooks（push 时）
	if item.Name == "settings" {
		filteredData, filteredTypes, err := filter.FilterLocalHooks(data)
		if err == nil && len(filteredTypes) > 0 {
			data = filteredData
			// 可以在这里记录被过滤的 hook 类型
		}
	}

	if len(data) == 0 {
		return "", true, nil
	}

	return string(data), false, nil
}

func (e *Engine) prepareWriteContent(item config.SyncItem, content string) (string, error) {
	if item.Type == "directory" {
		return content, nil
	}

	if item.Filter == nil {
		return content, nil
	}

	filtered, err := filter.FilterJSON([]byte(content), item.Filter)
	if err != nil {
		return "", err
	}
	content = string(filtered)

	localPath, err := config.ExpandPath(item.LocalPath)
	if err != nil {
		return "", err
	}
	existing, err := os.ReadFile(localPath)
	if err == nil {
		merged, err := filter.MergeJSON(existing, []byte(content), item.Filter)
		if err != nil {
			return "", err
		}
		content = string(merged)
	}

	return content, nil
}

func shouldMergeProjectMCP(item config.SyncItem, localPath string) bool {
	if item.Type != "file" {
		return false
	}
	if item.Name == "claude-json" {
		return true
	}
	return filepath.Base(localPath) == ".claude.json"
}

// writeLocalContent writes content to the local path
func (e *Engine) writeLocalContent(item config.SyncItem, content string) error {
	localPath, err := config.ExpandPath(item.LocalPath)
	if err != nil {
		return err
	}

	if item.Type == "directory" {
		return archive.UnpackDirectory(content, localPath)
	}

	// For files with filters, we need to merge
	if item.Filter != nil {
		filtered, err := filter.FilterJSON([]byte(content), item.Filter)
		if err != nil {
			return err
		}
		content = string(filtered)

		existing, err := os.ReadFile(localPath)
		if err == nil {
			merged, err := filter.MergeJSON(existing, []byte(content), item.Filter)
			if err != nil {
				return err
			}
			content = string(merged)
		}
	}

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
		return err
	}

	return os.WriteFile(localPath, []byte(content), 0644)
}

// findItem finds a sync item by name
func (e *Engine) findItem(name string) *config.SyncItem {
	for _, item := range e.cfg.SyncItems {
		if item.Name == name {
			return &item
		}
	}
	return nil
}

// calculateHash calculates SHA256 hash of content
func calculateHash(content string) string {
	hash := sha256.Sum256([]byte(content))
	return hex.EncodeToString(hash[:])
}

// HooksWarning 包含 hooks 本地内容警告信息
type HooksWarning struct {
	ItemName     string
	LocalMatches []string
	HookTypes    []string
}

// CheckRemoteHooksForLocalContent 检查远程配置中的 hooks 是否包含本地特定内容
func (e *Engine) CheckRemoteHooksForLocalContent() ([]HooksWarning, error) {
	remoteGist, err := e.client.Get(e.cfg.GistID)
	if err != nil {
		return nil, fmt.Errorf("failed to get gist: %w", err)
	}

	var warnings []HooksWarning

	for _, item := range e.cfg.GetEnabledItems() {
		// 只检查可能包含 hooks 的文件
		if item.Name != "settings" {
			continue
		}

		remoteFile, exists := remoteGist.Files[item.GistFile]
		if !exists {
			continue
		}

		analysis, err := filter.AnalyzeHooks([]byte(remoteFile.Content))
		if err != nil {
			continue
		}

		if analysis.HasLocalContent {
			warnings = append(warnings, HooksWarning{
				ItemName:     item.Name,
				LocalMatches: analysis.LocalMatches,
				HookTypes:    analysis.HookTypes,
			})
		}
	}

	return warnings, nil
}

// PullWithHooksStrategy 带有 hooks 策略的 pull
// hooksStrategy: "overwrite" - 覆盖本地 hooks, "keep" - 保留本地 hooks, "merge" - 智能合并
func (e *Engine) PullWithHooksStrategy(dryRun bool, force bool, hooksStrategy string) ([]ItemStatus, error) {
	remoteGist, err := e.client.Get(e.cfg.GistID)
	if err != nil {
		return nil, fmt.Errorf("failed to get gist: %w", err)
	}
	statuses, info, err := e.getStatusWithRemote(remoteGist)
	if err != nil {
		return nil, err
	}

	var results []ItemStatus
	appliedAny := false

	for _, status := range statuses {
		item := e.findItem(status.Name)
		if item == nil {
			continue
		}

		// Check if we should pull
		shouldPull := false
		switch status.Status {
		case StatusRemoteAhead:
			shouldPull = true
		case StatusConflict:
			if force {
				shouldPull = true
			} else {
				status.Error = fmt.Errorf("conflict detected, use --force to override")
				results = append(results, status)
				continue
			}
		case StatusSynced, StatusLocalAhead:
			results = append(results, status)
			continue
		case StatusError:
			results = append(results, status)
			continue
		}

		if shouldPull {
			remoteFile, exists := remoteGist.Files[item.GistFile]
			if !exists {
				results = append(results, status)
				continue
			}

			if !dryRun {
				content := remoteFile.Content

				// 对 settings 文件应用 hooks 策略
				if item.Name == "settings" && hooksStrategy != "overwrite" {
					localPath, _ := config.ExpandPath(item.LocalPath)
					localData, err := os.ReadFile(localPath)
					if err == nil {
						if hooksStrategy == "keep" {
							// 保留本地 hooks
							content, err = e.mergeKeepLocalHooks(localData, []byte(content))
							if err != nil {
								status.Error = err
								status.Status = StatusError
								results = append(results, status)
								continue
							}
						} else if hooksStrategy == "merge" {
							// 智能合并：只覆盖不含本地内容的 hooks
							merged, err := filter.MergeHooksSelectively(localData, []byte(content), true)
							if err != nil {
								status.Error = err
								status.Status = StatusError
								results = append(results, status)
								continue
							}
							content = string(merged)
						}
					}
				}

				if content == "" && item.Type != "directory" {
					results = append(results, status)
					continue
				}

				if err := e.writeLocalContent(*item, content); err != nil {
					status.Error = err
					status.Status = StatusError
					results = append(results, status)
					continue
				}
				appliedAny = true

				localHash, err := e.calculateLocalHash(*item)
				if err != nil {
					status.Error = err
					status.Status = StatusError
					results = append(results, status)
					continue
				}
				status.LocalHash = localHash
			}

			status.Status = StatusSynced
			results = append(results, status)
		}
	}

	if !dryRun {
		// Update state
		now := time.Now()
		for _, status := range results {
			if status.Status == StatusSynced && status.RemoteHash != "" {
				e.state.Items[status.Name] = config.ItemState{
					LocalHash:  status.LocalHash,
					RemoteHash: status.RemoteHash,
					LastSync:   &now,
				}
			}
		}
		e.state.LastSync = &now
		if appliedAny {
			e.state.Version = info.effectiveRemoteVersion
		}
		if err := e.state.Save(); err != nil {
			return nil, fmt.Errorf("failed to save state: %w", err)
		}

		if appliedAny && (info.effectiveRemoteVersion > info.remoteVersion || info.metaNeedsUpdate) {
			meta := info.meta
			meta.Version = info.effectiveRemoteVersion
			meta, _ = ensureSyncMetaRepo(meta)
			metaContent, err := marshalJSON(meta)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal sync meta: %w", err)
			}
			if _, err := e.client.Update(e.cfg.GistID, map[string]string{syncMetaFile: string(metaContent)}); err != nil {
				return nil, fmt.Errorf("failed to update gist meta: %w", err)
			}
		}
	}

	return results, nil
}

// mergeKeepLocalHooks 合并配置但保留本地 hooks
func (e *Engine) mergeKeepLocalHooks(local, remote []byte) (string, error) {
	var localObj, remoteObj map[string]interface{}

	if err := json.Unmarshal(local, &localObj); err != nil {
		return string(remote), nil
	}
	if err := json.Unmarshal(remote, &remoteObj); err != nil {
		return "", fmt.Errorf("failed to parse remote: %w", err)
	}

	// 保存本地 hooks
	localHooks := localObj["hooks"]

	// 用远程覆盖本地
	for key, value := range remoteObj {
		localObj[key] = value
	}

	// 恢复本地 hooks
	if localHooks != nil {
		localObj["hooks"] = localHooks
	}

	result, err := json.MarshalIndent(localObj, "", "  ")
	if err != nil {
		return "", err
	}

	return string(result), nil
}
