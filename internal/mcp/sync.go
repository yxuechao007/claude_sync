package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/yxuechao007/claude_sync/internal/diff"
)

// MCPConflict 表示一个 MCP 配置冲突
type MCPConflict struct {
	Key         string      // MCP server key
	LocalValue  interface{} // 本地配置
	RemoteValue interface{} // 远端配置
}

// ConflictResolution 冲突解决策略
type ConflictResolution int

const (
	ResolutionAsk       ConflictResolution = iota // 逐个询问
	ResolutionKeepLocal                           // 全部保留本地
	ResolutionUseRemote                           // 全部使用远端
)

// ClaudePreferences 表示 ~/.claude.json 的结构
type ClaudePreferences struct {
	MCPServers map[string]interface{} `json:"mcpServers,omitempty"`
	Projects   map[string]Project     `json:"projects,omitempty"`
	// 其他字段保持原样
	raw map[string]interface{}
}

// Project 表示项目配置
type Project struct {
	MCPServers map[string]interface{} `json:"mcpServers,omitempty"`
	// 其他字段保持原样
	raw map[string]interface{}
}

// SyncOptions MCP 同步选项
type SyncOptions struct {
	AutoYes   bool // 自动确认
	Silent    bool // 静默模式：如果已同步则不输出任何内容
	Overwrite bool // 覆盖项目 MCP 配置
}

// SyncMCPToCurrentProject 将全局 MCP 配置同步到当前项目
func SyncMCPToCurrentProject(autoYes bool, overwrite bool) error {
	return SyncMCPToCurrentProjectWithOptions(SyncOptions{AutoYes: autoYes, Silent: false, Overwrite: overwrite})
}

// SyncMCPToCurrentProjectWithOptions 带选项的 MCP 同步
func SyncMCPToCurrentProjectWithOptions(opts SyncOptions) error {
	// 获取当前工作目录
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("获取当前目录失败: %w", err)
	}

	// 读取 ~/.claude.json
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("获取用户目录失败: %w", err)
	}

	claudeJSONPath := filepath.Join(home, ".claude.json")
	data, err := os.ReadFile(claudeJSONPath)
	if err != nil {
		if os.IsNotExist(err) && opts.Silent {
			return nil // 静默模式下文件不存在时直接返回
		}
		return fmt.Errorf("读取 ~/.claude.json 失败: %w", err)
	}

	var prefs map[string]interface{}
	if err := json.Unmarshal(data, &prefs); err != nil {
		return fmt.Errorf("解析 ~/.claude.json 失败: %w", err)
	}

	// 获取全局 mcpServers
	globalMCP, ok := prefs["mcpServers"].(map[string]interface{})
	if !ok || len(globalMCP) == 0 {
		if !opts.Silent {
			fmt.Println("没有找到全局 MCP 配置")
		}
		return nil
	}

	// 获取或创建 projects
	projects, ok := prefs["projects"].(map[string]interface{})
	if !ok {
		projects = make(map[string]interface{})
		prefs["projects"] = projects
	}

	// 获取或创建当前项目配置
	projectConfig, ok := projects[cwd].(map[string]interface{})
	if !ok {
		projectConfig = make(map[string]interface{})
		projects[cwd] = projectConfig
	}

	// 获取项目当前的 mcpServers
	projectMCP, _ := projectConfig["mcpServers"].(map[string]interface{})
	if projectMCP == nil {
		projectMCP = make(map[string]interface{})
	}

	desiredMCP := globalMCP
	if !opts.Overwrite {
		desiredMCP = mergeMCPServers(projectMCP, globalMCP)
	}

	// 检查是否有变更
	oldMCPJSON, _ := json.MarshalIndent(projectMCP, "", "  ")
	newMCPJSON, _ := json.MarshalIndent(desiredMCP, "", "  ")

	if string(oldMCPJSON) == string(newMCPJSON) {
		// 静默模式：已同步则不输出
		if !opts.Silent {
			fmt.Println("项目 MCP 配置已是最新")
		}
		return nil
	}

	// 静默模式下也需要更新，但使用 autoYes
	if opts.Silent {
		opts.AutoYes = true
	}

	// 显示 diff (非静默模式)
	if !opts.Silent {
		diff.ShowDiff(fmt.Sprintf("projects[%s].mcpServers", cwd), string(oldMCPJSON), string(newMCPJSON))
	}

	// 确认
	result := diff.ConfirmChange("mcpServers", opts.AutoYes)
	for {
		switch result {
		case diff.ConfirmYes, diff.ConfirmAll:
			goto apply
		case diff.ConfirmNo:
			if !opts.Silent {
				fmt.Println("已跳过 MCP 配置更新")
			}
			return nil
		case diff.ConfirmQuit:
			return fmt.Errorf("用户取消操作")
		case diff.ConfirmPreview:
			diff.ShowPreview("mcpServers", string(newMCPJSON))
			result = diff.ConfirmChange("mcpServers", opts.AutoYes)
		default:
			if !opts.Silent {
				fmt.Println("已跳过 MCP 配置更新")
			}
			return nil
		}
	}

	// 应用更新
