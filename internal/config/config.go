package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	ConfigDir  = ".claude_sync"
	ConfigFile = "config.json"
	StateFile  = "state.json"
	RepoURL    = "https://github.com/yxuechao007/claude_sync"
)

// FilterConfig defines which fields to include/exclude for JSON files
type FilterConfig struct {
	IncludeFields []string `json:"include_fields,omitempty"`
	ExcludeFields []string `json:"exclude_fields,omitempty"`
}

// SyncItem represents a single item to sync
type SyncItem struct {
	Name      string        `json:"name"`
	LocalPath string        `json:"local_path"`
	GistFile  string        `json:"gist_file"`
	Enabled   bool          `json:"enabled"`
	Type      string        `json:"type,omitempty"` // "file" or "directory"
	Filter    *FilterConfig `json:"filter,omitempty"`
}

// Config holds the main configuration
type Config struct {
	GistID           string     `json:"gist_id"`
	GitHubTokenEnv   string     `json:"github_token_env"`
	SyncItems        []SyncItem `json:"sync_items"`
	LastSync         *time.Time `json:"last_sync,omitempty"`
	ConflictStrategy string     `json:"conflict_strategy"` // "ask", "local", "remote"
}

// SyncState tracks the state of each synced item
type SyncState struct {
	Items    map[string]ItemState `json:"items"`
	LastSync *time.Time           `json:"last_sync,omitempty"`
	Version  int                  `json:"version,omitempty"`
}

// ItemState tracks the hash and sync time for an item
type ItemState struct {
	LocalHash  string     `json:"local_hash"`
	RemoteHash string     `json:"remote_hash"`
	LastSync   *time.Time `json:"last_sync,omitempty"`
}

// GetConfigDir returns the path to the config directory
func GetConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(home, ConfigDir), nil
}

// GetConfigPath returns the path to the config file
func GetConfigPath() (string, error) {
	dir, err := GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, ConfigFile), nil
}

// GetStatePath returns the path to the state file
func GetStatePath() (string, error) {
	dir, err := GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, StateFile), nil
}

// Load loads the configuration from disk
func Load() (*Config, error) {
	path, err := GetConfigPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("config not found, run 'claude_sync init' first")
		}
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	return &cfg, nil
}

// Save saves the configuration to disk
func (c *Config) Save() error {
	dir, err := GetConfigDir()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	path, err := GetConfigPath()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}

// LoadState loads the sync state from disk
func LoadState() (*SyncState, error) {
	path, err := GetStatePath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &SyncState{Items: make(map[string]ItemState)}, nil
		}
		return nil, fmt.Errorf("failed to read state: %w", err)
	}

	var state SyncState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to parse state: %w", err)
	}

	if state.Items == nil {
		state.Items = make(map[string]ItemState)
	}

	return &state, nil
}

// Save saves the sync state to disk
func (s *SyncState) Save() error {
	dir, err := GetConfigDir()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	path, err := GetStatePath()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write state: %w", err)
	}

	return nil
}

// ExpandPath expands ~ to home directory
func ExpandPath(path string) (string, error) {
	if len(path) == 0 {
		return path, nil
	}

	if path[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if path == "~" {
			return home, nil
		}
		if strings.HasPrefix(path, "~/") {
			return filepath.Join(home, path[2:]), nil
		}
	}

	return path, nil
}

// DefaultConfig returns a config with default sync items
func DefaultConfig(gistID string) *Config {
	return &Config{
		GistID:           gistID,
		GitHubTokenEnv:   "GITHUB_TOKEN",
		ConflictStrategy: "ask",
		SyncItems: []SyncItem{
			{
				Name:      "settings",
				LocalPath: "~/.claude/settings.json",
				GistFile:  "settings.json",
				Enabled:   true,
				Type:      "file",
				Filter: &FilterConfig{
					// 同步偏好和 hooks，排除 env（设备特定环境变量）
					ExcludeFields: []string{"env"},
				},
			},
			{
				Name:      "output-styles",
				LocalPath: "~/.claude/output-styles",
				GistFile:  "output-styles.tar.gz",
				Enabled:   true,
				Type:      "directory",
			},
			{
				Name:      "plans",
				LocalPath: "~/.claude/plans",
				GistFile:  "plans.tar.gz",
				Enabled:   false, // 默认禁用，当前会话相关
				Type:      "directory",
			},
			{
				Name:      "todos",
				LocalPath: "~/.claude/todos",
				GistFile:  "todos.tar.gz",
				Enabled:   false, // 默认禁用，文件量大且设备特定
				Type:      "directory",
			},
			{
				Name:      "claude-json",
				LocalPath: "~/.claude.json",
				GistFile:  "claude.json",
				Enabled:   true,
				Type:      "file",
				Filter: &FilterConfig{
					IncludeFields: []string{
						"model",
						"autoUpdates",
						"showExpandedTodos",
						"thinkingMigrationComplete",
						"mcp",
						"mcpServers",
					},
				},
			},
			{
				Name:      "plugins-list",
				LocalPath: "~/.claude/plugins/known_marketplaces.json",
				GistFile:  "known_marketplaces.json",
				Enabled:   true,
				Type:      "file",
			},
			{
				Name:      "skills",
				LocalPath: "~/.claude/skills",
				GistFile:  "skills.tar.gz",
				Enabled:   true,
				Type:      "directory",
			},
		},
	}
}

// GetEnabledItems returns only enabled sync items
func (c *Config) GetEnabledItems() []SyncItem {
	var items []SyncItem
	for _, item := range c.SyncItems {
		if item.Enabled {
			items = append(items, item)
		}
	}
	return items
}

// GetGitHubToken retrieves the GitHub token from multiple sources
// Priority: 1. Environment variable  2. Token file  3. Not found
func (c *Config) GetGitHubToken() (string, error) {
	// 1. 尝试从环境变量获取
	token := os.Getenv(c.GitHubTokenEnv)
	if token != "" {
		return token, nil
	}

	// 2. 尝试从 token 文件获取
	home, err := os.UserHomeDir()
	if err == nil {
		tokenFile := filepath.Join(home, ConfigDir, "token")
		data, err := os.ReadFile(tokenFile)
		if err == nil {
			token = string(data)
			token = strings.TrimSpace(token)
			if token != "" {
				return token, nil
			}
		}
	}

	return "", fmt.Errorf("GitHub token not found. Run 'claude_sync init' to set up authentication")
}
