package artifacts

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPublishRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PIPER_HOME", t.TempDir())
	store, err := OpenDefault()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Publish("run", "job", "bad", root, []string{"../outside"}); err == nil {
		t.Fatal("expected traversal error")
	}
}

func TestPublishAndDownload(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PIPER_HOME", t.TempDir())
	if err := os.WriteFile(filepath.Join(root, "result.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, _ := OpenDefault()
	if _, err := store.Publish("run", "job", "result", root, []string{"result.txt"}); err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if err := store.Download("result", target); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(target, "result.txt")); err != nil {
		t.Fatal(err)
	}
}
