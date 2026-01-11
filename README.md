# claude-sync

Claude Code 配置同步工具，基于 GitHub Gist 在多设备间同步配置文件和目录。

## 功能

- GitHub OAuth Device Flow 认证（终端显示短码，网页输入授权）
- push/pull/status 常用同步命令
- **Diff 确认**：修改本地文件前显示差异并等待确认
- 目录打包同步（自动跳过隐藏文件）
- JSON 字段过滤（如排除本地环境变量）
- **Hooks 智能过滤**：Push 时自动过滤包含本地路径的 hooks
- hooks 风险检测与合并策略（覆盖/保留/智能合并）
- **MCP 项目同步**：将全局 MCP 配置同步到当前项目
- dry-run 预览与冲突提示

## 安装

### 一键安装（推荐）

```bash
curl -sSL https://raw.githubusercontent.com/yxuechao007/claude_sync/main/install.sh | sh
```

可选环境变量：
- `CLAUDE_SYNC_REPO`：GitHub 仓库（默认 `yxuechao007/claude_sync`）
- `CLAUDE_SYNC_VERSION`：版本号（默认 `latest`）
- `CLAUDE_SYNC_INSTALL_DIR`：安装目录（默认 `/usr/local/bin`）

### 从源码安装

```bash
go build -o claude-sync ./cmd
mv claude-sync /usr/local/bin/
```

## 使用

### 初始化

创建新的 Gist 或绑定已有 Gist：

```bash
claude-sync init                    # 自动复用 claude_sync Gist，找不到则创建
claude-sync init --token ghp_xxxx   # 使用 token
claude-sync init --gist-id <id>     # 绑定已有 Gist
```

认证方式：
1. **浏览器授权（推荐）**：打开浏览器输入短码授权，自动获取 token
2. **手动输入 token**：输入 GitHub Personal Access Token

Token 来源优先级：命令行参数 > `GITHUB_TOKEN` > 已保存 token > 交互式输入

Device Flow 提示：
- 授权页面不会显示短码，请将终端中显示的 `UserCode` 手动输入。
- 会自动打开带 `user_code` 的链接（支持直接预填）。
- 可用环境变量指定 OAuth App Client ID：`CLAUDE_SYNC_GITHUB_CLIENT_ID`（或 `GITHUB_OAUTH_CLIENT_ID`、`GITHUB_CLIENT_ID`）。

init 会在你的账号下查找包含 `claude-sync.meta.json` 且 `repo` 字段为 `https://github.com/yxuechao007/claude_sync` 的 Gist；找到就复用，找不到才创建新的。init 不会推送任何配置。

## 子命令使用场景
- `init`：初次使用或新设备接入现有 gist；创建/绑定 gist
- `push`：本地配置更新后上传到 gist，适合“本地为准”
- `pull`：从 gist 拉取配置到本地，适合“远端为准”
- `status`：查看本地与远端同步状态
- `config --list`：查看当前同步配置与启用项
- `mcp-apply`：将全局 MCP 同步到当前项目配置，默认合并，可 `--overwrite`
- `version`：查看工具版本

### 推送/拉取

```bash
# 推送本地配置到 Gist
claude-sync push
claude-sync push --dry-run          # 预览变更
claude-sync push --force            # 强制推送（覆盖冲突）

# 拉取 Gist 配置到本地
claude-sync pull                    # 显示 diff 并确认
claude-sync pull -y                 # 自动确认所有修改
claude-sync pull --force            # 强制拉取（覆盖冲突）
claude-sync pull --keep-hooks       # 保留本地 hooks
claude-sync pull --apply-mcp        # 同时同步 MCP 到当前项目
claude-sync pull --apply-mcp --apply-mcp-overwrite
```

### MCP 项目同步

将全局 MCP 配置同步到当前项目（解决每次新建项目都要复制 MCP 配置的问题）：

```bash
claude-sync mcp-apply               # 同步 MCP 到当前项目
claude-sync mcp-apply -y            # 自动确认
claude-sync mcp-apply --overwrite   # 覆盖项目 MCP（默认是合并）
```

