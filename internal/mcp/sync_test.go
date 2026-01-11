package mcp

import (
	"encoding/json"
	"testing"
)

func TestMergeMCPServersPreservesProject(t *testing.T) {
	project := map[string]interface{}{
		"a": "local",
	}
	global := map[string]interface{}{
		"a": "remote",
		"b": "global",
	}

	merged := mergeMCPServers(project, global)
	if merged["a"] != "local" {
		t.Fatalf("merged[a] = %v, want local", merged["a"])
	}
	if merged["b"] != "global" {
		t.Fatalf("merged[b] = %v, want global", merged["b"])
	}
}

func TestMergeMCPServersEmptyProject(t *testing.T) {
	global := map[string]interface{}{
		"b": "global",
	}

	merged := mergeMCPServers(nil, global)
	if merged["b"] != "global" {
		t.Fatalf("merged[b] = %v, want global", merged["b"])
	}
}

func TestMergeProjectMCPServersIntoGlobal(t *testing.T) {
	input := []byte(`{
  "mcpServers": {
    "global": {"url": "https://example.com"}
  },
  "projects": {
    "/work/a": {
      "mcpServers": {
        "local": {"url": "https://local"},
        "global": {"url": "https://override"}
      }
    }
  }
}`)

	merged, changed, err := MergeProjectMCPServersIntoGlobal(input)
	if err != nil {
		t.Fatalf("MergeProjectMCPServersIntoGlobal: %v", err)
	}
	if !changed {
		t.Fatalf("changed = false, want true")
	}

	var prefs map[string]interface{}
	if err := json.Unmarshal(merged, &prefs); err != nil {
		t.Fatalf("unmarshal merged: %v", err)
	}

	servers, _ := prefs["mcpServers"].(map[string]interface{})
	if servers["global"] == nil || servers["local"] == nil {
		t.Fatalf("merged mcpServers = %v, want global and local", servers)
	}
}

func TestMergeProjectMCPServersIntoGlobalNoChange(t *testing.T) {
	input := []byte(`{"mcpServers":{"only":"x"}}`)

	merged, changed, err := MergeProjectMCPServersIntoGlobal(input)
	if err != nil {
		t.Fatalf("MergeProjectMCPServersIntoGlobal: %v", err)
	}
	if changed {
		t.Fatalf("changed = true, want false")
	}
	if string(merged) != string(input) {
		t.Fatalf("merged = %s, want original input", string(merged))
	}
}
