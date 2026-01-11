package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/yxuechao007/claude_sync/internal/diff"
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
	AutoYes bool // 自动确认
	Silent  bool // 静默模式：如果已同步则不输出任何内容
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