默认行为会保留项目已有的 `mcpServers` 配置，仅补充全局缺失项；如需完全覆盖请使用 `--overwrite`。

### 状态与配置

```bash
claude-sync status                  # 查看同步状态
claude-sync config --list           # 查看同步项配置
claude-sync version                 # 查看版本
claude-sync help                    # 帮助信息
```

## 同步内容详解

### 存储后端

- **GitHub Gist**（私有）

### 配置目录

```
~/.claude-sync/
├── config.json   # 同步配置
├── state.json    # 同步状态（hash 记录）
└── token         # GitHub Token
```

### 同步项

| 名称 | 本地路径 | Gist 文件 | 类型 | 默认 | 字段过滤 |
|------|---------|----------|------|------|---------|
| settings | `~/.claude/settings.json` | `settings.json` | 文件 | 启用 | 排除 `env` |
| claude-json（即 `~/.claude.json`） | `~/.claude.json` | `claude.json` | 文件 | 启用 | 只同步指定字段* |
| output-styles | `~/.claude/output-styles/` | `output-styles.tar.gz` | 目录 | 启用 | 无 |
| plans | `~/.claude/plans/` | `plans.tar.gz` | 目录 | **禁用** | 无 |
| todos | `~/.claude/todos/` | `todos.tar.gz` | 目录 | **禁用** | 无 |
| skills | `~/.claude/skills/` | `skills.tar.gz` | 目录 | 启用 | 无 |
| plugins-list | `~/.claude/plugins/known_marketplaces.json` | `known_marketplaces.json` | 文件 | 启用 | 无 |

*claude-json（即 `~/.claude.json`）只同步字段：`mcp`, `mcpServers`, `model`, `autoUpdates`, `showExpandedTodos`, `thinkingMigrationComplete`

> **注意**：`plans` 和 `todos` 默认禁用，因为它们是会话相关的临时文件，文件量大且跨设备同步意义不大。如需启用，可修改 `~/.claude-sync/config.json`。

### 不同步的内容

| 目录/文件 | 原因 |
|----------|------|
| `history.jsonl` | 对话历史，敏感且设备特定 |
| `projects/` | 项目级配置 |
| `debug/` | 调试日志 |
| `cache/` | 缓存 |
| `statsig/` | 统计数据 |
| `shell-snapshots/` | Shell 快照 |
| `file-history/` | 文件历史 |
| `session-env/` | 会话环境 |

## 同步机制

### 文件同步

1. 读取本地文件
2. 应用字段过滤（IncludeFields / ExcludeFields）
3. **过滤本地 hooks**（Push 时自动过滤包含 `localhost`、`/Users/`、`/home/` 等本地路径的 hooks）
4. 上传到 Gist

### 目录同步

1. **打包**：tar.gz 压缩 → Base64 编码 → 存为 Gist 文件
2. **解包**：Base64 解码 → 解压 → 写入本地目录
3. **跳过隐藏文件**：目录内以 `.` 开头的文件/目录会被跳过（如 `.DS_Store`、`.git`）

```
~/.claude/output-styles/
├── coding-vibes.md        ← 同步 ✅
├── structural-thinking.md ← 同步 ✅
└── .DS_Store              ← 跳过 ❌
```

### 变更检测

- 使用 SHA256 计算内容 hash
- 记录 `LocalHash` 和 `RemoteHash` 到 `state.json`
- 状态判断：
  - `synced` - 本地 = 远程
  - `local_ahead` - 本地有新改动
  - `remote_ahead` - 远程有新改动
  - `conflict` - 双方都有改动

### Diff 确认

Pull 时会显示文件差异并等待确认：

```
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
文件: ~/.claude/settings.json
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
- "model": "sonnet"
+ "model": "opus"
  "alwaysThinkingEnabled": true
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
应用此修改? [y/N/a/q/p] (y=是, N=否, a=全部, q=退出, p=预览):
```

使用 `-y` 或 `--yes` 参数可跳过确认。

## Hooks 策略

