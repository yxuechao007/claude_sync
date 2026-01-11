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

	var statuses []ItemStatus

	for _, item := range e.cfg.GetEnabledItems() {
		status := e.getItemStatus(item, remoteGist)
		statuses = append(statuses, status)
	}

	return statuses, nil
}

// getItemStatus calculates the status for a single item
func (e *Engine) getItemStatus(item config.SyncItem, remoteGist *gist.Gist) ItemStatus {
	status := ItemStatus{
		Name:      item.Name,
		LocalPath: item.LocalPath,
		GistFile:  item.GistFile,
	}

	// Calculate local hash
	localHash, err := e.calculateLocalHash(item)
	if err != nil {
		status.Status = StatusError
		status.Error = err
		return status
	}
	status.LocalHash = localHash

	// Get remote hash
	remoteFile, exists := remoteGist.Files[item.GistFile]
	if !exists {
		if localHash == "" {
			status.Status = StatusSynced
		} else {
			status.Status = StatusLocalAhead
		}
		return status
	}

	remoteHash := calculateHash(remoteFile.Content)
	status.RemoteHash = remoteHash

	// Get last known state
	lastState, hasState := e.state.Items[item.Name]

	// Determine status
	if localHash == remoteHash {
		status.Status = StatusSynced
	} else if !hasState {
		// First sync, treat as conflict if both exist
		if localHash == "" {
			status.Status = StatusRemoteAhead
		} else {
			status.Status = StatusConflict
		}
	} else {
		if localHash == "" && lastState.LocalHash != "" {
			status.Status = StatusRemoteAhead
			return status
		}

		// Check what changed since last sync
		localChanged := localHash != lastState.LocalHash
		remoteChanged := remoteHash != lastState.RemoteHash

		if localChanged && remoteChanged {
			status.Status = StatusConflict
		} else if localChanged {
			status.Status = StatusLocalAhead
		} else if remoteChanged {
			status.Status = StatusRemoteAhead
		} else {
			status.Status = StatusSynced
		}
	}

	return status
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
	statuses, err := e.GetStatus()
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
		_, err := e.client.Update(e.cfg.GistID, updates)
		if err != nil {
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
		if err := e.state.Save(); err != nil {
			return nil, fmt.Errorf("failed to save state: %w", err)
		}
	}

	return results, nil
}

// Pull downloads content from the gist to local
func (e *Engine) Pull(dryRun bool, force bool) ([]ItemStatus, error) {
	statuses, err := e.GetStatus()
	if err != nil {
		return nil, err
	}

	remoteGist, err := e.client.Get(e.cfg.GistID)
	if err != nil {
		return nil, fmt.Errorf("failed to get gist: %w", err)
	}

	var results []ItemStatus

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
		if err := e.state.Save(); err != nil {
			return nil, fmt.Errorf("failed to save state: %w", err)
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
	statuses, err := e.GetStatus()
	if err != nil {
		return nil, err
	}

	remoteGist, err := e.client.Get(e.cfg.GistID)
	if err != nil {
		return nil, fmt.Errorf("failed to get gist: %w", err)
	}

	var results []ItemStatus

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
		if err := e.state.Save(); err != nil {
			return nil, fmt.Errorf("failed to save state: %w", err)
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
