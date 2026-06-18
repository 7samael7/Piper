package gitlab

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveConfigurationLocalIncludeAndExtends(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "ci"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "ci", "common.yml"), []byte(`
.base:
  image: golang:1.24
  variables:
    A: one
`), 0o644); err != nil {
		t.Fatal(err)
	}
	content := []byte(`
include:
  - local: ci/common.yml
test:
  extends: .base
  variables:
    B: two
  script: go test ./...
`)
	resolved, err := resolveConfiguration(repo, ".gitlab-ci.yml", content)
	if err != nil {
		t.Fatal(err)
	}
	text := string(resolved)
	if !strings.Contains(text, "golang:1.24") || !strings.Contains(text, "B: two") {
		t.Fatalf("unexpected resolved config:\n%s", text)
	}
}

func TestResolveConfigurationRejectsExtendsCycle(t *testing.T) {
	content := []byte(`
.one:
  extends: .two
.two:
  extends: .one
test:
  extends: .one
  script: echo test
`)
	if _, err := resolveConfiguration(t.TempDir(), ".gitlab-ci.yml", content); err == nil {
		t.Fatal("expected extends cycle error")
	}
}
