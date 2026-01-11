package mcp

import "testing"

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
