package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/pipeline-workbench/engine/internal/pipeline/model"
	"github.com/pipeline-workbench/engine/internal/providers/azure"
	"github.com/pipeline-workbench/engine/internal/providers/github"
	"github.com/pipeline-workbench/engine/internal/providers/gitlab"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: pipeline-cli <repo-path> [github|gitlab|azure]")
		os.Exit(2)
	}
	providerID := model.ProviderGitHub
	if len(os.Args) >= 3 {
		providerID = model.ProviderID(os.Args[2])
	}
	provider, err := providerFor(providerID)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	workflows, err := provider.Discover(context.Background(), os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "discover workflows: %v\n", err)
		os.Exit(1)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(workflows); err != nil {
		fmt.Fprintf(os.Stderr, "encode workflows: %v\n", err)
		os.Exit(1)
	}
}

func providerFor(providerID model.ProviderID) (model.Provider, error) {
	switch providerID {
	case model.ProviderGitHub:
		return github.NewProvider(), nil
	case model.ProviderGitLab:
		return gitlab.NewProvider(), nil
	case model.ProviderAzure:
		return azure.NewProvider(), nil
	default:
		return nil, fmt.Errorf("unknown provider %q", providerID)
	}
}
