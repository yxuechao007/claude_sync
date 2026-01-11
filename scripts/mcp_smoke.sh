#!/usr/bin/env sh
set -euf

(set -o pipefail) 2>/dev/null && set -o pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
tmp_dir="$(mktemp -d)"
bin_path="${tmp_dir}/claude_sync"
home_dir="${tmp_dir}/home"
project_dir="${tmp_dir}/project-a"
go_cache="${tmp_dir}/gocache"
go_modcache="${tmp_dir}/gomodcache"

cleanup() {
  rm -rf "$tmp_dir"
}
trap cleanup EXIT INT TERM

mkdir -p "$home_dir" "$project_dir"

cat > "${home_dir}/.claude.json" <<EOF
{
  "mcpServers": {
    "globalA": {
      "command": "node",
      "args": ["./server-a.js"]
    },
    "shared": {
      "command": "python",
      "args": ["-m", "server.shared"]
    }
  },
  "projects": {
    "/tmp/other-project": {
      "mcpServers": {
        "otherOnly": {
          "command": "bash",
          "args": ["-lc", "echo other"]
        }
      }
    },
    "${project_dir}": {
      "mcpServers": {
        "localOnly": {
          "command": "bash",
          "args": ["-lc", "echo local"]
        },
        "shared": {
          "command": "python",
          "args": ["-m", "server.local"]
        }
      }
    }
  }
}
EOF

echo "[1/3] build binary"
(cd "$repo_root" && GOCACHE="$go_cache" GOMODCACHE="$go_modcache" go build -o "$bin_path" ./cmd)

echo "[2/3] merge mcpServers into project (default merge)"
(cd "$project_dir" && HOME="$home_dir" "$bin_path" mcp-apply -y)

echo "[3/3] overwrite mcpServers in project"
(cd "$project_dir" && HOME="$home_dir" "$bin_path" mcp-apply -y --overwrite)

echo ""
echo "Final ~/.claude.json:"
cat "${home_dir}/.claude.json"
