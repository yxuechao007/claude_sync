package filter

import (
	"encoding/json"
	"regexp"
	"strings"
)

// LocalPatterns 用于检测设备特定内容的正则模式
var LocalPatterns = []*regexp.Regexp{
	regexp.MustCompile(`localhost:\d+`),           // localhost:59948
	regexp.MustCompile(`127\.0\.0\.1:\d+`),        // 127.0.0.1:8080
	regexp.MustCompile(`0\.0\.0\.0:\d+`),          // 0.0.0.0:3000
	regexp.MustCompile(`/Users/[^/]+/`),           // macOS 用户路径
	regexp.MustCompile(`/home/[^/]+/`),            // Linux 用户路径
	regexp.MustCompile(`C:\\Users\\[^\\]+\\`),     // Windows 用户路径
	regexp.MustCompile(`\$\{?HOME\}?`),            // $HOME 变量
	regexp.MustCompile(`~\/`),                     // ~ 路径
}

// HooksAnalysis hooks 分析结果
type HooksAnalysis struct {
	HasLocalContent bool     // 是否包含本地特定内容
	LocalMatches    []string // 匹配到的本地内容
	HookTypes       []string // hooks 类型列表
}

// AnalyzeHooks 分析 hooks 配置，检测是否包含设备特定内容
func AnalyzeHooks(data []byte) (*HooksAnalysis, error) {
	var obj map[string]interface{}
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, err
	}

	analysis := &HooksAnalysis{
		LocalMatches: []string{},
		HookTypes:    []string{},
	}

	hooks, ok := obj["hooks"]
	if !ok {
		return analysis, nil
	}

	hooksMap, ok := hooks.(map[string]interface{})
	if !ok {
		return analysis, nil
	}

	// 收集 hook 类型
	for hookType := range hooksMap {
		analysis.HookTypes = append(analysis.HookTypes, hookType)
	}

	// 序列化 hooks 部分来检查内容
	hooksJSON, err := json.Marshal(hooks)
	if err != nil {
		return analysis, nil
	}

	hooksStr := string(hooksJSON)

	// 检测本地特定模式
	for _, pattern := range LocalPatterns {
		matches := pattern.FindAllString(hooksStr, -1)
		for _, match := range matches {
			if !containsString(analysis.LocalMatches, match) {
				analysis.LocalMatches = append(analysis.LocalMatches, match)
				analysis.HasLocalContent = true
			}
		}
	}

	return analysis, nil
}

// ExtractHooks 从 JSON 中提取 hooks 部分
func ExtractHooks(data []byte) ([]byte, error) {
	var obj map[string]interface{}
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, err
	}

	hooks, ok := obj["hooks"]
	if !ok {
		return []byte("{}"), nil
	}

	return json.MarshalIndent(hooks, "", "  ")
}

// MergeHooksSelectively 选择性合并 hooks
// 如果 skipLocal 为 true，则跳过包含本地内容的 hooks
func MergeHooksSelectively(local, remote []byte, skipLocalContent bool) ([]byte, error) {
	var localObj, remoteObj map[string]interface{}

	if err := json.Unmarshal(local, &localObj); err != nil {
		localObj = make(map[string]interface{})
	}
	if err := json.Unmarshal(remote, &remoteObj); err != nil {
		return local, nil
	}

	localHooks, _ := localObj["hooks"].(map[string]interface{})
	remoteHooks, _ := remoteObj["hooks"].(map[string]interface{})

	if localHooks == nil {
		localHooks = make(map[string]interface{})
	}

	if remoteHooks == nil {
		localObj["hooks"] = localHooks
		return json.MarshalIndent(localObj, "", "  ")
	}

	// 合并远程 hooks 到本地
	for hookType, hookConfig := range remoteHooks {
		hookJSON, _ := json.Marshal(hookConfig)
		hookStr := string(hookJSON)

		// 检查是否包含本地特定内容
		hasLocal := false
		if skipLocalContent {
			for _, pattern := range LocalPatterns {
				if pattern.MatchString(hookStr) {
					hasLocal = true
					break
				}
			}
		}

		if !hasLocal {
			// 不包含本地内容，直接使用远程版本
			localHooks[hookType] = hookConfig
		}
		// 如果包含本地内容且 skipLocalContent=true，保留本地版本
	}

	localObj["hooks"] = localHooks

	// 合并其他字段（非 hooks）
	for key, value := range remoteObj {
		if key != "hooks" {
			localObj[key] = value
		}
	}

	return json.MarshalIndent(localObj, "", "  ")
}

// FormatLocalMatches 格式化本地匹配内容用于显示
func FormatLocalMatches(matches []string) string {
	if len(matches) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("检测到设备特定内容:\n")
	for _, match := range matches {
		sb.WriteString("  - ")
		sb.WriteString(match)
		sb.WriteString("\n")
	}
	return sb.String()
}

func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}
