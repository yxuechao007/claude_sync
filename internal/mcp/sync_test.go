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

func TestMergeMCPOnPullWithStrategyRemotePreservesLocalFields(t *testing.T) {
	local := []byte(`{
  "model": "local",
  "other": "keep",
  "mcpServers": {
    "local": {"url": "https://local"},
    "keep": {"url": "https://keep"}
  },
  "projects": {
    "/work/a": {
      "mcpServers": {
        "project": {"url": "https://project"}
      }
    }
  }
}`)
	remote := []byte(`{
  "model": "remote",
  "mcp": {"enabled": true},
  "mcpServers": {
    "local": {"url": "https://remote"}
  }
}`)

	merged, _, err := MergeMCPOnPullWithStrategy(local, remote, "remote", true)
	if err != nil {
		t.Fatalf("MergeMCPOnPullWithStrategy: %v", err)
	}

	var prefs map[string]interface{}
	if err := json.Unmarshal(merged, &prefs); err != nil {
		t.Fatalf("unmarshal merged: %v", err)
	}

	if prefs["model"] != "remote" {
		t.Fatalf("model = %v, want remote", prefs["model"])
	}
	if prefs["other"] != "keep" {
		t.Fatalf("other = %v, want keep", prefs["other"])
	}
	if prefs["mcp"] == nil {
		t.Fatalf("mcp missing after merge")
	}

	servers, _ := prefs["mcpServers"].(map[string]interface{})
	if servers == nil || servers["local"] == nil {
		t.Fatalf("mcpServers.local missing after merge: %v", servers)
	}
	if _, exists := servers["keep"]; exists {
		t.Fatalf("mcpServers.keep should be removed on remote strategy: %v", servers)
	}

	projects, _ := prefs["projects"].(map[string]interface{})
	if projects == nil || projects["/work/a"] == nil {
		t.Fatalf("projects should be preserved: %v", projects)
	}
}