apply:
	projectConfig["mcpServers"] = desiredMCP
	projects[cwd] = projectConfig

	// 写回文件
	newData, err := json.MarshalIndent(prefs, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}

	if err := os.WriteFile(claudeJSONPath, newData, 0644); err != nil {
		return fmt.Errorf("写入配置失败: %w", err)
	}

	if !opts.Silent {
		fmt.Printf("已将全局 MCP 配置同步到项目: %s\n", cwd)
	}
	return nil
}

// GetGlobalMCPServers 获取全局 MCP 配置
func GetGlobalMCPServers() (map[string]interface{}, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	claudeJSONPath := filepath.Join(home, ".claude.json")
	data, err := os.ReadFile(claudeJSONPath)
	if err != nil {
		return nil, err
	}

	var prefs map[string]interface{}
	if err := json.Unmarshal(data, &prefs); err != nil {
		return nil, err
	}

	mcpServers, _ := prefs["mcpServers"].(map[string]interface{})
	return mcpServers, nil
}

// ListProjects 列出所有已配置的项目
func ListProjects() ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	claudeJSONPath := filepath.Join(home, ".claude.json")
	data, err := os.ReadFile(claudeJSONPath)
	if err != nil {
		return nil, err
	}

	var prefs map[string]interface{}
	if err := json.Unmarshal(data, &prefs); err != nil {
		return nil, err
	}

	projects, ok := prefs["projects"].(map[string]interface{})
	if !ok {
		return nil, nil
	}

	var projectPaths []string
	for path := range projects {
		projectPaths = append(projectPaths, path)
	}

	return projectPaths, nil
}

func mergeMCPServers(projectMCP, globalMCP map[string]interface{}) map[string]interface{} {
	merged := make(map[string]interface{})
	for key, value := range projectMCP {
		merged[key] = value
	}
	for key, value := range globalMCP {
		if _, exists := merged[key]; !exists {
			merged[key] = value
		}
	}
	return merged
}

// MergeMCPOnPull 在 pull 时合并本地和远程的 MCP 配置（默认智能合并）
func MergeMCPOnPull(localData, remoteData []byte) ([]byte, bool, error) {
	return MergeMCPOnPullWithStrategy(localData, remoteData, "merge", true)
}

// MergeMCPOnPullWithStrategy 带策略的 MCP 配置合并
// strategy: "remote"(使用远端), "local"(保留本地), "merge"(智能合并)
func MergeMCPOnPullWithStrategy(localData, remoteData []byte, strategy string, autoYes bool) ([]byte, bool, error) {
	// 如果策略是使用远端，直接返回远端数据
	if strategy == "remote" {
		return mergeMCPPreferRemote(localData, remoteData)
	}

	// 如果策略是保留本地，只添加远端新增项
	if strategy == "local" {
		return mergeMCPKeepLocal(localData, remoteData)
	}

	// 智能合并策略
	return mergeMCPSmart(localData, remoteData, autoYes)
}

// mergeMCPPreferRemote 使用远端配置覆盖本地，但保留远端未包含的字段
func mergeMCPPreferRemote(localData, remoteData []byte) ([]byte, bool, error) {
	var localObj, remoteObj map[string]interface{}

	if err := json.Unmarshal(localData, &localObj); err != nil {
		return remoteData, true, nil
	}
	if err := json.Unmarshal(remoteData, &remoteObj); err != nil {
		return localData, false, nil
	}

	changed := false
	for key, value := range remoteObj {
		if !reflect.DeepEqual(localObj[key], value) {
			changed = true
		}
		localObj[key] = value
	}

	result, err := json.MarshalIndent(localObj, "", "  ")
	if err != nil {
		return nil, false, err
	}

	return result, changed, nil
}

