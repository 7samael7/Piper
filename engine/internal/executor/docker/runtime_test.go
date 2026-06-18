package docker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/7samael7/Piper/engine/internal/pipeline/model"
)

func TestResolveJobImageForSetupDotnet(t *testing.T) {
	job := model.Job{
		ID: "backend",
		Steps: []model.Step{{
			Uses: "actions/setup-dotnet@v4",
			With: map[string]string{"dotnet-version": "8.0.x"},
		}},
	}

	imageName, err := resolveJobImage("ubuntu:22.04", job, t.TempDir())
	if err != nil {
		t.Fatalf("resolve image: %v", err)
	}
	if imageName != "mcr.microsoft.com/dotnet/sdk:8.0" {
		t.Fatalf("image = %q", imageName)
	}
}

func TestResolveJobImageForSetupNode(t *testing.T) {
	job := model.Job{
		ID: "frontend",
		Steps: []model.Step{{
			Uses: "actions/setup-node@v4",
			With: map[string]string{"node-version": "20"},
		}},
	}

	imageName, err := resolveJobImage("ubuntu:22.04", job, t.TempDir())
	if err != nil {
		t.Fatalf("resolve image: %v", err)
	}
	if imageName != "node:20-bookworm" {
		t.Fatalf("image = %q", imageName)
	}
}

func TestResolveJobImageRejectsMultipleSetupRuntimes(t *testing.T) {
	job := model.Job{
		ID: "build",
		Steps: []model.Step{
			{Uses: "actions/setup-dotnet@v4", With: map[string]string{"dotnet-version": "8.0.x"}},
			{Uses: "actions/setup-node@v4", With: map[string]string{"node-version": "20"}},
		},
	}

	_, err := resolveJobImage("ubuntu:22.04", job, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "multiple setup runtimes") {
		t.Fatalf("expected multiple runtime error, got %v", err)
	}
}

func TestExplicitProviderImageTakesPrecedence(t *testing.T) {
	job := model.Job{
		Image: "registry.example/build:latest",
		Steps: []model.Step{{
			Uses: "actions/setup-node@v4",
			With: map[string]string{"node-version": "20"},
		}},
	}

	imageName, err := resolveJobImage("ubuntu:22.04", job, t.TempDir())
	if err != nil {
		t.Fatalf("resolve image: %v", err)
	}
	if imageName != job.Image {
		t.Fatalf("image = %q", imageName)
	}
}

func TestResolveJobImageForSetupGoVersion(t *testing.T) {
	job := model.Job{
		ID: "go",
		Steps: []model.Step{{
			Uses: "actions/setup-go@v5",
			With: map[string]string{"go-version": "1.24.x"},
		}},
	}

	imageName, err := resolveJobImage("ubuntu:22.04", job, t.TempDir())
	if err != nil {
		t.Fatalf("resolve image: %v", err)
	}
	if imageName != "golang:1.24-bookworm" {
		t.Fatalf("image = %q", imageName)
	}
}

func TestResolveJobImageForSetupGoVersionFile(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "engine"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "engine", "go.mod"), []byte("module example.com/test\n\ngo 1.25.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	job := model.Job{
		ID: "go",
		Steps: []model.Step{{
			Uses: "actions/setup-go@v5",
			With: map[string]string{"go-version-file": "engine/go.mod"},
		}},
	}

	imageName, err := resolveJobImage("ubuntu:22.04", job, repo)
	if err != nil {
		t.Fatalf("resolve image: %v", err)
	}
	if imageName != "golang:1.25.0-bookworm" {
		t.Fatalf("image = %q", imageName)
	}
}

func TestResolveJobImageForSetupGoPrefersExplicitVersion(t *testing.T) {
	job := model.Job{
		ID: "go",
		Steps: []model.Step{{
			Uses: "actions/setup-go@v5",
			With: map[string]string{
				"go-version":      "1.23",
				"go-version-file": "missing/go.mod",
			},
		}},
	}

	imageName, err := resolveJobImage("ubuntu:22.04", job, t.TempDir())
	if err != nil {
		t.Fatalf("resolve image: %v", err)
	}
	if imageName != "golang:1.23-bookworm" {
		t.Fatalf("image = %q", imageName)
	}
}

func TestBuildEnvEnablesDotnetRuntimeRollForward(t *testing.T) {
	job := model.Job{
		Steps: []model.Step{{
			Uses: "actions/setup-dotnet@v4",
			With: map[string]string{"dotnet-version": "9.0.x"},
		}},
	}

	env := buildEnv(model.RunRequest{Provider: model.ProviderGitHub}, job, model.Step{})
	if !containsEnv(env, "DOTNET_ROLL_FORWARD=Major") {
		t.Fatalf("expected DOTNET_ROLL_FORWARD in %v", env)
	}
}

func containsEnv(env []string, expected string) bool {
	for _, value := range env {
		if value == expected {
			return true
		}
	}
	return false
}
