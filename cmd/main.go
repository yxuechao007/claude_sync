package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/yxuechao007/claude_sync/internal/auth"
	"github.com/yxuechao007/claude_sync/internal/config"
	"github.com/yxuechao007/claude_sync/internal/gist"
	"github.com/yxuechao007/claude_sync/internal/mcp"
	"github.com/yxuechao007/claude_sync/internal/sync"
)

const version = "0.1.0"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "init":
		cmdInit(os.Args[2:])
	case "push":
		cmdPush(os.Args[2:])
	case "pull":
		cmdPull(os.Args[2:])
	case "status":
		cmdStatus(os.Args[2:])
	case "config":
		cmdConfig(os.Args[2:])
	case "mcp-apply":
		cmdMCPApply(os.Args[2:])
	case "version":
		fmt.Printf("claude-sync version %s\n", version)
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Printf("Unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`claude-sync - Claude Code Configuration Sync Tool

Usage:
  claude-sync <command> [options]

Commands:
  init       Initialize sync with a GitHub Gist
  push       Push local configuration to Gist
  pull       Pull configuration from Gist to local
  status     Show sync status for all items
  config     Manage sync configuration
  mcp-apply  Apply global MCP config to current project
  version    Show version information
  help       Show this help message

Options (pull/mcp-apply only):
  -y, --yes  Auto-confirm all changes (skip diff confirmation)

Examples:
  claude-sync init --token ghp_xxxx
  claude-sync push
  claude-sync pull --force
  claude-sync pull -y              # Auto-confirm all changes
  claude-sync mcp-apply            # Apply MCP to current project
  claude-sync mcp-apply --overwrite
  claude-sync status

Run 'claude-sync <command> -h' for more information on a command.`)
}

func cmdInit(args []string) {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	token := fs.String("token", "", "GitHub Personal Access Token (跳过交互式认证)")
	gistID := fs.String("gist-id", "", "Use existing Gist ID instead of creating new one")
	fs.Parse(args)

	fmt.Println("╔══════════════════════════════════════════════════════════╗")
	fmt.Println("║       claude-sync - Claude Code 配置同步工具             ║")
	fmt.Println("╚══════════════════════════════════════════════════════════╝")

	// Get token - 优先级: 命令行参数 > 环境变量 > 已保存 > 交互式获取
	ghToken := *token
	if ghToken == "" {
		ghToken = os.Getenv("GITHUB_TOKEN")
	}
	if ghToken == "" {
		// 尝试加载已保存的 token
		saved, err := auth.LoadSavedToken()
		if err == nil && saved != "" {
			ghToken = saved
			fmt.Println("\n✓ 使用已保存的 GitHub Token")
		}
	}
	if ghToken == "" {
		// 交互式获取 token
		var err error
		ghToken, err = auth.GetToken()
		if err != nil {
			fmt.Printf("\nError: %v\n", err)
			os.Exit(1)
		}
	}

	client := gist.NewClient(ghToken)

	var finalGistID string
	if *gistID != "" {
		// Verify the gist exists
		fmt.Printf("\n验证 Gist: %s... ", *gistID)
		_, err := client.Get(*gistID)
		if err != nil {
			fmt.Println("❌ 失败")
			fmt.Printf("Error: Failed to access gist: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("✓")
		finalGistID = *gistID
		fmt.Printf("使用已有 Gist: %s\n", finalGistID)
	} else {
		// Try to find an existing claude-sync gist
		fmt.Print("\n查找已有 Gist... ")
		existingID, err := findClaudeSyncGist(client)
		if err != nil {
			fmt.Println("❌ 失败")
			fmt.Printf("Error: Failed to list gists: %v\n", err)
			os.Exit(1)
		}
		if existingID != "" {
			fmt.Println("✓")
			finalGistID = existingID
			fmt.Printf("使用已有 Gist: %s\n", finalGistID)
		} else {
			fmt.Println("未找到，创建新的 Gist")
			fmt.Print("创建私有 Gist... ")
			metaContent, err := json.MarshalIndent(map[string]interface{}{
				"version": 0,
				"repo":    config.RepoURL,
			}, "", "  ")
			if err != nil {
				fmt.Println("❌ 失败")
				fmt.Printf("Error: Failed to create meta content: %v\n", err)
				os.Exit(1)
			}

			newGist, err := client.Create(
				"claude_sync",
				false, // private
				map[string]string{
					"claude-sync.meta.json": string(metaContent),
				},
			)
			if err != nil {
				fmt.Println("❌ 失败")
				fmt.Printf("Error: Failed to create gist: %v\n", err)
				os.Exit(1)
			}
			fmt.Println("✓")
			finalGistID = newGist.ID
			fmt.Printf("Gist URL: %s\n", newGist.HTMLURL)
		}
	}

	// Create config
	cfg := config.DefaultConfig(finalGistID)
	if err := cfg.Save(); err != nil {
		fmt.Printf("Error: Failed to save config: %v\n", err)
		os.Exit(1)
	}

	fmt.Println()
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("✅ 初始化完成!")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()
	fmt.Println("下一步:")
	fmt.Println("  1. 运行 'claude-sync push' 上传当前配置")
	fmt.Println()
	fmt.Println("在其他设备上:")
	fmt.Println("  1. 运行 'claude-sync init --gist-id " + finalGistID + "'")
	fmt.Println("  2. 运行 'claude-sync pull' 拉取配置")
}

func findClaudeSyncGist(client *gist.Client) (string, error) {
	const perPage = 100
	const maxPages = 5

	for page := 1; page <= maxPages; page++ {
		gists, err := client.List(page, perPage)
		if err != nil {
			return "", err
		}
		if len(gists) == 0 {
			return "", nil
		}

		for _, g := range gists {
			if g.Files == nil {
				continue
			}
			if _, ok := g.Files["claude-sync.meta.json"]; !ok {
				continue
			}

			full, err := client.Get(g.ID)
			if err != nil {
				continue
			}
			metaFile, ok := full.Files["claude-sync.meta.json"]
			if !ok {
				continue
			}
			repo, ok := parseMetaRepo(metaFile.Content)
			if ok && repo == config.RepoURL {
				return g.ID, nil
			}
		}
	}

	return "", nil
}

func parseMetaRepo(content string) (string, bool) {
	if strings.TrimSpace(content) == "" {
		return "", false
	}
	var meta struct {
		Repo string `json:"repo"`
	}
	if err := json.Unmarshal([]byte(content), &meta); err != nil {
		return "", false
	}
	if meta.Repo == "" {
		return "", false
	}
	return meta.Repo, true
}

func cmdPush(args []string) {
	fs := flag.NewFlagSet("push", flag.ExitOnError)
	dryRun := fs.Bool("dry-run", false, "Preview changes without actually pushing")
	force := fs.Bool("force", false, "Force push even if there are conflicts")
	fs.Parse(args)

	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	token, err := cfg.GetGitHubToken()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	engine, err := sync.NewEngine(cfg, token)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	if *dryRun {
		fmt.Println("Dry run - no changes will be made")
		fmt.Println()
	}

	results, err := engine.Push(*dryRun, *force)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	printResults("Push", results, *dryRun)
}

func cmdPull(args []string) {
	fs := flag.NewFlagSet("pull", flag.ExitOnError)
	dryRun := fs.Bool("dry-run", false, "Preview changes without actually pulling")
	force := fs.Bool("force", false, "Force pull even if there are conflicts")
	keepHooks := fs.Bool("keep-hooks", false, "Keep local hooks, don't overwrite with remote")
	autoYes := fs.Bool("y", false, "Auto-confirm all changes")
	autoYesLong := fs.Bool("yes", false, "Auto-confirm all changes")
	applyMCP := fs.Bool("apply-mcp", false, "Apply global MCP config to current project after pull")
	applyMCPOverwrite := fs.Bool("apply-mcp-overwrite", false, "Overwrite project MCP config when applying")
	fs.Parse(args)

	// 合并 -y 和 --yes
	confirmAll := *autoYes || *autoYesLong

	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	token, err := cfg.GetGitHubToken()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	engine, err := sync.NewEngine(cfg, token)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	// 设置自动确认模式
	engine.SetAutoYes(confirmAll)

	if *dryRun {
		fmt.Println("Dry run - no changes will be made")
		fmt.Println()
	}

	// Hooks 策略: overwrite(覆盖), keep(保留本地), merge(智能合并)
	hooksStrategy := "overwrite"
	if *keepHooks {
		hooksStrategy = "keep"
	}

	// Check for conflicts if not forcing
	if !*force && !*dryRun {
		statuses, err := engine.GetStatus()
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}

		hasConflicts := false
		for _, s := range statuses {
			if s.Status == sync.StatusConflict {
				hasConflicts = true
				break
			}
		}

		if hasConflicts && cfg.ConflictStrategy == "ask" {
			fmt.Println("Conflicts detected:")
			for _, s := range statuses {
				if s.Status == sync.StatusConflict {
					fmt.Printf("  - %s\n", s.Name)
				}
			}
			fmt.Print("\nOverwrite local changes? [y/N]: ")
			reader := bufio.NewReader(os.Stdin)
			response, _ := reader.ReadString('\n')
			response = strings.TrimSpace(strings.ToLower(response))
			if response != "y" && response != "yes" {
				fmt.Println("Aborted.")
				os.Exit(0)
			}
			*force = true
		}
	}

	// 检查远程 hooks 是否包含本地特定内容
	if !*dryRun && hooksStrategy == "overwrite" {
		warnings, err := engine.CheckRemoteHooksForLocalContent()
		if err == nil && len(warnings) > 0 {
			fmt.Println("\n⚠️  远程配置的 hooks 包含设备特定内容:")
			for _, w := range warnings {
				fmt.Printf("   配置: %s\n", w.ItemName)
				fmt.Printf("   Hook 类型: %v\n", w.HookTypes)
				fmt.Println("   检测到:")
				for _, match := range w.LocalMatches {
					fmt.Printf("     - %s\n", match)
				}
			}
			fmt.Println("\n如何处理 hooks?")
			fmt.Println("  [1] 覆盖本地 hooks (使用远程配置)")
			fmt.Println("  [2] 保留本地 hooks (只同步其他设置)")
			fmt.Println("  [3] 智能合并 (只覆盖不含本地内容的 hooks)")
			fmt.Println("  [4] 取消")
			fmt.Print("\n请选择 [1/2/3/4]: ")

			reader := bufio.NewReader(os.Stdin)
			response, _ := reader.ReadString('\n')
			response = strings.TrimSpace(response)

			switch response {
			case "1":
				hooksStrategy = "overwrite"
			case "2":
				hooksStrategy = "keep"
			case "3":
				hooksStrategy = "merge"
			default:
				fmt.Println("已取消。")
				os.Exit(0)
			}
		}
	}

	results, err := engine.PullWithHooksStrategy(*dryRun, *force, hooksStrategy)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	printResults("Pull", results, *dryRun)

	// 如果指定了 --apply-mcp，同步 MCP 到当前项目
	if *applyMCP && !*dryRun {
		fmt.Println()
		if err := mcp.SyncMCPToCurrentProject(confirmAll, *applyMCPOverwrite); err != nil {
			fmt.Printf("MCP 同步失败: %v\n", err)
		}
	}
}

// cmdMCPApply 将全局 MCP 配置应用到当前项目
func cmdMCPApply(args []string) {
	fs := flag.NewFlagSet("mcp-apply", flag.ExitOnError)
	autoYes := fs.Bool("y", false, "Auto-confirm changes")
	autoYesLong := fs.Bool("yes", false, "Auto-confirm changes")
	silent := fs.Bool("q", false, "Quiet/silent mode: no output if already synced")
	silentLong := fs.Bool("silent", false, "Quiet/silent mode: no output if already synced")
	overwrite := fs.Bool("overwrite", false, "Overwrite project MCP config (default merges)")
	fs.Parse(args)

	opts := mcp.SyncOptions{
		AutoYes:  *autoYes || *autoYesLong,
		Silent:   *silent || *silentLong,
		Overwrite: *overwrite,
	}

	if err := mcp.SyncMCPToCurrentProjectWithOptions(opts); err != nil {
		if !opts.Silent {
			fmt.Printf("Error: %v\n", err)
		}
		os.Exit(1)
	}
}

func cmdStatus(args []string) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	token, err := cfg.GetGitHubToken()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	engine, err := sync.NewEngine(cfg, token)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	statuses, err := engine.GetStatus()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Gist ID: %s\n\n", cfg.GistID)
	fmt.Println(sync.FormatStatusTable(statuses))

	// Summary
	synced := 0
	localAhead := 0
	remoteAhead := 0
	conflicts := 0
	errors := 0

	for _, s := range statuses {
		switch s.Status {
		case sync.StatusSynced:
			synced++
		case sync.StatusLocalAhead:
			localAhead++
		case sync.StatusRemoteAhead:
			remoteAhead++
		case sync.StatusConflict:
			conflicts++
		case sync.StatusError:
			errors++
		}
	}

	fmt.Printf("\nSummary: %d synced, %d local ahead, %d remote ahead, %d conflicts, %d errors\n",
		synced, localAhead, remoteAhead, conflicts, errors)
}

func cmdConfig(args []string) {
	fs := flag.NewFlagSet("config", flag.ExitOnError)
	list := fs.Bool("list", false, "List current sync items")
	fs.Parse(args)

	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	if *list {
		fmt.Printf("Gist ID: %s\n", cfg.GistID)
		fmt.Printf("Token Env: %s\n", cfg.GitHubTokenEnv)
		fmt.Printf("Conflict Strategy: %s\n\n", cfg.ConflictStrategy)

		fmt.Println("Sync Items:")
		fmt.Printf("%-20s %-10s %-8s %s\n", "NAME", "TYPE", "ENABLED", "PATH")
		fmt.Println(strings.Repeat("-", 70))

		for _, item := range cfg.SyncItems {
			enabled := "yes"
			if !item.Enabled {
				enabled = "no"
			}
			itemType := item.Type
			if itemType == "" {
				itemType = "file"
			}
			fmt.Printf("%-20s %-10s %-8s %s\n", item.Name, itemType, enabled, item.LocalPath)
		}
		return
	}

	fs.Usage()
}

func printResults(operation string, results []sync.ItemStatus, dryRun bool) {
	if dryRun {
		fmt.Printf("%s preview:\n\n", operation)
	} else {
		fmt.Printf("%s results:\n\n", operation)
	}

	for _, r := range results {
		fmt.Println(sync.FormatColoredStatus(r))
	}

	// Count results
	success := 0
	skipped := 0
	failed := 0

	for _, r := range results {
		if r.Error != nil {
			failed++
		} else if r.Status == sync.StatusSynced {
			success++
		} else {
			skipped++
		}
	}

	fmt.Printf("\n%d synced, %d skipped, %d failed\n", success, skipped, failed)
}
