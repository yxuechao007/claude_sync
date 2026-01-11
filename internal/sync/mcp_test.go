package sync

import "testing"

func TestMergeMCPValue_LocalAddsRemote(t *testing.T) {
	local := map[string]interface{}{"a": 1}
	remote := map[string]interface{}{}

	merged, localChanged, remoteChanged := mergeMCPValue(local, remote, false)
	mergedMap, ok := merged.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map, got %T", merged)
	}

	if mergedMap["a"] != 1 {
		t.Fatalf("merged value = %v, want 1", mergedMap["a"])
	}
	if localChanged {
		t.Fatalf("localChanged = true, want false")
	}
	if !remoteChanged {
		t.Fatalf("remoteChanged = false, want true")
	}
}

func TestMergeMCPValue_ConflictPreferLocal(t *testing.T) {
	local := map[string]interface{}{"a": 1}
	remote := map[string]interface{}{"a": 2}

	merged, localChanged, remoteChanged := mergeMCPValue(local, remote, true)
	mergedMap := merged.(map[string]interface{})

	if mergedMap["a"] != 1 {
		t.Fatalf("merged value = %v, want 1", mergedMap["a"])
	}
	if localChanged {
		t.Fatalf("localChanged = true, want false")
	}
	if !remoteChanged {
		t.Fatalf("remoteChanged = false, want true")
	}
}

func TestMergeMCPValue_ConflictPreferRemote(t *testing.T) {
	local := map[string]interface{}{"a": 1}
	remote := map[string]interface{}{"a": 2}

	merged, localChanged, remoteChanged := mergeMCPValue(local, remote, false)
	mergedMap := merged.(map[string]interface{})

	if mergedMap["a"] != 2 {
		t.Fatalf("merged value = %v, want 2", mergedMap["a"])
	}
	if !localChanged {
		t.Fatalf("localChanged = false, want true")
	}
	if remoteChanged {
		t.Fatalf("remoteChanged = true, want false")
	}
}

func TestMergeMCPValue_NonMapConflict(t *testing.T) {
	merged, localChanged, remoteChanged := mergeMCPValue("local", "remote", false)

	if merged.(string) != "remote" {
		t.Fatalf("merged value = %v, want %v", merged, "remote")
	}
	if !localChanged {
		t.Fatalf("localChanged = false, want true")
	}
	if remoteChanged {
		t.Fatalf("remoteChanged = true, want false")
	}
}
