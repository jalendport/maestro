// Package redact removes project strings from outbound JSON.
package redact

import (
	"bytes"
	"encoding/json"
	"strings"
)

func JSON(value any, terms []string) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil || len(terms) == 0 {
		return data, err
	}
	var tree any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&tree); err != nil {
		return nil, err
	}
	var apply func(any) any
	apply = func(value any) any {
		switch v := value.(type) {
		case string:
			for _, term := range terms {
				if term != "" {
					v = strings.ReplaceAll(v, term, "[redacted]")
				}
			}
			return v
		case map[string]any:
			result := make(map[string]any, len(v))
			for key, item := range v {
				result[apply(key).(string)] = apply(item)
			}
			return result
		case []any:
			for i, item := range v {
				v[i] = apply(item)
			}
		}
		return value
	}
	return json.Marshal(apply(tree))
}
