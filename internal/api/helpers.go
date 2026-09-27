package api

import (
	"encoding/json"
	"strconv"
)

func mapValue(value any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	if result, ok := value.(map[string]any); ok {
		return result
	}
	return map[string]any{}
}

func listValue(value any) []any {
	if result, ok := value.([]any); ok {
		return result
	}
	return []any{}
}

func intValue(value any, fallback int) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		if parsed, err := typed.Int64(); err == nil {
			return int(parsed)
		}
	case string:
		if parsed, err := strconv.Atoi(typed); err == nil {
			return parsed
		}
	}
	return fallback
}

func int64Value(value any, fallback int64) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	case json.Number:
		if parsed, err := typed.Int64(); err == nil {
			return parsed
		}
	case string:
		if parsed, err := strconv.ParseInt(typed, 10, 64); err == nil {
			return parsed
		}
	}
	return fallback
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	default:
		return ""
	}
}

func boolValue(value any) bool {
	if result, ok := value.(bool); ok {
		return result
	}
	if stringValue(value) == "1" || stringValue(value) == "true" {
		return true
	}
	return false
}

func mapList(value any) []map[string]any {
	if direct, ok := value.([]map[string]any); ok {
		return direct
	}
	items := listValue(value)
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if mapped, ok := item.(map[string]any); ok {
			result = append(result, mapped)
		}
	}
	return result
}

