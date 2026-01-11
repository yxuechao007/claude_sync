package sync

import (
	"fmt"
	"strings"
)

// DiffResult represents the difference between local and remote
type DiffResult struct {
	Name         string
	LocalPath    string
	GistFile     string
	Status       SyncStatus
	LocalLines   []string
	RemoteLines  []string
	AddedLines   int
	RemovedLines int
}

// SimpleDiff performs a simple line-by-line diff
func SimpleDiff(local, remote string) (added, removed int, changes []string) {
	localLines := strings.Split(local, "\n")
	remoteLines := strings.Split(remote, "\n")

	localSet := make(map[string]bool)
	for _, line := range localLines {
		localSet[line] = true
	}

	remoteSet := make(map[string]bool)
	for _, line := range remoteLines {
		remoteSet[line] = true
	}

	// Lines in local but not in remote (added locally)
	for _, line := range localLines {
		if !remoteSet[line] && strings.TrimSpace(line) != "" {
			added++
			changes = append(changes, fmt.Sprintf("+ %s", line))
		}
	}

	// Lines in remote but not in local (removed locally)
	for _, line := range remoteLines {
		if !localSet[line] && strings.TrimSpace(line) != "" {
			removed++
			changes = append(changes, fmt.Sprintf("- %s", line))
		}
	}

	return added, removed, changes
}

// FormatDiff formats the diff result for display
func FormatDiff(result DiffResult) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("=== %s ===\n", result.Name))
	sb.WriteString(fmt.Sprintf("Local:  %s\n", result.LocalPath))
	sb.WriteString(fmt.Sprintf("Remote: %s\n", result.GistFile))
	sb.WriteString(fmt.Sprintf("Status: %s\n", result.Status))

	if result.AddedLines > 0 || result.RemovedLines > 0 {
		sb.WriteString(fmt.Sprintf("Changes: +%d -%d lines\n", result.AddedLines, result.RemovedLines))
	}

	return sb.String()
}

// FormatStatusTable formats multiple statuses as a table
func FormatStatusTable(statuses []ItemStatus) string {
	var sb strings.Builder

	// Header
	sb.WriteString(fmt.Sprintf("%-20s %-15s %-40s\n", "NAME", "STATUS", "PATH"))
	sb.WriteString(strings.Repeat("-", 75) + "\n")

	for _, s := range statuses {
		statusStr := string(s.Status)
		if s.Error != nil {
			statusStr = fmt.Sprintf("%s (%s)", s.Status, s.Error.Error())
		}

		localPath := s.LocalPath
		if len(localPath) > 38 {
			localPath = "..." + localPath[len(localPath)-35:]
		}

		sb.WriteString(fmt.Sprintf("%-20s %-15s %-40s\n", s.Name, statusStr, localPath))
	}

	return sb.String()
}

// GetStatusSymbol returns a symbol for the status
func GetStatusSymbol(status SyncStatus) string {
	switch status {
	case StatusSynced:
		return "✓"
	case StatusLocalAhead:
		return "↑"
	case StatusRemoteAhead:
		return "↓"
	case StatusConflict:
		return "!"
	case StatusError:
		return "✗"
	case StatusNew:
		return "+"
	default:
		return "?"
	}
}

// GetStatusColor returns ANSI color code for the status
func GetStatusColor(status SyncStatus) string {
	switch status {
	case StatusSynced:
		return "\033[32m" // Green
	case StatusLocalAhead:
		return "\033[33m" // Yellow
	case StatusRemoteAhead:
		return "\033[36m" // Cyan
	case StatusConflict:
		return "\033[31m" // Red
	case StatusError:
		return "\033[31m" // Red
	case StatusNew:
		return "\033[35m" // Magenta
	default:
		return "\033[0m" // Reset
	}
}

// ColorReset is the ANSI code to reset color
const ColorReset = "\033[0m"

// FormatColoredStatus formats a single status with colors
func FormatColoredStatus(s ItemStatus) string {
	color := GetStatusColor(s.Status)
	symbol := GetStatusSymbol(s.Status)

	statusStr := string(s.Status)
	if s.Error != nil {
		statusStr = s.Error.Error()
	}

	return fmt.Sprintf("%s%s %s%s: %s", color, symbol, s.Name, ColorReset, statusStr)
}