// mergeMCPKeepLocal 保留本地配置，只添加远端新增项
func mergeMCPKeepLocal(localData, remoteData []byte) ([]byte, bool, error) {
	var localObj, remoteObj map[string]interface{}

	if err := json.Unmarshal(localData, &localObj); err != nil {
		return remoteData, true, nil
	}
	if err := json.Unmarshal(remoteData, &remoteObj); err != nil {
		return localData, false, nil
	}

	changed := false

	// 合并全局 mcpServers：只添加远端新增的
	localMCP, _ := localObj["mcpServers"].(map[string]interface{})
	remoteMCP, _ := remoteObj["mcpServers"].(map[string]interface{})
	if localMCP == nil {
		localMCP = make(map[string]interface{})
	}

	for key, value := range remoteMCP {
		if _, exists := localMCP[key]; !exists {
			localMCP[key] = value
			changed = true
		}
	}
	if len(localMCP) > 0 {
		localObj["mcpServers"] = localMCP
	}

	// 合并 projects：对已有项目也合并其 mcpServers（修复：只添加缺失的 key）
	localProjects, _ := localObj["projects"].(map[string]interface{})
	remoteProjects, _ := remoteObj["projects"].(map[string]interface{})
	if localProjects == nil {
		localProjects = make(map[string]interface{})
	}

	for projectPath, remoteProject := range remoteProjects {
		remoteProjectConfig, ok := remoteProject.(map[string]interface{})
		if !ok {
			continue
		}

		localProject, exists := localProjects[projectPath]
		if !exists {
			// 本地没有这个项目，直接添加
			localProjects[projectPath] = remoteProject
			changed = true
		} else {
			// 本地已有这个项目，合并其 mcpServers（只添加缺失的 key）
			localProjectConfig, _ := localProject.(map[string]interface{})
			if localProjectConfig == nil {
				localProjectConfig = make(map[string]interface{})
			}

			localProjectMCP, _ := localProjectConfig["mcpServers"].(map[string]interface{})
			remoteProjectMCP, _ := remoteProjectConfig["mcpServers"].(map[string]interface{})
			if localProjectMCP == nil {
				localProjectMCP = make(map[string]interface{})
			}

			for key, value := range remoteProjectMCP {
				if _, keyExists := localProjectMCP[key]; !keyExists {
					localProjectMCP[key] = value
					changed = true
				}
			}
			if len(localProjectMCP) > 0 {
				localProjectConfig["mcpServers"] = localProjectMCP
			}
			localProjects[projectPath] = localProjectConfig
		}
	}
	if len(localProjects) > 0 {
		localObj["projects"] = localProjects
	}

	// 其他字段：只添加本地没有的
	for key, value := range remoteObj {
		if key == "mcpServers" || key == "projects" {
			continue
		}
		if _, exists := localObj[key]; !exists {
			localObj[key] = value
			changed = true
		}
	}

	result, err := json.MarshalIndent(localObj, "", "  ")
	if err != nil {
		return nil, false, err
	}

	return result, changed, nil
}

// mergeMCPSmart 智能合并，检测冲突并询问用户
func mergeMCPSmart(localData, remoteData []byte, autoYes bool) ([]byte, bool, error) {
	var localObj, remoteObj map[string]interface{}

	if err := json.Unmarshal(localData, &localObj); err != nil {
		return remoteData, true, nil
	}
	if err := json.Unmarshal(remoteData, &remoteObj); err != nil {
		return localData, false, nil
	}

	changed := false
	useRemoteForAll := false // 用户选择"全部使用远端"
	useLocalForAll := false  // 用户选择"全部保留本地"

	// 合并全局 mcpServers
	localMCP, _ := localObj["mcpServers"].(map[string]interface{})
	remoteMCP, _ := remoteObj["mcpServers"].(map[string]interface{})
	if localMCP == nil {
		localMCP = make(map[string]interface{})
	}
	if remoteMCP == nil {
		remoteMCP = make(map[string]interface{})
	}

	for key, remoteValue := range remoteMCP {
		localValue, exists := localMCP[key]
		if !exists {
			// 本地没有，添加远端的
			localMCP[key] = remoteValue
			changed = true
		} else if !reflect.DeepEqual(localValue, remoteValue) {
			// 冲突：同一个 key 但值不同
			if autoYes || useLocalForAll {
				// 自动模式或已选择全部保留本地，保留本地
				continue
			} else if useRemoteForAll {
				// 已选择全部使用远端
				localMCP[key] = remoteValue
				changed = true
			} else {
				// 询问用户
				choice := askConflictResolution("mcpServers", key, localValue, remoteValue)
				switch choice {
				case "remote":
					localMCP[key] = remoteValue
					changed = true
				case "local":
					// 保留本地，不变
				case "remote_all":
					localMCP[key] = remoteValue
					changed = true
					useRemoteForAll = true
				case "local_all":
					useLocalForAll = true
				}
			}
		}
	}
	if len(localMCP) > 0 {
		localObj["mcpServers"] = localMCP
	}

	// 合并 projects
	localProjects, _ := localObj["projects"].(map[string]interface{})
	remoteProjects, _ := remoteObj["projects"].(map[string]interface{})
	if localProjects == nil {
		localProjects = make(map[string]interface{})
	}

	for projectPath, remoteProject := range remoteProjects {
		remoteProjectConfig, ok := remoteProject.(map[string]interface{})
		if !ok {
			continue
		}

		localProject, exists := localProjects[projectPath]
		var localProjectConfig map[string]interface{}
		if exists {
			localProjectConfig, _ = localProject.(map[string]interface{})
		}
		if localProjectConfig == nil {
			localProjectConfig = make(map[string]interface{})
		}

		// 合并项目的 mcpServers
		localProjectMCP, _ := localProjectConfig["mcpServers"].(map[string]interface{})
		remoteProjectMCP, _ := remoteProjectConfig["mcpServers"].(map[string]interface{})
		if localProjectMCP == nil {
			localProjectMCP = make(map[string]interface{})
		}

		for key, remoteValue := range remoteProjectMCP {
			localValue, exists := localProjectMCP[key]
			if !exists {
				localProjectMCP[key] = remoteValue
				changed = true
			} else if !reflect.DeepEqual(localValue, remoteValue) {
				if autoYes || useLocalForAll {
					continue
				} else if useRemoteForAll {
					localProjectMCP[key] = remoteValue
					changed = true
				} else {
					choice := askConflictResolution(fmt.Sprintf("projects[%s].mcpServers", projectPath), key, localValue, remoteValue)
					switch choice {
					case "remote":
						localProjectMCP[key] = remoteValue
						changed = true
					case "local":
						// 保留本地
					case "remote_all":
						localProjectMCP[key] = remoteValue
						changed = true
						useRemoteForAll = true
					case "local_all":
						useLocalForAll = true
					}
				}
			}
		}

		if len(localProjectMCP) > 0 {
			localProjectConfig["mcpServers"] = localProjectMCP
		}
		localProjects[projectPath] = localProjectConfig
	}

	if len(localProjects) > 0 {
		localObj["projects"] = localProjects
	}

	// 其他字段：用远程覆盖本地
	for key, value := range remoteObj {
		if key == "mcpServers" || key == "projects" {
			continue
		}
		localObj[key] = value
	}

	result, err := json.MarshalIndent(localObj, "", "  ")
	if err != nil {
		return nil, false, err
	}

	return result, changed, nil
}