### Push 时

自动过滤包含以下本地特定内容的 hooks：
- `localhost:端口`、`127.0.0.1:端口`
- `/Users/xxx/`（macOS）
- `/home/xxx/`（Linux）
- `C:\Users\xxx\`（Windows）

### Pull 时

如果检测到远端 hooks 含设备特定内容，会提示选择：
1. **覆盖本地 hooks** - 使用远程配置
2. **保留本地 hooks** - 只同步其他设置
3. **智能合并** - 只覆盖不含本地内容的 hooks
4. **取消**

也可用 `--keep-hooks` 直接保留本地 hooks。

## 配置文件字段分析

### ~/.claude/settings.json

| 字段 | 作用 | 是否同步 | 说明 |
|------|------|---------|------|
| `alwaysThinkingEnabled` | 是否总是启用思考模式 | ✅ | 用户偏好 |
| `model` | 默认模型（如 opus） | ✅ | 用户偏好 |
| `env` | 设备特定环境变量 | ❌ | 每台机器路径不同 |
| `hooks` | 钩子脚本配置 | ⚠️ | 自动过滤本地路径 |

### ~/.claude.json

| 字段 | 作用 | 是否同步 | 说明 |
|------|------|---------|------|
| `autoUpdates` | 自动更新开关 | ✅ | 用户偏好 |
| `model` | 默认模型 | ✅ | 用户偏好 |
| `mcp` / `mcpServers` | MCP 服务器配置 | ✅ | 用户偏好 |
| `tipsHistory` | 已显示的提示 | ✅ | 避免重复显示 |
| `showExpandedTodos` | Todo 展开状态 | ✅ | UI 偏好 |
| `numStartups` | 启动次数统计 | ❌ | 设备特定 |
| `installMethod` | 安装方式 | ❌ | 设备特定 |
| `customApiKeyResponses` | API Key 审批记录 | ❌ | 安全相关 |
| `cached*` | 各种缓存 | ❌ | 临时数据 |

## 自动同步 MCP 配置（推荐）

通过配置 Claude Code 的 `SessionStart` hook，可以在每次启动新会话时自动将全局 MCP 配置同步到当前项目。

### 配置方法

编辑 `~/.claude/settings.json`，添加以下配置：

```json
{
  "hooks": {
    "SessionStart": [
      {
        "matcher": "startup",
        "hooks": [
          {
            "type": "command",
            "command": "claude-sync mcp-apply -y 2>/dev/null || true"
          }
        ]
      }
    ]
  }
}
```

### 配置说明

- `SessionStart`：Claude Code 会话启动时触发
- `matcher: "startup"`：仅在新启动时触发（不包括 resume/continue）
- `claude-sync mcp-apply -y`：自动确认同步 MCP 配置
- `claude-sync mcp-apply --overwrite`：覆盖项目 MCP（不合并）
- `2>/dev/null || true`：静默执行，即使失败也不影响 Claude Code 启动

### 效果

配置后，每次在新项目目录中运行 Claude Code 时：
1. 自动检测全局 MCP 配置
2. 将 MCP 配置同步到当前项目的 `~/.claude.json` 中
3. 无需手动复制配置

### 可选的匹配器

- `startup`：从启动触发
- `resume`：从 `--resume`、`--continue` 或 `/resume` 触发
- `clear`：从 `/clear` 触发
- `compact`：从上下文压缩触发

如需在所有情况下都同步，可以省略 `matcher` 字段。

## 发布新版本

创建 tag 后会自动构建并发布到 GitHub Releases：

```bash
git tag v1.0.0
git push origin v1.0.0
```

支持的平台：
- Linux (amd64, arm64)
- macOS (amd64, arm64)
- Windows (amd64)

## 注意事项

- 本地缺失文件或空文件不会删除远端文件
- 目录同步不会包含隐藏文件/目录
- MCP 版本信息保存在 gist 的 `claude-sync.meta.json` 中
- 新初始化时 `plans` 和 `todos` 默认禁用，现有用户需手动修改配置

## License

MIT
