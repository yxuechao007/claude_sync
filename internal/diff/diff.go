package diff

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

const (
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorCyan   = "\033[36m"
	colorReset  = "\033[0m"
	colorGray   = "\033[90m"
)

// ConfirmResult 用户确认结果
type ConfirmResult int

const (
	ConfirmYes     ConfirmResult = iota // 确认应用
	ConfirmNo                           // 跳过此文件
	ConfirmAll                          // 应用所有
	ConfirmQuit                         // 退出
	ConfirmPreview                      // 预览完整内容
)

// ShowDiff 显示两个字符串的差异
func ShowDiff(filename, oldContent, newContent string) {
	fmt.Println()
	fmt.Printf("%s━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━%s\n", colorCyan, colorReset)
	fmt.Printf("%s文件: %s%s\n", colorYellow, filename, colorReset)
	fmt.Printf("%s━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━%s\n", colorCyan, colorReset)

	oldLines := strings.Split(oldContent, "\n")
	newLines := strings.Split(newContent, "\n")

	// 简单的逐行对比
	maxLines := len(oldLines)
	if len(newLines) > maxLines {
		maxLines = len(newLines)
	}

	// 限制显示行数
	displayLimit := 30
	changes := 0
	displayed := 0

	for i := 0; i < maxLines && displayed < displayLimit; i++ {
		var oldLine, newLine string
		if i < len(oldLines) {
			oldLine = oldLines[i]
		}
		if i < len(newLines) {
			newLine = newLines[i]
		}

		if oldLine != newLine {
			changes++
			if oldLine != "" {
				fmt.Printf("%s- %s%s\n", colorRed, truncateLine(oldLine, 80), colorReset)
				displayed++
			}
			if newLine != "" {
				fmt.Printf("%s+ %s%s\n", colorGreen, truncateLine(newLine, 80), colorReset)
				displayed++
			}
		} else if changes > 0 && displayed < displayLimit {
			// 显示上下文
			fmt.Printf("%s  %s%s\n", colorGray, truncateLine(oldLine, 80), colorReset)
			displayed++
		}
	}

	if maxLines > displayLimit {
		fmt.Printf("%s... 还有更多变更 ...%s\n", colorGray, colorReset)
	}

	fmt.Printf("%s━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━%s\n", colorCyan, colorReset)
}

// ConfirmChange 询问用户是否确认修改
func ConfirmChange(filename string, autoYes bool) ConfirmResult {
	if autoYes {
		return ConfirmYes
	}

	fmt.Printf("\n应用此修改? [y/N/a/q/p] ")
	fmt.Printf("%s(y=是, N=否, a=全部, q=退出, p=预览)%s: ", colorGray, colorReset)

	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(strings.ToLower(input))

	switch input {
	case "y", "yes":
		return ConfirmYes
	case "a", "all":
		return ConfirmAll
	case "q", "quit":
		return ConfirmQuit
	case "p", "preview":
		return ConfirmPreview
	default:
		return ConfirmNo
	}
}

// ShowPreview 显示完整内容预览
func ShowPreview(filename, content string) {
	fmt.Printf("\n%s完整内容预览: %s%s\n", colorYellow, filename, colorReset)
	fmt.Println(strings.Repeat("-", 60))

	lines := strings.Split(content, "\n")
	for i, line := range lines {
		fmt.Printf("%s%4d |%s %s\n", colorGray, i+1, colorReset, line)
	}

	fmt.Println(strings.Repeat("-", 60))
}

// truncateLine 截断过长的行
func truncateLine(line string, maxLen int) string {
	if len(line) > maxLen {
		return line[:maxLen-3] + "..."
	}
	return line
}

// FormatChangesSummary 格式化变更摘要
func FormatChangesSummary(applied, skipped, failed int) string {
	var parts []string
	if applied > 0 {
		parts = append(parts, fmt.Sprintf("%s%d 已应用%s", colorGreen, applied, colorReset))
	}
	if skipped > 0 {
		parts = append(parts, fmt.Sprintf("%s%d 已跳过%s", colorYellow, skipped, colorReset))
	}
	if failed > 0 {
		parts = append(parts, fmt.Sprintf("%s%d 失败%s", colorRed, failed, colorReset))
	}
	return strings.Join(parts, ", ")
}
