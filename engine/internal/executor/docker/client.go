package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/docker/docker/client"
)

type endpointCandidate struct {
	host    string
	label   string
	fromEnv bool
}

func connectDocker(ctx context.Context) (*client.Client, error) {
	candidates := dockerEndpointCandidates()
	attempts := make([]string, 0, len(candidates))

	for _, candidate := range candidates {
		opts := []client.Opt{client.WithAPIVersionNegotiation()}
		if candidate.fromEnv {
			opts = append(opts, client.FromEnv)
		} else {
			opts = append(opts, client.WithHost(candidate.host))
		}

		cli, err := client.NewClientWithOpts(opts...)
		if err != nil {
			attempts = append(attempts, fmt.Sprintf("%s: %v", candidate.label, err))
			continue
		}

		pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		_, err = cli.Ping(pingCtx)
		cancel()
		if err == nil {
			return cli, nil
		}

		_ = cli.Close()
		attempts = append(attempts, fmt.Sprintf("%s: %v", candidate.label, err))
	}

	return nil, fmt.Errorf(
		"Docker is unavailable. Start Docker Desktop, OrbStack, Colima, or another Docker-compatible daemon, then retry. Checked %s",
		strings.Join(attempts, "; "),
	)
}

func dockerEndpointCandidates() []endpointCandidate {
	candidates := make([]endpointCandidate, 0, 8)
	if host := strings.TrimSpace(os.Getenv("DOCKER_HOST")); host != "" {
		candidates = append(candidates, endpointCandidate{
			host:    host,
			label:   "DOCKER_HOST " + host,
			fromEnv: true,
		})
	}

	if name, host := activeDockerContext(); host != "" {
		candidates = append(candidates, endpointCandidate{
			host:  host,
			label: fmt.Sprintf("Docker context %q (%s)", name, host),
		})
	}

	candidates = append(candidates, endpointCandidate{
		host:  client.DefaultDockerHost,
		label: "default Docker endpoint " + client.DefaultDockerHost,
	})

	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		candidates = appendSocketCandidates(candidates, home,
			".docker/run/docker.sock",
			".orbstack/run/docker.sock",
			".colima/default/docker.sock",
			".rd/docker.sock",
		)
	case "linux":
		if runtimeDir := strings.TrimSpace(os.Getenv("XDG_RUNTIME_DIR")); runtimeDir != "" {
			candidates = append(candidates, socketCandidate(filepath.Join(runtimeDir, "docker.sock")))
		}
		candidates = appendSocketCandidates(candidates, home,
			".docker/run/docker.sock",
			".colima/default/docker.sock",
		)
	}

	return deduplicateCandidates(candidates)
}

func activeDockerContext() (string, string) {
	contextName := strings.TrimSpace(os.Getenv("DOCKER_CONTEXT"))
	dockerConfigDir, err := dockerConfigDirectory()
	if err != nil {
		return "", ""
	}

	if contextName == "" {
		data, err := os.ReadFile(filepath.Join(dockerConfigDir, "config.json"))
		if err != nil {
			return "", ""
		}
		var config struct {
			CurrentContext string `json:"currentContext"`
		}
		if json.Unmarshal(data, &config) != nil {
			return "", ""
		}
		contextName = strings.TrimSpace(config.CurrentContext)
	}
	if contextName == "" || contextName == "default" {
		return "", ""
	}

	metaPaths, _ := filepath.Glob(filepath.Join(dockerConfigDir, "contexts", "meta", "*", "meta.json"))
	for _, metaPath := range metaPaths {
		data, err := os.ReadFile(metaPath)
		if err != nil {
			continue
		}
		var metadata struct {
			Name      string `json:"Name"`
			Endpoints struct {
				Docker struct {
					Host string `json:"Host"`
				} `json:"docker"`
			} `json:"Endpoints"`
		}
		if json.Unmarshal(data, &metadata) == nil && metadata.Name == contextName {
			return contextName, strings.TrimSpace(metadata.Endpoints.Docker.Host)
		}
	}
	return contextName, ""
}

func dockerConfigDirectory() (string, error) {
	if configDir := strings.TrimSpace(os.Getenv("DOCKER_CONFIG")); configDir != "" {
		return configDir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".docker"), nil
}

func appendSocketCandidates(candidates []endpointCandidate, home string, paths ...string) []endpointCandidate {
	if home == "" {
		return candidates
	}
	for _, socketPath := range paths {
		candidates = append(candidates, socketCandidate(filepath.Join(home, socketPath)))
	}
	return candidates
}

func socketCandidate(socketPath string) endpointCandidate {
	host := "unix://" + socketPath
	return endpointCandidate{host: host, label: "local socket " + host}
}

func deduplicateCandidates(candidates []endpointCandidate) []endpointCandidate {
	seen := make(map[string]struct{}, len(candidates))
	result := make([]endpointCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.host == "" {
			continue
		}
		if _, ok := seen[candidate.host]; ok {
			continue
		}
		seen[candidate.host] = struct{}{}
		result = append(result, candidate)
	}
	return result
}
