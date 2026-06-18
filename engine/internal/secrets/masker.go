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
