package sync

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	cfg           *config.Config
	state         *config.SyncState
	client        *gist.Client
	autoYes       bool   // 自动确认所有修改
	mergeStrategy string // 合并策略: "remote", "local", "merge"
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

// SetMergeStrategy 设置合并策略
func (e *Engine) SetMergeStrategy(strategy string) {
	e.mergeStrategy = strategy
}

// GetMergeStrategy 获取合并策略
func (e *Engine) GetMergeStrategy() string {
	if e.mergeStrategy == "" {
		return "merge" // 默认智能合并
	}
	return e.mergeStrategy
}

// CheckFirstSyncWithLocalConfig 检查是否是首次同步且本地有配置
func (e *Engine) CheckFirstSyncWithLocalConfig() (isFirstSync bool, hasLocalConfig bool) {
	// 首次同步：state.Version = 0
	isFirstSync = e.state.Version == 0

	if !isFirstSync {
		return false, false
	}

	// 检查本地是否有配置文件
	for _, item := range e.cfg.GetEnabledItems() {
		localPath, err := config.ExpandPath(item.LocalPath)
		if err != nil {
			continue
		}

		if item.Type == "directory" {
			// 检查目录是否存在且有非隐藏文件（修复：跳过仅有隐藏文件的目录）
			entries, err := os.ReadDir(localPath)
			if err == nil {
				hasNonHidden := false
				for _, entry := range entries {
					if !strings.HasPrefix(entry.Name(), ".") {
						hasNonHidden = true
						break
					}
				}
				if hasNonHidden {
					hasLocalConfig = true
					break
				}
			}
		} else {
			// 检查文件是否存在且非空
			info, err := os.Stat(localPath)
			if err == nil && info.Size() > 0 {
				hasLocalConfig = true
				break
			}
		}
	}

	return isFirstSync, hasLocalConfig
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

	// 特殊情况：本地从未同步过（state.Version = 0）且远端有版本
	isFirstSync := e.state.Version == 0 && info.remoteVersion > 0

	// 计算全局方向（仅用于双方都有改动时的冲突解决）
	switch {
	case !info.localDirty && !info.remoteDirty:
		info.direction = directionSynced
	case isFirstSync:
		info.direction = directionRemote
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

		// 基于 per-item hash 判断每个文件的状态（修复全局 version 覆盖问题）
		if snap.localHash == snap.remoteHash {
			// 内容相同，已同步
			status.Status = StatusSynced
		} else if snap.localChanged && !snap.remoteChanged {
			// 只有本地改了，本地领先
			status.Status = StatusLocalAhead
		} else if !snap.localChanged && snap.remoteChanged {
			// 只有远端改了，远端领先
			status.Status = StatusRemoteAhead
		} else if snap.localChanged && snap.remoteChanged {
			// 双方都改了，冲突（使用全局 version 决定优先级）
			switch info.direction {
			case directionLocal:
				status.Status = StatusLocalAhead
			case directionRemote:
				status.Status = StatusRemoteAhead
			default:
				status.Status = StatusConflict
			}
		} else {
			// 都没改但 hash 不同（理论上不会发生，兜底为冲突）
			status.Status = StatusConflict
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
		case StatusRemoteAhead:
			// 远端有更新，需要先 pull
			status.Error = fmt.Errorf("remote is ahead, run 'claude_sync pull' first")
			results = append(results, status)
			continue
		case StatusSynced:
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
	keptLocal := make(map[string]string)

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

				preparedContent, skipWrite, err := e.prepareWriteContent(*item, remoteFile.Content)
				if err != nil {
					status.Error = err
					status.Status = StatusError
					results = append(results, status)
					continue
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
				if item.Type != "directory" && localContent != "" && !skipWrite {
					diff.ShowDiff(item.LocalPath, localContent, preparedContent)
					result := diff.ConfirmChange(item.LocalPath, e.autoYes)

					switch result {
					case diff.ConfirmNo:
						status.Status = StatusLocalAhead // 保持本地版本
						keptLocal[status.Name] = status.RemoteHash
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
							keptLocal[status.Name] = status.RemoteHash
							results = append(results, status)
							continue
						} else if result == diff.ConfirmQuit {
							return results, fmt.Errorf("用户取消操作")
						} else if result == diff.ConfirmAll {
							e.autoYes = true
						}
					}
				}

				if !skipWrite {
					if err := e.writeLocalContent(*item, preparedContent, true); err != nil {
						status.Error = err
						status.Status = StatusError
						results = append(results, status)
						continue
					}
					appliedAny = true
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

			if e.GetMergeStrategy() == "local" && status.LocalHash != status.RemoteHash {
				status.Status = StatusLocalAhead
				keptLocal[status.Name] = status.RemoteHash
			} else {
				status.Status = StatusSynced
			}
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
		for name, remoteHash := range keptLocal {
			e.state.Items[name] = config.ItemState{
				LocalHash:  remoteHash,
				RemoteHash: remoteHash,
				LastSync:   &now,
			}
		}
		e.state.LastSync = &now

		// 始终同步本地 version 到远端 version（修复：即使内容没变也要同步 version）
		// 这样可以避免后续本地改动被误判为"远端领先"
		if info.effectiveRemoteVersion > e.state.Version {
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

func (e *Engine) prepareWriteContent(item config.SyncItem, content string) (string, bool, error) {
	strategy := e.GetMergeStrategy()
	if item.Type == "directory" {
		if strategy == "local" {
			return "", true, nil
		}
		return content, false, nil
	}

	localPath, err := config.ExpandPath(item.LocalPath)
	if err != nil {
		return "", false, err
	}

	// 对 claude-json 特殊处理：先过滤字段，再合并 MCP 配置
	if item.Name == "claude-json" || filepath.Base(localPath) == ".claude.json" {
		// 先应用 filter 过滤不需要的字段
		if item.Filter != nil {
			filtered, err := filter.FilterJSON([]byte(content), item.Filter)
			if err == nil {
				content = string(filtered)
			}
		}

		existing, err := os.ReadFile(localPath)
		if err == nil && len(existing) > 0 {
			// 使用与 writeLocalContent 一致的策略（修复：保证 diff 与实际写入一致）
			merged, _, err := mcp.MergeMCPOnPullWithStrategy(existing, []byte(content), strategy, e.autoYes)
			if err == nil {
				content = string(merged)
			}
		}
		return content, false, nil
	}

	// 对于非过滤文件，根据策略决定是否保留本地
	if item.Filter == nil {
		if strategy == "local" {
			// 保留本地：如果本地文件存在，返回本地内容
			existing, err := os.ReadFile(localPath)
			if err == nil && len(existing) > 0 {
				return string(existing), true, nil
			}
		}
		// 使用远端或智能合并：直接返回远端内容
		return content, false, nil
	}

	filtered, err := filter.FilterJSON([]byte(content), item.Filter)
	if err != nil {
		return "", false, err
	}
	content = string(filtered)

	existing, err := os.ReadFile(localPath)
	if err == nil {
		// 根据策略处理（与 writeLocalContent 一致）
		if strategy == "local" {
			merged, err := filter.MergeJSONKeepLocal(existing, []byte(content), item.Filter)
			if err != nil {
				return "", false, err
			}
			content = string(merged)
		} else {
			// remote 或 merge 策略
			merged, err := filter.MergeJSON(existing, []byte(content), item.Filter)
			if err != nil {
				return "", false, err
			}
			content = string(merged)
		}
	}

	return content, false, nil
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
func (e *Engine) writeLocalContent(item config.SyncItem, content string, prepared bool) error {
	localPath, err := config.ExpandPath(item.LocalPath)
	if err != nil {
		return err
	}

	if prepared {
		if item.Type == "directory" {
			return archive.UnpackDirectory(content, localPath)
		}
		// Ensure parent directory exists
		if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
			return err
		}
		return os.WriteFile(localPath, []byte(content), 0644)
	}

	strategy := e.GetMergeStrategy()

	if item.Type == "directory" {
		if strategy == "local" {
			return nil
		}
		return archive.UnpackDirectory(content, localPath)
	}

	// 对 claude-json 特殊处理：先过滤字段，再合并 MCP 配置
	if item.Name == "claude-json" || filepath.Base(localPath) == ".claude.json" {
		// 先应用 filter 过滤不需要的字段（修复：防止远端手动添加的字段被写回本地）
		if item.Filter != nil {
			filtered, err := filter.FilterJSON([]byte(content), item.Filter)
			if err == nil {
				content = string(filtered)
			}
		}

		existing, err := os.ReadFile(localPath)
		if err == nil && len(existing) > 0 {
			// 根据策略处理
			merged, _, err := mcp.MergeMCPOnPullWithStrategy(existing, []byte(content), strategy, e.autoYes)
			if err == nil {
				content = string(merged)
			}
		}
	} else if item.Filter != nil {
		// For other files with filters, we need to merge
		filtered, err := filter.FilterJSON([]byte(content), item.Filter)
		if err != nil {
			return err
		}
		content = string(filtered)

		existing, err := os.ReadFile(localPath)
		if err == nil {
			// 根据策略处理
			if strategy == "remote" {
				// 使用远端，但保留本地未同步的字段
				merged, err := filter.MergeJSON(existing, []byte(content), item.Filter)
				if err != nil {
					return err
				}
				content = string(merged)
			} else if strategy == "local" {
				// 保留本地，只添加远端新增的字段
				merged, err := filter.MergeJSONKeepLocal(existing, []byte(content), item.Filter)
				if err != nil {
					return err
				}
				content = string(merged)
			} else {
				// 智能合并
				merged, err := filter.MergeJSON(existing, []byte(content), item.Filter)
				if err != nil {
					return err
				}
				content = string(merged)
			}
		}
	} else {
		// 对于非过滤文件，根据策略决定是否保留本地（修复：--keep-local 对非过滤文件生效）
		if strategy == "local" {
			existing, err := os.ReadFile(localPath)
			if err == nil && len(existing) > 0 {
				// 保留本地：直接返回，不写入
				return nil
			}
		}
		// 使用远端或智能合并：继续写入
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
	keptLocal := make(map[string]string)

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

				preparedContent, skipWrite, err := e.prepareWriteContent(*item, content)
				if err != nil {
					status.Error = err
					status.Status = StatusError
					results = append(results, status)
					continue
				}

				if !skipWrite {
					if err := e.writeLocalContent(*item, preparedContent, true); err != nil {
						status.Error = err
						status.Status = StatusError
						results = append(results, status)
						continue
					}
					appliedAny = true
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

			if e.GetMergeStrategy() == "local" && status.LocalHash != status.RemoteHash {
				status.Status = StatusLocalAhead
				keptLocal[status.Name] = status.RemoteHash
			} else {
				status.Status = StatusSynced
			}
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
		for name, remoteHash := range keptLocal {
			e.state.Items[name] = config.ItemState{
				LocalHash:  remoteHash,
				RemoteHash: remoteHash,
				LastSync:   &now,
			}
		}
		e.state.LastSync = &now

		// 始终同步本地 version 到远端 version（修复：即使内容没变也要同步 version）
		if info.effectiveRemoteVersion > e.state.Version {
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
