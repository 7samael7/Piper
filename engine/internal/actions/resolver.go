package actions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/7samael7/Piper/engine/internal/pipeline/model"
	"github.com/7samael7/Piper/engine/internal/providers/yamlutil"
	"github.com/7samael7/Piper/engine/internal/workspace"
	"gopkg.in/yaml.v3"
)

type Step struct {
	Name  string
	Run   string
	Shell string
	Uses  string
	With  map[string]string
	Env   map[string]string
}

type Action struct {
	Reference     string
	Name          string
	Using         string
	Main          string
	Pre           string
	Post          string
	Image         string
	Entrypoint    string
	Args          []string
	Steps         []Step
	HostPath      string
	ContainerPath string
	ResolvedSHA   string
	MutableRef    bool
	Remote        bool
}

type Resolver struct {
	root string
}

func OpenDefault() (*Resolver, error) {
	root := os.Getenv("PIPER_HOME")
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		root = filepath.Join(home, ".piper")
	}
	root = filepath.Join(root, "actions")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	return &Resolver{root: root}, nil
}

func (r *Resolver) CacheRoot() string {
	if r == nil {
		return ""
	}
	return r.root
}

func (r *Resolver) Resolve(ctx context.Context, uses, repoPath string, allowRemote bool) (*Action, error) {
	reference := strings.TrimSpace(uses)
	if strings.HasPrefix(reference, "./") || strings.HasPrefix(reference, "../") {
		hostPath, err := workspace.Resolve(repoPath, reference)
		if err != nil {
			return nil, err
		}
		action, err := load(reference, hostPath)
		if action != nil {
			relative, _ := filepath.Rel(repoPath, hostPath)
			action.ContainerPath = "/workspace/" + filepath.ToSlash(relative)
		}
		return action, err
	}
	repository, ref, ok := strings.Cut(reference, "@")
	if !ok || strings.Count(repository, "/") < 1 {
		return nil, fmt.Errorf("invalid action reference %q", reference)
	}
	if !allowRemote {
		return nil, fmt.Errorf("third-party action %s requires explicit consent", reference)
	}
	parts := strings.Split(repository, "/")
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid remote action %q", reference)
	}
	repoSlug := parts[0] + "/" + parts[1]
	subpath := strings.Join(parts[2:], "/")
	cacheKey := hash(repoSlug + "@" + ref)
	cachePath := filepath.Join(r.root, cacheKey)
	if _, err := os.Stat(filepath.Join(cachePath, ".git")); os.IsNotExist(err) {
		_ = os.RemoveAll(cachePath)
		url := "https://github.com/" + repoSlug + ".git"
		command := exec.CommandContext(ctx, "git", "clone", "--filter=blob:none", "--no-checkout", url, cachePath)
		if output, cloneErr := command.CombinedOutput(); cloneErr != nil {
			return nil, fmt.Errorf("clone action %s: %s: %w", reference, strings.TrimSpace(string(output)), cloneErr)
		}
	}
	if output, err := exec.CommandContext(ctx, "git", "-C", cachePath, "fetch", "--depth=1", "origin", ref).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("fetch action %s: %s: %w", reference, strings.TrimSpace(string(output)), err)
	}
	if output, err := exec.CommandContext(ctx, "git", "-C", cachePath, "checkout", "--detach", "FETCH_HEAD").CombinedOutput(); err != nil {
		return nil, fmt.Errorf("checkout action %s: %s: %w", reference, strings.TrimSpace(string(output)), err)
	}
	shaOutput, err := exec.CommandContext(ctx, "git", "-C", cachePath, "rev-parse", "HEAD").Output()
	if err != nil {
		return nil, fmt.Errorf("resolve action SHA: %w", err)
	}
	actionPath := filepath.Join(cachePath, filepath.FromSlash(subpath))
	action, err := load(reference, actionPath)
	if action != nil {
		action.Remote = true
		action.ResolvedSHA = strings.TrimSpace(string(shaOutput))
		action.MutableRef = !looksLikeSHA(ref)
		containerPath := "/piper-actions/" + cacheKey
		if subpath != "" {
			containerPath += "/" + filepath.ToSlash(subpath)
		}
		action.ContainerPath = containerPath
	}
	return action, err
}

func load(reference, path string) (*Action, error) {
	filename := filepath.Join(path, "action.yml")
	content, err := os.ReadFile(filename)
	if os.IsNotExist(err) {
		filename = filepath.Join(path, "action.yaml")
		content, err = os.ReadFile(filename)
	}
	if err != nil {
		return nil, fmt.Errorf("read action metadata for %s: %w", reference, err)
	}
	root, err := yamlutil.RootMapping(content)
	if err != nil {
		return nil, err
	}
	runs := yamlutil.MappingValue(root, "runs")
	action := &Action{
		Reference:  reference,
		Name:       yamlutil.ScalarString(yamlutil.MappingValue(root, "name")),
		Using:      strings.ToLower(yamlutil.ScalarString(yamlutil.MappingValue(runs, "using"))),
		Main:       yamlutil.ScalarString(yamlutil.MappingValue(runs, "main")),
		Pre:        yamlutil.ScalarString(yamlutil.MappingValue(runs, "pre")),
		Post:       yamlutil.ScalarString(yamlutil.MappingValue(runs, "post")),
		Image:      yamlutil.ScalarString(yamlutil.MappingValue(runs, "image")),
		Entrypoint: yamlutil.ScalarString(yamlutil.MappingValue(runs, "entrypoint")),
		Args:       yamlutil.StringSlice(yamlutil.MappingValue(runs, "args")),
		HostPath:   path,
	}
	if steps := yamlutil.MappingValue(runs, "steps"); steps != nil && steps.Kind == yaml.SequenceNode {
		for _, item := range steps.Content {
			if item.Kind != yaml.MappingNode {
				continue
			}
			action.Steps = append(action.Steps, Step{
				Name:  yamlutil.ScalarString(yamlutil.MappingValue(item, "name")),
				Run:   yamlutil.ScriptString(yamlutil.MappingValue(item, "run")),
				Shell: yamlutil.ScalarString(yamlutil.MappingValue(item, "shell")),
				Uses:  yamlutil.ScalarString(yamlutil.MappingValue(item, "uses")),
				With:  yamlutil.StringMap(yamlutil.MappingValue(item, "with")),
				Env:   yamlutil.StringMap(yamlutil.MappingValue(item, "env")),
			})
		}
	}
	return action, nil
}

func hash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:16]
}

func looksLikeSHA(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, char := range value {
		if !strings.ContainsRune("0123456789abcdefABCDEF", char) {
			return false
		}
	}
	return true
}

func Condition(provider model.ProviderID, value string) *model.ConditionSpec {
	if value == "" {
		return nil
	}
	return &model.ConditionSpec{Provider: provider, Original: value, Kind: "action"}
}