// askConflictResolution 询问用户如何解决冲突
func askConflictResolution(context, key string, localValue, remoteValue interface{}) string {
	localJSON, _ := json.MarshalIndent(localValue, "  ", "  ")
	remoteJSON, _ := json.MarshalIndent(remoteValue, "  ", "  ")

	fmt.Printf("\n⚠️  配置冲突: %s.%s\n", context, key)
	fmt.Println("┌─ 本地配置:")
	fmt.Printf("│  %s\n", strings.ReplaceAll(string(localJSON), "\n", "\n│  "))
	fmt.Println("├─ 远端配置:")
	fmt.Printf("│  %s\n", strings.ReplaceAll(string(remoteJSON), "\n", "\n│  "))
	fmt.Println("└─")
	fmt.Println("\n选择:")
	fmt.Println("  [1] 使用远端配置")
	fmt.Println("  [2] 保留本地配置")
	fmt.Println("  [3] 全部使用远端 (后续冲突不再询问)")
	fmt.Println("  [4] 全部保留本地 (后续冲突不再询问)")
	fmt.Print("请选择 [1/2/3/4]: ")

	reader := bufio.NewReader(os.Stdin)
	response, _ := reader.ReadString('\n')
	response = strings.TrimSpace(response)

	switch response {
	case "1":
		return "remote"
	case "2":
		return "local"
	case "3":
		return "remote_all"
	case "4":
		return "local_all"
	default:
		return "local" // 默认保留本地
	}
}

// MergeProjectMCPServersIntoGlobal merges per-project MCP servers into global MCP servers.
// It does not modify local files; it only returns updated JSON content.
func MergeProjectMCPServersIntoGlobal(data []byte) ([]byte, bool, error) {
	var prefs map[string]interface{}
	if err := json.Unmarshal(data, &prefs); err != nil {
		return nil, false, err
	}

	globalMCP, _ := prefs["mcpServers"].(map[string]interface{})
	if globalMCP == nil {
		globalMCP = make(map[string]interface{})
	}

	projects, _ := prefs["projects"].(map[string]interface{})
	if len(projects) == 0 {
		return data, false, nil
	}

	changed := false
	for _, project := range projects {
		projectConfig, ok := project.(map[string]interface{})
		if !ok {
			continue
		}
		projectMCP, _ := projectConfig["mcpServers"].(map[string]interface{})
		if len(projectMCP) == 0 {
			continue
		}
		for key, value := range projectMCP {
			if _, exists := globalMCP[key]; !exists {
				globalMCP[key] = value
				changed = true
			}
		}
	}

	if !changed {
		return data, false, nil
	}

	prefs["mcpServers"] = globalMCP
	merged, err := json.MarshalIndent(prefs, "", "  ")
	if err != nil {
		return nil, false, err
	}

	return merged, true, nil
}
