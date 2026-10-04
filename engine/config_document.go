package engine

import (
	"encoding/json"
	"fmt"
)

func parseJSONObject(data []byte, source string) (map[string]any, error) {
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("parse %s: %w", source, err)
	}
	if document == nil {
		return nil, fmt.Errorf("parse %s: top-level value must be an object", source)
	}
	return document, nil
}

func marshalJSONObject(document map[string]any) ([]byte, error) {
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func findTaggedObject(document map[string]any, field, tag string) (map[string]any, error) {
	items, err := requiredArray(document, field, "client template")
	if err != nil {
		return nil, err
	}
	for _, value := range items {
		item, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("client template %s entries must be objects", field)
		}
		if itemTag, _ := item["tag"].(string); itemTag == tag {
			return item, nil
		}
	}
	return nil, fmt.Errorf("client template has no %s entry tagged %q", field, tag)
}

func requiredObject(parent map[string]any, field, context string) (map[string]any, error) {
	object, ok := parent[field].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s.%s must be an object", context, field)
	}
	return object, nil
}

func requiredArray(parent map[string]any, field, context string) ([]any, error) {
	array, ok := parent[field].([]any)
	if !ok {
		return nil, fmt.Errorf("%s.%s must be an array", context, field)
	}
	return array, nil
}

func requiredFirstObject(parent map[string]any, field, context string) (map[string]any, error) {
	array, err := requiredArray(parent, field, context)
	if err != nil {
		return nil, err
	}
	if len(array) != 1 {
		return nil, fmt.Errorf("%s.%s must contain exactly one entry", context, field)
	}
	object, ok := array[0].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s.%s entry must be an object", context, field)
	}
	return object, nil
}

func removeTaggedObjects(items []any, tag string) ([]any, bool) {
	filtered := make([]any, 0, len(items))
	removed := false
	for _, value := range items {
		item, ok := value.(map[string]any)
		if ok {
			if itemTag, _ := item["tag"].(string); itemTag == tag {
				removed = true
				continue
			}
		}
		filtered = append(filtered, value)
	}
	return filtered, removed
}

func containsString(value any, target string) bool {
	items, ok := value.([]any)
	if !ok {
		return false
	}
	for _, item := range items {
		if text, ok := item.(string); ok && text == target {
			return true
		}
	}
	return false
}
