package docker

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/7samael7/Piper/engine/internal/pipeline/model"
)

const (
	setupDotnetAction = "actions/setup-dotnet"
	setupNodeAction   = "actions/setup-node"
)

var runtimeVersionPattern = regexp.MustCompile(`^v?(\d+)(?:\.(\d+|x|\*))?(?:\.(\d+|x|\*))?(?:[-+].*)?$`)

func resolveJobImage(defaultImage string, job model.Job) (string, error) {
	if job.Image != "" {
		return job.Image, nil
	}

	imageName := ""
	setupAction := ""
	for _, step := range job.Steps {
		var candidate string
		var err error
		switch actionName(step.Uses) {
		case setupDotnetAction:
			candidate, err = dotnetImage(step.With["dotnet-version"])
		case setupNodeAction:
			candidate, err = nodeImage(step.With["node-version"])
		default:
			continue
		}
		if err != nil {
			return "", fmt.Errorf("%s: %w", step.Uses, err)
		}
		if imageName != "" && imageName != candidate {
			return "", fmt.Errorf(
				"job %s uses multiple setup runtimes (%s and %s), which cannot be represented by one local Docker image",
				job.ID,
				setupAction,
				step.Uses,
			)
		}
		imageName = candidate
		setupAction = step.Uses
	}

	if imageName == "" {
		return defaultImage, nil
	}
	return imageName, nil
}

func dotnetImage(version string) (string, error) {
	tag, err := normalizedRuntimeVersion(version, true)
	if err != nil {
		return "", fmt.Errorf("unsupported dotnet-version %q: %w", version, err)
	}
	return "mcr.microsoft.com/dotnet/sdk:" + tag, nil
}

func nodeImage(version string) (string, error) {
	tag, err := normalizedRuntimeVersion(version, false)
	if err != nil {
		return "", fmt.Errorf("unsupported node-version %q: %w", version, err)
	}
	return "node:" + tag + "-bookworm", nil
}

func normalizedRuntimeVersion(value string, requireMinor bool) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("a concrete major version is required for local execution")
	}
	if strings.ContainsAny(value, "\r\n") {
		return "", fmt.Errorf("multiple runtime versions are not supported locally")
	}

	matches := runtimeVersionPattern.FindStringSubmatch(value)
	if matches == nil {
		return "", fmt.Errorf("expected a version such as 8.0.x or 20")
	}

	major := matches[1]
	minor := matches[2]
	patch := matches[3]
	if minor == "x" || minor == "*" {
		minor = ""
	}
	if patch == "x" || patch == "*" {
		patch = ""
	}
	if requireMinor && minor == "" {
		minor = "0"
	}

	parts := []string{major}
	if minor != "" {
		parts = append(parts, minor)
	}
	if patch != "" {
		parts = append(parts, patch)
	}
	return strings.Join(parts, "."), nil
}

func actionName(uses string) string {
	name, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(uses)), "@")
	return name
}

func isSetupAction(uses string) bool {
	switch actionName(uses) {
	case setupDotnetAction, setupNodeAction:
		return true
	default:
		return false
	}
}

func jobUsesAction(job model.Job, name string) bool {
	for _, step := range job.Steps {
		if actionName(step.Uses) == name {
			return true
		}
	}
	return false
}

func setupActionMessage(step model.Step, imageName string) string {
	switch actionName(step.Uses) {
	case setupDotnetAction:
		return fmt.Sprintf("actions/setup-dotnet is approximated locally with Docker image %s; framework roll-forward may be used because hosted runner tool caches are not reproduced.", imageName)
	case setupNodeAction:
		return fmt.Sprintf("actions/setup-node is approximated locally with Docker image %s; action caching and hosted-runner behavior are not emulated.", imageName)
	default:
		return fmt.Sprintf("Action %s is unsupported locally and was skipped.", step.Uses)
	}
}
