package actions

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveLocalCompositeAction(t *testing.T) {
	t.Setenv("PIPER_HOME", t.TempDir())
	repo := t.TempDir()
	actionDir := filepath.Join(repo, ".github", "actions", "hello")
	if err := os.MkdirAll(actionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(actionDir, "action.yml"), []byte(`
name: Hello
runs:
  using: composite
  steps:
    - shell: bash
      run: echo hello
`), 0o644); err != nil {
		t.Fatal(err)
	}
	resolver, err := OpenDefault()
	if err != nil {
		t.Fatal(err)
	}
	action, err := resolver.Resolve(context.Background(), "./.github/actions/hello", repo, false)
	if err != nil {
		t.Fatal(err)
	}
	if action.Using != "composite" || len(action.Steps) != 1 {
		t.Fatalf("unexpected action: %#v", action)
	}
}

func TestResolveRemoteRequiresConsent(t *testing.T) {
	t.Setenv("PIPER_HOME", t.TempDir())
	resolver, _ := OpenDefault()
	if _, err := resolver.Resolve(context.Background(), "owner/repo@v1", t.TempDir(), false); err == nil {
		t.Fatal("expected consent error")
	}
}
