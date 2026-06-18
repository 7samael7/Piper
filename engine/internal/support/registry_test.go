package support

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func TestRegistryContractAndReferences(t *testing.T) {
	registry, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	root := repositoryRoot(t)
	anchors := map[string]bool{}
	for _, entry := range registry.Features {
		if anchors[entry.Documentation] {
			t.Fatalf("duplicate documentation anchor %q", entry.Documentation)
		}
		anchors[entry.Documentation] = true
		for _, testPath := range entry.RelatedTests {
			if filepath.IsAbs(testPath) || strings.Contains(testPath, "..") || !strings.HasSuffix(testPath, "_test.go") {
				t.Fatalf("%s has invalid related test path %q", entry.ID, testPath)
			}
			if _, err := os.Stat(filepath.Join(root, testPath)); err != nil {
				t.Fatalf("%s references missing test %s: %v", entry.ID, testPath, err)
			}
		}
	}
}

func TestGeneratedProviderSupportIsCurrent(t *testing.T) {
	registry := MustDefault()
	root := repositoryRoot(t)
	current, err := os.ReadFile(filepath.Join(root, "docs", "provider-support.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(current) != string(RenderProviderSupport(registry)) {
		t.Fatal("docs/provider-support.md is stale; run `cd engine && go run ./cmd/supportdoc -write`")
	}
}

func TestStaticFeatureReferencesExistInRegistry(t *testing.T) {
	registry := MustDefault()
	root := repositoryRoot(t)
	pattern := regexp.MustCompile(`(?:support\.Ref|supportpkgRef)\("([^"]+)"`)
	err := filepath.Walk(filepath.Join(root, "engine", "internal"), func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for _, match := range pattern.FindAllSubmatch(content, -1) {
			id := string(match[1])
			if _, ok := registry.Get(id); !ok {
				t.Errorf("%s references missing registry feature %q", path, id)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSupportContractGolden(t *testing.T) {
	root := repositoryRoot(t)
	value, err := os.ReadFile(filepath.Join(root, "engine", "internal", "support", "testdata", "contract.sha256"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := ContractDigest(MustDefault()), strings.TrimSpace(string(value)); got != want {
		t.Fatalf("support contract changed: got %s want %s; review statuses and update the golden intentionally", got, want)
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate support package")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", ".."))
}
