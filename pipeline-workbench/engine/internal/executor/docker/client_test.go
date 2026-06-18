package docker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestActiveDockerContextReadsEndpoint(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DOCKER_CONTEXT", "")

	writeJSON(t, filepath.Join(home, ".docker", "config.json"), map[string]string{
		"currentContext": "orbstack",
	})
	writeJSON(t, filepath.Join(home, ".docker", "contexts", "meta", "context-id", "meta.json"), map[string]interface{}{
		"Name": "orbstack",
		"Endpoints": map[string]interface{}{
			"docker": map[string]interface{}{
				"Host": "unix:///Users/test/.orbstack/run/docker.sock",
			},
		},
	})

	name, host := activeDockerContext()
	if name != "orbstack" {
		t.Fatalf("expected context name orbstack, got %q", name)
	}
	if host != "unix:///Users/test/.orbstack/run/docker.sock" {
		t.Fatalf("unexpected context host %q", host)
	}
}

func TestDockerEndpointCandidatesPreferEnvironmentAndContext(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DOCKER_HOST", "unix:///tmp/explicit-docker.sock")
	t.Setenv("DOCKER_CONTEXT", "colima")

	writeJSON(t, filepath.Join(home, ".docker", "contexts", "meta", "context-id", "meta.json"), map[string]interface{}{
		"Name": "colima",
		"Endpoints": map[string]interface{}{
			"docker": map[string]interface{}{
				"Host": "unix:///tmp/context-docker.sock",
			},
		},
	})

	candidates := dockerEndpointCandidates()
	if len(candidates) < 2 {
		t.Fatalf("expected at least two Docker endpoint candidates, got %d", len(candidates))
	}
	if candidates[0].host != "unix:///tmp/explicit-docker.sock" || !candidates[0].fromEnv {
		t.Fatalf("expected DOCKER_HOST first, got %#v", candidates[0])
	}
	if candidates[1].host != "unix:///tmp/context-docker.sock" {
		t.Fatalf("expected active context second, got %#v", candidates[1])
	}
}

func TestDeduplicateCandidatesKeepsFirstDescription(t *testing.T) {
	candidates := deduplicateCandidates([]endpointCandidate{
		{host: "unix:///tmp/docker.sock", label: "explicit"},
		{host: "unix:///tmp/docker.sock", label: "fallback"},
	})

	if len(candidates) != 1 || candidates[0].label != "explicit" {
		t.Fatalf("unexpected candidates %#v", candidates)
	}
}

func writeJSON(t *testing.T, filename string, value interface{}) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		t.Fatalf("create test directory: %v", err)
	}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal test JSON: %v", err)
	}
	if err := os.WriteFile(filename, data, 0o644); err != nil {
		t.Fatalf("write test JSON: %v", err)
	}
}
