package secrets

import "strings"

type Masker struct {
	values []string
}

func NewMasker(secretMap map[string]string) Masker {
	values := make([]string, 0, len(secretMap))
	for _, value := range secretMap {
		if len(value) >= 3 {
			values = append(values, value)
		}
	}
	return Masker{values: values}
}

func (m Masker) Mask(input string) string {
	output := input
	for _, value := range m.values {
		output = strings.ReplaceAll(output, value, "***")
	}
	return output
}

func (m Masker) MaskValue(value interface{}) interface{} {
	switch typed := value.(type) {
	case string:
		return m.Mask(typed)
	case map[string]interface{}:
		result := make(map[string]interface{}, len(typed))
		for key, item := range typed {
			result[key] = m.MaskValue(item)
		}
		return result
	case []interface{}:
		result := make([]interface{}, len(typed))
		for index, item := range typed {
			result[index] = m.MaskValue(item)
		}
		return result
	default:
		return value
	}
}
