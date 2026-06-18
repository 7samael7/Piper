package yamlutil

import (
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

func RootMapping(content []byte) (*yaml.Node, error) {
	var document yaml.Node
	if err := yaml.Unmarshal(content, &document); err != nil {
		return nil, err
	}
	if len(document.Content) == 0 || document.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("workflow root must be a YAML mapping")
	}
	return document.Content[0], nil
}

func MappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func HasKey(node *yaml.Node, key string) bool {
	return MappingValue(node, key) != nil
}

func MappingKeys(node *yaml.Node) []string {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	keys := make([]string, 0, len(node.Content)/2)
	for i := 0; i+1 < len(node.Content); i += 2 {
		keys = append(keys, node.Content[i].Value)
	}
	return keys
}

func ScalarString(node *yaml.Node) string {
	if node == nil {
		return ""
	}
	if node.Kind == yaml.ScalarNode {
		return node.Value
	}
	return YAMLToString(node)
}

func ScalarBool(node *yaml.Node) bool {
	if node == nil || node.Kind != yaml.ScalarNode {
		return false
	}
	value := strings.ToLower(node.Value)
	return value == "true" || value == "yes" || value == "on"
}

func ScalarOrListString(node *yaml.Node) string {
	if node == nil {
		return ""
	}
	if node.Kind == yaml.SequenceNode {
		return strings.Join(StringSlice(node), ", ")
	}
	return ScalarString(node)
}

func StringSlice(node *yaml.Node) []string {
	if node == nil {
		return []string{}
	}
	if node.Kind == yaml.SequenceNode {
		values := make([]string, 0, len(node.Content))
		for _, item := range node.Content {
			value := ScalarString(item)
			if value != "" {
				values = append(values, value)
			}
		}
		return values
	}
	value := ScalarString(node)
	if value == "" {
		return []string{}
	}
	return []string{value}
}

func ScriptString(node *yaml.Node) string {
	if node == nil {
		return ""
	}
	if node.Kind == yaml.SequenceNode {
		lines := make([]string, 0, len(node.Content))
		for _, item := range node.Content {
			value := ScalarString(item)
			if value != "" {
				lines = append(lines, value)
			}
		}
		return strings.Join(lines, "\n")
	}
	return ScalarString(node)
}

func StringMap(node *yaml.Node) map[string]string {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	values := make(map[string]string, len(node.Content)/2)
	for i := 0; i+1 < len(node.Content); i += 2 {
		values[node.Content[i].Value] = ScalarString(node.Content[i+1])
	}
	return values
}

func MergeStringMaps(values ...map[string]string) map[string]string {
	var merged map[string]string
	for _, value := range values {
		if len(value) == 0 {
			continue
		}
		if merged == nil {
			merged = map[string]string{}
		}
		for key, item := range value {
			merged[key] = item
		}
	}
	return merged
}

func ImageName(node *yaml.Node) string {
	if node == nil {
		return ""
	}
	if node.Kind == yaml.MappingNode {
		return ScalarString(MappingValue(node, "name"))
	}
	return ScalarString(node)
}

func YAMLToString(node *yaml.Node) string {
	if node == nil {
		return ""
	}
	bytes, err := yaml.Marshal(node)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(bytes))
}

func FirstLine(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "script"
	}
	if index := strings.IndexByte(value, '\n'); index >= 0 {
		value = value[:index]
	}
	if len(value) > 64 {
		return value[:61] + "..."
	}
	return value
}

func StringifyScalar(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return typed
	case int:
		return strconv.Itoa(typed)
	case bool:
		if typed {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", typed)
	}
}
