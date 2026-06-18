package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveRejectsTraversalAndEscapingSymlink(t *testing.T) {
	root := t.TempDir()
	if _, err := Resolve(root, "../secret"); err == nil {
		t.Fatal("expected traversal error")
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "outside")); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(root, "outside"); err == nil {
		t.Fatal("expected symlink escape error")
	}
}
