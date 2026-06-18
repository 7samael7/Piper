package caches

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCacheHitAndMiss(t *testing.T) {
	t.Setenv("PIPER_HOME", t.TempDir())
	store, _ := OpenDefault()
	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, "deps"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "deps", "item"), []byte("cached"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Save("repo", "key-v1", workspace, []string{"deps"}); err != nil {
		t.Fatal(err)
	}
	_ = os.RemoveAll(filepath.Join(workspace, "deps"))
	record, err := store.Restore("repo", "missing", []string{"key-"}, workspace, []string{"deps"})
	if err != nil || record == nil {
		t.Fatalf("restore = %#v, %v", record, err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "deps", "item")); err != nil {
		t.Fatal(err)
	}
}
