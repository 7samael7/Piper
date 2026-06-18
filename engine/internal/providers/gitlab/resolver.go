package gitlab

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/7samael7/Piper/engine/internal/workspace"
	"gopkg.in/yaml.v3"
)

func resolveConfiguration(repoPath, workflowPath string, content []byte) ([]byte, error) {
	visited := map[string]bool{}
	resolved, err := resolveFile(repoPath, workflowPath, content, visited)
	if err != nil {
		return nil, err
	}
	if err := resolveExtends(resolved); err != nil {
		return nil, err
	}
	return yaml.Marshal(resolved)
}

func resolveFile(repoPath, workflowPath string, content []byte, visited map[string]bool) (map[string]interface{}, error) {
	clean := filepath.Clean(workflowPath)
	if visited[clean] {
		return nil, fmt.Errorf("GitLab include cycle detected at %s", workflowPath)
	}
	visited[clean] = true
	defer delete(visited, clean)
	var current map[string]interface{}
	if err := yaml.Unmarshal(content, &current); err != nil {
		return nil, err
	}
	combined := map[string]interface{}{}
	for _, include := range localIncludes(current["include"]) {
		includePath := filepath.Clean(filepath.Join(filepath.Dir(clean), include))
		fullPath, resolveErr := workspace.Resolve(repoPath, includePath)
		if resolveErr != nil {
			return nil, resolveErr
		}
		data, err := os.ReadFile(fullPath)
		if err != nil {
			return nil, fmt.Errorf("read GitLab include %s: %w", include, err)
		}
		resolved, err := resolveFile(repoPath, includePath, data, visited)
		if err != nil {
			return nil, err
		}
		combined = deepMerge(combined, resolved)
	}
	if !hasRemoteIncludes(current["include"]) {
		delete(current, "include")
	}
	return deepMerge(combined, current), nil
}

func hasRemoteIncludes(value interface{}) bool {
	switch typed := value.(type) {
	case nil, string:
		return false
	case []interface{}:
		for _, item := range typed {
			if hasRemoteIncludes(item) {
				return true
			}
		}
	case map[string]interface{}:
		_, local := typed["local"]
		return !local
	}
	return false
}

func localIncludes(value interface{}) []string {
	result := []string{}
	switch typed := value.(type) {
	case string:
		result = append(result, typed)
	case []interface{}:
		for _, item := range typed {
			result = append(result, localIncludes(item)...)
		}
	case map[string]interface{}:
		if local, ok := typed["local"].(string); ok {
			result = append(result, local)
		}
	}
	return result
}

func resolveExtends(root map[string]interface{}) error {
	reserved := gitlabReservedKeys
	resolved := map[string]map[string]interface{}{}
	var resolve func(string, map[string]bool) (map[string]interface{}, error)
	resolve = func(name string, stack map[string]bool) (map[string]interface{}, error) {
		if value := resolved[name]; value != nil {
			return value, nil
		}
		if stack[name] {
			return nil, fmt.Errorf("GitLab extends cycle detected at %s", name)
		}
		raw, ok := root[name].(map[string]interface{})
		if !ok {
			return nil, nil
		}
		stack[name] = true
		merged := map[string]interface{}{}
		for _, parent := range stringValues(raw["extends"]) {
			inherited, err := resolve(parent, stack)
			if err != nil {
				return nil, err
			}
			if inherited != nil {
				merged = deepMerge(merged, inherited)
			}
		}
		delete(stack, name)
		child := cloneMap(raw)
		delete(child, "extends")
		merged = deepMerge(merged, child)
		resolved[name] = merged
		return merged, nil
	}
	for name, value := range root {
		if reserved[name] {
			continue
		}
		if _, ok := value.(map[string]interface{}); ok {
			resolvedJob, err := resolve(name, map[string]bool{})
			if err != nil {
				return err
			}
			root[name] = resolvedJob
		}
	}
	return nil
}

func stringValues(value interface{}) []string {
	switch typed := value.(type) {
	case string:
		return []string{typed}
	case []interface{}:
		result := []string{}
		for _, item := range typed {
			if value, ok := item.(string); ok {
				result = append(result, value)
			}
		}
		return result
	default:
		return nil
	}
}

func deepMerge(left, right map[string]interface{}) map[string]interface{} {
	result := cloneMap(left)
	for key, value := range right {
		rightMap, rightOK := value.(map[string]interface{})
		leftMap, leftOK := result[key].(map[string]interface{})
		if rightOK && leftOK {
			result[key] = deepMerge(leftMap, rightMap)
		} else {
			result[key] = value
		}
	}
	return result
}

func cloneMap(input map[string]interface{}) map[string]interface{} {
	result := map[string]interface{}{}
	for key, value := range input {
		result[key] = value
	}
	return result
}
