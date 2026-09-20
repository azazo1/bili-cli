package watch

import "strings"

func mergeSeen(previous, current []string) []string {
	seen := make(map[string]struct{}, len(previous)+len(current))
	result := make([]string, 0, maxSeenIDs)
	for _, group := range [][]string{current, previous} {
		for _, id := range group {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			if _, exists := seen[id]; exists {
				continue
			}
			seen[id] = struct{}{}
			result = append(result, id)
			if len(result) >= maxSeenIDs {
				return result
			}
		}
	}
	return result
}

func newIDs(previous, current []string) []string {
	have := make(map[string]struct{}, len(previous))
	for _, id := range previous {
		id = strings.TrimSpace(id)
		if id != "" {
			have[id] = struct{}{}
		}
	}
	result := make([]string, 0)
	for i := len(current) - 1; i >= 0; i-- {
		id := strings.TrimSpace(current[i])
		if id == "" {
			continue
		}
		if _, exists := have[id]; exists {
			continue
		}
		have[id] = struct{}{}
		result = append(result, id)
	}
	return result
}

func hasFired(fired []string, key string) bool {
	for _, item := range fired {
		if item == key {
			return true
		}
	}
	return false
}

func titleMatches(title, contains string) bool {
	contains = strings.TrimSpace(contains)
	if contains == "" {
		return true
	}
	return strings.Contains(strings.ToLower(title), strings.ToLower(contains))
}

func videoURL(bvid string) string {
	if bvid == "" {
		return ""
	}
	return "https://www.bilibili.com/video/" + bvid
}

func liveURL(roomID string) string {
	if roomID == "" {
		return ""
	}
	return "https://live.bilibili.com/" + roomID
}

func dynamicURL(id string) string {
	if id == "" {
		return ""
	}
	return "https://t.bilibili.com/" + id
}
